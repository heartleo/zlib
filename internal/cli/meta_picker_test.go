package cli

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// interactiveMeta makes `zlib meta` believe it runs on a terminal.
func interactiveMeta(t *testing.T, interactive bool) {
	t.Helper()
	old := metaIsInteractive
	metaIsInteractive = func() bool { return interactive }
	t.Cleanup(func() { metaIsInteractive = old })
}

func isUsage(err error) bool {
	var u *usageError
	return errors.As(err, &u)
}

// Without a file `zlib meta` is a usage error, as it always was, except on a
// terminal with no other options, where it opens the file picker.
func TestMetaCommandWithoutAFileIsAUsageErrorOffATerminal(t *testing.T) {
	interactiveMeta(t, false)
	for _, args := range [][]string{{}, {"--fetch"}, {"--json"}, {"-t", "x"}, {"a.epub", "b.epub"}} {
		_, _, err := runMetaWithStderr(t, args...)
		if !isUsage(err) {
			t.Errorf("%v: err = %v, want a usage error (exit 2)", args, err)
		}
	}
}

func TestMetaCommandWithoutAFileKeepsOtherOptionsAsUsageErrors(t *testing.T) {
	interactiveMeta(t, true)
	for _, args := range [][]string{
		{"--json"}, {"-t", "x"}, {"--isbn", "9780000000002"}, {"--cover", "c.png"}, {"--fetch", "--json"},
		{"--fetch", "-t", "x"}, {"--fetch", "--isbn", "9780000000002"}, {"a.epub", "b.epub"},
	} {
		_, _, err := runMetaWithStderr(t, args...)
		if !isUsage(err) {
			t.Errorf("%v: err = %v, want a usage error (exit 2)", args, err)
		}
	}
}

func TestMetaCommandWithoutAFileOpensThePickerOnATerminal(t *testing.T) {
	interactiveMeta(t, true)
	for _, args := range [][]string{{}, {"--fetch"}} {
		// The test process has no terminal, so the program itself cannot run:
		// what matters is that the arguments are accepted and the command
		// goes on to start the shell, which then says it needs a terminal. That
		// message names no Kindle command, which is what a `zlib meta` user
		// must not be told to use.
		_, _, err := runMetaWithStderr(t, args...)
		if err == nil || isUsage(err) || isInteractive() {
			t.Fatalf("%v: err = %v, want the command to reach the interactive program", args, err)
		}
		if !strings.Contains(err.Error(), "requires a terminal") || strings.Contains(strings.ToLower(err.Error()), "kindle") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestStandaloneMetaWithoutAFileOpensTheFilePicker(t *testing.T) {
	m, err := newStandaloneShellCommand("/meta")
	if err != nil {
		t.Fatal(err)
	}
	if m.kind != "meta-file" || m.standaloneTitle != "Metadata" {
		t.Fatalf("kind = %q, title = %q", m.kind, m.standaloneTitle)
	}
	screen := ansi.Strip(m.View().Content)
	if !strings.Contains(screen, "Fix metadata") {
		t.Errorf("not the metadata file picker:\n%s", screen)
	}
	// Backing out of the picker ends the program, like the other standalone pages.
	m, _ = shellStep(m, key("esc"))
	if _, cmd := shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}}); cmd == nil {
		t.Error("the standalone picker did not exit on cancellation")
	}
}
