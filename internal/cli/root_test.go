package cli

import (
	"errors"
	"io"
	"testing"
)

func TestFormatCLIErrorConvertsEOFToFriendlyMessage(t *testing.T) {
	got := formatCLIError(errors.New(`Get "https://z-library.sk/s/%E4%B8%89%E4%BD%93?&page=1": EOF`))
	want := "✗ Error: network request failed. Please check your connection and try again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatCLIErrorConvertsWrappedEOFToFriendlyMessage(t *testing.T) {
	got := formatCLIError(errors.Join(errors.New("failed to fetch book"), io.EOF))
	want := "✗ Error: network request failed. Please check your connection and try again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatCLIErrorLeavesNonNetworkErrorsUntouched(t *testing.T) {
	got := formatCLIError(errors.New("search query is empty"))
	want := "✗ Error: search query is empty"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestShouldUseSessionDomainSkipsLegacyDefault(t *testing.T) {
	for _, domain := range []string{"https://z-lib.id", "https://z-lib.sk"} {
		if shouldUseSessionDomain(domain) {
			t.Fatalf("expected legacy default session domain %q to be ignored", domain)
		}
	}
}

func TestShouldUseSessionDomainAllowsCustomMirror(t *testing.T) {
	if !shouldUseSessionDomain("https://example.invalid") {
		t.Fatal("expected custom session domain to be used")
	}
}

func TestShouldUseSessionDomainRespectsEnvOverride(t *testing.T) {
	t.Setenv("ZLIB_DOMAIN", "https://zlib.li")

	if shouldUseSessionDomain("https://example.invalid") {
		t.Fatal("expected ZLIB_DOMAIN to override session domain")
	}
}
