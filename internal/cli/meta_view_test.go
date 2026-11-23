package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// snapshotFile records the bytes and modification time of path.
func snapshotFile(t *testing.T, path string) ([]byte, time.Time) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, info.ModTime()
}

func assertUnchanged(t *testing.T, path string, data []byte, mtime time.Time) {
	t.Helper()
	got, gotTime := snapshotFile(t, path)
	if !bytes.Equal(got, data) || !gotTime.Equal(mtime) {
		t.Fatalf("%s changed: bytes equal = %v, mtime %v -> %v", path, bytes.Equal(got, data), mtime, gotTime)
	}
}

func newViewPage(t *testing.T, path string) metaPageModel {
	t.Helper()
	m, err := newMetaPageModel(path, nil, nil, withMetaViewOnly())
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(metaPageModel)
}

func TestMetaViewerOpensOnTheDetailsAndOffersNothingElse(t *testing.T) {
	m := newViewPage(t, writeTestEPUB(t))
	if m.stage != metaInfo || m.Init() != nil {
		t.Fatalf("stage = %v, want the details page with no startup command", m.stage)
	}
	screen := metaScreen(m)
	if !strings.Contains(screen, "View metadata") || strings.Contains(screen, "Fix metadata") {
		t.Fatalf("title is not the viewer's:\n%s", screen)
	}
	for _, hint := range []string{"search", "write", "rename", "edit"} {
		if strings.Contains(strings.ToLower(screen), hint) {
			t.Errorf("viewer offers %q:\n%s", hint, screen)
		}
	}
}

// Whatever is pressed, the viewer stays on the details and never touches the file.
func TestMetaViewerCannotChangeTheFile(t *testing.T) {
	path := writeTestEPUB(t)
	data, mtime := snapshotFile(t, path)
	m := newViewPage(t, path)
	for _, k := range []string{"e", "s", "n", "m", "r", "a", "t", "p", "x", "u", "f", "w", "space", "tab", "ctrl+s", "ctrl+d", "up", "down", "?", "?"} {
		var cmd tea.Cmd
		m, cmd = metaStep(t, m, metaKey(k))
		if m.stage != metaInfo && !(k == "?") {
			t.Fatalf("key %q left the details: stage = %v", k, m.stage)
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("key %q quit the viewer", k)
			}
		}
	}
	assertUnchanged(t, path, data, mtime)
}

func TestMetaViewerLeavesWithTheCloseKeys(t *testing.T) {
	for _, k := range []string{"esc", "q", "enter", "i"} {
		m := newViewPage(t, writeTestEPUB(t))
		m, cmd := metaStep(t, m, metaKey(k))
		if !m.cancelled || cmd == nil {
			t.Fatalf("%s: cancelled = %v, cmd = %v; want the viewer to quit", k, m.cancelled, cmd)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: command is not a quit", k)
		}
	}
}

func TestMetaViewerFitsASmallTerminal(t *testing.T) {
	m := newViewPage(t, writeTestEPUB(t))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if screen := metaScreen(next.(metaPageModel)); !strings.Contains(screen, "View metadata") {
		t.Fatalf("small terminal lost the title:\n%s", screen)
	}
}

func TestMetaViewPrintsAndLeavesAReadOnlyFileAlone(t *testing.T) {
	interactiveMeta(t, false)
	path := writeTestEPUB(t)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	data, mtime := snapshotFile(t, path)
	for _, args := range [][]string{{path, "--view"}, {path, "--view", "--json"}} {
		out, _, err := runMetaWithStderr(t, args...)
		if err != nil || strings.TrimSpace(out) == "" {
			t.Fatalf("%v: out = %q, err = %v", args, out, err)
		}
	}
	assertUnchanged(t, path, data, mtime)
}

func TestMetaViewRefusesEveryOptionThatWritesOrLooksUp(t *testing.T) {
	interactiveMeta(t, false)
	path := writeTestEPUB(t)
	data, mtime := snapshotFile(t, path)
	for _, extra := range [][]string{{"-t", "x"}, {"--fetch"}, {"--rename"}, {"--cover", "c.jpg"}, {"--source", "openlibrary"}, {"--isbn", "9780441013593"}, {"--from-opf", "x.opf"}, {"--to-opf", "x.opf"}, {"--get-cover", "c.jpg"}, {"--rename-to", "x"}} {
		_, _, err := runMetaWithStderr(t, append([]string{path, "--view"}, extra...)...)
		if !isUsage(err) {
			t.Errorf("--view %v: err = %v, want a usage error", extra, err)
		}
	}
	assertUnchanged(t, path, data, mtime)
}

func TestMetaViewWithoutAFile(t *testing.T) {
	interactiveMeta(t, false)
	if _, _, err := runMetaWithStderr(t, "--view"); !isUsage(err) {
		t.Fatalf("off a terminal: err = %v, want a usage error", err)
	}
	interactiveMeta(t, true)
	cmd := metaCmd
	cmd.ResetFlags()
	addMetaFlags(cmd)
	if err := cmd.Flags().Set("view", "true"); err != nil {
		t.Fatal(err)
	}
	if !metaOpensPicker(cmd, nil) {
		t.Fatal("--view alone on a terminal should open the picker")
	}
	if err := cmd.Flags().Set("fetch", "true"); err != nil {
		t.Fatal(err)
	}
	if metaOpensPicker(cmd, nil) {
		t.Fatal("--view with another option must not open the picker")
	}
}

func TestShellMetaViewCommand(t *testing.T) {
	path := writeTestEPUB(t)
	data, mtime := snapshotFile(t, path)
	m := newShellModel()
	m.dispatch(`/meta --view "` + path + `"`)
	page, ok := m.page.(metaPageModel)
	if !ok || m.kind != "meta" || !page.viewOnly || page.stage != metaInfo {
		t.Fatalf("kind = %q, page = %T, status = %q", m.kind, m.page, m.status)
	}
	m = newShellModel()
	m.dispatch("/meta --view")
	if picker, ok := m.page.(sendFileModel); !ok || picker.target.title != "View metadata" || m.kind != "meta-view-file" {
		t.Fatalf("kind = %q, page = %T", m.kind, m.page)
	}
	m = newShellModel()
	m.dispatch("/meta --view " + path + ".missing")
	if m.page != nil || !strings.Contains(m.status, "File not found") {
		t.Fatalf("page = %T, status = %q", m.page, m.status)
	}
	assertUnchanged(t, path, data, mtime)
}
