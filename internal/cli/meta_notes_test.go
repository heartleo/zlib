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
