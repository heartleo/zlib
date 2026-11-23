package metasource

import (
	"slices"
	"testing"
)

func TestRankOrderPutsAnISBNMatchFirstThenScoreAndKeepsTiesInGivenOrder(t *testing.T) {
	rows := []Candidate{
		{Title: "low", Match: MatchTitle, Score: 0.5},
		{Title: "tie-a", Match: MatchTitle, Score: 0.8},
		{Title: "isbn", Match: MatchISBN, Score: 1},
		{Title: "tie-b", Match: MatchTitle, Score: 0.8},
	}
	var got []string
	for _, i := range RankOrder(Query{Title: "x"}, rows) {
		got = append(got, rows[i].Title)
	}
	if want := []string{"isbn", "tie-a", "tie-b", "low"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRankOrderPrefersTheEditionThatAgreesWithTheFile(t *testing.T) {
	rows := []Candidate{
		{Title: "other year", Match: MatchTitle, Score: 0.8, Published: "1999"},
		{Title: "same year", Match: MatchTitle, Score: 0.8, Published: "2005-08-02"},
	}
	got := RankOrder(Query{Title: "x", Year: "2005"}, rows)
	if rows[got[0]].Title != "same year" {
		t.Fatalf("order = %v", got)
	}
}
