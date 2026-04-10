package zlib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return DownloadResult{}, err
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
		body, _ := io.ReadAll(resp.Body)
		_ = body
		return DownloadResult{}, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	filename := filenameFromResponse(resp, downloadURL)
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
