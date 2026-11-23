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
	// VerifiedFingerprint is Fingerprint() as of the last successful connection
	// test. Any change to the fields it covers makes it stale.
	VerifiedFingerprint string `json:"verified,omitempty"`
}

// Fingerprint identifies every field a connection test depends on.
func (s Settings) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(s.Protocol), s.BaseURL, s.Model, s.APIKey, fmt.Sprint(s.WebSearch),
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
	return fmt.Sprintf("{%s %s model=%s key=%s web_search=%t}", s.Protocol, s.BaseURL, s.Model, s.MaskedKey(), s.WebSearch)
}

func (s Settings) GoString() string { return s.String() }

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
	return nil
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
