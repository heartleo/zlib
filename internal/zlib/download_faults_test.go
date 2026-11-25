package zlib

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// downloadFault is one way a transfer can go wrong, as the server side of it.
type downloadFault struct {
	name   string
	handle func(w http.ResponseWriter, r *http.Request)
	// cancelAfter cancels the caller's context this long into the transfer.
	cancelAfter time.Duration
}

func downloadFaults() []downloadFault {
	head := func(w http.ResponseWriter) {
		w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
	}
	flushThen := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	return []downloadFault{
		{name: "body shorter than Content-Length", handle: func(w http.ResponseWriter, r *http.Request) {
			head(w)
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte("partial"))
		}},
		{name: "connection dropped mid-body", handle: func(w http.ResponseWriter, r *http.Request) {
			head(w)
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = bufio.NewWriter(conn).Flush()
				_ = conn.Close()
			}
		}},
		{name: "not found", handle: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }},
		{name: "gateway never recovers", handle: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{name: "stalls mid-body", handle: func(w http.ResponseWriter, r *http.Request) { head(w); flushThen(w, r) }},
		{name: "caller cancels mid-body", cancelAfter: 100 * time.Millisecond, handle: func(w http.ResponseWriter, r *http.Request) { head(w); flushThen(w, r) }},
	}
}

// Whatever goes wrong during a transfer, the destination is exactly what it was
// before (the old file, or nothing) and no temporary file stays behind. Issue #24
// was a failed re-download that destroyed a good copy; this replays every way a
// transfer can fail against both a present and an absent file.
func TestDownloadFaultsNeverTouchTheDestination(t *testing.T) {
	setRetryBackoff(t, time.Millisecond)
	setStallTimeout(t, 200*time.Millisecond)
	for _, fault := range downloadFaults() {
		for _, existing := range []bool{false, true} {
			name := fault.name + "/absent"
			if existing {
				name = fault.name + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(fault.handle))
				t.Cleanup(server.Close)
				dir := t.TempDir()
				dest := filepath.Join(dir, "book.epub")
				if existing {
					if err := os.WriteFile(dest, []byte("good copy"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if fault.cancelAfter > 0 {
					time.AfterFunc(fault.cancelAfter, cancel)
				}

				if _, err := loggedInClient().DownloadWithContext(ctx, server.URL+"/dl", dir, nil); err == nil {
					t.Fatal("the transfer failed but Download returned no error")
				}
				if existing {
					if body, err := os.ReadFile(dest); err != nil || string(body) != "good copy" {
						t.Fatalf("existing file = %q, err = %v; want it untouched", body, err)
					}
					assertOnlyFiles(t, dir, "book.epub")
				} else {
					assertOnlyFiles(t, dir)
				}
			})
		}
	}
}
