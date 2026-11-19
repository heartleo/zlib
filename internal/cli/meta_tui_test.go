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
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
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

// searchFromPrepare presses Enter on the prepare page and feeds the answer of
// the lookup it starts back to the page.
func searchFromPrepare(t *testing.T, m metaPageModel) metaPageModel {
	t.Helper()
	if m.stage != metaPrepare {
		t.Fatalf("stage = %v, want the prepare page", m.stage)
	}
	m, cmd := metaStep(t, m, key("enter"))
	if m.stage != metaLooking || cmd == nil {
		t.Fatalf("enter did not start a search: stage=%v editing=%v", m.stage, m.editing)
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaLookupDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	return m
}

// searchFromPrepareModel is searchFromPrepare for a page held by another model,
// such as the shell, and returns it as a tea.Model.
func searchFromPrepareModel(t *testing.T, m metaPageModel) tea.Model {
	t.Helper()
	return searchFromPrepare(t, m)
}

// metaPageAfterLookup opens the page, searches and feeds the answer to it.
func metaPageAfterLookup(t *testing.T, lookup metaLookupFunc, write metaWriteFunc, opts ...metaOption) metaPageModel {
	t.Helper()
	m, err := newMetaPageModel(writeTestEPUB(t), lookup, write, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return searchFromPrepare(t, m)
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
	// Wide enough for the catalogues' full names in the sources column.
	m = sized(t, m, 120, 30)
	screen := metaScreen(m)
	for _, want := range []string{"Fix metadata", "book.epub · EPUB", "Match", "Title", "Authors", "Year", "Source", "esc back"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	// One row per match, its cells in aligned columns.
	first, second := rowWith(t, screen, "ISBN  Before "), rowWith(t, screen, " 71%  Before (2nd ed.)")
	for _, cell := range []string{"Frank Herbert", "2005", "[Open Library+Google Books]"} {
		if !strings.Contains(first, cell) {
			t.Errorf("the first row %q lacks %q", first, cell)
		}
	}
	if strings.Index(first, "[") != strings.Index(second, "[") {
		t.Errorf("the source columns are not aligned:\n%s\n%s", first, second)
	}
}

// rowWith is the line of screen that holds s.
func rowWith(t *testing.T, screen, s string) string {
	t.Helper()
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	t.Fatalf("no line holds %q:\n%s", s, screen)
	return ""
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

func TestMetaPageNoMatchAndRetry(t *testing.T) {
	var calls atomic.Int32
	m := metaPageAfterLookup(t, answerWith(metasource.Result{}, nil, &calls), nil)
	if m.stage != metaFailed || !strings.Contains(metaScreen(m), "No catalogue knows this book") {
		t.Fatalf("stage = %v:\n%s", m.stage, metaScreen(m))
	}
	m, cmd := metaStep(t, m, metaKey("r"))
	if m.stage != metaLooking || m.gen != 2 || cmd == nil { // the first search was gen 1
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
	// A late answer to the first lookup (generation 1) is ignored.
	m, _ = metaStep(t, m, metaLookupDone{gen: 1, res: twoCandidates()})
	if m.stage != metaLooking {
		t.Fatalf("a stale answer changed the page to %v", m.stage)
	}
	m, _ = metaStep(t, m, metaLookupDone{gen: 2, res: twoCandidates()})
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
	m, _ = metaStep(t, m, metaKey("enter")) // the lookup is running and blocked on its context
	if m.stage != metaLooking {
		t.Fatalf("stage = %v", m.stage)
	}
	// Esc stops the lookup and goes back to the page the search started from.
	m, cmd := metaStep(t, m, metaKey("esc"))
	if m.cancelled || cmd != nil || m.stage != metaPrepare {
		t.Fatalf("cancelled = %v, stage = %v", m.cancelled, m.stage)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the lookup context was not cancelled")
	}
}

// Init searches nothing, however often it runs: the page waits on the prepare
// step, and Enter starts exactly one lookup.
func TestMetaPageSearchesOnlyOnEnterHoweverOftenInitRuns(t *testing.T) {
	var calls atomic.Int32
	m, err := newMetaPageModel(writeTestEPUB(t), answerWith(twoCandidates(), nil, &calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.Init()
	m.Init()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("Init searched: the lookup ran %d times", got)
	}
	m = searchFromPrepare(t, m)
	m.Init()
	if got := calls.Load(); got != 1 {
		t.Fatalf("lookup ran %d times after Enter, want 1", got)
	}
	if m.stage != metaCandidates {
		t.Errorf("stage = %v", m.stage)
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
