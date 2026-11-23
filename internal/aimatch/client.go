package aimatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	userAgent    = "zlib-ai-match (+https://github.com/heartleo/zlib)"
	maxBodyBytes = 4 << 20
	// maxPauseTurns bounds how often an Anthropic web search is resumed.
	maxPauseTurns = 4
)

// Request is one prompt. WebSearch asks the model to use its web search tool.
type Request struct {
	System    string
	User      string
	WebSearch bool
}

// Client calls the configured model. It carries the API key, so it refuses
// redirects (net/http would forward x-api-key to another host) and is only
// built for validated settings.
type Client struct {
	s    Settings
	http *http.Client
}

// ErrRedirected means the endpoint answered with a redirect.
var ErrRedirected = errors.New("the endpoint redirected the request; set the base URL to the final address")

// NewClient validates s and builds a client on rt (nil: the default transport).
func NewClient(s Settings, rt http.RoundTripper) (*Client, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return newClient(s, rt), nil
}

// NewLister builds a client that can only list models, so it needs no model
// name yet.
func NewLister(s Settings, rt http.RoundTripper) (*Client, error) {
	if err := s.ValidateEndpoint(); err != nil {
		return nil, err
	}
	return newClient(s, rt), nil
}

func newClient(s Settings, rt http.RoundTripper) *Client {
	s.BaseURL = strings.TrimRight(s.BaseURL, "/")
	return &Client{s: s, http: &http.Client{
		Transport:     rt,
		Timeout:       3 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// APIError is a failed model request. Message is cleaned and has the key removed.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("model endpoint answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("model endpoint answered HTTP %d: %s", e.Status, e.Message)
}

// Generate sends r and returns the model's text answer.
func (c *Client) Generate(ctx context.Context, r Request) (string, error) {
	if c.s.Protocol == ProtocolAnthropic {
		return c.anthropic(ctx, r)
	}
	if r.WebSearch {
		return c.openAIResponses(ctx, r)
	}
	return c.openAIChat(ctx, r)
}

// Models lists the model ids the endpoint offers, sorted.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	path := "/models"
	if c.s.Protocol == ProtocolAnthropic {
		path = "/v1/models?limit=1000"
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range out.Data {
		if id := ebookmeta.CleanLine(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Test checks that the model answers, and, when web search is on, that a
// search request works too. On success the caller marks the settings verified.
func (c *Client) Test(ctx context.Context) error {
	const prompt = "Reply with the single word OK."
	got, err := c.Generate(ctx, Request{User: prompt})
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) == "" {
		return errors.New("the model returned an empty answer")
	}
	if c.s.WebSearch {
		got, err := c.Generate(ctx, Request{User: prompt, WebSearch: true})
		if err != nil {
			return fmt.Errorf("web search: %w", err)
		}
		if strings.TrimSpace(got) == "" {
			return errors.New("web search: the model returned an empty answer")
		}
	}
	return nil
}

func (c *Client) openAIChat(ctx context.Context, r Request) (string, error) {
	var msgs []map[string]string
	if r.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": r.System})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": r.User})
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.do(ctx, http.MethodPost, "/chat/completions", map[string]any{"model": c.s.Model, "messages": msgs}, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("the model returned no answer")
	}
	return out.Choices[0].Message.Content, nil
}

// openAIResponses uses the Responses API, the OpenAI route that carries the
// web search tool. Endpoints without it fail here, which the connection test
// reports.
func (c *Client) openAIResponses(ctx context.Context, r Request) (string, error) {
	body := map[string]any{
		"model": c.s.Model, "input": r.User,
		"tools": []map[string]string{{"type": "web_search"}},
	}
	if r.System != "" {
		body["instructions"] = r.System
	}
	var out struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := c.do(ctx, http.MethodPost, "/responses", body, &out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, item := range out.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				sb.WriteString(part.Text)
			}
		}
	}
	return sb.String(), nil
}

func (c *Client) anthropic(ctx context.Context, r Request) (string, error) {
	msgs := []map[string]any{{"role": "user", "content": r.User}}
	for range maxPauseTurns {
		body := map[string]any{"model": c.s.Model, "max_tokens": 4096, "messages": msgs}
		if r.System != "" {
			body["system"] = r.System
		}
		if r.WebSearch {
			body["tools"] = []map[string]any{{"type": "web_search_20250305", "name": "web_search", "max_uses": 5}}
		}
		var out struct {
			StopReason string            `json:"stop_reason"`
			Content    []json.RawMessage `json:"content"`
		}
		if err := c.do(ctx, http.MethodPost, "/v1/messages", body, &out); err != nil {
			return "", err
		}
		if out.StopReason != "pause_turn" {
			var sb strings.Builder
			for _, raw := range out.Content {
				var b struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if json.Unmarshal(raw, &b) == nil && b.Type == "text" {
					sb.WriteString(b.Text)
				}
			}
			return sb.String(), nil
		}
		// A long web search pauses the turn; send it back unchanged to resume.
		msgs = append(msgs, map[string]any{"role": "assistant", "content": out.Content})
	}
	return "", errors.New("the model kept pausing; try again")
}

// do sends one request and decodes a 2xx JSON body into out.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.s.BaseURL+path, rd)
	if err != nil {
		return c.redact(err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.s.Protocol == ProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
		if c.s.APIKey != "" {
			req.Header.Set("x-api-key", c.s.APIKey)
		}
	} else if c.s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.s.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redact(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return c.redact(err)
	}
	if len(data) > maxBodyBytes {
		return errors.New("response too large")
	}
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return ErrRedirected
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return &APIError{Status: resp.StatusCode, Message: c.errorMessage(data)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("the endpoint answered with something that is not the expected JSON")
	}
	return nil
}

// errorMessage pulls the provider's message out of an error body, cleaned,
// shortened and without the API key (some providers echo a key prefix).
func (c *Client) errorMessage(data []byte) string {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	msg := ""
	if json.Unmarshal(data, &e) == nil && len(e.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		var str string
		switch {
		case json.Unmarshal(e.Error, &obj) == nil && obj.Message != "":
			msg = obj.Message
		case json.Unmarshal(e.Error, &str) == nil:
			msg = str
		}
	}
	msg = ebookmeta.CleanLine(c.redactText(msg))
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	return msg
}

func (c *Client) redactText(s string) string {
	if c.s.APIKey != "" {
		s = strings.ReplaceAll(s, c.s.APIKey, "[key]")
		if len(c.s.APIKey) > 8 {
			s = strings.ReplaceAll(s, c.s.APIKey[:8], "[key]")
		}
	}
	return s
}

// redact removes the request URL (transport errors carry it) and the key.
func (c *Client) redact(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		err = ue.Unwrap()
	}
	return errors.New(c.redactText(err.Error()))
}
