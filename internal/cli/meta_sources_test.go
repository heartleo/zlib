package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

func fakeSourceChoices() []metaSourceChoice {
	return []metaSourceChoice{
		{name: "openlibrary", label: "Open Library", enabled: true, available: true},
		{name: "google", label: "Google Books", enabled: true, available: true, note: "shared quota"},
		{name: "douban", label: "Douban", available: true},
		{name: "weread", label: "WeRead", note: "needs ZLIB_WEREAD_API_KEY"},
	}
}

// sourcesPage is a page on its candidates, built with a choice of sources whose
// factory records the sources it was asked to search and answers offline. The
// first search, made from the prepare page, is already behind it and is not
// counted in asked.
func sourcesPage(t *testing.T, choices []metaSourceChoice, asked *[][]string, queries *[]metasource.Query) metaPageModel {
	t.Helper()
	m := preparedSourcesPage(t, choices, asked, queries)
	m = searchFromPrepare(t, m)
	*asked = nil
	return m
}

// preparedSourcesPage is the same page before any search: on the prepare page.
func preparedSourcesPage(t *testing.T, choices []metaSourceChoice, asked *[][]string, queries *[]metasource.Query) metaPageModel {
	t.Helper()
	factory := func(names []string) metaLookupFunc {
		*asked = append(*asked, names)
		return recordingLookup(twoCandidates(), queries)
	}
	m, err := newMetaPageModel(writeTestEPUB(t), recordingLookup(twoCandidates(), queries), nil, withMetaSources(choices, factory))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The page opens on what is known and which sources will be asked. Nothing is
// sent until Enter.
func TestMetaPageOpensOnThePrepareStepAndSearchesNothingYet(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m.Init() // what the program does first
	if m.stage != metaPrepare || len(queries) != 0 || len(asked) != 0 {
		t.Fatalf("stage=%v after %d searches and %d source builds", m.stage, len(queries), len(asked))
	}
	screen := metaScreen(m)
	for _, want := range []string{
		"Search for", "Title", "Before", "from the file", // the embedded title of the test file
		"[x] Open Library", "[x] Google Books", "[ ] Douban", "[ ] WeRead", "shared quota", "needs ZLIB_WEREAD_API_KEY",
		"Before", "EPUB",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the prepare page lacks %q:\n%s", want, screen)
		}
	}
	// Enter is the only thing that sends anything.
	m = searchFromPrepare(t, m)
	if len(queries) != 1 || m.stage != metaCandidates {
		t.Errorf("after Enter: %d searches, stage %v", len(queries), m.stage)
	}
}

// The file name is read first and the file second, and the page says which gave
// what.
func TestMetaPagePrepareShowsTheFileNameBeforeTheEmbeddedMetadata(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "一读就上瘾的中国史 (温伯陵).epub")
	data, err := os.ReadFile(writeTestEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{
		Title: new("一读就上瘾的中国史(爆款历史大号“温乎”首部作品集)"), Authors: &[]string{"温伯陵"},
	}); err != nil {
		t.Fatal(err)
	}
	m, err := newMetaPageModel(p, recordingLookup(twoCandidates(), new([]metasource.Query)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.query.Title != "一读就上瘾的中国史" || m.extraction.TitleFrom != "file name" || m.extraction.AltTitle == "" {
		t.Fatalf("query = %+v, from %q, alt %q", m.query, m.extraction.TitleFrom, m.extraction.AltTitle)
	}
	screen := metaScreen(m)
	for _, want := range []string{"一读就上瘾的中国史", "from the file name", "press t to use it", "温伯陵", "温乎"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the prepare page lacks %q:\n%s", want, screen)
		}
	}
	// The book as the file has it is in the card on top, then the terms read
	// from the file name, then the sources.
	card, search := strings.Index(screen, "温乎"), strings.Index(screen, "Search for")
	if card < 0 || search < 0 || card >= search {
		t.Errorf("the order is wrong (card %d, search %d):\n%s", card, search, screen)
	}
	// t switches to the file's own title, and again back.
	m, _ = metaStep(t, m, key("t"))
	if !strings.Contains(m.query.Title, "爆款历史大号") || m.extraction.TitleFrom != "embedded" || m.query.TitleGuessed {
		t.Errorf("after t: %q from %q guessed=%v", m.query.Title, m.extraction.TitleFrom, m.query.TitleGuessed)
	}
	m, _ = metaStep(t, m, key("t"))
	if m.query.Title != "一读就上瘾的中国史" {
		t.Errorf("after t twice: %q", m.query.Title)
	}
}

func TestMetaPageSourcePanelListsEverySourceAndWhyOneIsOff(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	if !strings.Contains(metaScreen(m), "s sources") {
		t.Errorf("the candidates page does not offer the sources:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("s"))
	if m.stage != metaPrepare {
		t.Fatalf("s did not open the prepare page: stage=%v", m.stage)
	}
	screen := metaScreen(m)
	for _, want := range []string{
		"[x] Open Library", "[x] Google Books", "[ ] Douban", "[ ] WeRead",
		"shared quota", "needs ZLIB_WEREAD_API_KEY",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the page lacks %q:\n%s", want, screen)
		}
	}
}

func TestMetaPageSourcePanelTogglesAndSearchesAgainWithTheChosenSources(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	// Down to Douban and switch it on; Google Books off.
	m, _ = metaStep(t, m, metaKey("up")) // wraps to the last row (WeRead)
	m, _ = metaStep(t, m, metaKey("up")) // Douban
	m, _ = metaStep(t, m, metaKey("space"))
	m, _ = metaStep(t, m, metaKey("up")) // Google Books
	m, _ = metaStep(t, m, metaKey("space"))
	if !strings.Contains(metaScreen(m), "[x] Douban") || !strings.Contains(metaScreen(m), "[ ] Google Books") {
		t.Fatalf("the toggles are not shown:\n%s", metaScreen(m))
	}
	m, cmd := metaStep(t, m, key("enter"))
	if m.stage != metaLooking || cmd == nil {
		t.Fatalf("enter did not search again: stage=%v", m.stage)
	}
	if want := [][]string{{"openlibrary", "douban"}}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("the lookup was rebuilt for %v, want %v", asked, want)
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaLookupDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	if m.stage != metaCandidates || len(queries) != 2 {
		t.Errorf("stage=%v lookups=%d", m.stage, len(queries))
	}
	// The query is the same: only the sources changed.
	if !reflect.DeepEqual(queries[0], queries[1]) {
		t.Errorf("the search changed with the sources: %+v then %+v", queries[0], queries[1])
	}
}

func TestMetaPageSourcePanelRefusesASourceThatCannotRun(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, metaKey("up")) // WeRead, which has no key
	m, _ = metaStep(t, m, metaKey("space"))
	screen := metaScreen(m)
	if !strings.Contains(screen, "[ ] WeRead") || !strings.Contains(screen, "ZLIB_WEREAD_API_KEY") {
		t.Fatalf("a source with no key was switched on or unexplained:\n%s", screen)
	}
	if !strings.Contains(m.message, "ZLIB_WEREAD_API_KEY") {
		t.Errorf("message = %q", m.message)
	}
}

func TestMetaPageSourcePanelNeedsAtLeastOneSource(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off
	m, _ = metaStep(t, m, metaKey("down"))
	m, _ = metaStep(t, m, metaKey("space")) // Google Books off
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaPrepare || len(asked) != 0 || len(queries) != 0 || !strings.Contains(m.message, "at least one source") {
		t.Fatalf("a search with no source was started: stage=%v asked=%v message=%q", m.stage, asked, m.message)
	}
}

// Leaving the prepare page that the results opened goes back to them, with the
// choice they were made with.
func TestMetaPageSourcePanelCancelKeepsTheChoicesAndTheResults(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off, not applied
	for _, k := range []string{"esc", "ctrl+c", "q"} {
		next, cmd := metaStep(t, m, key(k))
		if next.stage != metaCandidates || next.cancelled || cmd != nil || len(asked) != 0 || len(next.res.Candidates) != 2 {
			t.Errorf("%s: stage=%v cancelled=%v asked=%v", k, next.stage, next.cancelled, asked)
		}
	}
	m, _ = metaStep(t, m, key("esc"))
	m, _ = metaStep(t, m, key("s"))
	if !strings.Contains(metaScreen(m), "[x] Open Library") {
		t.Errorf("a cancelled toggle was kept:\n%s", metaScreen(m))
	}
}

// With nothing to go back to, Esc on the prepare page closes the page.
func TestMetaPagePrepareEscLeavesWhenItIsTheFirstPage(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, cmd := metaStep(t, m, key("esc"))
	if !m.cancelled || cmd == nil || len(queries) != 0 {
		t.Errorf("cancelled=%v quit=%v searches=%d", m.cancelled, cmd != nil, len(queries))
	}
}

// Esc during the first search comes back to the prepare page, not out of it.
func TestMetaPageEscDuringTheFirstSearchReturnsToThePreparePage(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off, so there is a draft to keep
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaLooking {
		t.Fatalf("stage = %v", m.stage)
	}
	m, cmd := metaStep(t, m, key("esc"))
	if m.stage != metaPrepare || m.cancelled || cmd != nil {
		t.Fatalf("esc: stage=%v cancelled=%v", m.stage, m.cancelled)
	}
	if !strings.Contains(metaScreen(m), "[ ] Open Library") {
		t.Errorf("the toggles made before the search were lost:\n%s", metaScreen(m))
	}
}

// From the results, s opens the prepare page and Enter searches again. Esc
// during that search returns to the prepare page with the toggles as they were,
// and a second Esc returns to the results the search would have replaced.
func TestMetaPageEscDuringARepeatSearchReturnsToTheReopenedPreparePage(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaLooking {
		t.Fatalf("stage = %v", m.stage)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaPrepare || !strings.Contains(metaScreen(m), "[ ] Open Library") {
		t.Fatalf("esc did not return to the prepare page with its toggles: stage=%v\n%s", m.stage, metaScreen(m))
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || m.cancelled {
		t.Fatalf("a second esc: stage=%v cancelled=%v", m.stage, m.cancelled)
	}
	m, _ = metaStep(t, m, key("s"))
	if !strings.Contains(metaScreen(m), "[x] Open Library") {
		t.Errorf("the results forgot the sources they came from:\n%s", metaScreen(m))
	}
}

func TestMetaPageWithoutChoicesHasNoSources(t *testing.T) {
	var queries []metasource.Query
	m, err := newMetaPageModel(writeTestEPUB(t), recordingLookup(twoCandidates(), &queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	if screen := metaScreen(m); strings.Contains(screen, "Sources") || strings.Contains(screen, "[x]") {
		t.Errorf("the page offers sources it cannot rebuild:\n%s", screen)
	}
	m = searchFromPrepare(t, m)
	if strings.Contains(metaScreen(m), "sources") {
		t.Errorf("the results offer sources:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("s"))
	if m.stage != metaCandidates {
		t.Error("s left the results with no sources to choose")
	}
}

// r saves the sources that are on as the default, and only when asked.
func TestMetaPageRemembersTheSourcesOnlyWhenAsked(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	var saved [][]string
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m.saveSources = func(names []string) (string, error) { saved = append(saved, names); return "", nil }
	m, _ = metaStep(t, m, metaKey("up")) // WeRead
	m, _ = metaStep(t, m, metaKey("up")) // Douban
	m, _ = metaStep(t, m, metaKey("space"))
	if len(saved) != 0 {
		t.Fatalf("toggling saved: %v", saved)
	}
	m, _ = metaStep(t, m, metaKey("ctrl+d"))
	if want := [][]string{{"openlibrary", "google", "douban"}}; !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved = %v, want %v", saved, want)
	}
	if !strings.Contains(m.message, "Saved as the default") || !strings.Contains(m.message, "Douban") {
		t.Errorf("message = %q", m.message)
	}
	// Searching saves nothing more.
	m, _ = metaStep(t, m, key("enter"))
	if len(saved) != 1 {
		t.Errorf("searching saved again: %v", saved)
	}
}

func TestMetaPageRememberReportsAFailureAndRefusesNothing(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, metaKey("ctrl+d"))
	if !strings.Contains(m.message, "cannot be saved") {
		t.Errorf("no way to save: %q", m.message)
	}
	m.saveSources = func([]string) (string, error) { return "", errors.New("disk full") }
	m, _ = metaStep(t, m, metaKey("ctrl+d"))
	if !strings.Contains(m.message, "Could not save") || !strings.Contains(m.message, "disk full") {
		t.Errorf("a failed save: %q", m.message)
	}
	m.saveSources = func([]string) (string, error) { return "Process environment", nil }
	m, _ = metaStep(t, m, metaKey("ctrl+d"))
	if !strings.Contains(m.message, "Saved, but Process environment takes precedence") {
		t.Errorf("an overridden save: %q", m.message)
	}
	// With no source on there is nothing to save.
	m, _ = metaStep(t, m, metaKey("space"))
	m, _ = metaStep(t, m, metaKey("down"))
	m, _ = metaStep(t, m, metaKey("space"))
	m, _ = metaStep(t, m, metaKey("ctrl+d"))
	if !strings.Contains(m.message, "at least one source") {
		t.Errorf("saving nothing: %q", m.message)
	}
}

func TestMetaPagePrepareFitsCompactTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m.message = "Set ZLIB_WEREAD_API_KEY in .env first"
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

// What matters on this page is what will be searched, and the sources: they
// must be on screen on the terminals people use, and when the page is the
// shell's, which has one row fewer.
func TestMetaPagePrepareShowsTheTermsAndSourcesOnShortTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	for _, size := range [][2]int{{80, 24}, {80, 23}, {60, 16}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		screen := ansi.Strip(next.(metaPageModel).View().Content)
		for _, want := range []string{"Search for", "Before", "[x] Open Library", "[ ] WeRead"} {
			if !strings.Contains(screen, want) {
				t.Errorf("%dx%d: %q is not on screen:\n%s", size[0], size[1], want, screen)
			}
		}
		if got := footer(screen); !strings.Contains(got, "enter") || !strings.Contains(got, "esc") {
			t.Errorf("%dx%d: the footer %q lost enter or esc", size[0], size[1], got)
		}
	}
}

// The default choices come from the environment. They say whether the WeRead
// key is set, and never show it.
func TestDefaultMetaSourceChoicesReadTheEnvironmentWithoutShowingTheKey(t *testing.T) {
	const fakeKey = "wrk-fake-key-0000"
	t.Setenv(metaSourcesEnv, "")
	t.Setenv(wereadAPIKeyEnv, "")
	byName := func() map[string]metaSourceChoice {
		out := map[string]metaSourceChoice{}
		for _, c := range defaultMetaSourceChoices() {
			out[c.name] = c
		}
		return out
	}
	c := byName()
	if !c["openlibrary"].enabled || c["google"].enabled || c["douban"].enabled || c["weread"].enabled {
		t.Errorf("defaults = %+v", c)
	}
	if c["weread"].available || !strings.Contains(c["weread"].note, wereadAPIKeyEnv) {
		t.Errorf("weread with no key = %+v", c["weread"])
	}

	t.Setenv(wereadAPIKeyEnv, fakeKey)
	t.Setenv(metaSourcesEnv, "weread,douban")
	c = byName()
	if !c["weread"].enabled || !c["weread"].available || !c["douban"].enabled || c["openlibrary"].enabled {
		t.Errorf("configured = %+v", c)
	}
	for _, ch := range c {
		if strings.Contains(ch.note, fakeKey) || strings.Contains(ch.label, fakeKey) {
			t.Errorf("the key reached the panel: %+v", ch)
		}
	}
	if !strings.Contains(c["weread"].note, "key set") {
		t.Errorf("weread note = %q", c["weread"].note)
	}
}

func TestMetaLookupForBuildsTheNamedSources(t *testing.T) {
	var got []string
	old := newMetaSourcesFor
	newMetaSourcesFor = func(names []string) []metasource.Source {
		got = names
		return []metasource.Source{&fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}}}
	}
	t.Cleanup(func() { newMetaSourcesFor = old })
	res, err := newMetaLookupFor([]string{"douban", "weread"})(context.Background(), metasource.Query{Title: "Before"})
	if err != nil || len(res.Candidates) != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if !reflect.DeepEqual(got, []string{"douban", "weread"}) {
		t.Errorf("sources built for %v", got)
	}
}

// WeRead has no way to search by ISBN, so a search with no title says so
// instead of quietly returning nothing from it.
func TestMetaPageSaysWeReadNeedsATitle(t *testing.T) {
	choices := fakeSourceChoices()
	choices[3].available, choices[3].enabled, choices[3].note = true, true, "key set"
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, choices, &asked, &queries)
	if strings.Contains(metaScreen(m), "WeRead needs a title") {
		t.Errorf("the note shows although the search has a title:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = metaStep(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeIntoEditor(t, m, "9780441013593")
	m = submitQuery(t, m)
	if !strings.Contains(metaScreen(m), "WeRead needs a title") {
		t.Errorf("no note that WeRead cannot search by ISBN:\n%s", metaScreen(m))
	}
}
