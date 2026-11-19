package metasource

import (
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// The comparison panels show MatchValue next to the file's value, so every
// field a match can propose must have one: a field the panel cannot read from
// the match shows as empty, as if the source had said nothing about it.
func TestMatchValueReadsEveryFieldAPlanProposes(t *testing.T) {
	c := fullCandidate()
	c.Series, c.SeriesIndex = "Dune Chronicles", 1
	c.CoverURL = "https://i.gr-assets.com/images/x/1.jpg"
	plan := Plan(&ebookmeta.Metadata{}, c)
	if len(plan) == 0 {
		t.Fatal("the plan is empty")
	}
	for _, ch := range plan {
		if MatchValue(c, ch.Field) == "" {
			t.Errorf("MatchValue(%q) is empty, but the plan proposes %q", ch.Field, ch.New)
		}
	}
	if got := MatchValue(c, FieldSeries); got != "Dune Chronicles #1" {
		t.Errorf("series = %q", got)
	}
}
