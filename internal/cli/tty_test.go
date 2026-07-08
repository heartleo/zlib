package cli

import (
	"os"
	"testing"
)

// A regular file (like a redirect or pipe target) is never a terminal, so the
// CLI must fall back to its plain, non-interactive download/send path.
func TestIsTerminalFalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer f.Close()

	if isTerminal(f) {
		t.Errorf("isTerminal(regular file) = true, want false")
	}
}

// Under `go test` stdin/stdout are not terminals, so isInteractive must report
// false — the guard that routes callers to the plain path.
func TestIsInteractiveFalseUnderTest(t *testing.T) {
	if isInteractive() {
		t.Errorf("isInteractive() = true under test, want false")
	}
}
