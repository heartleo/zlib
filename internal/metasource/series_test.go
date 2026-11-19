package metasource

import (
	"context"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestGoodreadsSeriesFromTheTitleSuffix(t *testing.T) {
	for title, want := range map[string]struct {
		name  string
		index float64
	}{
		"Dune (Dune, #1)": {"Dune", 1},
		"Words of Radiance (The Stormlight Archive, #2)": {"The Stormlight Archive", 2},
		"Interlude (Some Series, #2.5)":                  {"Some Series", 2.5},
		"Dune":                                           {},
		"The Dune Box Set (Dune, #1-3)":                  {}, // a range is not one book's position
		"Title (Series)":                                 {},
		"Title (Series, #x)":                             {},
		"Title (Series, #99999)":                         {}, // not a position
		"A (Nested (Series), #1)":                        {},
		"Parens (In The Middle) After (The Series, #4)":  {"The Series", 4},
		"": {},
	} {
		name, index := goodreadsSeries(title)
		if name != want.name || index != want.index {
			t.Errorf("goodreadsSeries(%q) = %q, %v; want %q, %v", title, name, index, want.name, want.index)
		}
	}
}

func TestGoodreadsCandidateCarriesTheSeriesAndAPlainTitle(t *testing.T) {
	src, _ := goodreadsServer(t, nil, map[string]string{
		"Dune": "[" + grItem("1", "Dune", "Dune (Dune, #1)", "Frank Herbert", "x", false) + "]",
	})
	got, err := src.Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].Title != "Dune" || got[0].Series != "Dune" || got[0].SeriesIndex != 1 || got[0].SeriesLabel() != "Dune #1" {
		t.Fatalf("candidate = %+v", got[0])
	}
}

func seriesCandidate(series string, index float64) Candidate {
	c := cand("Goodreads", "Dune", []string{"Frank Herbert"}, "")
	c.Match, c.Score = MatchTitle, 0.99
	c.Series, c.SeriesIndex = series, index
	return c
}

func seriesChange(t *testing.T, plan []Change) Change {
	t.Helper()
	for _, ch := range plan {
		if ch.Field == FieldSeries {
			return ch
		}
	}
	t.Fatalf("no series row in %v", fields(plan))
	return Change{}
}

func TestPlanFillsAnEmptySeriesEvenForATitleMatch(t *testing.T) {
	// A series belongs to the work, so a title match may fill it, unlike an
	// edition field.
	cur := &ebookmeta.Metadata{Title: "Dune"}
	ch := seriesChange(t, Plan(cur, seriesCandidate("Dune", 1)))
	if !ch.Fill || !ch.DefaultSelected() || ch.Old != "" || ch.New != "Dune #1" || ch.Note != "" {
		t.Fatalf("change = %+v", ch)
	}
	got := BuildChanges([]Change{ch}, func(Change) bool { return true })
	if got == nil || got.Series == nil || *got.Series != "Dune" || got.SeriesIndex == nil || *got.SeriesIndex != 1 {
		t.Fatalf("changes = %+v", got)
	}
}

func TestPlanDoesNotReplaceTheFilesSeriesUnasked(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "Dune", Series: "Arrakis", SeriesIndex: 3}
	ch := seriesChange(t, Plan(cur, seriesCandidate("Dune", 1)))
	if ch.Fill || ch.DefaultSelected() || ch.Old != "Arrakis #3" {
		t.Fatalf("change = %+v", ch)
	}
}

func TestPlanLeavesSeriesAloneWhenItAgrees(t *testing.T) {
	for name, tc := range map[string]struct {
		cur  *ebookmeta.Metadata
		cand Candidate
	}{
		"the same series and position": {&ebookmeta.Metadata{Title: "Dune", Series: "dune", SeriesIndex: 1}, seriesCandidate("Dune", 1)},
		// The candidate adds nothing, and must not drop the file's position.
		"the same series without a position": {&ebookmeta.Metadata{Title: "Dune", Series: "Dune", SeriesIndex: 1}, seriesCandidate("Dune", 0)},
		"no series from the source":          {&ebookmeta.Metadata{Title: "Dune"}, seriesCandidate("", 0)},
	} {
		for _, f := range fields(Plan(tc.cur, tc.cand)) {
			if f == string(FieldSeries) {
				t.Errorf("%s: a series row was proposed", name)
			}
		}
	}
}

func TestPlanAddsAPositionToTheFilesSeriesWithoutOne(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "Dune", Series: "Dune"}
	ch := seriesChange(t, Plan(cur, seriesCandidate("Dune", 1)))
	if ch.Fill || ch.DefaultSelected() || ch.Old != "Dune" || ch.New != "Dune #1" {
		t.Fatalf("change = %+v", ch)
	}
}

func TestPlanOfAnUnconfirmedISBNMatchSelectsNoSeries(t *testing.T) {
	c := seriesCandidate("Dune", 1)
	c.Match, c.Score = MatchISBN, 0.4 // the ISBN agrees but the title does not
	ch := seriesChange(t, Plan(&ebookmeta.Metadata{Title: "Other"}, c))
	if ch.DefaultSelected() || ch.Note != NoteOtherBook {
		t.Fatalf("change = %+v", ch)
	}
}

func TestMergeTakesTheSeriesAndFillsAMissingPosition(t *testing.T) {
	a := seriesCandidate("Dune", 0)
	b := seriesCandidate("DUNE", 1)
	other := seriesCandidate("Arrakis", 7)
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}}

	got := rankAndMerge(q, []Candidate{a, b}, 0.45, 10)
	if len(got) != 1 || got[0].Series != "Dune" || got[0].SeriesIndex != 1 {
		t.Fatalf("merged = %+v", got)
	}
	// A different series from a later source does not replace the first.
	got = rankAndMerge(q, []Candidate{a, other}, 0.45, 10)
	if len(got) != 1 || got[0].Series != "Dune" || got[0].SeriesIndex != 0 {
		t.Fatalf("merged = %+v", got)
	}
}
