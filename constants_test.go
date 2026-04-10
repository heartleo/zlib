package zlib

import (
	"net/http"
	"net/url"
	"testing"
)

func TestExtensionString(t *testing.T) {
	tests := []struct {
		ext  Extension
		want string
	}{
		{ExtPDF, "PDF"},
		{ExtEPUB, "EPUB"},
		{ExtDJVU, "DJVU"},
	}
	for _, tt := range tests {
		if got := tt.ext.String(); got != tt.want {
			t.Errorf("Extension.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestOrderOptionString(t *testing.T) {
	tests := []struct {
		opt  OrderOption
		want string
	}{
		{OrderPopular, "popular"},
		{OrderNewest, "date_created"},
		{OrderRecent, "date_updated"},
	}
	for _, tt := range tests {
		if got := tt.opt.String(); got != tt.want {
			t.Errorf("OrderOption.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestSetDefaultDomain(t *testing.T) {
	original := CurrentDefaultDomain()
	t.Cleanup(func() {
		SetDefaultDomain(original)
	})

	SetDefaultDomain("https://example.com")
	if got := CurrentDefaultDomain(); got != "https://example.com" {
		t.Fatalf("CurrentDefaultDomain() = %q, want %q", got, "https://example.com")
	}

	SetDefaultDomain("")
	if got := CurrentDefaultDomain(); got != DefaultDomain {
		t.Fatalf("CurrentDefaultDomain() = %q, want %q", got, DefaultDomain)
	}
}

func TestSetDefaultDomainTrimsTrailingSlash(t *testing.T) {
	original := CurrentDefaultDomain()
	t.Cleanup(func() {
		SetDefaultDomain(original)
	})

	SetDefaultDomain("https://z-lib.sk/")
	if got := CurrentDefaultDomain(); got != "https://z-lib.sk" {
		t.Fatalf("CurrentDefaultDomain() = %q, want %q", got, "https://z-lib.sk")
	}
}

func TestNewClientAppliesProxyFromEnv(t *testing.T) {
	t.Setenv(EnvProxy, "http://127.0.0.1:7890")

	client := NewClient()

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected http transport to be configured from proxy env")
	}

	reqURL, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("failed to parse request url: %v", err)
	}

	proxyURL, err := transport.Proxy(&http.Request{URL: reqURL})
	if err != nil {
		t.Fatalf("proxy func returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy url = %v, want %q", proxyURL, "http://127.0.0.1:7890")
	}
}
