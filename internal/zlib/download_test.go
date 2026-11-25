package zlib

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
		{"terminal escapes", "Dune\x1b]52;c;ZXZpbA==\x07\x1b[2J.epub", "Dune]52;c;ZXZpbA==[2J.epub"},
		{"control only", "\x1b\x07", "download"},
		{"colon kept off windows", "Dune: Messiah.epub", "Dune: Messiah.epub"},
		{"bidi override", "book\u202Ebupe.exe", "bookbupe.exe"},
		{"raw C1 byte", "Dune\x9b2J.epub", "Dune\uFFFD2J.epub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilenameFor(tt.in, "linux"); got != tt.want {
				t.Errorf("sanitizeFilenameFor(%q, linux) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilenameForWindows(t *testing.T) {
	for in, want := range map[string]string{
		"Dune: Messiah.epub": "Dune_ Messiah.epub",
		`a<b>c"d|e?f*g.pdf`:  "a_b_c_d_e_f_g.pdf",
		"CON.epub":           "_CON.epub",
		"lpt1":               "_lpt1",
		"Console.epub":       "Console.epub",
		"trailing dot.":      "trailing dot",
		`C:\books\Dune.epub`: "Dune.epub",
	} {
		if got := sanitizeFilenameFor(in, "windows"); got != want {
			t.Errorf("sanitizeFilenameFor(%q, windows) = %q, want %q", in, got, want)
		}
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

// The retry budget and which statuses are final are checked against a model for
// every server sequence in TestDownloadRetriesFollowTheModelForEverySequence.

// setStallTimeout overrides the stall timeout for the duration of a test.
func setStallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := downloadStallTimeout
	downloadStallTimeout = d
	t.Cleanup(func() { downloadStallTimeout = original })
}

// assertOnlyFiles fails unless dir holds exactly the named files, which is how
// the tests check that no temporary download file was left behind.
func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("files in dir = %q, want %q", got, want)
	}
}

func TestDownloadFailureKeepsExistingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise more bytes than are sent, so the client sees a truncated body.
		w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	existing := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(existing, []byte("good copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loggedInClient().Download(server.URL+"/dl", dir, nil); err == nil {
		t.Fatal("Download() error = nil, want truncated body error")
	}
	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("existing file: %v", err)
	}
	if string(body) != "good copy" {
		t.Errorf("existing file = %q, want it untouched", body)
	}
	assertOnlyFiles(t, dir, "book.epub")
}

func TestDownloadReplacesExistingFileOnSuccess(t *testing.T) {
	server, _ := downloadServer(t, 0, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "book.epub"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := loggedInClient().Download(server.URL+"/dl", dir, nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	body, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "book body" {
		t.Errorf("body = %q, want %q", body, "book body")
	}
	assertOnlyFiles(t, dir, "book.epub")
}

func TestDownloadStallTimesOut(t *testing.T) {
	tests := []struct {
		name        string
		sendHeaders bool
	}{
		{"before response headers", false},
		{"mid body", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setStallTimeout(t, 50*time.Millisecond)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.sendHeaders {
					w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
					w.Header().Set("Content-Length", "1000")
					_, _ = w.Write([]byte("partial"))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			dir := t.TempDir()

			start := time.Now()
			_, err := loggedInClient().Download(server.URL+"/dl", dir, nil)
			if !errors.Is(err, ErrDownloadFailed) {
				t.Fatalf("Download() error = %v, want ErrDownloadFailed", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("Download() took %s, want it to stop near the stall timeout", elapsed)
			}
			assertOnlyFiles(t, dir)
		})
	}
}

func TestDownloadSlowButSteadyIsNotStalled(t *testing.T) {
	setStallTimeout(t, 200*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
		// Five chunks 60ms apart: the whole transfer outlasts the stall
		// timeout, but no single gap does.
		for range 5 {
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	t.Cleanup(server.Close)

	result, err := loggedInClient().Download(server.URL+"/dl", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if result.Size != 25 {
		t.Errorf("Size = %d, want 25", result.Size)
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
