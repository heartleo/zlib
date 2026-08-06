package cli

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// Quoting is what makes tilde expansion our job: a shell expands ~ only when it
// is unquoted, so `--dir "~/Downloads"` arrives verbatim and must resolve to the
// same path the shell would have produced for `--dir ~/Downloads`.
func TestExpandTilde(t *testing.T) {
	home := isolateConfigHome(t)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", home},
		{"quoted home path", "~/Downloads", filepath.Join(home, "Downloads")},
		{"nested", "~/a/b", filepath.Join(home, "a", "b")},
		{"other user is left alone", "~other/Downloads", "~other/Downloads"},
		{"tilde inside path is left alone", "books/~/x", "books/~/x"},
		{"relative", "./books", "./books"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandTilde(tt.in)
			if err != nil {
				t.Fatalf("expandTilde(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("expandTilde(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every command that owns a --dir flag must read it through destDirFlag, or the
// expansion silently stops applying to that command.
func TestDestDirFlagExpandsTilde(t *testing.T) {
	home := isolateConfigHome(t)

	for _, cmd := range []*cobra.Command{downloadCmd, searchCmd, historyCmd} {
		t.Run(cmd.Name(), func(t *testing.T) {
			if err := cmd.Flags().Set("dir", "~/Downloads"); err != nil {
				t.Fatalf("Set(dir) error = %v", err)
			}
			got, err := destDirFlag(cmd)
			if err != nil {
				t.Fatalf("destDirFlag() error = %v", err)
			}
			if want := filepath.Join(home, "Downloads"); got != want {
				t.Errorf("destDirFlag() = %q, want %q", got, want)
			}
		})
	}
}

// The saved path is printed through tildePath, so an expanded --dir must render
// back the way the user typed it.
func TestTildePathRoundTrip(t *testing.T) {
	isolateConfigHome(t)

	expanded, err := expandTilde("~/Downloads/book.epub")
	if err != nil {
		t.Fatalf("expandTilde() error = %v", err)
	}
	if got, want := tildePath(expanded), filepath.Join("~", "Downloads", "book.epub"); got != want {
		t.Errorf("tildePath(%q) = %q, want %q", expanded, got, want)
	}
}
