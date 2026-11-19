package metasource

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// testKey has the shape of a real Google API key but is not one.
const testKey = "AIzaTestKeyNotReal0123456789abcdefghi"

func TestValidAPIKey(t *testing.T) {
	for in, want := range map[string]bool{
		testKey:                          true,
		strings.Repeat("a", 20):          true,
		strings.Repeat("a", 128):         true,
		strings.Repeat("a", 19):          false,
		strings.Repeat("a", 129):         false,
		"":                               false,
		"has space 0123456789012345":     false,
		testKey + "\r\nX-Evil: 1":        false,
		"dollar$sign0123456789012345":    false,
		"unicode-密钥-0123456789012345678": false,
	} {
		if got := ValidAPIKey(in); got != want {
			t.Errorf("ValidAPIKey(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGoogleKeyIsSentInAHeaderNeverInTheURL(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Goog-Api-Key"); got != testKey {
			t.Errorf("header = %q", got)
		}
		if strings.Contains(r.URL.String(), testKey) || r.URL.Query().Has("key") {
			t.Errorf("the key is in the URL: %s", r.URL)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("other credentials sent")
		}
		w.Write([]byte(`{}`))
	})
	// Surrounding whitespace, as from a hand-edited .env, is ignored.
	if _, err := newGoogleBooks(srv.URL, nil, "  "+testKey+"\n").Identify(context.Background(), Query{Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleWithoutAKeySendsNoKeyHeader(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["X-Goog-Api-Key"]; ok {
			t.Error("a key header was sent without a key")
		}
		w.Write([]byte(`{}`))
	})
	if _, err := newGoogleBooks(srv.URL, nil, "").Identify(context.Background(), Query{Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleKeyStatusesAreTold(t *testing.T) {
	invalid := `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"reason":"API_KEY_INVALID"}]}}`
	cases := []struct {
		name   string
		key    string
		status int
		body   string
		want   error
		text   string
	}{
		{"invalid key", testKey, 400, invalid, ErrKeyRejected, ""},
		{"key not allowed", testKey, 403, `{"error":{"code":403}}`, ErrKeyRejected, ""},
		{"quota with a key", testKey, 429, `{}`, ErrRateLimited, "rate limited for this API key"},
		{"shared quota", "", 429, `{}`, ErrRateLimited, "rate limited"},
		{"other bad request", testKey, 400, `{"error":{"code":400,"details":[{"reason":"OTHER"}]}}`, nil, "unexpected HTTP status 400"},
		{"forbidden without a key", "", 403, `{}`, nil, "unexpected HTTP status 403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			_, err := newGoogleBooks(srv.URL, nil, tc.key).Identify(context.Background(), Query{Title: "Dune"})
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) || tc.text != "" && !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("err = %v", err)
			}
			if tc.key != "" && strings.Contains(err.Error(), tc.key) {
				t.Errorf("the key is in the error: %v", err)
			}
		})
	}
}

func TestGoogleMalformedKeyIsNeverSent(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("a request was made with a malformed key") })
	for _, key := range []string{"short", "has space 0123456789012345", testKey + "\r\nX-Evil: 1"} {
		_, err := newGoogleBooks(srv.URL, nil, key).Identify(context.Background(), Query{Title: "Dune"})
		if !errors.Is(err, ErrKeyMalformed) {
			t.Errorf("key %q: err = %v", key, err)
		}
		if strings.Contains(err.Error(), "short") && strings.Contains(err.Error(), "space") {
			t.Errorf("the key is in the error: %v", err)
		}
	}
}

func TestGoogleKeyDoesNotLeakIntoTransportErrorsOrOtherHosts(t *testing.T) {
	other := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("the key reached another host: %v", r.Header) })
	redirect := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	if _, err := newGoogleBooks(redirect.URL, nil, testKey).Identify(context.Background(), Query{Title: "Dune"}); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
	dead := serve(t, func(http.ResponseWriter, *http.Request) {})
	url := dead.URL
	dead.Close()
	if _, err := newGoogleBooks(url, nil, testKey).Identify(context.Background(), Query{Title: "Dune"}); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
}
