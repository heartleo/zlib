package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// editCandidate differs from the test file ("Before", no authors) in every
// field the editor can change, so each has a row.
func editCandidate() metasource.Candidate {
	return metasource.Candidate{
		Title: "Dune", Authors: []string{"Frank Herbert"}, Publisher: "Ace", Published: "2005-08-02",
		Tags: []string{"Fiction"}, Description: "A desert planet.\nSecond paragraph.", EditionLevel: true,
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9780441013593"}},
		Sources:     []string{"Fake"}, Match: metasource.MatchISBN, Score: 1,
	}
}

func editPage(t *testing.T, write metaWriteFunc) metaPageModel {
	t.Helper()
	m := metaPageAfterLookup(t, answerWith(metasource.Result{Candidates: []metasource.Candidate{editCandidate()}}, nil, nil), write)
	m, _ = metaStep(t, m, metaKey("enter"))
	if m.stage != metaFields {
		t.Fatalf("stage = %v", m.stage)
	}
	return m
}

func rowOf(t *testing.T, m metaPageModel, field metasource.Field) int {
	t.Helper()
	for i, ch := range m.plan {
		if ch.Field == field {
			return i
		}
	}
	t.Fatalf("no %s row in %+v", field, m.plan)
	return -1
}

func focusRow(t *testing.T, m metaPageModel, field metasource.Field) metaPageModel {
	t.Helper()
	rowOf(t, m, field) // the match must propose it
	want := -1
	for i, r := range m.benchRows() {
		if r.field == field {
			want = i
		}
	}
	for range len(m.benchRows()) + 2 {
		if m.focus == want {
			return m
		}
		m, _ = metaStep(t, m, metaKey("down"))
	}
	t.Fatalf("could not focus %s", field)
	return m
}

func typeIntoEditor(t *testing.T, m metaPageModel, s string) metaPageModel {
	t.Helper()
	for _, r := range s {
		if r == ' ' {
			m, _ = metaStep(t, m, metaKey("space"))
		} else {
			m, _ = metaStep(t, m, key(string(r)))
		}
	}
	return m
}

// applyAndWait presses Ctrl+S and runs the write command, so the injected
// write function has seen the changes when it returns.
func applyAndWait(t *testing.T, m metaPageModel) metaPageModel {
	t.Helper()
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	if m.stage != metaWriting || cmd == nil {
		t.Fatalf("stage = %v after applying:\n%s", m.stage, metaScreen(m))
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaWriteDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	return m
}

// clearInput deletes everything before the cursor, which sits at the end.
func clearInput(t *testing.T, m metaPageModel) metaPageModel {
	t.Helper()
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	return m
}

func TestMetaPageEditorOwnsTheKeyboard(t *testing.T) {
	m := editPage(t, nil)
	m = focusRow(t, m, metasource.FieldPublisher)
	focus := m.focus
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	// q, j, k, e and space are text here, not page commands.
	m = typeIntoEditor(t, m, "q j k e")
	if !m.editing || m.stage != metaFields || m.cancelled || m.focus != focus {
		t.Fatalf("a page key acted while editing: editing=%v stage=%v cancelled=%v focus=%d", m.editing, m.stage, m.cancelled, m.focus)
	}
	if got := m.editor.values()[0]; got != "q j k e" {
		t.Errorf("typed text = %q", got)
	}
	// Esc closes only the editor and keeps the proposed value.
	m, cmd := metaStep(t, m, key("esc"))
	if m.editing || m.stage != metaFields || cmd != nil {
		t.Fatalf("esc: editing=%v stage=%v cmd=%v", m.editing, m.stage, cmd != nil)
	}
	if got := m.plan[rowOf(t, m, metasource.FieldPublisher)]; got.New != "Ace" || got.Edited() {
		t.Errorf("a cancelled edit changed the row: %+v", got)
	}
	// ctrl+c cancels the editor too, instead of leaving the page.
	m, _ = metaStep(t, m, key("e"))
	m, cmd = metaStep(t, m, key("ctrl+c"))
	if m.editing || m.stage != metaFields || m.cancelled || cmd != nil {
		t.Fatalf("ctrl+c: editing=%v stage=%v cancelled=%v", m.editing, m.stage, m.cancelled)
	}
}

func TestMetaPageEditedRowDoesNotSurviveGoingBackAndReopening(t *testing.T) {
	// Going back to the candidates and reopening plans the fields again from
	// what the catalogue said, so an edit never leaks into another match.
	m := editPage(t, nil)
	m = focusRow(t, m, metasource.FieldTitle)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Changed")
	m, _ = metaStep(t, m, key("enter"))
	m, _ = metaStep(t, m, key("esc"))
	m, _ = metaStep(t, m, key("enter"))
	if got := m.plan[rowOf(t, m, metasource.FieldTitle)]; got.New != "Dune" || got.Edited() {
		t.Errorf("an old edit came back: %+v", got)
	}
}

func TestMetaPageEditorFitsCompactTerminals(t *testing.T) {
	m := editPage(t, nil)
	m = focusRow(t, m, metasource.FieldAuthors)
	m, _ = metaStep(t, m, key("e"))
	m.editor.err = "Enter at least one author, separated by &"
	for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 12}, {24, 8}, {20, 5}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(ansi.Strip(next.(metaPageModel).View().Content), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%dx%d: line wider than the terminal: %q", size[0], size[1], l)
				break
			}
		}
	}
	// At the usual size the footer names what the keys do.
	screen := ansi.Strip(m.View().Content)
	if !strings.Contains(screen, "enter") || !strings.Contains(screen, "esc") {
		t.Errorf("the footer does not say how to accept or cancel:\n%s", screen)
	}
}

func TestMetaPageEditedRowsApplyTogetherWithPlainOnes(t *testing.T) {
	var written *ebookmeta.Changes
	m := editPage(t, func(_ string, c *ebookmeta.Changes) error { written = c; return nil })
	m = focusRow(t, m, metasource.FieldAuthors)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Herbert, Frank")
	m, _ = metaStep(t, m, key("enter"))
	applyAndWait(t, m)
	if written == nil || written.Authors == nil || !reflect.DeepEqual(*written.Authors, []string{"Herbert, Frank"}) {
		t.Fatalf("authors written: %+v", written)
	}
	if written.Publisher == nil || *written.Publisher != "Ace" {
		t.Errorf("the plain rows were not written with the edited one: %+v", written)
	}
}

// recordingLookup answers every lookup with res and records the queries it got.
func recordingLookup(res metasource.Result, queries *[]metasource.Query) metaLookupFunc {
	return func(_ context.Context, q metasource.Query) (metasource.Result, error) {
		*queries = append(*queries, q)
		return res, nil
	}
}

// submitQuery presses Enter in the query editor and runs the lookup it starts,
// feeding the answer back to the page.
func submitQuery(t *testing.T, m metaPageModel) metaPageModel {
	t.Helper()
	m, cmd := metaStep(t, m, key("enter"))
	if m.stage != metaLooking || cmd == nil {
		t.Fatalf("enter did not start a search: stage=%v editing=%v\n%s", m.stage, m.editing, metaScreen(m))
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaLookupDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	return m
}

func TestMetaPageEditsTheSearchAndLooksAgain(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	if len(queries) != 1 || queries[0].Title != "Before" {
		t.Fatalf("first query = %+v", queries)
	}
	m, _ = metaStep(t, m, key("e"))
	if !m.editing || !strings.Contains(metaScreen(m), "Title") || !strings.Contains(metaScreen(m), "Authors") || !strings.Contains(metaScreen(m), "ISBN") {
		t.Fatalf("the search editor is not open with its three fields:\n%s", metaScreen(m))
	}
	if got := m.editor.values(); got[0] != "Before" {
		t.Errorf("title field = %q, want the current search", got[0])
	}
	// The editor owns the keyboard: q and j are text, Tab moves on.
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Dune q j")
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeIntoEditor(t, m, "Frank Herbert")
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeIntoEditor(t, m, "0-441-01359-7")
	if m.stage != metaCandidates || m.cancelled {
		t.Fatalf("a page key acted while editing: stage=%v cancelled=%v", m.stage, m.cancelled)
	}
	m = submitQuery(t, m)
	if len(queries) != 2 {
		t.Fatalf("%d lookups, want 2", len(queries))
	}
	q := queries[1]
	if q.Title != "Dune q j" || !reflect.DeepEqual(q.Authors, []string{"Frank Herbert"}) || !reflect.DeepEqual(q.ISBNs, []string{"9780441013593"}) || len(q.Unconfirmed) != 0 {
		t.Fatalf("second query = %+v", q)
	}
	if m.stage != metaCandidates || m.editing || m.query.Title != "Dune q j" {
		t.Errorf("stage=%v editing=%v query=%+v", m.stage, m.editing, m.query)
	}
}

func TestMetaPageSearchEditWorksAfterNothingMatched(t *testing.T) {
	var queries []metasource.Query
	answers := []metasource.Result{{}, twoCandidates()}
	lookup := func(_ context.Context, q metasource.Query) (metasource.Result, error) {
		queries = append(queries, q)
		return answers[len(queries)-1], nil
	}
	m := metaPageAfterLookup(t, lookup, nil)
	if m.stage != metaFailed {
		t.Fatalf("stage = %v", m.stage)
	}
	if !strings.Contains(metaScreen(m), "e edit search") || !strings.Contains(metaScreen(m), "Press e to change the title") {
		t.Errorf("the failed page does not offer to edit the search:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Dune")
	m = submitQuery(t, m)
	if m.stage != metaCandidates || len(queries) != 2 || queries[1].Title != "Dune" {
		t.Fatalf("stage=%v queries=%+v", m.stage, queries)
	}
}

func TestMetaPageSearchEditCanBeCancelledAndRefusesAnEmptySearch(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m, _ = metaStep(t, m, key("enter")) // nothing left to search for
	if !m.editing || !strings.Contains(metaScreen(m), "title, an author or an ISBN") || len(queries) != 1 {
		t.Fatalf("an empty search was started: editing=%v lookups=%d\n%s", m.editing, len(queries), metaScreen(m))
	}
	m, cmd := metaStep(t, m, key("esc"))
	if m.editing || m.stage != metaCandidates || cmd != nil || m.cancelled || len(queries) != 1 {
		t.Fatalf("esc: editing=%v stage=%v cancelled=%v lookups=%d", m.editing, m.stage, m.cancelled, len(queries))
	}
	if m.query.Title != "Before" {
		t.Errorf("a cancelled edit changed the search: %+v", m.query)
	}
}

func TestMetaPageSearchEditRefusesABadISBN(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	m, _ = metaStep(t, m, key("e"))
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeIntoEditor(t, m, "9780441013594")
	m, _ = metaStep(t, m, key("enter"))
	if !m.editing || len(queries) != 1 || !strings.Contains(metaScreen(m), "9780441013594") {
		t.Fatalf("a bad ISBN started a search or went unexplained: editing=%v lookups=%d\n%s", m.editing, len(queries), metaScreen(m))
	}
}

// An ISBN the download or the book supplied is a hint. Editing only the title
// must leave it one: it is judged against the title searched, at the hint bar,
// and typing a new title does not by itself make it the book's own ISBN.
func TestMetaPageSearchEditKeepsHintISBNsUnconfirmed(t *testing.T) {
	var queries []metasource.Query
	hint := metaHint{ISBNs: []string{"9780441013593"}}
	m, err := newMetaPageModel(writeTestEPUB(t), recordingLookup(twoCandidates(), &queries), nil, withMetaHint(hint))
	if err != nil {
		t.Fatal(err)
	}
	m = searchFromPrepare(t, m)
	if !reflect.DeepEqual(queries[0].Unconfirmed, []string{"9780441013593"}) {
		t.Fatalf("the hint was not unconfirmed: %+v", queries[0])
	}
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Dune Messiah")
	m = submitQuery(t, m)
	q := queries[1]
	if q.Title != "Dune Messiah" || !reflect.DeepEqual(q.ISBNs, []string{"9780441013593"}) || !reflect.DeepEqual(q.Unconfirmed, []string{"9780441013593"}) {
		t.Fatalf("editing the title promoted the hint: %+v", q)
	}
}

func TestMetaPageSearchEditorFitsCompactTerminals(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	m, _ = metaStep(t, m, key("e"))
	m.editor.err = "Enter a title, an author or an ISBN to search for"
	for _, size := range [][2]int{{80, 24}, {60, 24}, {60, 16}, {40, 12}, {24, 8}, {20, 5}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(ansi.Strip(next.(metaPageModel).View().Content), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%dx%d: line wider than the terminal: %q", size[0], size[1], l)
				break
			}
		}
	}
}
