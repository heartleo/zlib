package zlib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultDownloadRetries is how many times a download request is reissued
// after a transient failure — a connection error, or one of the gateway
// statuses isRetryableStatus accepts. Other failures are never retried.
// EnvRetries overrides it, and 0 disables retrying.
const DefaultDownloadRetries = 3

// downloadRetryBase is the first backoff delay; it doubles on every further
// attempt. It is a var so tests can shorten it.
var downloadRetryBase = time.Second

// downloadStallTimeout bounds how long a download may go without receiving
// data, from the first request to the last byte. A slow transfer that keeps
// making progress is never cut off; a connection that stops delivering is,
// instead of hanging an unattended command forever. It is a var so tests can
// shorten it.
var downloadStallTimeout = 2 * time.Minute

// errDownloadStalled is the cancellation cause recorded when
// downloadStallTimeout fires.
var errDownloadStalled = errors.New("download stalled")

// resolveDownloadRetries reads a retry count from raw, falling back to
// DefaultDownloadRetries when it is unset, unparseable or negative.
func resolveDownloadRetries(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return DefaultDownloadRetries
	}
	return n
}

type DownloadResult struct {
	FilePath string
	Size     int64
}

func (c *Client) Download(downloadURL, destDir string, progressFn func(written, total int64)) (DownloadResult, error) {
	return c.DownloadWithContext(context.Background(), downloadURL, destDir, progressFn)
}

// DownloadWithContext saves the file at downloadURL into destDir. The file is
// written to a temporary name and only renamed into place once complete, so a
// failed or cancelled download never truncates or removes an existing file of
// the same name.
func (c *Client) DownloadWithContext(ctx context.Context, downloadURL, destDir string, progressFn func(written, total int64)) (DownloadResult, error) {
	if !c.loggedIn {
		return DownloadResult{}, ErrNotLoggedIn
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := time.AfterFunc(downloadStallTimeout, func() { cancel(errDownloadStalled) })
	defer stall.Stop()
	progress := func(written, total int64) {
		stall.Reset(downloadStallTimeout)
		if progressFn != nil {
			progressFn(written, total)
		}
	}

	result, err := c.download(ctx, downloadURL, destDir, progress)
	if err != nil && errors.Is(context.Cause(ctx), errDownloadStalled) {
		return DownloadResult{}, fmt.Errorf("%w: no data received for %s", ErrDownloadFailed, downloadStallTimeout)
	}
	return result, err
}

func (c *Client) download(ctx context.Context, downloadURL, destDir string, progressFn func(written, total int64)) (DownloadResult, error) {
	resolvedURL, err := c.resolveEAPIFileURL(ctx, downloadURL)
	if err != nil {
		return DownloadResult{}, err
	}
	downloadURL = resolvedURL

	// Use a separate client with no timeout for downloads (CDN can be slow);
	// DownloadWithContext bounds stalls instead of total duration.
	dlClient := &http.Client{
		Jar:           c.httpClient.Jar,
		Transport:     c.httpClient.Transport,
		CheckRedirect: c.checkDownloadRedirect,
	}

	resp, err := c.openDownload(ctx, dlClient, downloadURL)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("%w: HTTP %d", ErrDownloadFailed, resp.StatusCode)
	}

	filename := sanitizeFilename(filenameFromResponse(resp, downloadURL))
	destPath := filepath.Join(destDir, filename)
	written, err := saveDownload(resp, destPath, progressFn)
	if err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{FilePath: destPath, Size: written}, nil
}

// saveDownload streams resp.Body into a temporary file beside destPath and
// renames it over destPath only after the body has been read in full. On any
// error the temporary file is removed and destPath is left untouched.
func saveDownload(resp *http.Response, destPath string, progressFn func(written, total int64)) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".zlib-*.part")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			nw, writeErr := tmp.Write(buf[:n])
			if writeErr != nil {
				return 0, writeErr
			}
			written += int64(nw)
			progressFn(written, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}

	// CreateTemp uses 0600; give the finished file ordinary permissions.
	if err := tmp.Chmod(0o644); err != nil {
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return 0, err
	}
	committed = true
	return written, nil
}

// openDownload issues the download request, retrying with exponential backoff
// the two failures that are plausibly transient: a connection error, and a
// gateway status the CDN often serves for one request and not the next. A
// response that merely reports failure — 4xx, or any 5xx outside
// isRetryableStatus — is returned as is, since retrying it would only repeat
// the same answer. Only the request is retried: once bytes are flowing the
// caller owns the stream. The returned response may still carry a non-2xx
// status the caller must handle.
func (c *Client) openDownload(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	var lastErr error
	delay := downloadRetryBase
	for attempt := 0; attempt <= c.downloadRetries; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, delay); err != nil {
				return nil, err
			}
			delay *= 2
		}

		req, err := c.newDownloadRequest(ctx, rawURL)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = fmt.Errorf("%w: %w", ErrDownloadFailed, err)
			continue
		}
		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}
		lastErr = fmt.Errorf("%w: HTTP %d", ErrDownloadFailed, resp.StatusCode)
		drainAndClose(resp.Body)
	}
	return nil, lastErr
}

func (c *Client) newDownloadRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", c.domain+"/")
	if c.sendsCredentials(req.URL) {
		for k, v := range c.cookieSnapshot() {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
	}
	return req, nil
}

// sendsCredentials reports whether a download request to u may carry the
// account cookies. Only the configured mirror's own origin qualifies: the
// file link EAPI returns usually points at a CDN host, and a signed CDN link
// needs no account credentials, so handing remix_userkey to it would only
// leak a reusable session. Comparing the scheme too keeps an HTTPS session
// off a plain-HTTP URL on the same host.
func (c *Client) sendsCredentials(u *url.URL) bool {
	if u == nil {
		return false
	}
	base, err := url.Parse(c.domain)
	if err != nil {
		return false
	}
	return u.Scheme == base.Scheme && strings.EqualFold(u.Host, base.Host)
}

// checkDownloadRedirect applies checkRedirect and additionally drops the
// manually attached Cookie header on every hop that leaves the mirror's
// origin. net/http strips Cookie only on a change of domain, so without this
// an HTTPS-to-HTTP redirect on the same host would resend the session in
// clear text. Cookies the jar adds afterwards follow its own scoping.
func (c *Client) checkDownloadRedirect(req *http.Request, via []*http.Request) error {
	if err := checkRedirect(req, via); err != nil {
		return err
	}
	if !c.sendsCredentials(req.URL) {
		req.Header.Del("Cookie")
		req.Header.Del("remix-userid")
		req.Header.Del("remix-userkey")
	}
	return nil
}

// isRetryableStatus reports whether a status code is a gateway failure the
// next request may well survive. The list is deliberately narrow: 500 and the
// other 5xx codes usually mean the origin itself rejected the request, so
// repeating it just burns attempts.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// drainAndClose consumes a bounded amount of the response body before closing
// it, so the connection can be reused by the next attempt.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64*1024))
	_ = body.Close()
}

// sleepContext waits for d, returning early if ctx is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func filenameFromResponse(resp *http.Response, fallbackURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		for _, part := range strings.Split(cd, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "filename=") {
				name := strings.TrimPrefix(part, "filename=")
				name = strings.Trim(name, `"'`)
				if name != "" {
					return cleanFilename(name)
				}
			}
		}
	}
	parts := strings.Split(fallbackURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "download"
}

// sanitizeFilename makes a download filename safe to write and to print.
// The name can originate from a server-controlled Content-Disposition header,
// so without this a malicious mirror could use ../ (or a leading slash) to
// escape destDir and overwrite arbitrary files via filepath.Join, or embed
// terminal escape sequences that reach the screen in "Saved to:".
func sanitizeFilename(name string) string {
	return sanitizeFilenameFor(name, runtime.GOOS)
}

func sanitizeFilenameFor(name, goos string) string {
	// Normalize Windows separators so path.Base splits on them too, regardless
	// of the host OS.
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.Map(func(r rune) rune {
		if isBidiControl(r) {
			return -1
		}
		return r
	}, stripControl(path.Base(name), ""))
	if goos == "windows" {
		name = windowsFilename(name)
	}
	if name == "" || name == "." || name == ".." || name == "/" {
		return "download"
	}
	return name
}

// windowsFilename replaces what Windows rejects in a file name. Titles such
// as "Dune: Messiah" are common, and the final rename failed on them after
// the whole book had been downloaded.
func windowsFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	stem, _, _ := strings.Cut(name, ".")
	switch strings.ToUpper(strings.TrimSpace(stem)) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	return name
}

func cleanFilename(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	// Match trailing " (z-library.sk, ...)" or similar z-lib domain groups
	if idx := strings.LastIndex(base, " ("); idx >= 0 {
		tail := base[idx+2:]
		if strings.HasSuffix(tail, ")") && strings.Contains(tail, "z-lib") {
			base = base[:idx]
		}
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return name
	}
	return base + ext
}
