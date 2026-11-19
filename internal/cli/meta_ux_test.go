package cli

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

func sized(t *testing.T, m metaPageModel, w, h int) metaPageModel {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(metaPageModel)
}

func screenOf(m metaPageModel) string { return ansi.Strip(m.View().Content) }

// richFields is the review page of a match against a file that has most fields
// already, so it proposes replacements as well as additions.
func richFields(t *testing.T) metaPageModel {
	t.Helper()
	m, _ := richPage(t)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields || len(m.plan) < 5 {
		t.Fatalf("stage %v with %d rows", m.stage, len(m.plan))
	}
	return m
}

func focusField(t *testing.T, m metaPageModel, f metasource.Field) metaPageModel {
	t.Helper()
	for i, ch := range m.plan {
		if ch.Field == f {
			m.focus = i
			return m
		}
	}
	t.Fatalf("no %s row", f)
	return m
}

// longPlan repeats the rows of m until it has n, to test a plan taller than the
// screen.
func longPlan(m metaPageModel, n int) metaPageModel {
	plan, sel := m.plan, m.selected
	for len(m.plan) < n {
		m.plan = append(m.plan, plan[len(m.plan)%len(plan)])
		m.selected = append(m.selected, sel[len(m.selected)%len(sel)])
	}
	return m
}

func checkFieldsScroll(t *testing.T, name string, m metaPageModel) {
	t.Helper()
	m = longPlan(m, 14)
	n := len(m.plan)
	var sizes [][2]int
	for h := 8; h <= 30; h++ {
		sizes = append(sizes, [2]int{80, h}, [2]int{40, h})
	}
	for _, size := range sizes {
		p := sized(t, m, size[0], size[1])
		for i := range n + 2 { // every row, then Apply and Back
			p.focus = i
			screen := screenOf(p)
			lines := strings.Split(screen, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%dx%d: %d lines", size[0], size[1], len(lines))
			}
			if !strings.Contains(screen, "Apply") || !strings.Contains(screen, "Back") {
				t.Fatalf("%s %dx%d focus %d: the buttons are off the screen:\n%s", name, size[0], size[1], i, screen)
			}
			if i < n && !strings.Contains(screen, "> [") {
				t.Errorf("%s %dx%d focus %d: the focused row is off the screen:\n%s", name, size[0], size[1], i, screen)
			}
			if size[1] >= 11 && size[1] < 26 && !strings.Contains(screen, "more") {
				t.Errorf("%s %dx%d focus %d: hidden rows are not counted:\n%s", name, size[0], size[1], i, screen)
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Fatalf("%dx%d: line wider than the terminal: %q", size[0], size[1], l)
				}
			}
		}
	}
}

func TestMetaPageHelpListsEveryKeyOfThePageAndCloses(t *testing.T) {
	m := sized(t, richFields(t), 80, 24)
	m, _ = metaStep(t, m, key("?"))
	if !m.helpOpen {
		t.Fatal("? did not open the help")
	}
	screen := screenOf(m)
	for _, want := range []string{"Keys", "space", "switch between the match", "a ", "for every field", "ctrl+s", "write the changed fields", "n ", "rename", "esc"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the help lacks %q:\n%s", want, screen)
		}
	}
	// A key other than close does nothing while the help is open.
	m, _ = metaStep(t, m, key("a"))
	if !m.helpOpen || m.selected[len(m.selected)-1] != true || m.selected[0] {
		t.Errorf("a acted through the help")
	}
	for _, k := range []string{"?", "esc", "q", "enter"} {
		next, _ := metaStep(t, m, key(k))
		if next.helpOpen || next.stage != metaFields || next.cancelled {
			t.Errorf("%s: help %v stage %v cancelled %v", k, next.helpOpen, next.stage, next.cancelled)
		}
	}
	// Each page has its own keys.
	var asked [][]string
	var queries []metasource.Query
	prep := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	prep.saveSources = func([]string) (string, error) { return "", nil }
	prep = sized(t, prep, 80, 40)
	prep, _ = metaStep(t, prep, key("?"))
	if s := screenOf(prep); !strings.Contains(s, "ctrl+d") || !strings.Contains(s, "save the sources") || !strings.Contains(s, "show everything in the file") {
		t.Errorf("the prepare help lacks its keys:\n%s", s)
	}
	// Keys that do nothing on the page are not listed.
	bare, _ := richPage(t) // no sources, no saver, no other title
	bare = sized(t, bare, 80, 40)
	bare.extraction.AltTitle = ""
	bare, _ = metaStep(t, bare, key("?"))
	for _, k := range []string{"ctrl+d", "space ", "t      "} {
		if s := screenOf(bare); strings.Contains(s, "  "+k) {
			t.Errorf("the help lists %q, which does nothing here:\n%s", k, s)
		}
	}
}

// While an editor is open, ? is text.
func TestMetaPageQuestionMarkIsTextInTheEditor(t *testing.T) {
	m := richFields(t)
	m, _ = metaStep(t, m, key("e"))
	m, _ = metaStep(t, m, key("?"))
	if m.helpOpen || !strings.HasSuffix(m.editor.values()[0], "?") {
		t.Errorf("help %v, field %q", m.helpOpen, m.editor.values()[0])
	}
}

func TestMetaPageSubtitleSaysWhichStep(t *testing.T) {
	prep, _ := richPage(t)
	prep = sized(t, prep, 100, 30)
	fields := sized(t, richFields(t), 100, 30)
	cands := fields
	cands.stage = metaCandidates
	for want, page := range map[string]metaPageModel{
		"Step 1 of 3: Search":         prep,
		"Step 2 of 3: Choose a match": cands,
		"Step 3 of 3: Review changes": fields,
	} {
		if !strings.Contains(screenOf(page), want) {
			t.Errorf("the subtitle lacks %q:\n%s", want, screenOf(page))
		}
	}
	// Narrow terminals get the short form, and the file name keeps its place.
	narrow := sized(t, fields, 52, 20)
	if s := screenOf(narrow); !strings.Contains(s, "3/3") || !strings.Contains(s, "Whatever (Nobody).epub") {
		t.Errorf("the narrow subtitle:\n%s", s)
	}
}

// The results say what choosing a match would do to the file, from the same
// plan the review page uses.
func TestMetaResultsSayWhatAMatchWouldChange(t *testing.T) {
	m, _ := richPage(t)
	m = sized(t, searchFromPrepare(t, m), 90, 30) // the details layout; wider shows the panels
	var add, differ string
	for _, r := range m.candidateEffect(m.res.Candidates[m.cursor]) {
		switch r[0] {
		case "Would add":
			add = r[1]
		case "Differs on":
			differ = r[1]
		}
	}
	if !strings.Contains(add, "ID isbn") || !strings.Contains(differ, "Title") || !strings.Contains(differ, "kept unless you select them") {
		t.Errorf("rows:\n%s\n%s", add, differ)
	}
	if s := screenOf(m); !strings.Contains(s, "Would add") || !strings.Contains(s, "Differs on") {
		t.Errorf("the details do not say what the match would change:\n%s", s)
	}
	// A match found by title only fills empty edition fields, but they start
	// unselected: the results must not promise to add them.
	title := dune()
	title.Title, title.Match, title.Score = "Whatever", metasource.MatchTitle, 0.8
	effect := m.candidateEffect(title)
	for _, r := range effect {
		if r[0] == "Would add" && strings.Contains(r[1], "ID isbn") {
			t.Errorf("a title-only match promises its ISBN: %v", effect)
		}
	}
	found := false
	for _, r := range effect {
		found = found || (r[0] == "Could add" && strings.Contains(r[1], "ID isbn") && strings.Contains(r[1], "not selected"))
	}
	if !found {
		t.Errorf("the ISBN is not offered as unselected: %v", effect)
	}
	// What the results promise is exactly what the next page selects.
	f := m
	f.res = metasource.Result{Candidates: []metasource.Candidate{title}}
	f.cursor = 0
	f = f.openFields()
	for i, ch := range f.plan {
		if f.selected[i] && ch.Field == "identifier:isbn" {
			t.Errorf("the ISBN of a title-only match starts selected")
		}
	}
	// A match that changes nothing says so.
	same := metaPageModel{current: &ebookmeta.Metadata{Title: "Dune"}, format: ebookmeta.FormatEPUB}
	if rows := same.candidateEffect(metasource.Candidate{Title: "Dune"}); len(rows) != 1 || !strings.Contains(rows[0][1], "nothing") {
		t.Errorf("rows = %v", rows)
	}
}

// On a short terminal the help scrolls, so the keys at its end can be read.
func TestMetaPageHelpScrollsOnShortTerminals(t *testing.T) {
	m := sized(t, richFields(t), 80, 14)
	m, _ = metaStep(t, m, key("?"))
	first := screenOf(m)
	if !strings.Contains(first, "%") || !strings.Contains(footer(first), "scroll") {
		t.Fatalf("the short help does not say it scrolls:\n%s", first)
	}
	for range 20 {
		m, _ = metaStep(t, m, metaKey("down"))
	}
	if last := screenOf(m); !strings.Contains(last, "ctrl+c") || !strings.Contains(last, "100%") || !m.helpOpen {
		t.Errorf("the end of the help is not reachable:\n%s", last)
	}
}

// Page Up and Page Down move a page of results, as the help says.
func TestMetaResultsPageKeysMoveAPage(t *testing.T) {
	var cands []metasource.Candidate
	for i := range 20 {
		c := dune()
		c.Title = "Before " + itoa(i)
		cands = append(cands, c)
	}
	var queries []metasource.Query
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(metasource.Result{Candidates: cands}, &queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	m = sized(t, searchFromPrepare(t, m), 80, 30)
	rows := m.listRows()
	m, _ = metaStep(t, m, key("pgdown"))
	if m.cursor != rows || rows < 2 {
		t.Errorf("pgdown moved to %d, want %d", m.cursor, rows)
	}
	m, _ = metaStep(t, m, key("pgup"))
	if m.cursor != 0 {
		t.Errorf("pgup moved to %d", m.cursor)
	}
}

// A PDF cannot store a publisher or a date: the results say so rather than
// claiming the file has them already.
func TestMetaResultsNameFieldsTheFormatCannotStore(t *testing.T) {
	m := metaPageModel{current: &ebookmeta.Metadata{Title: "Dune", Authors: []string{"Frank Herbert"}}, format: ebookmeta.FormatPDF}
	c := metasource.Candidate{Title: "Dune", Authors: []string{"Frank Herbert"}, Publisher: "Ace", Published: "2005", Match: metasource.MatchTitle, Score: 0.9}
	rows := m.candidateEffect(c)
	var got string
	for _, r := range rows {
		got += r[0] + ": " + r[1] + "\n"
	}
	if strings.Contains(got, "already has these values") || !strings.Contains(got, "Cannot store: Publisher") || !strings.Contains(got, "(PDF has no place for them)") {
		t.Errorf("rows:\n%s", got)
	}
}

// A non-ASCII date is cut by cells, not bytes.
func TestMetaResultsYearIsNotCutByBytes(t *testing.T) {
	m, _ := richPage(t)
	c := dune()
	c.Published = "二〇〇八年"
	m.res = metasource.Result{Candidates: []metasource.Candidate{c}}
	m.stage, m.cursor = metaCandidates, 0
	if s := screenOf(sized(t, m, 100, 30)); strings.Contains(s, "\uFFFD") || !strings.Contains(s, "二〇") {
		t.Errorf("the year cell:\n%s", s)
	}
}

// The prepare page, top to bottom: the book's detail card, then what is searched,
// then the sources; and on short terminals the card gives way before the terms
// and the sources do.
func TestMetaPrepareStacksCardTermsAndSources(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(twoCandidates(), &queries), nil,
		withMetaSources(fakeSourceChoices(), func(names []string) metaLookupFunc {
			asked = append(asked, names)
			return recordingLookup(twoCandidates(), &queries)
		}), withMetaSourceSaver(func([]string) (string, error) { return "", nil }))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}, {80, 23}, {60, 16}, {40, 12}} {
		screen := screenOf(sized(t, m, size[0], size[1]))
		if n := len(strings.Split(screen, "\n")); n > size[1] {
			t.Fatalf("%dx%d: %d lines", size[0], size[1], n)
		}
		search, sources := strings.Index(screen, "Search for"), strings.Index(screen, "Sources")
		if search < 0 || sources < search {
			t.Errorf("%dx%d: terms and sources are out of order or cut (search %d, sources %d):\n%s", size[0], size[1], search, sources, screen)
		}
		if size[1] >= 16 && !strings.Contains(screen, "[ ] WeRead") {
			t.Errorf("%dx%d: the last source is cut:\n%s", size[0], size[1], screen)
		}
		if size[1] >= 23 {
			if card := strings.Index(screen, "Dune"); card < 0 || card > search {
				t.Errorf("%dx%d: the card is not on top:\n%s", size[0], size[1], screen)
			}
		}
	}
	// With room for a cover the card puts it beside the text, inside one border.
	screen := screenOf(sized(t, m, 120, 40))
	var row string
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, "Frank Herbert & Brian Herbert") {
			row = l
		}
	}
	if !strings.HasPrefix(row, "│ ") || !strings.Contains(row, "│  Frank") && !strings.Contains(row, "  Frank") || !strings.Contains(screen, "no cover") {
		t.Errorf("the cover and the metadata are not side by side:\n%s", screen)
	}
}
func TestMetaFileDetailsScroll(t *testing.T) {
	m, _ := richPage(t)
	m = sized(t, m, 50, 14)
	m, _ = metaStep(t, m, key("i"))
	first := screenOf(m)
	if !strings.Contains(first, "%") || !strings.Contains(footer(first), "scroll") {
		t.Fatalf("the details do not say they scroll:\n%s", first)
	}
	for range 30 {
		m, _ = metaStep(t, m, metaKey("down"))
	}
	last := screenOf(m)
	if !strings.Contains(strings.Join(strings.Fields(last), " "), "long life.") || !strings.Contains(last, "100%") {
		t.Errorf("the end of the description is not reachable:\n%s", last)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaPrepare {
		t.Errorf("esc: stage %v", m.stage)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// Values in the card come from the file: control characters in an identifier
// never reach the screen.
func TestMetaPrepareCardCleansWhatTheFileHolds(t *testing.T) {
	m, _ := richPage(t)
	m.current.SetIdentifier("isbn", "9780441013593\x1b[31m\x07")
	m.current.SetIdentifier(metasource.SchemeZlibID, "12\x1b]0;x\x07:ab")
	screen := sized(t, m, 100, 40).View().Content
	if strings.Contains(screen, "\x1b[31m") || strings.Contains(screen, "\x1b]0;") || strings.Contains(screen, "\x07") {
		t.Errorf("a control sequence from the file reached the screen: %q", screen)
	}
}
