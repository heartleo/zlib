package zlib

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// FetchBookDetails fans out one goroutine per id over a shared Client, and every
// request can rewrite c_token. Before the cookie map was guarded this raced and
// aborted the process with "fatal error: concurrent map writes". Run under
// -race to catch a regression.
func TestFetchBookDetailsIsRaceFreeUnderChallenge(t *testing.T) {
	shortenChallengeBackoff(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every book page answers with a challenge once, forcing each
		// goroutine to write c_token concurrently.
		if _, err := r.Cookie("c_token"); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, challengePageHTML())
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/book/")
		fmt.Fprintf(w, `<html><body><h1 itemprop="name">Book %s</h1></body></html>`, id)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	ids := make([]string, 0, 12)
	for i := range 12 {
		ids = append(ids, fmt.Sprintf("%d", i+1))
	}

	got := c.FetchBookDetails(ids)
	if len(got) != len(ids) {
		t.Fatalf("FetchBookDetails() returned %d entries, want %d", len(got), len(ids))
	}
}

// When the warm-up fetch fails, the rest are skipped: the server is challenging
// /book/ pages and every one of them would fail the same slow way.
func TestFetchBookDetailsSkipsRestWhenWarmupFails(t *testing.T) {
	shortenChallengeBackoff(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, challengePageHTML())
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	ids := []string{"1", "2", "3", "4", "5"}
	got := c.FetchBookDetails(ids)
	if len(got) != len(ids) {
		t.Fatalf("FetchBookDetails() returned %d entries, want %d", len(got), len(ids))
	}
	// Only the warm-up id may be attempted, and it burns the challenge budget.
	if n := int(requests.Load()); n != maxChallengeAttempts {
		t.Errorf("FetchBookDetails() made %d requests, want %d (warm-up only)", n, maxChallengeAttempts)
	}
}

func TestCookieAccessorsAreConcurrentSafe(t *testing.T) {
	c := NewClient(WithDomain("https://example.com"))
	c.SetCookies(map[string]string{"remix_userid": "1"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 500 {
			c.setCookie("c_token", fmt.Sprintf("token-%d", i))
		}
	}()
	for range 500 {
		_ = c.cookieSnapshot()
	}
	<-done

	if c.Cookies()["remix_userid"] != "1" {
		t.Error("setCookie clobbered an unrelated cookie")
	}
}

// Cookies must hand back a copy: a caller mutating the result used to corrupt
// the client's own cookie map.
func TestCookiesReturnsCopy(t *testing.T) {
	c := NewClient(WithDomain("https://example.com"))
	c.SetCookies(map[string]string{"remix_userid": "1"})

	c.Cookies()["remix_userid"] = "tampered"

	if got := c.Cookies()["remix_userid"]; got != "1" {
		t.Errorf("Cookies() leaked the internal map, remix_userid = %q, want %q", got, "1")
	}
}
