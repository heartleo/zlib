package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// mergedSources is a lookup answer with a merge of three catalogues, one of two
// and one of a single catalogue: the widest label decides the column.
func mergedSources() metasource.Result {
	three := dune()
	three.Title = "Dune"
	three.Sources = []string{"Douban", "Goodreads", "Open Library"}
	three.Match, three.Score = metasource.MatchTitle, 0.99
	three.Identifiers = append(three.Identifiers,
		ebookmeta.Identifier{Scheme: "goodreads", Value: "44767458"},
		ebookmeta.Identifier{Scheme: "openlibrary", Value: "OL1M"})
	two := metasource.Candidate{Title: "Dune Messiah", Authors: []string{"Frank Herbert"}, Sources: []string{"Open Library", "Google Books"},
		Match: metasource.MatchTitle, Score: 0.8}
	one := metasource.Candidate{Title: "The Road to Dune", Authors: []string{"Frank Herbert"}, Sources: []string{"Goodreads"},
		Match: metasource.MatchTitle, Score: 0.71}
	return metasource.Result{Candidates: []metasource.Candidate{three, two, one}}
}

// sourceCell is the bracketed sources of a result row.
func sourceCell(t *testing.T, row string) string {
	t.Helper()
	i := strings.Index(row, "[")
	if i < 0 {
		t.Fatalf("no sources on the row %q", row)
	}
	j := strings.Index(row[i:], "]")
	if j < 0 {
		t.Fatalf("the sources are cut short on the row %q", row)
	}
	return row[i : i+j+1]
}

// bracketCell is the display column of the sources on a row: a cut title ends
// in a three-byte ellipsis, so byte offsets would not line up.
func bracketCell(row string) int { return ansi.StringWidth(row[:strings.Index(row, "[")]) }

// Each result row names where it came from, whole, at every width. The names
// are full while they fit, then every row uses the short ones: a label is
// never cut, and rows never mix the two forms.
func TestMetaResultsNameEverySourceWholeAtEveryWidth(t *testing.T) {
	for _, tc := range []struct {
		width      int
		three, one string
	}{
		{120, "[Douban+Goodreads+Open Library]", "[Goodreads]"},
		{100, "[Douban+Goodreads+Open Library]", "[Goodreads]"},
		{80, "[DB+GR+OL]", "[GR]"},
		{60, "[DB+GR+OL]", "[GR]"},
	} {
		m := metaPageAfterLookup(t, answerWith(mergedSources(), nil, nil), nil)
		m = sized(t, m, tc.width, 30)
		screen := metaScreen(m)
		first := rowWith(t, screen, "99%  Dune ")
		second := rowWith(t, screen, "Dune Messiah")
		third := rowWith(t, screen, "The Road") // the title itself may be cut at 60
		if got := sourceCell(t, first); got != tc.three {
			t.Errorf("width %d: first row sources %q, want %q\n%s", tc.width, got, tc.three, screen)
		}
		if got := sourceCell(t, third); got != tc.one {
			t.Errorf("width %d: third row sources %q, want %q", tc.width, got, tc.one)
		}
		for _, row := range []string{first, second, third} {
			if cell := sourceCell(t, row); strings.Contains(cell, "…") {
				t.Errorf("width %d: a source was cut: %q", tc.width, cell)
			}
		}
		if bracketCell(first) != bracketCell(second) || bracketCell(first) != bracketCell(third) {
			t.Errorf("width %d: the source column is not aligned:\n%s\n%s\n%s", tc.width, first, second, third)
		}
	}
}

// The sources take their room first, but the titles stay readable: at 80
// columns the full title of a result still fits beside its authors.
func TestMetaResultsKeepTitlesReadableBesideTheSources(t *testing.T) {
	m := sized(t, metaPageAfterLookup(t, answerWith(mergedSources(), nil, nil), nil), 80, 30)
	row := rowWith(t, metaScreen(m), "The Road to Dune")
	if !strings.Contains(row, "The Road to Dune") || !strings.Contains(row, "Frank Herbert") {
		t.Errorf("the title or the authors were cut: %q", row)
	}
}

// Short names are only a fallback for the list; the selected result's full
// names are always on the page, here in the details.
func TestMetaResultsDetailsKeepTheFullSourceNames(t *testing.T) {
	m := sized(t, metaPageAfterLookup(t, answerWith(mergedSources(), nil, nil), nil), 80, 30)
	if screen := metaScreen(m); !strings.Contains(screen, "Douban, Goodreads, Open Library") {
		t.Errorf("the details do not name the sources:\n%s", screen)
	}
}

// A long label such as "ID openlibrary" does not run into its value or lose
// its end: the label column is as wide as the longest label shown.
func TestMetaPanelsKeepLongIdentifierLabelsApartFromTheirValues(t *testing.T) {
	var queries []metasource.Query
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(mergedSources(), &queries), nil)
	if err != nil {
		t.Fatal(err)
	}
	m = sized(t, m, 120, 40)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields {
		t.Fatalf("stage %v", m.stage)
	}
	for _, screen := range []string{metaScreen(m), metaScreen(sized(t, m, 90, 40))} {
		for _, want := range []string{"ID goodreads", "ID openlibrary"} {
			row := rowWith(t, screen, want)
			after := row[strings.Index(row, want)+len(want):]
			if !strings.HasPrefix(after, " ") {
				t.Errorf("%q runs into its value: %q", want, strings.TrimSpace(ansi.Strip(row)))
			}
		}
	}
}
