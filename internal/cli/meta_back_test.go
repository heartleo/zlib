package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// Esc on the results goes back a step, to the prepare page where the terms and
// the sources are set, instead of leaving the page. Enter there searches again
// and Esc leaves, as it does the first time the page opens.
func TestMetaResultsEscGoesBackToThePreparePage(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	if m.stage != metaCandidates {
		t.Fatalf("stage = %v", m.stage)
	}
	m, cmd := metaStep(t, m, key("esc"))
	if m.stage != metaPrepare || m.cancelled || cmd != nil {
		t.Fatalf("esc: stage=%v cancelled=%v quit=%v", m.stage, m.cancelled, cmd != nil)
	}
	// The page the search started from: its terms and the sources as searched.
	screen := metaScreen(m)
	for _, want := range []string{"Step 1 of 3: Search", "Search for", "[x] Open Library", "[x] Google Books", "[ ] Douban"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the prepare page lacks %q:\n%s", want, screen)
		}
	}
	if len(queries) != 1 {
		t.Errorf("going back searched: %d lookups", len(queries))
	}
	// There is nothing to return to from here: Esc leaves.
	left, cmd := metaStep(t, m, key("esc"))
	if !left.cancelled || cmd == nil {
		t.Errorf("a second esc did not leave: cancelled=%v quit=%v", left.cancelled, cmd != nil)
	}
	// Enter searches again, with the same terms.
	m, cmd = metaStep(t, m, key("enter"))
	if m.stage != metaLooking || cmd == nil {
		t.Fatalf("enter did not search again: stage=%v", m.stage)
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaLookupDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	if m.stage != metaCandidates || len(queries) != 2 || !reflect.DeepEqual(queries[0], queries[1]) {
		t.Errorf("stage=%v lookups=%d", m.stage, len(queries))
	}
}

// Going back keeps the search as the user edited it, and the sources they had
// chosen, so the page is the one the next search starts from.
func TestMetaResultsEscKeepsAnEditedSearchAndTheChosenSources(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Another title")
	m = submitQuery(t, m)
	if m.stage != metaCandidates || m.query.Title != "Another title" {
		t.Fatalf("stage=%v title=%q", m.stage, m.query.Title)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaPrepare || !strings.Contains(metaScreen(m), "Another title") {
		t.Errorf("the edited search is not on the prepare page (stage %v):\n%s", m.stage, metaScreen(m))
	}
}

// The page that says nothing was found goes back the same way, so the sources
// can be changed from it; q and Ctrl+C still leave.
func TestMetaNoMatchEscGoesBackAndQLeaves(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	failed := m
	failed.stage, failed.res = metaFailed, metasource.Result{}
	back, cmd := metaStep(t, failed, key("esc"))
	if back.stage != metaPrepare || back.cancelled || cmd != nil {
		t.Errorf("esc on no match: stage=%v cancelled=%v", back.stage, back.cancelled)
	}
	for _, page := range []metaPageModel{m, failed} {
		for _, k := range []string{"q", "ctrl+c"} {
			left, cmd := metaStep(t, page, key(k))
			if !left.cancelled || cmd == nil {
				t.Errorf("%s on stage %v did not leave", k, page.stage)
			}
		}
	}
}

// The s key is unchanged: it looks at the sources and Esc returns to the same
// results, which Esc from the results themselves does not keep.
func TestMetaSourcesKeyStillReturnsToTheSameResults(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || len(m.res.Candidates) != 2 {
		t.Errorf("s then esc: stage=%v results=%d", m.stage, len(m.res.Candidates))
	}
}

// A page built without sources has no prepare page to go back to: Esc leaves.
func TestMetaResultsEscLeavesWhenThereAreNoSourcesToChoose(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	if len(m.sources) != 0 {
		t.Skip("this page has sources")
	}
	left, cmd := metaStep(t, m, key("esc"))
	if !left.cancelled || cmd == nil {
		t.Errorf("esc did not leave: cancelled=%v quit=%v", left.cancelled, cmd != nil)
	}
}
