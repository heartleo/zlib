package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// The one-line summary above the panels is never cut in the middle of a word:
// it names the fields it adds and counts the rest, and shortens itself to fit.
func TestResultsSummaryLineFitsWithoutCuttingWords(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(perSourceResult(), nil, nil), nil)
	c := m.res.Candidates[0]
	for w := 50; w <= 200; w++ {
		line := m.candidateEffectLine(c, w)
		if ansi.StringWidth(line) > w || strings.Contains(line, "…") {
			t.Errorf("width %d: %q is cut or too wide", w, line)
		}
	}
	long := m.candidateEffectLine(c, 200)
	if !strings.Contains(long, "to check (≠, not selected)") {
		t.Errorf("the full form lacks the meaning of ≠: %q", long)
	}
	// What the summary adds is what the plan selects, field for field.
	for _, f := range m.effectOf(c).adds {
		if !strings.Contains(long, f) {
			t.Errorf("the summary does not name %q: %q", f, long)
		}
	}
	// On a page wide enough for the panels it is the line under the list.
	screen := metaScreen(sized(t, m, 120, 34))
	if row := rowWith(t, screen, "to check"); strings.Contains(row, "…") {
		t.Errorf("the summary is cut at 120 columns: %q", row)
	}
}

func TestResultsSummaryLineSaysWhenNothingChanges(t *testing.T) {
	m := metaPageModel{current: &ebookmeta.Metadata{Title: "Dune"}, format: ebookmeta.FormatEPUB}
	if got := m.candidateEffectLine(metasource.Candidate{Title: "Dune"}, 100); !strings.Contains(got, "Changes nothing") {
		t.Errorf("line = %q", got)
	}
	// A format that cannot store what the match has says so, not "nothing".
	pdf := metaPageModel{current: &ebookmeta.Metadata{Title: "Dune", Authors: []string{"Frank Herbert"}}, format: ebookmeta.FormatPDF}
	c := metasource.Candidate{Title: "Dune", Authors: []string{"Frank Herbert"}, Publisher: "Ace", Published: "2005", Match: metasource.MatchTitle, Score: 0.9}
	if got := pdf.candidateEffectLine(c, 100); !strings.Contains(got, "Changes nothing this file can store") {
		t.Errorf("line = %q", got)
	}
}
