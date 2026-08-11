package zlib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
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

func (c *Client) DownloadWithContext(ctx context.Context, downloadURL, destDir string, progressFn func(written, total int64)) (DownloadResult, error) {
	if !c.loggedIn {
		return DownloadResult{}, ErrNotLoggedIn
	}

	// Use a separate client with no timeout for downloads (CDN can be slow)
	dlClient := &http.Client{
		Jar:       c.httpClient.Jar,
		Transport: c.httpClient.Transport,
		// No Timeout — let the download run as long as needed
	}

	resp, err := c.openDownload(ctx, dlClient, downloadURL)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		if loc != "" {
			return c.DownloadWithContext(ctx, loc, destDir, progressFn)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("%w: HTTP %d", ErrDownloadFailed, resp.StatusCode)
	}

	filename := sanitizeFilename(filenameFromResponse(resp, downloadURL))
	destPath := filepath.Join(destDir, filename)

	f, err := os.Create(destPath)
	if err != nil {
		return DownloadResult{}, err
	}
	defer f.Close()

	total := resp.ContentLength
	var written int64

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			nw, writeErr := f.Write(buf[:n])
			if writeErr != nil {
				return DownloadResult{}, writeErr
			}
			written += int64(nw)
			if progressFn != nil {
				progressFn(written, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// Remove incomplete file on error (e.g. context cancellation)
			f.Close()
			os.Remove(destPath)
			return DownloadResult{}, readErr
		}
	}

	return DownloadResult{FilePath: destPath, Size: written}, nil
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
			lastErr = err
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
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	return req, nil
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

// sanitizeFilename strips any directory components from a download filename.
// The name can originate from a server-controlled Content-Disposition header,
// so without this a malicious mirror could use ../ (or a leading slash) to
// escape destDir and overwrite arbitrary files via filepath.Join.
func sanitizeFilename(name string) string {
	// Normalize Windows separators so path.Base splits on them too, regardless
	// of the host OS.
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(name)
	if name == "" || name == "." || name == ".." || name == "/" {
		return "download"
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
