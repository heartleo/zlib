package cli

import (
	"path/filepath"
	"testing"
)

// fakeHome points os.UserHomeDir at a temp directory for the duration of the
// test and returns it. USERPROFILE is set alongside HOME so the test is not
// POSIX-only.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestExpandTildeResolvesLeadingTildeSlash(t *testing.T) {
	home := fakeHome(t)

	got := expandTilde("~/Downloads")
	want := filepath.Join(home, "Downloads")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExpandTildeResolvesBareTilde(t *testing.T) {
	home := fakeHome(t)

	if got := expandTilde("~"); got != home {
		t.Fatalf("expected %q, got %q", home, got)
	}
}

func TestExpandTildeLeavesOtherPathsUntouched(t *testing.T) {
	fakeHome(t)

	for _, path := range []string{
		".",
		"",
		"/tmp/books",
		"Downloads",
		"./~",
		"books/~backup",
		"~tester/Downloads", // another user's home is not ours to resolve
	} {
		if got := expandTilde(path); got != path {
			t.Fatalf("expandTilde(%q) = %q, want it unchanged", path, got)
		}
	}
}

func TestNewDownloadModelExpandsTildeInDir(t *testing.T) {
	home := fakeHome(t)

	m := newDownloadModel("123", "~/Downloads", nil)
	want := filepath.Join(home, "Downloads")
	if m.dir != want {
		t.Fatalf("expected dir %q, got %q", want, m.dir)
	}
}
