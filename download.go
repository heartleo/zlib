package zlib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxDownloadAttempts = 3

// downloadRetrySleep is the pause between retryable download failures.
// Tests replace it so they do not wait on real wall-clock backoff.
var downloadRetrySleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

	var lastStatus int
	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		result, status, err := c.downloadOnce(ctx, downloadURL, destDir, progressFn)
		if err == nil {
			return result, nil
		}
		lastStatus = status
		if !isRetryableDownloadStatus(status) || attempt == maxDownloadAttempts {
			return DownloadResult{}, err
		}
		// 200ms, 400ms — short enough to feel snappy, long enough to clear a blip.
		if sleepErr := downloadRetrySleep(ctx, time.Duration(attempt)*200*time.Millisecond); sleepErr != nil {
			return DownloadResult{}, sleepErr
		}
	}
	// Unreachable, but keeps the compiler happy if the loop shape changes.
	return DownloadResult{}, fmt.Errorf("HTTP %d", lastStatus)
}

// downloadOnce performs a single GET of downloadURL. status is the HTTP status
// when the failure came from a response; it is 0 for transport / local errors.
func (c *Client) downloadOnce(ctx context.Context, downloadURL, destDir string, progressFn func(written, total int64)) (DownloadResult, int, error) {
	// Use a separate client with no timeout for downloads (CDN can be slow)
	dlClient := &http.Client{
		Jar:       c.httpClient.Jar,
		Transport: c.httpClient.Transport,
		// No Timeout — let the download run as long as needed
	}

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return DownloadResult{}, 0, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", c.domain+"/")
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	resp, err := dlClient.Do(req)
	if err != nil {
		return DownloadResult{}, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return c.downloadOnce(ctx, loc, destDir, progressFn)
		}
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return DownloadResult{}, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	filename := sanitizeFilename(filenameFromResponse(resp, downloadURL))
	destPath := filepath.Join(destDir, filename)

	f, err := os.Create(destPath)
	if err != nil {
		return DownloadResult{}, 0, err
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
				return DownloadResult{}, 0, writeErr
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
			return DownloadResult{}, 0, readErr
		}
	}

	return DownloadResult{FilePath: destPath, Size: written}, resp.StatusCode, nil
}

func isRetryableDownloadStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
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
