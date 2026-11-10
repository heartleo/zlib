package zlib

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type coverTransport func(*http.Request) (*http.Response, error)

func (f coverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchCoverDiskCache(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	c := NewClient(WithDomain("https://mirror.example"))
	c.SetCoverCacheDir(t.TempDir())
	calls := 0
	c.httpClient.Transport = coverTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(pngData.Bytes())), Request: r}, nil
	})
	for range 2 {
		if _, err := c.FetchCover(t.Context(), "/cover.png"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("network calls = %d, want 1 (second fetch from disk)", calls)
	}
	u := "https://mirror.example/cover.png"
	if err := os.WriteFile(c.coverCachePath(u), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchCover(t.Context(), "/cover.png"); err != nil || calls != 2 {
		t.Fatalf("corrupt entry must be refetched: err=%v calls=%d", err, calls)
	}
}

func TestPruneCoverCacheDropsOldest(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for i := range coverCacheMaxFiles + 2 {
		path := filepath.Join(dir, fmt.Sprintf("%04d.img", i))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mod := old.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	pruneCoverCache(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) != coverCacheMaxFiles {
		t.Fatalf("entries = %d, want %d", len(entries), coverCacheMaxFiles)
	}
	if _, err := os.Stat(filepath.Join(dir, "0000.img")); err == nil {
		t.Fatal("oldest entry survived pruning")
	}
}

func TestFetchCoverCredentialBoundaryAndLimits(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"valid", "oversize", "invalid", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			c := NewClient(WithDomain("https://mirror.example"))
			c.SetCookies(map[string]string{"remix_userkey": "FAKE-KEY"})
			calls := 0
			c.httpClient.Transport = coverTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if len(r.Header) != 0 {
					t.Fatalf("cover received headers: %v", r.Header)
				}
				body := pngData.String()
				status := http.StatusOK
				header := make(http.Header)
				switch kind {
				case "oversize":
					body = strings.Repeat("x", (2<<20)+1)
				case "invalid":
					body = "bad image"
				case "redirect":
					status = http.StatusFound
					header.Set("Location", "http://other.example/cover.png")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			img, err := c.FetchCover(t.Context(), "/cover.png")
			if kind == "valid" {
				if err != nil || img.Bounds().Dx() != 20 {
					t.Fatalf("cover decode: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe or invalid cover accepted")
			}
			if calls != 1 {
				t.Fatal("unsafe redirect followed")
			}
		})
	}
}
