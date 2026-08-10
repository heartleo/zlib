package zlib

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "book.epub", "book.epub"},
		{"unix traversal", "../../../../etc/passwd", "passwd"},
		{"windows traversal", `..\..\..\Windows\System32\evil.dll`, "evil.dll"},
		{"absolute path", "/etc/cron.d/job", "job"},
		{"nested dirs", "a/b/c/book.pdf", "book.pdf"},
		{"dotdot only", "..", "download"},
		{"empty", "", "download"},
		{"dot", ".", "download"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsRetryableDownloadStatus(t *testing.T) {
	for _, code := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if !isRetryableDownloadStatus(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}
	for _, code := range []int{
		0,
		http.StatusOK,
		http.StatusNoContent,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
	} {
		if isRetryableDownloadStatus(code) {
			t.Errorf("expected %d not to be retryable", code)
		}
	}
}

func TestDownloadRetriesTransient502ThenSucceeds(t *testing.T) {
	origSleep := downloadRetrySleep
	downloadRetrySleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { downloadRetrySleep = origSleep })

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Disposition", `filename="book.epub"`)
		_, _ = io.WriteString(w, "epub-bytes")
	}))
	t.Cleanup(server.Close)

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	dir := t.TempDir()
	result, err := c.Download(server.URL+"/dl/abc", dir, nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3 (two 502s then success)", hits.Load())
	}
	body, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "epub-bytes" {
		t.Fatalf("body = %q", body)
	}
	if filepath.Base(result.FilePath) != "book.epub" {
		t.Fatalf("FilePath base = %q", result.FilePath)
	}
}

func TestDownloadBoundsRetryableErrorBodyDrain(t *testing.T) {
	origSleep := downloadRetrySleep
	downloadRetrySleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { downloadRetrySleep = origSleep })

	var bytesRead atomic.Int64
	c := NewClient()
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       &countingEndlessBody{bytesRead: &bytesRead},
			Header:     make(http.Header),
		}, nil
	})
	c.domain = "https://example.test"
	c.loggedIn = true

	_, err := c.Download(c.domain+"/dl/streaming-error", t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP 502", err)
	}
	if got, want := bytesRead.Load(), int64(maxDownloadAttempts*maxErrorResponseBodySize); got != want {
		t.Fatalf("read %d error-body bytes, want bounded total %d", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type countingEndlessBody struct {
	bytesRead *atomic.Int64
}

func (b *countingEndlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.bytesRead.Add(int64(len(p)))
	return len(p), nil
}

func (*countingEndlessBody) Close() error { return nil }

type failingDownloadFile struct {
	file *os.File
}

func (f *failingDownloadFile) Write(p []byte) (int, error) {
	n := len(p) / 2
	if _, err := f.file.Write(p[:n]); err != nil {
		return 0, err
	}
	return n, errors.New("injected write failure")
}

func (f *failingDownloadFile) Close() error {
	return f.file.Close()
}

func TestDownloadRemovesPartialFileOnWriteFailure(t *testing.T) {
	origCreate := createDownloadFile
	createDownloadFile = func(name string) (downloadFile, error) {
		f, err := os.Create(name)
		if err != nil {
			return nil, err
		}
		return &failingDownloadFile{file: f}, nil
	}
	t.Cleanup(func() { createDownloadFile = origCreate })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `filename="partial.epub"`)
		_, _ = io.WriteString(w, "download contents")
	}))
	t.Cleanup(server.Close)

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true
	dir := t.TempDir()

	_, err := c.Download(server.URL+"/dl/partial", dir, nil)
	if err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v, want injected write failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "partial.epub")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial destination still exists; stat error = %v", statErr)
	}
}

func TestDownloadGivesUpAfterRetryableExhaustion(t *testing.T) {
	origSleep := downloadRetrySleep
	downloadRetrySleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { downloadRetrySleep = origSleep })

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	_, err := c.Download(server.URL+"/dl/abc", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP 502", err)
	}
	// Must not carry the old doubled "download failed: download failed:" prefix.
	if strings.Count(err.Error(), "download failed") != 0 {
		t.Fatalf("library error should be bare, got %q", err)
	}
	if hits.Load() != int32(maxDownloadAttempts) {
		t.Fatalf("hits = %d, want %d", hits.Load(), maxDownloadAttempts)
	}
}

func TestDownloadDoesNotRetryHTTP404(t *testing.T) {
	origSleep := downloadRetrySleep
	var slept bool
	downloadRetrySleep = func(context.Context, time.Duration) error {
		slept = true
		return nil
	}
	t.Cleanup(func() { downloadRetrySleep = origSleep })

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	_, err := c.Download(server.URL+"/dl/missing", t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("error = %v, want HTTP 404", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (no retry on 404)", hits.Load())
	}
	if slept {
		t.Fatal("should not sleep when status is not retryable")
	}
}

func TestDownloadRetryHonorsContextCancel(t *testing.T) {
	origSleep := downloadRetrySleep
	t.Cleanup(func() { downloadRetrySleep = origSleep })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	downloadRetrySleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := c.DownloadWithContext(ctx, server.URL+"/dl/abc", t.TempDir(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
