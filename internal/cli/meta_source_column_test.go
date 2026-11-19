package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// perSourceResult is a lookup answer as the lookup gives it: the same book from
// three catalogues as three rows, each one catalogue's own answer, and other
// books after them.
func perSourceResult() metasource.Result {
	row := func(source, title string, score float64, ids ...ebookmeta.Identifier) metasource.Candidate {
		return metasource.Candidate{Title: title, Authors: []string{"Frank Herbert"}, Sources: []string{source}, Match: metasource.MatchTitle,
			Score: score, Identifiers: ids}
	}
	return metasource.Result{Candidates: []metasource.Candidate{
		row("Douban", "Dune", 0.99, ebookmeta.Identifier{Scheme: "douban", Value: "1291997"}),
		row("Goodreads", "Dune", 0.99, ebookmeta.Identifier{Scheme: "goodreads", Value: "44767458"}),
		row("Open Library", "Dune", 0.99),
		row("Google Books", "Dune Messiah", 0.8),
		row("Goodreads", "The Road to Dune", 0.71),
	}}
}

// sourceCell is the bracketed source of a result row.
func sourceCell(t *testing.T, row string) string {
	t.Helper()
	i := strings.Index(row, "[")
	if i < 0 {
		t.Fatalf("no source on the row %q", row)
	}
	j := strings.Index(row[i:], "]")
	if j < 0 {
		t.Fatalf("the source is cut short on the row %q", row)
	}
	return row[i : i+j+1]
}

// bracketCell is the display column of the source on a row: a cut title ends in
// a three-byte ellipsis, so byte offsets would not line up.
func bracketCell(row string) int { return ansi.StringWidth(row[:strings.Index(row, "[")]) }

// Each result row names its catalogue, whole, at every width, and the names line
// up in one column. Rows of different catalogues stay separate rows.
func TestMetaResultsNameEachRowsSourceWholeAtEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120} {
		m := sized(t, metaPageAfterLookup(t, answerWith(perSourceResult(), nil, nil), nil), width, 30)
		screen := metaScreen(m)
		var cells []string
		col := -1
		for _, want := range []string{"[Douban]", "[Goodreads]", "[Open Library]", "[Google Books]"} {
			if !strings.Contains(screen, want) {
				t.Errorf("width %d: no %s on the rows:\n%s", width, want, screen)
			}
		}
		for _, row := range strings.Split(screen, "\n") {
			if !strings.Contains(row, "[") || !strings.Contains(row, "%") {
				continue
			}
			cell := sourceCell(t, row)
			cells = append(cells, cell)
			if strings.Contains(cell, "+") || strings.Contains(cell, "…") {
				t.Errorf("width %d: a row names more than one catalogue or is cut: %q", width, cell)
			}
			if col < 0 {
				col = bracketCell(row)
			} else if got := bracketCell(row); got != col {
				t.Errorf("width %d: the source column is not aligned (%d and %d):\n%s", width, col, got, screen)
			}
		}
		if len(cells) != 5 {
			t.Errorf("width %d: %d result rows, want 5 (one per source answer): %v", width, len(cells), cells)
		}
	}
}

// The source takes its room first, but the titles stay readable: at 80 columns
// the full title of a result still fits beside its authors.
func TestMetaResultsKeepTitlesReadableBesideTheSource(t *testing.T) {
	m := sized(t, metaPageAfterLookup(t, answerWith(perSourceResult(), nil, nil), nil), 80, 30)
	row := rowWith(t, metaScreen(m), "The Road to Dune")
	if !strings.Contains(row, "The Road to Dune") || !strings.Contains(row, "Frank Herbert") {
		t.Errorf("the title or the authors were cut: %q", row)
	}
}

// The details of the selected row name its catalogue in full.
func TestMetaResultsDetailsNameTheSource(t *testing.T) {
	m := sized(t, metaPageAfterLookup(t, answerWith(perSourceResult(), nil, nil), nil), 80, 30)
	if screen := metaScreen(m); !strings.Contains(screen, "Source") || !strings.Contains(screen, "Douban") {
		t.Errorf("the details do not name the source:\n%s", screen)
	}
}

// A long label such as "ID openlibrary" does not run into its value or lose
// its end: the label column is as wide as the longest label shown.
func TestMetaPanelsKeepLongIdentifierLabelsApartFromTheirValues(t *testing.T) {
	long := metasource.Candidate{Title: "Dune", Authors: []string{"Frank Herbert"}, Sources: []string{"Open Library"},
		Match: metasource.MatchTitle, Score: 0.99,
		Identifiers: []ebookmeta.Identifier{{Scheme: "goodreads", Value: "44767458"}, {Scheme: "openlibrary", Value: "OL1M"}}}
	var queries []metasource.Query
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(metasource.Result{Candidates: []metasource.Candidate{long}}, &queries), nil)
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
