package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fileModelStep feeds msg to m and runs any directory read it starts.
func fileModelStep(t *testing.T, m sendFileModel, msg tea.Msg) sendFileModel {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(sendFileModel)
	if cmd != nil {
		if read, ok := cmd().(bookFileListMsg); ok {
			next, _ = m.Update(read)
			m = next.(sendFileModel)
		}
	}
	return m
}

func bookDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWeReadFilePickerListsOnlySupportedBooks(t *testing.T) {
	dir := bookDir(t, "Dune.epub", "NOTES.TXT", "paper.pdf", "cover.jpg", "draft.docx", "readme", "books/")
	m := newWeReadFileModel(dir)
	m = fileModelStep(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = fileModelStep(t, m, m.Init()())

	var names []string
	for _, e := range m.files.entries {
		names = append(names, e.name)
	}
	if got, want := strings.Join(names, ","), "books,Dune.epub,NOTES.TXT,paper.pdf"; got != want {
		t.Fatalf("entries = %s, want %s", got, want)
	}
	screen := ansi.Strip(m.View().Content)
	for _, hidden := range []string{"cover.jpg", "draft.docx", "readme"} {
		if strings.Contains(screen, hidden) {
			t.Fatalf("picker shows unsupported %s:\n%s", hidden, screen)
		}
	}
	if !strings.Contains(screen, "books"+string(filepath.Separator)) || !strings.Contains(screen, "Showing folders and EPUB, PDF, MOBI, AZW3, TXT files") {
		t.Fatalf("picker lacks the folder or the format line:\n%s", screen)
	}

	// Every file row is selectable: Enter on the first file confirms it.
	m = fileModelStep(t, m, key("down"))
	m = fileModelStep(t, m, key("enter"))
	if !m.confirm || m.path != filepath.Join(dir, "Dune.epub") {
		t.Fatalf("confirm = %v, path = %q", m.confirm, m.path)
	}
}

func TestWeReadFilePickerNavigatesFolders(t *testing.T) {
	dir := bookDir(t, "a/", "b/", "b/inner.mobi", "b/skip.zip")
	m := newWeReadFileModel(dir)
	m = fileModelStep(t, m, m.Init()())
	m = fileModelStep(t, m, key("down"))
	m = fileModelStep(t, m, key("enter"))
	if m.files.dir != filepath.Join(dir, "b") || len(m.files.entries) != 1 || m.files.entries[0].name != "inner.mobi" {
		t.Fatalf("inside b: dir %q, entries %+v", m.files.dir, m.files.entries)
	}
	if screen := ansi.Strip(m.View().Content); strings.Contains(screen, "skip.zip") {
		t.Fatalf("unsupported file shown:\n%s", screen)
	}

	m = fileModelStep(t, m, key("left"))
	if m.files.dir != dir || m.files.entries[m.files.cursor].name != "b" {
		t.Fatalf("parent: dir %q, cursor on %q, want back on b", m.files.dir, m.files.entries[m.files.cursor].name)
	}

	// A read for a folder already left must not replace the current list.
	stale := bookFileListMsg{dir: filepath.Join(dir, "a"), entries: []bookFileEntry{{name: "late.epub"}}}
	next, _ := m.Update(stale)
	if got := next.(sendFileModel); len(got.files.entries) != 2 {
		t.Fatalf("stale read replaced entries: %+v", got.files.entries)
	}
}

func TestWeReadFilePickerEmptyFolderSaysWhy(t *testing.T) {
	m := newWeReadFileModel(bookDir(t, "image.png"))
	m = fileModelStep(t, m, m.Init()())
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "No supported files here") {
		t.Fatalf("empty folder:\n%s", screen)
	}
	m = fileModelStep(t, m, key("enter"))
	if m.confirm {
		t.Fatal("Enter in an empty folder opened a confirmation")
	}
}
