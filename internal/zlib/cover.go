package zlib

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FetchCover fetches an untrusted image without account cookies or EAPI headers.
// It shares only the configured transport (including the user's proxy).
func (c *Client) FetchCover(ctx context.Context, raw string) (image.Image, error) {
	base, err := url.Parse(c.domain)
	if err != nil {
		return nil, fmt.Errorf("invalid cover origin")
	}
	u, err := base.Parse(raw)
	if err != nil || raw == "" {
		return nil, fmt.Errorf("invalid cover URL")
	}
	validate := func(u *url.URL) error {
		if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" {
			return fmt.Errorf("invalid cover URL")
		}
		return nil
	}
	if err := validate(u); err != nil {
		return nil, err
	}
	if img := c.readCoverCache(u.String()); img != nil {
		return img, nil
	}
	client := &http.Client{Transport: c.httpClient.Transport, Timeout: 6 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many cover redirects")
			}
			return validate(req.URL)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid cover request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cover unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover unavailable")
	}
	const maxBytes = 2 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, fmt.Errorf("cover exceeds size limit")
	}
	img, err := decodeCover(data)
	if err != nil {
		return nil, err
	}
	c.writeCoverCache(u.String(), data)
	return img, nil
}

// decodeCover applies the dimension limits before decoding untrusted bytes.
func decodeCover(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4_000_000 {
		return nil, fmt.Errorf("unsupported cover dimensions")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

const (
	coverCacheMaxFiles = 300
	coverCacheMaxBytes = 96 << 20
)

// SetCoverCacheDir enables the on-disk cover cache. Entries are keyed by the
// SHA-256 of the cover URL and re-validated on every read.
func (c *Client) SetCoverCacheDir(dir string) { c.coverCacheDir = dir }

func (c *Client) coverCachePath(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return filepath.Join(c.coverCacheDir, hex.EncodeToString(sum[:])+".img")
}

func (c *Client) readCoverCache(raw string) image.Image {
	if c.coverCacheDir == "" {
		return nil
	}
	path := c.coverCachePath(raw)
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 2<<20 {
		return nil
	}
	img, err := decodeCover(data)
	if err != nil {
		_ = os.Remove(path) // corrupt entry
		return nil
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now) // keep recently used entries
	return img
}

func (c *Client) writeCoverCache(raw string, data []byte) {
	if c.coverCacheDir == "" || os.MkdirAll(c.coverCacheDir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.coverCacheDir, ".cover-*.part")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), c.coverCachePath(raw)) != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	pruneCoverCache(c.coverCacheDir)
}

// pruneCoverCache removes the least recently used entries past the caps.
func pruneCoverCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(e.Name(), ".img") {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if len(files) <= coverCacheMaxFiles && total <= coverCacheMaxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	remaining := len(files)
	for _, f := range files {
		if remaining <= coverCacheMaxFiles && total <= coverCacheMaxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			remaining--
		}
	}
}
