package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

func reviewResult() metasource.Result {
	wereadOnly := metasource.Candidate{
		Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Published: "2008-01",
		EditionLevel: true, Sources: []string{"WeRead"}, Match: metasource.MatchTitle, Score: 0.9,
		Identifiers: []ebookmeta.Identifier{{Scheme: "weread", Value: "695233"}},
	}
	both := metasource.Candidate{
		Title: "Dune", Authors: []string{"Frank Herbert"}, EditionLevel: true,
		Sources: []string{"Google Books", "Open Library"}, Match: metasource.MatchISBN, Score: 1,
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9780441013593"}},
	}
	work := metasource.Candidate{Title: "Dune (work)", Sources: []string{"Open Library"}, Match: metasource.MatchTitle, Score: 0.6}
	return metasource.Result{Candidates: []metasource.Candidate{wereadOnly, both, work}}
}

func reviewPage(t *testing.T) metaPageModel {
	t.Helper()
	return metaPageAfterLookup(t, answerWith(reviewResult(), nil, nil), nil)
}

// A row says where its match came from, so a WeRead entry is not mistaken for
// a merged result of several catalogues.
func TestMetaPageRowsNameTheirSources(t *testing.T) {
	m := reviewPage(t)
	screen := metaScreen(m)
	for _, want := range []string{"[WeRead]", "[Google Books+Open Library]", "[Open Library]"} {
		if !strings.Contains(screen, want) {
			t.Errorf("no %s on the rows:\n%s", want, screen)
		}
	}
}

// A match with no ISBN says so in its details, and one with an ISBN shows it
// on its own row, so the user can see how sure the edition is.
func TestMetaPageDetailsSayWhetherAnISBNIsKnown(t *testing.T) {
	m := reviewPage(t)
	if screen := metaScreen(m); !strings.Contains(screen, "no ISBN listed") {
		t.Errorf("a WeRead entry without an ISBN is not marked:\n%s", screen)
	}
	m, _ = metaStep(t, m, metaKey("down"))
	screen := metaScreen(m)
	if strings.Contains(screen, "no ISBN listed") {
		t.Errorf("an entry with an ISBN is marked as having none:\n%s", screen)
	}
	if !strings.Contains(screen, "ISBN") || !strings.Contains(screen, "9780441013593") {
		t.Errorf("the ISBN is not shown:\n%s", screen)
	}
	// A work-level result describes no edition, so no ISBN is promised.
	m, _ = metaStep(t, m, metaKey("down"))
	if screen := metaScreen(m); strings.Contains(screen, "no ISBN listed") {
		t.Errorf("a work-level result is marked as an edition without an ISBN:\n%s", screen)
	}
}

func TestMetaPageReviewFitsCompactTerminals(t *testing.T) {
	m := reviewPage(t)
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
