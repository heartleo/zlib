package zlib

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// setRetryBackoff overrides the retry backoff for the duration of a test.
func setRetryBackoff(t *testing.T, d time.Duration) {
	t.Helper()
	original := downloadRetryBase
	downloadRetryBase = d
	t.Cleanup(func() { downloadRetryBase = original })
}

// downloadServer serves "book body" on /dl, but answers the first failures
// requests with failStatus. It reports how many requests it received.
func downloadServer(t *testing.T, failures int, failStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(requests.Add(1)) <= failures {
			w.WriteHeader(failStatus)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
		_, _ = w.Write([]byte("book body"))
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func loggedInClient() *Client {
	c := NewClient()
	c.loggedIn = true
	return c
}

func TestDownloadRetriesTransientGatewayError(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	server, requests := downloadServer(t, 2, http.StatusBadGateway)

	result, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
	if got := filepath.Base(result.FilePath); got != "book.epub" {
		t.Errorf("FilePath base = %q, want book.epub", got)
	}
	body, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "book body" {
		t.Errorf("body = %q, want %q", body, "book body")
	}
}

func TestDownloadGivesUpAfterMaxAttempts(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	wantRequests := DefaultDownloadRetries + 1
	server, requests := downloadServer(t, wantRequests, http.StatusServiceUnavailable)

	_, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil)
	if !errors.Is(err, ErrDownloadFailed) {
		t.Fatalf("Download() error = %v, want ErrDownloadFailed", err)
	}
	if got := int(requests.Load()); got != wantRequests {
		t.Errorf("requests = %d, want %d", got, wantRequests)
	}
}

func TestDownloadDoesNotRetryPermanentStatus(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	server, requests := downloadServer(t, 1, http.StatusNotFound)

	_, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil)
	if !errors.Is(err, ErrDownloadFailed) {
		t.Fatalf("Download() error = %v, want ErrDownloadFailed", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestResolveDownloadRetries(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"unset", "", DefaultDownloadRetries},
		{"custom", "5", 5},
		{"padded", "  4  ", 4},
		{"no retry", "0", 0},
		{"negative", "-2", DefaultDownloadRetries},
		{"garbage", "many", DefaultDownloadRetries},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveDownloadRetries(tt.in); got != tt.want {
				t.Errorf("resolveDownloadRetries(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestDownloadHonoursRetriesEnv(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	t.Setenv(EnvRetries, "5")
	server, requests := downloadServer(t, 5, http.StatusBadGateway)

	if _, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := requests.Load(); got != 6 {
		t.Errorf("requests = %d, want 6", got)
	}
}

func TestDownloadRetriesDisabledByEnv(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	t.Setenv(EnvRetries, "0")
	server, requests := downloadServer(t, 1, http.StatusBadGateway)

	_, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil)
	if !errors.Is(err, ErrDownloadFailed) {
		t.Fatalf("Download() error = %v, want ErrDownloadFailed", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestDownloadCancelledDuringBackoff(t *testing.T) {
	setRetryBackoff(t, time.Minute)
	server, requests := downloadServer(t, DefaultDownloadRetries+1, http.StatusGatewayTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for requests.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	_, err := loggedInClient().DownloadWithContext(ctx, server.URL+"/dl", t.TempDir(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadWithContext() error = %v, want context.Canceled", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}
