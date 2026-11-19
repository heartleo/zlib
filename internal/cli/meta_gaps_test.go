package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// footer is the last non-empty line of the screen: the key hints.
func footer(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, " \n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

// Every real page has sources, so its footer is the long one. At the widths
// the other pages are tested at, the keys that matter must still be on screen.
func TestMetaPageFooterKeepsItsKeysOnNarrowTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	candidates := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	failed := candidates
	failed.stage, failed.res = metaFailed, metasource.Result{}
	for name, page := range map[string]metaPageModel{"candidates": candidates, "failed": failed} {
		for _, tc := range []struct {
			size [2]int
			want []string
		}{
			{[2]int{100, 24}, []string{"e edit search", "s sources", "esc back"}},
			{[2]int{80, 24}, []string{"e edit search", "s sources", "esc back"}},
			{[2]int{60, 24}, []string{"e ", "s sources", "esc back"}},
			{[2]int{40, 16}, []string{"e", "s", "esc"}},
		} {
			next, _ := page.Update(tea.WindowSizeMsg{Width: tc.size[0], Height: tc.size[1]})
			got := footer(ansi.Strip(next.(metaPageModel).View().Content))
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("%s %dx%d: footer %q lacks %q", name, tc.size[0], tc.size[1], got, want)
				}
			}
			if ansi.StringWidth(got) > tc.size[0] || strings.Contains(got, "…") {
				t.Errorf("%s %dx%d: footer %q is cut off", name, tc.size[0], tc.size[1], got)
			}
		}
	}
}

func emptyMetadataEPUB(t *testing.T) string {
	t.Helper()
	p := writeTestEPUB(t)
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("")}); err != nil {
		t.Fatal(err)
	}
	return p
}

// A file with no title, author or ISBN used to be refused, so it could not be
// looked up in the page at all. It opens the search editor instead.
func TestMetaPageOpensTheSearchEditorForAFileWithNoMetadata(t *testing.T) {
	var queries []metasource.Query
	m, err := newMetaPageModel(emptyMetadataEPUB(t), recordingLookup(twoCandidates(), &queries), nil)
	if err != nil {
		t.Fatalf("the file was refused: %v", err)
	}
	if m.Init() == nil {
		t.Log("no cursor blink command")
	}
	if !m.editing || m.editKind != editQuery || !strings.Contains(metaScreen(m), "Edit the search") {
		t.Fatalf("the search editor is not open:\n%s", metaScreen(m))
	}
	if len(queries) != 0 {
		t.Fatalf("an empty search was sent: %+v", queries)
	}
	// An empty submission is refused with the reason.
	m, _ = metaStep(t, m, key("enter"))
	if !m.editing || !strings.Contains(metaScreen(m), "title, an author or an ISBN") || len(queries) != 0 {
		t.Fatalf("an empty search was accepted:\n%s", metaScreen(m))
	}
	// Typing a title and accepting it returns to the prepare page, which shows
	// it as typed; Enter there searches.
	m = typeIntoEditor(t, m, "Dune")
	m, _ = metaStep(t, m, key("enter"))
	if m.editing || m.stage != metaPrepare || m.query.Title != "Dune" || len(queries) != 0 {
		t.Fatalf("editing=%v stage=%v query=%+v lookups=%d", m.editing, m.stage, m.query, len(queries))
	}
	if screen := metaScreen(m); !strings.Contains(screen, "typed by you") || !strings.Contains(screen, "Dune") {
		t.Errorf("the typed title is not shown as typed:\n%s", screen)
	}
	m = searchFromPrepare(t, m)
	if len(queries) != 1 || queries[0].Title != "Dune" || queries[0].TitleGuessed || m.stage != metaCandidates {
		t.Fatalf("queries = %+v, stage = %v", queries, m.stage)
	}
}

func TestMetaPageWithNoMetadataCanBeCancelledAndReopened(t *testing.T) {
	var queries []metasource.Query
	m, err := newMetaPageModel(emptyMetadataEPUB(t), recordingLookup(twoCandidates(), &queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.editing || m.stage != metaPrepare || m.cancelled {
		t.Fatalf("esc: editing=%v stage=%v cancelled=%v", m.editing, m.stage, m.cancelled)
	}
	if screen := metaScreen(m); !strings.Contains(screen, "Nothing to search yet") || !strings.Contains(screen, "e edit search") {
		t.Errorf("the page does not explain itself:\n%s", screen)
	}
	// Enter must not send an empty search; it asks what to search for instead.
	m, _ = metaStep(t, m, key("enter"))
	if !m.editing || len(queries) != 0 {
		t.Fatalf("enter: editing=%v lookups=%d", m.editing, len(queries))
	}
	m, _ = metaStep(t, m, key("esc"))
	m, cmd := metaStep(t, m, key("esc"))
	if !m.cancelled || cmd == nil {
		t.Errorf("a second esc did not leave the page: cancelled=%v", m.cancelled)
	}
}

func TestShellMetaOpensAFileWithNoMetadata(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake"})
	m := newShellModel()
	m.dispatch(`/meta "` + emptyMetadataEPUB(t) + `"`)
	page, ok := m.page.(metaPageModel)
	if !ok || m.kind != "meta" || !page.editing {
		t.Fatalf("kind = %q, page = %T, status = %q", m.kind, m.page, m.status)
	}
}

// Through the shell: while the editor is open, the keys the shell itself uses
// (/ opens its command line, ctrl+k its palette) and q are text, and a paste
// reaches the field.
func TestShellMetaEditorTakesSlashQCtrlKAndPaste(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}})
	m := newShellModel()
	m.dispatch(`/meta "` + writeTestEPUB(t) + `"`)
	// Search from the prepare page, then hand the page back to the shell.
	m.page = searchFromPrepareModel(t, m.page.(metaPageModel))
	if got := m.page.(metaPageModel); got.stage != metaCandidates {
		t.Fatalf("stage = %v", got.stage)
	}
	m, _ = shellStep(m, key("e"))
	if !m.page.(metaPageModel).editing {
		t.Fatal("e did not open the editor through the shell")
	}
	for _, k := range []tea.Msg{key("/"), key("q"), tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, key("j"), tea.PasteMsg{Content: " pasted"}} {
		m, _ = shellStep(m, k)
	}
	page := m.page.(metaPageModel)
	if !page.editing || m.kind != "meta" || m.typing || m.input.Value() != "" {
		t.Fatalf("the shell took a key: editing=%v kind=%q typing=%v input=%q", page.editing, m.kind, m.typing, m.input.Value())
	}
	if got := page.editor.values()[0]; !strings.Contains(got, "/qj") || !strings.HasSuffix(got, " pasted") {
		t.Errorf("the field holds %q", got)
	}
	// Esc closes only the editor: the page, and the shell, stay.
	m, _ = shellStep(m, key("esc"))
	if page = m.page.(metaPageModel); page.editing || page.stage != metaCandidates || m.kind != "meta" {
		t.Errorf("esc: editing=%v stage=%v kind=%q", page.editing, page.stage, m.kind)
	}
}

// Standalone `zlib meta` (the picker): choosing a file opens the page rather
// than ending the program, and finishing lands on a page that says what was
// written.
func TestStandaloneMetaPickerOpensThePageAndReportsTheResult(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}})
	p := writeTestEPUB(t)
	m, err := newStandaloneShellCommand("/meta")
	if err != nil {
		t.Fatal(err)
	}
	picker := m.page.(sendFileModel)
	picker.accepted, picker.path = true, p
	m.page = picker
	m, cmd := shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if _, ok := m.page.(metaPageModel); !ok || m.kind != "meta" {
		t.Fatalf("choosing a file did not open the page: kind=%q page=%T status=%q", m.kind, m.page, m.status)
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("the program quit after choosing a file")
		}
	}
	// The page writes something and ends: the program shows the outcome.
	page := m.page.(metaPageModel)
	page.applied = []string{"title"}
	m.page = page
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	screen := ansi.Strip(m.View().Content)
	if m.kind != "info" || !strings.Contains(screen, "Updated 1 field in book.epub") {
		t.Fatalf("finishing did not report the result: kind=%q\n%s", m.kind, screen)
	}
}
