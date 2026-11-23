// Package aimatch is the optional AI metadata match: it talks to an
// OpenAI-compatible or Anthropic-compatible model the user configured, and
// ranks candidates that the metasource catalogues returned. It never writes a
// metadata value itself: a model can only point at catalogue rows (see Match).
package aimatch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Protocol is the wire format of a model endpoint.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

// Protocols lists the supported protocols in the order they are offered.
var Protocols = []Protocol{ProtocolOpenAI, ProtocolAnthropic}

// Label is the protocol as shown to the user.
func (p Protocol) Label() string {
	switch p {
	case ProtocolOpenAI:
		return "OpenAI-compatible"
	case ProtocolAnthropic:
		return "Anthropic-compatible"
	}
	return string(p)
}

// Settings is the saved AI model configuration. AI matching is off until a
// model is configured and Verified reports true.
type Settings struct {
	Protocol Protocol `json:"protocol"`
	// BaseURL is the API root: for OpenAI-compatible endpoints it includes the
	// version path (https://api.openai.com/v1); for Anthropic-compatible ones it
	// does not (https://api.anthropic.com).
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
	// WebSearch lets the model search the web, when the endpoint supports it.
	WebSearch bool `json:"web_search,omitempty"`
	// Headers are extra request headers sent with every request to this endpoint,
	// for a gateway that wants one (a session id, say). Their values can be
	// secrets: they are saved like the key and never shown or logged.
	Headers []Header `json:"headers,omitempty"`
	// VerifiedFingerprint is Fingerprint() as of the last successful connection
	// test. Any change to the fields it covers makes it stale.
	VerifiedFingerprint string `json:"verified,omitempty"`
}

// Fingerprint identifies every field a connection test depends on.
func (s Settings) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(s.Protocol), s.BaseURL, s.Model, s.APIKey, fmt.Sprint(s.WebSearch), headersFingerprint(s.Headers),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Configured reports whether a model is set, tested or not.
func (s Settings) Configured() bool { return s.BaseURL != "" && s.Model != "" }

// Verified reports whether the saved connection test still describes these
// settings. Editing the protocol, URL, model, key or web search makes it false.
func (s Settings) Verified() bool {
	return s.Configured() && s.VerifiedFingerprint == s.Fingerprint()
}

// MarkVerified records a successful connection test for the current settings.
func (s *Settings) MarkVerified() { s.VerifiedFingerprint = s.Fingerprint() }

// MaskedKey shows that a key is set without revealing it.
func (s Settings) MaskedKey() string {
	switch {
	case s.APIKey == "":
		return "not set"
	case len(s.APIKey) <= 8:
		return "set"
	}
	return "set (…" + s.APIKey[len(s.APIKey)-4:] + ")"
}

// String and GoString keep the key out of logs and %v dumps.
func (s Settings) String() string {
	return fmt.Sprintf("{%s %s model=%s key=%s web_search=%t headers=%d}", s.Protocol, s.BaseURL, s.Model, s.MaskedKey(), s.WebSearch, len(s.Headers))
}

func (s Settings) GoString() string { return s.String() }

// Header is one custom request header.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MaxHeaders and maxHeaderValue bound what a configuration can carry.
const (
	MaxHeaders     = 10
	maxHeaderValue = 1024
)

// reservedHeaders are set by zlib itself or by the HTTP stack; a custom header
// cannot replace them, so a typo cannot break authentication or framing.
var reservedHeaders = map[string]bool{
	"authorization": true, "x-api-key": true, "anthropic-version": true, "content-type": true,
	"content-length": true, "host": true, "transfer-encoding": true, "connection": true,
	"user-agent": true, "accept": true, "proxy-authorization": true, "cookie": true,
}

// ValidateHeader checks a header: a valid name that is not reserved, and a
// value without control characters (which would split the request).
func ValidateHeader(h Header) error {
	if h.Name == "" {
		return errors.New("a header needs a name")
	}
	for _, r := range h.Name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return fmt.Errorf("%q is not a valid header name", h.Name)
		}
	}
	if reservedHeaders[strings.ToLower(h.Name)] {
		return fmt.Errorf("%s is set by zlib and cannot be overridden", h.Name)
	}
	if h.Value == "" {
		return fmt.Errorf("%s needs a value", h.Name)
	}
	if len(h.Value) > maxHeaderValue {
		return fmt.Errorf("the value of %s is longer than %d characters", h.Name, maxHeaderValue)
	}
	for _, r := range h.Value {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return fmt.Errorf("the value of %s has a control character", h.Name)
		}
	}
	return nil
}

// ValidateHeaders checks every header and that no name repeats.
func ValidateHeaders(hs []Header) error {
	if len(hs) > MaxHeaders {
		return fmt.Errorf("at most %d custom headers", MaxHeaders)
	}
	seen := map[string]bool{}
	for _, h := range hs {
		if err := ValidateHeader(h); err != nil {
			return err
		}
		k := strings.ToLower(h.Name)
		if seen[k] {
			return fmt.Errorf("%s is listed twice", h.Name)
		}
		seen[k] = true
	}
	return nil
}

// ParseHeaderLine reads "Name: value". An empty value is allowed here (the
// editor uses it to mean "keep the saved value"); ValidateHeader rejects it.
func ParseHeaderLine(line string) (Header, error) {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return Header{}, errors.New("write the header as Name: value")
	}
	return Header{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)}, nil
}

func headersFingerprint(hs []Header) string {
	var sb strings.Builder
	for _, h := range hs {
		sb.WriteString(strings.ToLower(h.Name) + "\x01" + h.Value + "\x02")
	}
	return sb.String()
}

// DefaultModel is the model the setup suggests when none is chosen yet.
const DefaultModel = "deepseek-v4.1-flash"

// KeyOptional reports whether the endpoint needs no API key: a local server.
func (s Settings) KeyOptional() bool { return isLoopbackURL(s.BaseURL) }

// ValidateEndpoint checks the protocol, URL and key: what is needed to reach
// the endpoint, for example to list its models.
func (s Settings) ValidateEndpoint() error {
	if s.Protocol != ProtocolOpenAI && s.Protocol != ProtocolAnthropic {
		return fmt.Errorf("unknown protocol %q (use openai or anthropic)", s.Protocol)
	}
	if err := ValidateBaseURL(s.BaseURL); err != nil {
		return err
	}
	if s.APIKey == "" && !isLoopbackURL(s.BaseURL) {
		return errors.New("no API key set")
	}
	return nil
}

// Validate checks that the settings can be used to build a client.
func (s Settings) Validate() error {
	if err := s.ValidateEndpoint(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Model) == "" {
		return errors.New("no model set")
	}
	return ValidateHeaders(s.Headers)
}

// ValidateBaseURL requires an https URL without credentials; plain http is
// allowed only for a loopback host (a local Ollama or LM Studio), so a key is
// never sent in the clear over a network.
func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("base URL is not a valid URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base URL must not contain credentials, a query or a fragment")
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopbackURL(raw):
		return nil
	}
	return errors.New("base URL must use https (http is allowed only for localhost)")
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultBaseURL is the official address of a protocol, offered as the first
// value of the base URL step; the user can change it to any compatible endpoint.
func DefaultBaseURL(p Protocol) string {
	switch p {
	case ProtocolOpenAI:
		return "https://api.openai.com/v1"
	case ProtocolAnthropic:
		return "https://api.anthropic.com"
	}
	return ""
}
