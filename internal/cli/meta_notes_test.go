package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

func resultWithNotes() metasource.Result {
	c := dune()
	c.Match, c.Score = metasource.MatchTitle, 0.8
	c.Sources = []string{"Open Library"}
	c.Notes = []string{"its ISBN differs from the file's: another edition of the book"}
	other := dune()
	other.Title, other.Match, other.Score, other.Sources = "Before (2nd ed.)", metasource.MatchTitle, 0.7, []string{"Google Books"}
	return metasource.Result{Candidates: []metasource.Candidate{c, other}}
}

func TestTheReasonsBehindARankAreShownOnNarrowAndWideResults(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(resultWithNotes(), nil, nil), nil)
	narrow := metaScreen(sized(t, m, 90, 30))
	if !strings.Contains(narrow, "Evidence") || !strings.Contains(narrow, "another edition of the book") {
		t.Errorf("the details do not give the reason:\n%s", narrow)
	}
	wide := sized(t, m, 120, 30)
	if !wide.compareFits() {
		t.Fatal("the comparison should fit at 120x30")
	}
	if s := metaScreen(wide); !strings.Contains(s, "Evidence: its ISBN differs") {
		t.Errorf("the comparison page does not give the reason:\n%s", s)
	}
}

func TestNotesTravelInTheJSONOfALookup(t *testing.T) {
	raw, err := json.Marshal(metaCandidateJSON{Candidate: resultWithNotes().Candidates[0]})
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &back); err != nil || len(back.Notes) != 1 || !strings.Contains(back.Notes[0], "another edition") {
		t.Fatalf("notes = %v, %v\n%s", back.Notes, err, raw)
	}
}

// A title, authors or ISBN given on the command line or through the MCP tool
// replaces what was read from the file, and keeps what the file says about its
// edition: the year, publisher and language that the evidence needs.
func TestOverridingTheQueryKeepsTheFilesEditionFacts(t *testing.T) {
	x := metasource.Extraction{Query: metasource.Query{Title: "Dune", Authors: []string{"Frank Herbert"}, Year: "2003", Publisher: "Ace", Language: "en-US"}}
	title, authors, isbn := "Dune Messiah", "Frank Herbert", "9780441172696"
	if err := overrideExtractionQuery(&x, &title, &authors, &isbn); err != nil {
		t.Fatal(err)
	}
	q := x.Query
	if q.Title != "Dune Messiah" || q.Year != "2003" || q.Publisher != "Ace" || q.Language != "en-US" {
		t.Fatalf("query = %+v", q)
	}
}

// The status line's first job is to say that a source failed. The reasons behind
// the highlighted row follow it and never push it off the line.
func TestASourceFailureStaysAheadOfTheEvidenceOnTheComparisonPage(t *testing.T) {
	res := resultWithNotes()
	res.Candidates[0].Notes = []string{"its ISBN differs from the file's: another edition of the book", "published 1999, 6 years from the file's 2005"}
	res.Errors = []metasource.SourceError{{Source: "Google Books", Err: metasource.ErrRateLimited}}
	m := sized(t, metaPageAfterLookup(t, answerWith(res, nil, nil), nil), 100, 30)
	if !m.compareFits() {
		t.Fatal("the comparison should fit at 100x30")
	}
	status := m.sourceNote()
	i, j := strings.Index(status, "rate limited"), strings.Index(status, "Evidence:")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the failure does not come first: %q", status)
	}
	if strings.Contains(status, "6 years") {
		t.Errorf("more than the first reason is on the line: %q", status)
	}
	if screen := metaScreen(m); !strings.Contains(screen, "rate limited") {
		t.Errorf("the failure is not on the screen:\n%s", screen)
	}
}
