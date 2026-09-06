package cli

import (
	"os"
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

// The commands are package-level singletons, so a flag set by one test leaks
// into the next. Restore the declared default and clear Changed after use.
func resetDirFlag(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	flag := cmd.Flags().Lookup("dir")
	if flag == nil {
		t.Fatalf("%s has no --dir flag", cmd.Name())
	}
	t.Cleanup(func() {
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("restoring --dir on %s: %v", cmd.Name(), err)
		}
		flag.Changed = false
	})
}

// Every command that owns a --dir flag must read it through destDirFlag, or the
// expansion silently stops applying to that command.
func TestDestDirFlagExpandsTilde(t *testing.T) {
	home := isolateConfigHome(t)
	unsetEnvForTest(t, downloadDirEnvVar)

	for _, cmd := range []*cobra.Command{downloadCmd, searchCmd, historyCmd} {
		t.Run(cmd.Name(), func(t *testing.T) {
			resetDirFlag(t, cmd)
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

// ZLIB_DOWNLOAD_DIR spares users from repeating --dir on every download, so each
// command that saves a file must honour it — reading the flag directly would
// silently skip the fallback for that command.
func TestDestDirFlagFallsBackToEnv(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv(downloadDirEnvVar, "~/Books")

	for _, cmd := range []*cobra.Command{downloadCmd, searchCmd, historyCmd} {
		t.Run(cmd.Name(), func(t *testing.T) {
			resetDirFlag(t, cmd)
			got, err := destDirFlag(cmd)
			if err != nil {
				t.Fatalf("destDirFlag() error = %v", err)
			}
			if want := filepath.Join(home, "Books"); got != want {
				t.Errorf("destDirFlag() = %q, want env dir %q", got, want)
			}
		})
	}
}

// An explicit --dir is the user's intent for this one run and must outrank the
// configured default, including when it happens to name the flag's own default.
func TestDestDirFlagPrefersExplicitFlagOverEnv(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(downloadDirEnvVar, "/from/env")

	for _, dir := range []string{"/from/flag", "."} {
		t.Run(dir, func(t *testing.T) {
			resetDirFlag(t, downloadCmd)
			if err := downloadCmd.Flags().Set("dir", dir); err != nil {
				t.Fatalf("Set(dir) error = %v", err)
			}
			got, err := destDirFlag(downloadCmd)
			if err != nil {
				t.Fatalf("destDirFlag() error = %v", err)
			}
			if got != dir {
				t.Errorf("destDirFlag() = %q, want explicit flag %q", got, dir)
			}
		})
	}
}

// A blank or whitespace-only value is what an unfinished .env line leaves
// behind; treating it as configuration would silently save into the filesystem
// root's idea of "", so it must fall through to the flag default instead.
func TestDestDirFlagIgnoresBlankEnv(t *testing.T) {
	isolateConfigHome(t)

	for _, value := range []string{"", "   "} {
		t.Run(value, func(t *testing.T) {
			resetDirFlag(t, downloadCmd)
			t.Setenv(downloadDirEnvVar, value)
			got, err := destDirFlag(downloadCmd)
			if err != nil {
				t.Fatalf("destDirFlag() error = %v", err)
			}
			if got != "." {
				t.Errorf("destDirFlag() = %q, want flag default %q", got, ".")
			}
		})
	}
}

// The point of the variable is that users configure it once in a file rather
// than exporting it every session, so cover the whole chain the CLI actually
// runs: ~/.config/zlib/.env -> loadDotEnv -> destDirFlag.
func TestDestDirFlagReadsConfigDotEnv(t *testing.T) {
	writeConfigDotEnv(t, "ZLIB_DOWNLOAD_DIR=~/Library/Books\n")
	t.Chdir(t.TempDir()) // empty cwd, so only the config-dir .env applies
	unsetEnvForTest(t, downloadDirEnvVar)
	resetDirFlag(t, downloadCmd)

	if err := loadDotEnv(); err != nil {
		t.Fatalf("loadDotEnv() error = %v", err)
	}
	got, err := destDirFlag(downloadCmd)
	if err != nil {
		t.Fatalf("destDirFlag() error = %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	if want := filepath.Join(home, "Library", "Books"); got != want {
		t.Errorf("destDirFlag() = %q, want %q from config .env", got, want)
	}
}

// With nothing configured at all, the long-standing behaviour must hold: files
// land in the current directory.
func TestDestDirFlagDefaultsToCurrentDir(t *testing.T) {
	isolateConfigHome(t)
	unsetEnvForTest(t, downloadDirEnvVar)
	resetDirFlag(t, downloadCmd)

	got, err := destDirFlag(downloadCmd)
	if err != nil {
		t.Fatalf("destDirFlag() error = %v", err)
	}
	if got != "." {
		t.Errorf("destDirFlag() = %q, want %q", got, ".")
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
