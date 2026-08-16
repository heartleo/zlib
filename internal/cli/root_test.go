package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
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

func executeRootForTest(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		rootCmd.SetArgs(nil)
	})

	_, err := rootCmd.ExecuteC()
	if errOut.Len() > 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	return out.String(), err
}

func TestHiddenCompletionCommandGeneratesZshScript(t *testing.T) {
	out, err := executeRootForTest(t, "completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh error = %v", err)
	}
	if !strings.Contains(out, "#compdef zlib") {
		t.Fatalf("completion zsh output missing #compdef zlib header")
	}
}

func TestHiddenCompletionCommandDoesNotAppearInHelp(t *testing.T) {
	out, err := executeRootForTest(t, "--help")
	if err != nil {
		t.Fatalf("--help error = %v", err)
	}
	if strings.Contains(out, "completion") {
		t.Fatalf("hidden completion command should not appear in help:\n%s", out)
	}
}

func TestResolveClientDomain(t *testing.T) {
	tests := []struct {
		name          string
		envDomain     string
		sessionDomain string
		want          string
	}{
		{
			name:          "env wins over a stale session domain",
			envDomain:     "https://mirror.invalid",
			sessionDomain: "https://z-lib.sk",
			want:          "https://mirror.invalid",
		},
		{
			name:          "session domain applies when env is unset",
			envDomain:     "",
			sessionDomain: "https://z-lib.sk",
			want:          "https://z-lib.sk",
		},
		{
			name:          "blank env is treated as unset",
			envDomain:     "   ",
			sessionDomain: "https://z-lib.sk",
			want:          "https://z-lib.sk",
		},
		{
			name:          "nothing to apply when both are empty",
			envDomain:     "",
			sessionDomain: "",
			want:          "",
		},
		{
			name:          "env applies when there is no session domain",
			envDomain:     "https://mirror.invalid",
			sessionDomain: "",
			want:          "https://mirror.invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveClientDomain(tt.envDomain, tt.sessionDomain); got != tt.want {
				t.Fatalf("resolveClientDomain(%q, %q) = %q, want %q", tt.envDomain, tt.sessionDomain, got, tt.want)
			}
		})
	}
}
