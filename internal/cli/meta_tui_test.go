package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

func metaKey(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	}
	return key(s)
}

func metaStep(t *testing.T, m metaPageModel, msg tea.Msg) (metaPageModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(metaPageModel), cmd
}

func metaScreen(m metaPageModel) string { return ansi.Strip(m.View().Content) }

// answerWith returns a lookup that answers with res and counts its calls.
func answerWith(res metasource.Result, err error, calls *atomic.Int32) metaLookupFunc {
	return func(context.Context, metasource.Query) (metasource.Result, error) {
		if calls != nil {
			calls.Add(1)
		}
		return res, err
	}
}

// metaPageAfterLookup runs the lookup and feeds its answer to the page.
func metaPageAfterLookup(t *testing.T, lookup metaLookupFunc, write metaWriteFunc, opts ...metaOption) metaPageModel {
	t.Helper()
	m, err := newMetaPageModel(writeTestEPUB(t), lookup, write, opts...)
	if err != nil {
		t.Fatal(err)
	}
	msg := m.start()()
	m, _ = metaStep(t, m, msg)
	return m
}

func twoCandidates() metasource.Result {
	exact := dune()
	exact.Sources = []string{"Open Library", "Google Books"}
	exact.Match, exact.Score = metasource.MatchISBN, 1
	other := metasource.Candidate{Title: "Before (2nd ed.)", Sources: []string{"Google Books"}, Match: metasource.MatchTitle, Score: 0.71}
	return metasource.Result{Candidates: []metasource.Candidate{exact, other}}
}

func TestMetaPageListsCandidatesWithMatchKind(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	if m.stage != metaCandidates {
		t.Fatalf("stage = %v", m.stage)
	}
	screen := metaScreen(m)
	for _, want := range []string{"Fix metadata", "book.epub · EPUB", "ISBN  Before — Frank Herbert (2005)", " 71%  Before (2nd ed.)", "Details", "Open Library, Google Books", "esc back"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestMetaPageFlagsAnISBNWithADifferentTitle(t *testing.T) {
	res := twoCandidates()
	res.Candidates[0].Score = 0.5
	m := metaPageAfterLookup(t, answerWith(res, nil, nil), nil)
	if !strings.Contains(metaScreen(m), "ISBN? Before") {
		t.Errorf("row lacks the ISBN? marker:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, metaKey("enter"))
	if !strings.Contains(metaScreen(m), "ISBN matches but the title is different") {
		t.Errorf("fields page lacks the warning:\n%s", metaScreen(m))
	}
	// Probably another book: the rows are offered, none starts selected.
	if len(m.plan) == 0 {
		t.Fatal("no rows offered")
	}
	for i, ch := range m.plan {
		if m.selected[i] {
			t.Errorf("%s starts selected for a probable other book", ch.Field)
		}
	}
	if screen := metaScreen(m); strings.Contains(screen, "[x]") || !strings.Contains(screen, "(may be another book)") {
		t.Errorf("rows are not all unchecked and tagged:\n%s", screen)
	}
	// Ctrl+S with nothing selected writes nothing.
	if m, cmd := metaStep(t, m, metaKey("ctrl+s")); cmd != nil || m.stage != metaFields {
		t.Error("applied an empty selection")
	}
}

func TestMetaPageMarksGuessedEditionRowsForATitleOnlyMatch(t *testing.T) {
	c := dune()
	c.Languages, c.Tags = []string{"en"}, []string{"Classics"}
	c.Sources, c.Match, c.Score = []string{"Google Books"}, metasource.MatchTitle, 0.83
	m := metaPageAfterLookup(t, answerWith(metasource.Result{Candidates: []metasource.Candidate{c}}, nil, nil), nil)
	m.current.Languages = nil
	m, _ = metaStep(t, m, metaKey("enter"))
	screen := metaScreen(m)
	for _, want := range []string{
		"Found by title only: check the edition",
		"[x] Authors", "[x] Description", "[x] Tags",
		"[ ] Publisher", "[ ] Languages", "[ ] Published", "[ ] ID isbn",
		"(check edition)",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	// Selecting a guessed row is a deliberate act.
	for i, ch := range m.plan {
		if ch.Field == metasource.FieldPublisher {
			m.focus = i
		}
	}
	m, _ = metaStep(t, m, metaKey("space"))
	if !strings.Contains(metaScreen(m), "[x] Publisher") {
		t.Errorf("a guessed row could not be selected:\n%s", metaScreen(m))
	}
}

func TestMetaPagePreselectsOnlyEmptyFields(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	m, _ = metaStep(t, m, metaKey("enter"))
	if m.stage != metaFields {
		t.Fatalf("stage = %v", m.stage)
	}
	var on, off []string
	for i, ch := range m.plan {
		if m.selected[i] {
			on = append(on, string(ch.Field))
		} else {
			off = append(off, string(ch.Field))
		}
	}
	// The file has the title "Before" and an English language; the rest is empty.
	if !slices.Contains(on, "authors") || !slices.Contains(on, "identifier:isbn") || !slices.Contains(on, "publisher") {
		t.Errorf("empty fields not selected: on=%v", on)
	}
	for _, f := range off {
		if f == "authors" || f == "publisher" {
			t.Errorf("%s should be selected", f)
		}
	}
	screen := metaScreen(m)
	for _, want := range []string{"[x] Authors", "(empty) → Frank Herbert", "[x] ID isbn", "Apply", "Back", "ctrl+s apply"} {
		if !strings.Contains(screen, want) {
			t.Errorf("fields screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestMetaPageAppliesOnlyTheSelectedFields(t *testing.T) {
	var wrote *ebookmeta.Changes
	var wrotePath string
	write := func(path string, c *ebookmeta.Changes) error { wrotePath, wrote = path, c; return nil }
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), write)
	m, _ = metaStep(t, m, metaKey("enter"))
	for i, ch := range m.plan { // deselect the publisher
		if ch.Field == metasource.FieldPublisher {
			m.focus = i
		}
	}
	m, _ = metaStep(t, m, metaKey("space"))
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	if m.stage != metaWriting || cmd == nil {
		t.Fatalf("stage = %v", m.stage)
	}
	if !strings.Contains(metaScreen(m), "Writing the file") {
		t.Errorf("no writing state:\n%s", metaScreen(m))
	}
	// Run the write command from the batch and feed its result back.
	var done tea.Msg
	for _, c := range batchCmds(cmd) {
		if msg := c(); msg != nil {
			if _, ok := msg.(metaWriteDone); ok {
				done = msg
			}
		}
	}
	if done == nil {
		t.Fatal("the write command never ran")
	}
	if wrotePath != m.path || wrote == nil || wrote.Authors == nil || wrote.Publisher != nil || wrote.Title != nil || wrote.Identifiers["isbn"] != "9780441013593" {
		t.Fatalf("wrote %+v to %s", wrote, wrotePath)
	}
	m, cmd = metaStep(t, m, done)
	if cmd == nil || m.cancelled || !slices.Contains(m.applied, "authors") || slices.Contains(m.applied, "publisher") {
		t.Fatalf("applied = %v, cancelled = %v", m.applied, m.cancelled)
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("did not quit: %T", msg)
	}
	if got := m.summary(); !strings.HasPrefix(got, "Updated ") || !strings.HasSuffix(got, " in book.epub") {
		t.Errorf("summary = %q", got)
	}
}

// batchCmds flattens a tea.Batch into its commands.
func batchCmds(cmd tea.Cmd) []tea.Cmd {
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Cmd{func() tea.Msg { return msg }}
	}
	var out []tea.Cmd
	for _, c := range b {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

func TestMetaPageWriteFailureStaysOnTheFieldsPage(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	m, _ = metaStep(t, m, metaKey("enter"))
	m.stage = metaWriting
	m.applied = []string{"authors"}
	m, cmd := metaStep(t, m, metaWriteDone{err: errors.New("disk full")})
	if cmd != nil || m.stage != metaFields || len(m.applied) != 0 {
		t.Fatalf("stage = %v, applied = %v", m.stage, m.applied)
	}
	if !strings.Contains(metaScreen(m), "Could not write the file: disk full") {
		t.Errorf("error not shown:\n%s", metaScreen(m))
	}
}

func TestMetaPageNeedsAtLeastOneField(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	m, _ = metaStep(t, m, metaKey("enter"))
	for i := range m.selected {
		m.selected[i] = false
	}
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	if cmd != nil || m.stage != metaFields || !strings.Contains(metaScreen(m), "Select at least one field") {
		t.Fatalf("stage = %v:\n%s", m.stage, metaScreen(m))
	}
}

func TestMetaPageHidesFieldsTheFormatCannotStore(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	m.format = ebookmeta.FormatPDF
	m, _ = metaStep(t, m, metaKey("enter"))
	for _, ch := range m.plan {
		if ch.Field == metasource.FieldPublisher || strings.HasPrefix(string(ch.Field), "identifier:") {
			t.Errorf("offered %s for a PDF", ch.Field)
		}
	}
	if !strings.Contains(metaScreen(m), "PDF files cannot store: publisher") {
		t.Errorf("hidden fields not named:\n%s", metaScreen(m))
	}
}

func TestMetaPageNothingToAddOffersOnlyBack(t *testing.T) {
	res := metasource.Result{Candidates: []metasource.Candidate{{Title: "Before", Sources: []string{"A"}, Match: metasource.MatchTitle, Score: 0.9}}}
	m := metaPageAfterLookup(t, answerWith(res, nil, nil), nil)
	m, _ = metaStep(t, m, metaKey("enter"))
	if len(m.plan) != 0 || !strings.Contains(metaScreen(m), "Nothing to add") || strings.Contains(metaScreen(m), "Apply") {
		t.Fatalf("plan = %v:\n%s", m.plan, metaScreen(m))
	}
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	if cmd != nil || m.stage != metaFields {
		t.Fatal("ctrl+s should do nothing without fields")
	}
	m, _ = metaStep(t, m, metaKey("enter"))
	if m.stage != metaCandidates {
		t.Fatalf("stage = %v", m.stage)
	}
}

func TestMetaPageNoMatchAndRetry(t *testing.T) {
	var calls atomic.Int32
	m := metaPageAfterLookup(t, answerWith(metasource.Result{}, nil, &calls), nil)
	if m.stage != metaFailed || !strings.Contains(metaScreen(m), "No catalogue knows this book") {
		t.Fatalf("stage = %v:\n%s", m.stage, metaScreen(m))
	}
	m, cmd := metaStep(t, m, metaKey("r"))
	if m.stage != metaLooking || m.gen != 1 || cmd == nil {
		t.Fatalf("stage = %v, gen = %d", m.stage, m.gen)
	}
	// The retry runs a second lookup on a fresh run.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("lookup ran %d times, want 2", got)
	}
	// A late answer to the first lookup is ignored.
	m, _ = metaStep(t, m, metaLookupDone{gen: 0, res: twoCandidates()})
	if m.stage != metaLooking {
		t.Fatalf("a stale answer changed the page to %v", m.stage)
	}
	m, _ = metaStep(t, m, metaLookupDone{gen: 1, res: twoCandidates()})
	if m.stage != metaCandidates {
		t.Fatalf("stage = %v", m.stage)
	}
}

func TestMetaPageUnreachableCataloguesNameEachSource(t *testing.T) {
	res := metasource.Result{Errors: []metasource.SourceError{{Source: "Open Library", Err: errors.New("connection refused")}, {Source: "Google Books", Err: metasource.ErrRateLimited}}}
	m := metaPageAfterLookup(t, answerWith(res, metasource.ErrAllSourcesFailed, nil), nil)
	screen := metaScreen(m)
	for _, want := range []string{"No catalogue could be reached", "Open Library: connection refused", "Google Books: rate limited", "ZLIB_PROXY", "r retry"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestMetaPagePartialFailureIsNotedOnTheList(t *testing.T) {
	res := twoCandidates()
	res.Errors = []metasource.SourceError{{Source: "Google Books", Err: metasource.ErrRateLimited}}
	m := metaPageAfterLookup(t, answerWith(res, nil, nil), nil)
	if !strings.Contains(metaScreen(m), "Google Books: rate limited") {
		t.Errorf("failed source not noted:\n%s", metaScreen(m))
	}
}

func TestMetaPageEscCancelsALookupInFlight(t *testing.T) {
	cancelled := make(chan struct{})
	lookup := func(ctx context.Context, q metasource.Query) (metasource.Result, error) {
		<-ctx.Done()
		close(cancelled)
		return metasource.Result{}, ctx.Err()
	}
	m, err := newMetaPageModel(writeTestEPUB(t), lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.start() // the lookup is running and blocked on its context
	m, cmd := metaStep(t, m, metaKey("esc"))
	if !m.cancelled || cmd == nil {
		t.Fatalf("cancelled = %v", m.cancelled)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the lookup context was not cancelled")
	}
}

func TestMetaPageLooksUpOnceHoweverOftenInitRuns(t *testing.T) {
	var calls atomic.Int32
	m, err := newMetaPageModel(writeTestEPUB(t), answerWith(twoCandidates(), nil, &calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.Init()
	m.Init()
	m.start()()
	if got := calls.Load(); got != 1 {
		t.Fatalf("lookup ran %d times", got)
	}
}

func TestMetaPageRefusesAFileWithNothingToSearchWith(t *testing.T) {
	p := writeTestEPUB(t)
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("")}); err != nil {
		t.Fatal(err)
	}
	if _, err := newMetaPageModel(p, nil, nil); err == nil || !strings.Contains(err.Error(), "no title, author or ISBN") {
		t.Fatalf("err = %v", err)
	}
}

func TestShellMetaCommandOpensThePage(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake"})
	if !slices.ContainsFunc(shellCommands, func(c shellCommand) bool { return c.name == "meta" }) {
		t.Fatal("meta is not a shell command")
	}
	m := newShellModel()
	m.dispatch(`/meta "` + writeTestEPUB(t) + `"`)
	if _, ok := m.page.(metaPageModel); !ok || m.kind != "meta" {
		t.Fatalf("kind = %q, page = %T, status = %q", m.kind, m.page, m.status)
	}
	m = newShellModel()
	m.dispatch("/meta " + writeTestEPUB(t) + ".missing")
	if m.page != nil || !strings.Contains(m.status, "File not found") {
		t.Fatalf("page = %T, status = %q", m.page, m.status)
	}
	m = newShellModel()
	m.dispatch("/meta")
	if page, ok := m.page.(sendFileModel); !ok || page.target.title != "Fix metadata" || m.kind != "meta-file" {
		t.Fatalf("kind = %q, page = %T", m.kind, m.page)
	}
}

func TestShellMetaDoneReportsWhatWasWritten(t *testing.T) {
	page := metaPageModel{path: "/books/book.epub", run: newMetaRun(), applied: []string{"title", "authors"}}
	m := newShellModel()
	m.metaDone(page)
	if !strings.Contains(m.status, "Updated 2 fields in book.epub") {
		t.Fatalf("status = %q", m.status)
	}
	m = newShellModel()
	page.cancelled = true
	m.metaDone(page)
	if m.status != "" {
		t.Fatalf("a cancelled page left %q", m.status)
	}
}

func TestMetaPageFitsCompactTerminals(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	looking := m
	looking.stage = metaLooking
	failed := m
	failed.stage, failed.res = metaFailed, metasource.Result{Errors: []metasource.SourceError{{Source: "Open Library", Err: errors.New("connection refused")}}}
	failed.err = metasource.ErrAllSourcesFailed
	fields, _ := metaStep(t, m, metaKey("enter"))
	writing := fields
	writing.stage = metaWriting
	for name, page := range map[string]metaPageModel{"looking": looking, "failed": failed, "candidates": m, "fields": fields, "writing": writing} {
		for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 12}, {24, 8}, {20, 5}} {
			next, _ := page.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			lines := strings.Split(ansi.Strip(next.(metaPageModel).View().Content), "\n")
			if len(lines) > size[1] {
				t.Errorf("%s %dx%d: %d lines", name, size[0], size[1], len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Errorf("%s %dx%d: line wider than the terminal: %q", name, size[0], size[1], l)
					break
				}
			}
		}
	}
}

func TestMetaPageKeepsTheCandidateListScrolledToTheCursor(t *testing.T) {
	var cands []metasource.Candidate
	for i := range 12 {
		cands = append(cands, metasource.Candidate{Title: "Book " + string(rune('A'+i)), Sources: []string{"A"}, Match: metasource.MatchTitle, Score: 0.9})
	}
	m := metaPageAfterLookup(t, answerWith(metasource.Result{Candidates: cands}, nil, nil), nil)
	m.height = 14
	for range 11 {
		m, _ = metaStep(t, m, metaKey("down"))
	}
	if m.cursor != 11 || !strings.Contains(metaScreen(m), "> ") || !strings.Contains(metaScreen(m), "Book L") {
		t.Fatalf("cursor = %d, top = %d:\n%s", m.cursor, m.top, metaScreen(m))
	}
	if strings.Contains(metaScreen(m), "Book A") {
		t.Errorf("the list did not scroll:\n%s", metaScreen(m))
	}
}
