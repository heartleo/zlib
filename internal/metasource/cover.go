package metasource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register the decoders DecodeConfig uses
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	maxCoverBytes = 10 << 20
	// A smaller image is a placeholder or a thumbnail of a thumbnail.
	minCoverSide = 50
	maxCoverSide = 20000
)

// coverHosts are the hosts a cover may be fetched from: Open Library's cover
// server and the Internet Archive hosts it redirects to, Google Books, and
// Douban's image servers (imgN.doubanio.com), and Goodreads' two: i.gr-assets.com
// for the suggestion list's covers and m.media-amazon.com for a book page's. Both
// answer a cover directly, without a redirect and without a Referer.
var coverHosts = hostPolicy{"covers.openlibrary.org", ".archive.org", "books.google.com", ".doubanio.com", goodreadsCoverHost, goodreadsPageCoverHost}

// doubanCoverReferer is sent to Douban's image servers, which answer HTTP 418
// to a request without a Referer on a Douban site. Other hosts get none.
const doubanCoverReferer = "https://book.douban.com/"

// coverReferer returns the Referer a cover request to u needs, or "".
func coverReferer(u *url.URL) string {
	if name := strings.ToLower(u.Hostname()); strings.HasSuffix(name, ".doubanio.com") {
		return doubanCoverReferer
	}
	return ""
}

// TrustedCoverURL reports whether FetchCover would accept rawURL: https on a
// known cover host. It downloads nothing.
func TrustedCoverURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && u.Scheme == "https" && coverHosts.allows(u)
}

// FetchCover downloads the cover image at rawURL through rt (nil for the
// default transport). The URL must be https on a known cover host, every
// redirect is held to the same rule, and the result must be a JPEG, PNG, GIF
// or WebP image of a sensible size.
func FetchCover(ctx context.Context, rt http.RoundTripper, rawURL string) (*ebookmeta.Cover, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !coverHosts.allows(u) {
		return nil, errors.New("the cover is not on a trusted host")
	}
	return fetchCover(ctx, newHTTPClient(rt, 30*time.Second, coverHosts, "https"), rawURL)
}

func fetchCover(ctx context.Context, c *http.Client, rawURL string) (*ebookmeta.Cover, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/*")
	if ref := coverReferer(req.URL); ref != "" {
		req.Header.Set("Referer", ref)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, stripURL(err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.New("the source has no cover for this book")
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	data, err := readLimited(resp.Body, maxCoverBytes)
	if err != nil {
		return nil, err
	}
	return checkCoverImage(data)
}

// checkCoverImage accepts data when it is an image of a usable size.
func checkCoverImage(data []byte) (*ebookmeta.Cover, error) {
	mt := ebookmeta.SniffImage(data)
	if mt == "" {
		return nil, errors.New("the download is not a JPEG, PNG, GIF or WebP image")
	}
	if mt != "image/webp" { // the standard library cannot read WebP headers
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("unreadable image: %w", err)
		}
		if cfg.Width < minCoverSide || cfg.Height < minCoverSide {
			return nil, fmt.Errorf("the image is too small (%dx%d)", cfg.Width, cfg.Height)
		}
		if cfg.Width > maxCoverSide || cfg.Height > maxCoverSide {
			return nil, fmt.Errorf("the image is too large (%dx%d)", cfg.Width, cfg.Height)
		}
	}
	return &ebookmeta.Cover{Data: data, MediaType: mt}, nil
}
