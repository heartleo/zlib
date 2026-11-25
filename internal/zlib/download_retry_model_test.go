package zlib

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// step is what the server does to one request.
type step int

const (
	stepOK step = iota
	stepBadGateway
	stepUnavailable
	stepGatewayTimeout
	stepServerError // 500: excluded from retries on purpose
	stepNotFound
	stepReset // the connection is cut before any response
	stepCount
)

func (s step) String() string {
	return [...]string{"200", "502", "503", "504", "500", "404", "reset"}[s]
}

// retryModel is the retry rule written as plainly as possible, independent of
// the implementation: the request is reissued after a connection error or a
// 502, 503 or 504, at most `retries` times. It returns how many requests are made
// and whether the download succeeds.
func retryModel(script []step, retries int) (requests int, ok bool) {
	for i := range script {
		requests++
		switch script[i] {
		case stepOK:
			return requests, true
		case stepBadGateway, stepUnavailable, stepGatewayTimeout, stepReset:
			if requests > retries {
				return requests, false
			}
		default:
			return requests, false
		}
	}
	return requests, false // the script ended while the model still wanted to retry
}

func scripts(length int) [][]step {
	if length == 0 {
		return [][]step{{}}
	}
	var out [][]step
	for _, rest := range scripts(length - 1) {
		for s := step(0); s < stepCount; s++ {
			out = append(out, append([]step{s}, rest...))
		}
	}
	return out
}

// Every sequence of server behaviours, up to the retry budget, makes the
// implementation issue the same requests and reach the same outcome as the
// model. It replays the decisions behind issue #12 (retry gateway errors) and
// the choice that 500 and 4xx are final.
func TestDownloadRetriesFollowTheModelForEverySequence(t *testing.T) {
	setRetryBackoff(t, time.Microsecond)
	const retries = 2
	var mu sync.Mutex
	var script []step
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		s := script[min(requests, len(script)-1)]
		requests++
		mu.Unlock()
		// net/http replays a GET silently when a reused connection is reset. A
		// fresh connection per request keeps that out of the request count.
		w.Header().Set("Connection", "close")
		switch s {
		case stepOK:
			w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
			_, _ = w.Write([]byte("book body"))
		case stepBadGateway:
			w.WriteHeader(http.StatusBadGateway)
		case stepUnavailable:
			w.WriteHeader(http.StatusServiceUnavailable)
		case stepGatewayTimeout:
			w.WriteHeader(http.StatusGatewayTimeout)
		case stepServerError:
			w.WriteHeader(http.StatusInternalServerError)
		case stepNotFound:
			w.WriteHeader(http.StatusNotFound)
		case stepReset:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
	}))
	t.Cleanup(server.Close)

	client := loggedInClient()
	client.downloadRetries = retries
	for _, sc := range scripts(retries + 1) {
		mu.Lock()
		script, requests = sc, 0
		mu.Unlock()
		dir := t.TempDir()
		_, err := client.Download(server.URL+"/dl", dir, nil)

		wantRequests, wantOK := retryModel(sc, retries)
		mu.Lock()
		got := requests
		mu.Unlock()
		if got != wantRequests || (err == nil) != wantOK {
			t.Fatalf("server script %v: %d requests and err = %v; the model says %d requests and success = %v", sc, got, err, wantRequests, wantOK)
		}
		if !wantOK && !errors.Is(err, ErrDownloadFailed) {
			t.Fatalf("server script %v: err = %v, want it to wrap ErrDownloadFailed", sc, err)
		}
		if wantOK {
			if body, readErr := os.ReadFile(dir + "/book.epub"); readErr != nil || string(body) != "book body" {
				t.Fatalf("server script %v: file = %q, err = %v", sc, body, readErr)
			}
		}
	}
	t.Logf("%s", fmt.Sprint(len(scripts(retries+1)), " sequences replayed"))
}

// .env is loaded by cli.Execute after every init() has run, so the retry count
// must be read when a client is built, not when the package starts. A client
// built after the variable changes sees the change; a value read in init() would
// not.
func TestRetryCountIsReadWhenTheClientIsBuilt(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want int
	}{
		{"", DefaultDownloadRetries},
		{"0", 0},
		{"7", 7},
		{"-1", DefaultDownloadRetries},
		{"many", DefaultDownloadRetries},
	} {
		t.Setenv(EnvRetries, tc.env)
		if got := NewClient().downloadRetries; got != tc.want {
			t.Errorf("%s=%q: a new client retries %d times, want %d", EnvRetries, tc.env, got, tc.want)
		}
	}
}
