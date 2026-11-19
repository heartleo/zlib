package cli

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// richEPUB is a book whose own metadata fills most fields, named like a download.
func richEPUB(t *testing.T) string {
	t.Helper()
	p := namedEPUB(t, "Whatever (Nobody).epub")
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{
		Title:       new("Dune"),
		Authors:     &[]string{"Frank Herbert", "Brian Herbert"},
		Publisher:   new("Ace Books"),
		Published:   new("1990-09-01"),
		Tags:        &[]string{"Fiction", "Classics"},
		Series:      new("Dune Chronicles"),
		SeriesIndex: new(1.0),
		Description: new("A desert planet.\nA second paragraph about the spice melange, which is the most valuable substance in the known universe and grants long life."),
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func richPage(t *testing.T) (metaPageModel, *[]metasource.Query) {
	t.Helper()
	queries := new([]metasource.Query)
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(twoCandidates(), queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, queries
}

// searchTerms is the part of a prepare screen between the "Search for" rule and
// the sources: the rows that say what will be searched.
func searchTerms(screen string) string {
	start := strings.Index(screen, "Search for")
	if start < 0 {
		return ""
	}
	if end := strings.Index(screen[start:], "\nSources"); end >= 0 {
		return screen[start : start+end]
	}
	return screen[start:]
}

// The page shows what the file already holds, field by field, before anything
// is asked of a catalogue.
func TestMetaPagePrepareShowsTheFilesOwnMetadataBeforeAnySearch(t *testing.T) {
	m, queries := richPage(t)
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(metaPageModel)
	screen := metaScreen(m)
	for _, want := range []string{
		"Dune", "Frank Herbert & Brian Herbert", "EPUB", "Ace Books", "1990-09-01", "Dune Chronicles #1", "Fiction, Classics", "A desert planet.",
		"m edit file",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the page lacks %q:\n%s", want, screen)
		}
	}
	// With no ISBN to search by there is no ISBN row to say "none"; with one,
	// the row says where it came from.
	if strings.Contains(screen, "(none)") || strings.Contains(searchTerms(screen), "ISBN") {
		t.Errorf("the terms have an ISBN row for a search without an ISBN:\n%s", screen)
	}
	withISBN := m
	withISBN.query.ISBNs = []string{"9780441013593"}
	withISBN.extraction.ISBNFrom = []string{"embedded"}
	if terms := searchTerms(metaScreen(withISBN)); !strings.Contains(terms, "ISBN") || !strings.Contains(terms, "9780441013593") {
		t.Errorf("the terms lack the ISBN row:\n%s", terms)
	}
	// The card with the book comes first, then what will be searched; nothing
	// has been sent.
	if card, search := strings.Index(screen, "Frank Herbert & Brian"), strings.Index(screen, "Search for"); card < 0 || search < card {
		t.Errorf("the order is wrong (card %d, search %d)", card, search)
	}
	if len(*queries) != 0 || m.stage != metaPrepare {
		t.Errorf("showing the file searched: %d queries, stage %v", len(*queries), m.stage)
	}
	// Only the start of the description is in the card.
	if strings.Contains(screen, "known universe and grants long life") && strings.Contains(screen, "second paragraph about the spice melange, which is the most valuable substance in the known universe and grants long life.") {
		t.Errorf("the whole description is in the card:\n%s", screen)
	}
}
func TestMetaPagePrepareSaysAFileHasNoMetadataOfItsOwn(t *testing.T) {
	var queries []metasource.Query
	m, err := newMetaPageModel(emptyMetadataEPUB(t), recordingLookup(twoCandidates(), &queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.editing = false // the empty file opens the search editor; look at the page behind it
	m.current = &ebookmeta.Metadata{}
	if screen := metaScreen(sized(t, m, 100, 40)); !strings.Contains(screen, "The file has no metadata of its own.") {
		t.Errorf("an empty file is not described:\n%s", screen)
	}
}

// The details page shows everything, with the description wrapped, from the
// prepare page and back, without searching.
func TestMetaPageFileDetailsShowTheWholeDescription(t *testing.T) {
	m, queries := richPage(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = next.(metaPageModel)
	m, _ = metaStep(t, m, key("i"))
	if m.stage != metaInfo {
		t.Fatalf("i did not open the details: stage %v", m.stage)
	}
	screen := ansi.Strip(m.View().Content)
	squashed := strings.Join(strings.Fields(screen), " ")
	for _, want := range []string{"File details", "Ace Books", "A desert planet.", "second paragraph about the spice melange", "known universe and grants long life."} {
		if !strings.Contains(squashed, want) {
			t.Errorf("the details lack %q:\n%s", want, screen)
		}
	}
	for _, l := range strings.Split(screen, "\n") {
		if ansi.StringWidth(l) > 60 {
			t.Errorf("a line is wider than the terminal: %q", l)
		}
	}
	for _, k := range []string{"esc", "i", "q", "enter"} {
		back, _ := metaStep(t, m, key(k))
		if back.stage != metaPrepare || back.cancelled {
			t.Errorf("%s: stage %v cancelled %v", k, back.stage, back.cancelled)
		}
	}
	if len(*queries) != 0 {
		t.Errorf("the details page searched: %d queries", len(*queries))
	}
}

func TestMetaPageFileDetailsFitCompactTerminals(t *testing.T) {
	m, _ := richPage(t)
	m, _ = metaStep(t, m, key("i"))
	for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 12}, {24, 8}} {
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
		if !strings.Contains(strings.Join(lines, "\n"), "esc") {
			t.Errorf("%dx%d: no way back is shown", size[0], size[1])
		}
	}
}

// The terms and sources stay on screen on a short terminal; the file's metadata
// is below them, and i shows it whole.
func TestMetaPagePrepareStillFitsWithTheFilesMetadata(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(twoCandidates(), &queries), nil,
		withMetaSources(fakeSourceChoices(), func(names []string) metaLookupFunc {
			asked = append(asked, names)
			return recordingLookup(twoCandidates(), &queries)
		}))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(ansi.Strip(next.(metaPageModel).View().Content), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		screen := strings.Join(lines, "\n")
		for _, want := range []string{"Search for", "[x] Open Library"} {
			if size[1] >= 16 && !strings.Contains(screen, want) {
				t.Errorf("%dx%d: %q is not on screen:\n%s", size[0], size[1], want, screen)
			}
		}
		if got := footer(screen); !strings.Contains(got, "esc") || !strings.Contains(got, "enter") {
			t.Errorf("%dx%d: footer %q", size[0], size[1], got)
		}
	}
}

// metadataRows is what the command prints too.
func TestMetadataRowsAreWhatTheCommandPrints(t *testing.T) {
	m := &ebookmeta.Metadata{Title: "Dune", Authors: []string{"Frank Herbert"}, Series: "Saga", SeriesIndex: 2.5, Rating: 4.5}
	rows := metadataRows(ebookmeta.FormatEPUB, m)
	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[1]
	}
	if got["Format"] != "EPUB" || got["Title"] != "Dune" || got["Author"] != "Frank Herbert" || got["Series"] != "Saga #2.5" || got["Rating"] != "4.5/5" {
		t.Errorf("rows = %v", rows)
	}
	if _, ok := got["Description"]; ok {
		t.Errorf("the description is a row: %v", rows)
	}
}

// A plain --fetch shows what the file holds before the matches, and --json carries
// it as current.
func TestMetaFetchShowsTheFilesOwnMetadataBeforeTheMatches(t *testing.T) {
	c := dune()
	c.Title, c.Authors = "Whatever", []string{"Nobody"} // what the file name says, which is searched
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{c}})
	p := richEPUB(t)
	out, _, err := runMetaWithStderr(t, p, "--fetch")
	if err != nil {
		t.Fatal(err)
	}
	screen := ansi.Strip(out)
	file, match := strings.Index(screen, "In the file"), strings.Index(screen, "1. ")
	if file < 0 || match < file {
		t.Fatalf("the file's metadata does not come first (file %d, match %d):\n%s", file, match, screen)
	}
	for _, want := range []string{"Name", "Whatever (Nobody).epub", "Title", "Dune", "Authors", "Frank Herbert & Brian Herbert", "Publisher", "Ace Books", "Series", "Dune Chronicles #1", "Description", "A desert planet."} {
		if !strings.Contains(screen[:match], want) {
			t.Errorf("the block before the matches lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen[:match], "second paragraph") {
		t.Errorf("the whole description is shown:\n%s", screen)
	}

	jsonOut, _, err := runMetaWithStderr(t, p, "--fetch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jsonOut, "In the file") {
		t.Errorf("the heading leaked into the JSON:\n%s", jsonOut)
	}
	var doc struct {
		Current struct {
			Title     string   `json:"title"`
			Authors   []string `json:"authors"`
			Publisher string   `json:"publisher"`
			Series    string   `json:"series"`
		} `json:"current"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if doc.Current.Title != "Dune" || len(doc.Current.Authors) != 2 || doc.Current.Publisher != "Ace Books" || doc.Current.Series != "Dune Chronicles" {
		t.Errorf("current = %+v", doc.Current)
	}
}

// The file's metadata is shown even when the lookup then finds nothing.
func TestMetaFetchShowsTheFilesOwnMetadataEvenWhenNothingMatches(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake"})
	out, _, err := runMetaWithStderr(t, richEPUB(t), "--fetch")
	if err == nil {
		t.Fatal("expected no match")
	}
	if screen := ansi.Strip(out); !strings.Contains(screen, "In the file") || !strings.Contains(screen, "Ace Books") {
		t.Errorf("the file's metadata is missing:\n%s", screen)
	}
}
