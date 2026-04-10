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
