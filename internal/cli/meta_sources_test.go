package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

func fakeSourceChoices() []metaSourceChoice {
	return []metaSourceChoice{
		{name: "openlibrary", label: "Open Library", enabled: true, available: true},
		{name: "google", label: "Google Books", enabled: true, available: true, note: "shared quota"},
		{name: "douban", label: "Douban", available: true, note: "sends the title to Douban"},
		{name: "weread", label: "WeRead", note: "needs ZLIB_WEREAD_API_KEY (not set)"},
	}
}

// sourcesPage is a page on its candidates, with a source panel whose factory
// records the sources it was asked to search and answers offline.
func sourcesPage(t *testing.T, choices []metaSourceChoice, asked *[][]string, queries *[]metasource.Query) metaPageModel {
	t.Helper()
	factory := func(names []string) metaLookupFunc {
		*asked = append(*asked, names)
		return recordingLookup(twoCandidates(), queries)
	}
	return metaPageAfterLookup(t, recordingLookup(twoCandidates(), queries), nil, withMetaSources(choices, factory))
}

func TestMetaPageSourcePanelListsEverySourceAndWhyOneIsOff(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	if !strings.Contains(metaScreen(m), "s sources") {
		t.Errorf("the candidates page does not offer the source panel:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("s"))
	screen := metaScreen(m)
	for _, want := range []string{
		"[x] Open Library", "[x] Google Books", "[ ] Douban", "[ ] WeRead",
		"shared quota", "needs ZLIB_WEREAD_API_KEY (not set)", "Tencent", "this search",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the panel lacks %q:\n%s", want, screen)
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
	if m.stage != metaCandidates || len(queries) != 2 || m.panel {
		t.Errorf("stage=%v lookups=%d panel=%v", m.stage, len(queries), m.panel)
	}
	// The query is the same: only the sources changed.
	if !reflect.DeepEqual(queries[0], queries[1]) {
		t.Errorf("the search changed with the sources: %+v then %+v", queries[0], queries[1])
	}
}

func TestMetaPageSourcePanelRefusesASourceThatCannotRun(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
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
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off
	m, _ = metaStep(t, m, metaKey("down"))
	m, _ = metaStep(t, m, metaKey("space")) // Google Books off
	m, _ = metaStep(t, m, key("enter"))
	if !m.panel || len(asked) != 0 || !strings.Contains(m.message, "at least one source") {
		t.Fatalf("a search with no source was started: panel=%v asked=%v message=%q", m.panel, asked, m.message)
	}
}

func TestMetaPageSourcePanelCancelKeepsTheChoicesAndTheResults(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, metaKey("space")) // Open Library off, not applied
	for _, k := range []string{"esc", "ctrl+c", "q"} {
		next, cmd := metaStep(t, m, key(k))
		if next.panel || next.stage != metaCandidates || next.cancelled || cmd != nil || len(asked) != 0 {
			t.Errorf("%s: panel=%v stage=%v cancelled=%v asked=%v", k, next.panel, next.stage, next.cancelled, asked)
		}
	}
	m, _ = metaStep(t, m, key("esc"))
	m, _ = metaStep(t, m, key("s"))
	if !strings.Contains(metaScreen(m), "[x] Open Library") {
		t.Errorf("a cancelled toggle was kept:\n%s", metaScreen(m))
	}
}

func TestMetaPageWithoutChoicesHasNoSourcePanel(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	if strings.Contains(metaScreen(m), "sources") {
		t.Errorf("the page offers sources it cannot rebuild:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("s"))
	if m.panel {
		t.Error("s opened a panel with no sources")
	}
}

func TestMetaPageSourcePanelFitsCompactTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
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
	if !c["openlibrary"].enabled || !c["google"].enabled || c["douban"].enabled || c["weread"].enabled {
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
