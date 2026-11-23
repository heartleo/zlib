package aimatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Built at run time and low entropy, so no key-shaped literal sits in source.
var (
	testKey  = strings.Repeat("k", 24)
	otherKey = strings.Repeat("o", 24)
)

func settingsFor(t *testing.T, p Protocol, url string) Settings {
	t.Helper()
	return Settings{Protocol: p, BaseURL: url, APIKey: testKey, Model: "m1"}
}

func newTestClient(t *testing.T, s Settings) *Client {
	t.Helper()
	c, err := NewClient(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOpenAIChatSendsKeyAndReadsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body struct {
			Model    string              `json:"model"`
			Messages []map[string]string `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "m1" || len(body.Messages) != 2 || body.Messages[0]["role"] != "system" {
			t.Errorf("unexpected body %+v", body)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hello"}}]}`)
	}))
	defer srv.Close()
	got, err := newTestClient(t, settingsFor(t, ProtocolOpenAI, srv.URL+"/v1/")).Generate(context.Background(), Request{System: "s", User: "u"})
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestOpenAIWebSearchUsesResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/responses" || !strings.Contains(string(b), `"web_search"`) {
			t.Errorf("unexpected request %s %s", r.URL.Path, b)
		}
		fmt.Fprint(w, `{"output":[{"type":"web_search_call"},{"type":"message","content":[{"type":"output_text","text":"found"}]}]}`)
	}))
	defer srv.Close()
	got, err := newTestClient(t, settingsFor(t, ProtocolOpenAI, srv.URL+"/v1")).Generate(context.Background(), Request{User: "u", WebSearch: true})
	if err != nil || got != "found" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAnthropicSendsHeadersToolAndResumesPausedTurn(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") == "" || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"web_search"`) {
			t.Errorf("web search tool missing: %s", b)
		}
		if calls == 1 {
			fmt.Fprint(w, `{"stop_reason":"pause_turn","content":[{"type":"server_tool_use","id":"x","name":"web_search","input":{}}]}`)
			return
		}
		if !strings.Contains(string(b), `"server_tool_use"`) {
			t.Errorf("paused content was not sent back: %s", b)
		}
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
	}))
	defer srv.Close()
	got, err := newTestClient(t, settingsFor(t, ProtocolAnthropic, srv.URL)).Generate(context.Background(), Request{User: "u", WebSearch: true})
	if err != nil || got != "done" || calls != 2 {
		t.Fatalf("got %q, %v after %d calls", got, err, calls)
	}
}

func TestAnthropicBaseURLPastedWithV1IsNotDoubled(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
			return
		}
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"OK"}]}`)
	}))
	defer srv.Close()
	for _, base := range []string{srv.URL + "/gateway/v1", srv.URL + "/gateway/v1/", srv.URL + "/gateway"} {
		paths = nil
		c := newTestClient(t, settingsFor(t, ProtocolAnthropic, base))
		if _, err := c.Generate(context.Background(), Request{User: "u"}); err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		if len(paths) != 1 || paths[0] != "/gateway/v1/messages" {
			t.Errorf("%s: requested %v, want /gateway/v1/messages", base, paths)
		}
	}
}

func TestRedirectIsRefusedAndKeyNeverReachesTheTarget(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			leaked = true
		}
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := newTestClient(t, settingsFor(t, ProtocolAnthropic, srv.URL)).Generate(context.Background(), Request{User: "u"})
	if !errors.Is(err, ErrRedirected) || leaked {
		t.Fatalf("err=%v leaked=%v", err, leaked)
	}
}

func TestErrorTextHasNoKeyAndNoControlCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		msg := "bad key " + testKey + " and prefix " + testKey[:8] + "\x1b[31mred"
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg}})
	}))
	defer srv.Close()
	_, err := newTestClient(t, settingsFor(t, ProtocolOpenAI, srv.URL)).Generate(context.Background(), Request{User: "u"})
	var api *APIError
	if !errors.As(err, &api) || api.Status != 401 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey[:8]) || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("error leaks key or control characters: %q", err)
	}
}

func TestTransportErrorDropsURLAndKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused
	_, err := newTestClient(t, settingsFor(t, ProtocolOpenAI, url)).Generate(context.Background(), Request{User: "u"})
	if err == nil || strings.Contains(err.Error(), url) {
		t.Fatalf("err = %v", err)
	}
}

func TestModelsDiscoveryBothProtocols(t *testing.T) {
	for _, tc := range []struct {
		p    Protocol
		path string
	}{{ProtocolOpenAI, "/v1/models"}, {ProtocolAnthropic, "/v1/models"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != tc.path {
				t.Errorf("%s: path %s", tc.p, r.URL.Path)
			}
			fmt.Fprint(w, `{"data":[{"id":"b"},{"id":"a"}]}`)
		}))
		base := srv.URL
		if tc.p == ProtocolOpenAI {
			base += "/v1"
		}
		ids, err := newTestClient(t, settingsFor(t, tc.p, base)).Models(context.Background())
		srv.Close()
		if err != nil || len(ids) != 2 || ids[0] != "a" {
			t.Fatalf("%s: %v %v", tc.p, ids, err)
		}
	}
}

func TestConnectionTestChecksWebSearchWhenOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer srv.Close()
	s := settingsFor(t, ProtocolOpenAI, srv.URL+"/v1")
	if err := newTestClient(t, s).Test(context.Background()); err != nil {
		t.Fatalf("without web search: %v", err)
	}
	s.WebSearch = true
	err := newTestClient(t, s).Test(context.Background())
	if err == nil || !strings.Contains(err.Error(), "web search") {
		t.Fatalf("web search failure not reported: %v", err)
	}
}

func TestVerificationGoesStaleOnAnyEdit(t *testing.T) {
	s := Settings{Protocol: ProtocolOpenAI, BaseURL: "https://x.example/v1", APIKey: testKey, Model: "m"}
	if s.Verified() {
		t.Fatal("unverified settings report verified")
	}
	s.MarkVerified()
	if !s.Verified() {
		t.Fatal("not verified after MarkVerified")
	}
	for name, edit := range map[string]func(*Settings){
		"protocol": func(s *Settings) { s.Protocol = ProtocolAnthropic },
		"url":      func(s *Settings) { s.BaseURL = "https://y.example/v1" },
		"model":    func(s *Settings) { s.Model = "n" },
		"key":      func(s *Settings) { s.APIKey = otherKey },
		"search":   func(s *Settings) { s.WebSearch = true },
	} {
		c := s
		edit(&c)
		if c.Verified() {
			t.Errorf("editing %s kept the verification", name)
		}
	}
}

func TestValidateBaseURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://api.example.com/v1":      true,
		"http://127.0.0.1:11434/v1":       true,
		"http://localhost:1234/v1":        true,
		"http://api.example.com/v1":       false,
		"https://user:pw@api.example.com": false,
		"https://api.example.com/v1?k=1":  false,
		"ftp://api.example.com":           false,
		"not a url":                       false,
	} {
		if err := ValidateBaseURL(raw); (err == nil) != ok {
			t.Errorf("ValidateBaseURL(%q) = %v, want ok=%v", raw, err, ok)
		}
	}
	s := Settings{Protocol: ProtocolOpenAI, BaseURL: "https://api.example.com/v1", Model: "m"}
	if s.Validate() == nil {
		t.Error("a remote endpoint without a key validated")
	}
	s.BaseURL = "http://127.0.0.1:11434/v1"
	if err := s.Validate(); err != nil {
		t.Errorf("a local endpoint needs no key: %v", err)
	}
}

func TestSettingsNeverPrintTheKey(t *testing.T) {
	s := Settings{Protocol: ProtocolOpenAI, BaseURL: "https://x.example/v1", APIKey: testKey, Model: "m"}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), s.MaskedKey()} {
		if strings.Contains(out, testKey) {
			t.Errorf("key printed in %q", out)
		}
	}
}

func TestCutOffAndRefusedAnswersAreErrorsNotEmptyAnswers(t *testing.T) {
	for _, tc := range []struct {
		p    Protocol
		body string
		want error
	}{
		{ProtocolAnthropic, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"{\"ranking\":["}]}`, ErrTruncated},
		{ProtocolAnthropic, `{"stop_reason":"refusal","content":[]}`, ErrRefused},
		{ProtocolOpenAI, `{"choices":[{"finish_reason":"length","message":{"content":"{"}}]}`, ErrTruncated},
		{ProtocolOpenAI, `{"choices":[{"finish_reason":"content_filter","message":{"content":""}}]}`, ErrRefused},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
		base := srv.URL
		if tc.p == ProtocolOpenAI {
			base += "/v1"
		}
		_, err := newTestClient(t, settingsFor(t, tc.p, base)).Generate(context.Background(), Request{User: "u"})
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.p, err, tc.want)
		}
	}
}
