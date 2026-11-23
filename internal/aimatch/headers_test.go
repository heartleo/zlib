package aimatch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// headerValue is built at run time so no secret-shaped literal sits in source.
var headerValue = strings.Repeat("h", 20)

func TestCustomHeadersAreSentOnBothProtocolsAndOnEveryRoute(t *testing.T) {
	for _, p := range Protocols {
		var seen []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, r.Header.Get("X-Session-Id"))
			switch {
			case strings.HasSuffix(r.URL.Path, "/models"):
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "m"}}})
			case p == ProtocolAnthropic:
				fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"OK"}]}`)
			default:
				fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}]}`)
			}
		}))
		base := srv.URL
		if p == ProtocolOpenAI {
			base += "/v1"
		}
		s := settingsFor(t, p, base)
		s.Headers = []Header{{Name: "x-session-id", Value: headerValue}}
		c := newTestClient(t, s)
		if _, err := c.Models(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := c.Test(context.Background()); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		if len(seen) != 2 {
			t.Fatalf("%s: %d requests", p, len(seen))
		}
		for _, v := range seen {
			if v != headerValue {
				t.Errorf("%s: header value %q", p, v)
			}
		}
	}
}

func TestCustomHeaderCannotReplaceWhatZlibSets(t *testing.T) {
	for _, name := range []string{"Authorization", "x-api-key", "X-API-KEY", "Anthropic-Version", "Content-Type", "Host", "Content-Length", "Transfer-Encoding", "Cookie", "User-Agent"} {
		if err := ValidateHeader(Header{Name: name, Value: headerValue}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestHeaderValidation(t *testing.T) {
	for _, tc := range []struct {
		h  Header
		ok bool
	}{
		{Header{"x-session-id", "abc123"}, true},
		{Header{"X-Custom_1", "a value with spaces"}, true},
		{Header{"", "v"}, false},
		{Header{"bad name", "v"}, false},
		{Header{"bad:name", "v"}, false},
		{Header{"x-a", ""}, false},
		{Header{"x-a", "line1\r\nInjected: yes"}, false},
		{Header{"x-a", "nul\x00byte"}, false},
		{Header{"x-a", strings.Repeat("v", 1025)}, false},
	} {
		if err := ValidateHeader(tc.h); (err == nil) != tc.ok {
			t.Errorf("ValidateHeader(%q) = %v, want ok=%v", tc.h, err, tc.ok)
		}
	}
	if ValidateHeaders([]Header{{"x-a", "1"}, {"X-A", "2"}}) == nil {
		t.Error("a name listed twice was accepted")
	}
	many := make([]Header, MaxHeaders+1)
	for i := range many {
		many[i] = Header{Name: fmt.Sprintf("x-h%d", i), Value: "v"}
	}
	if ValidateHeaders(many) == nil {
		t.Error("too many headers were accepted")
	}
	// A bad header keeps the settings from building a client at all.
	s := settingsFor(t, ProtocolOpenAI, "https://api.example.com/v1")
	s.Headers = []Header{{Name: "Authorization", Value: "x"}}
	if _, err := NewClient(s, nil); err == nil {
		t.Error("a client was built with a reserved header")
	}
}

func TestParseHeaderLine(t *testing.T) {
	h, err := ParseHeaderLine("  X-Session-Id :  a: b  ")
	if err != nil || h.Name != "X-Session-Id" || h.Value != "a: b" {
		t.Fatalf("%+v, %v", h, err)
	}
	if h, err := ParseHeaderLine("x-a:"); err != nil || h.Value != "" {
		t.Fatalf("%+v, %v", h, err)
	}
	if _, err := ParseHeaderLine("no colon"); err == nil {
		t.Fatal("a line without a colon was accepted")
	}
}

func TestEditingAHeaderMakesTheVerificationStale(t *testing.T) {
	s := Settings{Protocol: ProtocolOpenAI, BaseURL: "https://x.example/v1", APIKey: testKey, Model: "m", Headers: []Header{{"x-a", "1"}}}
	s.MarkVerified()
	for name, edit := range map[string]func(*Settings){
		"value":   func(s *Settings) { s.Headers = []Header{{"x-a", "2"}} },
		"name":    func(s *Settings) { s.Headers = []Header{{"x-b", "1"}} },
		"added":   func(s *Settings) { s.Headers = append(s.Headers[:1:1], Header{"x-c", "1"}) },
		"removed": func(s *Settings) { s.Headers = nil },
	} {
		c := s
		edit(&c)
		if c.Verified() {
			t.Errorf("%s: the verification survived", name)
		}
	}
}

func TestHeaderValuesNeverAppearInErrorsOrPrintedSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "bad header " + r.Header.Get("X-Session-Id")}})
	}))
	defer srv.Close()
	s := settingsFor(t, ProtocolOpenAI, srv.URL+"/v1")
	s.Headers = []Header{{Name: "x-session-id", Value: headerValue}}
	_, err := newTestClient(t, s).Generate(context.Background(), Request{User: "u"})
	if err == nil || strings.Contains(err.Error(), headerValue) {
		t.Fatalf("err = %v", err)
	}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s)} {
		if strings.Contains(out, headerValue) {
			t.Errorf("header value printed in %q", out)
		}
	}
}

func TestCustomHeadersDoNotFollowARedirect(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Session-Id") != "" {
			leaked = true
		}
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	s := settingsFor(t, ProtocolOpenAI, srv.URL+"/v1")
	s.Headers = []Header{{Name: "x-session-id", Value: headerValue}}
	if _, err := newTestClient(t, s).Generate(context.Background(), Request{User: "u"}); err == nil || leaked {
		t.Fatalf("err=%v leaked=%v", err, leaked)
	}
}
