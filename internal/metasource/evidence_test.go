package metasource

import (
	"slices"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// row builds an edition-level candidate; the Douban id keeps rows with one
// title apart, as they would be in a real answer.
func row(id, title, author, published string, languages ...string) Candidate {
	c := Candidate{Title: title, Sources: []string{"A"}, Published: published, Languages: languages, EditionLevel: true,
		Identifiers: []ebookmeta.Identifier{{Scheme: "douban", Value: id}}}
	if author != "" {
		c.Authors = []string{author}
	}
	return c
}

// ranked is the rows in the order a lookup lists them, as "Title|Author".
func ranked(q Query, in ...Candidate) ([]string, []Candidate) {
	out := rankAndMerge(q, in, defaultMinScore, 20)
	names := make([]string, len(out))
	for i, c := range out {
		a := ""
		if len(c.Authors) > 0 {
			a = c.Authors[0]
		}
		names[i] = c.Title + "|" + a
	}
	return names, out
}

func before(t *testing.T, names []string, first, second string) {
	t.Helper()
	i, j := slices.Index(names, first), slices.Index(names, second)
	if i < 0 || j < 0 {
		t.Fatalf("%q or %q missing from %v", first, second, names)
	}
	if i > j {
		t.Errorf("%q should rank before %q: %v", first, second, names)
	}
}

// notBefore is before for a row that may have been left out of the list as too
// weak a match: absent is fine, present must not rank above.
func notBefore(t *testing.T, names []string, first, second string) {
	t.Helper()
	if slices.Index(names, second) < 0 {
		return
	}
	before(t, names, first, second)
}

func notesOf(rows []Candidate, title, author string) string {
	for _, c := range rows {
		if c.Title == title && len(c.Authors) > 0 && c.Authors[0] == author {
			return strings.Join(c.Notes, "; ")
		}
	}
	return ""
}

// The case that started it: of everything a search for "Dune" returns, the book
// ranks first, and what only looks like it ranks lower with its reason.
func TestTheDuneFamilyIsToldApart(t *testing.T) {
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}, Year: "1965"}
	messiah := row("1", "Dune Messiah", "Frank Herbert", "1969")
	brian := row("2", "Dune", "Brian Herbert", "1965")
	guide := row("3", "Dune Study Guide", "GradeSaver", "2010")
	book := row("4", "Dune", "Frank Herbert", "1965")
	volume2 := row("5", "Dune 2", "Frank Herbert", "1969")

	names, rows := ranked(q, messiah, brian, guide, book, volume2)
	if names[0] != "Dune|Frank Herbert" {
		t.Fatalf("the book is not first: %v", names)
	}
	// Some of the look-alikes are too far from "Dune" to be listed at all; the
	// ones that are listed rank below the book.
	for _, other := range []string{"Dune Messiah|Frank Herbert", "Dune|Brian Herbert", "Dune Study Guide|GradeSaver", "Dune 2|Frank Herbert"} {
		notBefore(t, names, "Dune|Frank Herbert", other)
	}
	before(t, names, "Dune|Frank Herbert", "Dune|Brian Herbert")
	before(t, names, "Dune|Frank Herbert", "Dune 2|Frank Herbert")

	// The reasons are on the rows, whether or not a lookup lists them.
	for _, want := range []struct {
		c    Candidate
		note string
	}{
		{messiah, "sequel or companion"},
		{brian, "given name"},
		{guide, "summary or study guide"},
		{volume2, "sequel or companion"},
	} {
		if got := strings.Join(candidateNotes(q, want.c), "; "); !strings.Contains(got, want.note) {
			t.Errorf("%s by %s: notes %q lack %q", want.c.Title, want.c.Authors[0], got, want.note)
		}
	}
	_ = rows
}

func TestVolumesOfOneSeriesAreToldApart(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		in    []Candidate
		first string
		after string
	}{
		{"the second volume, written with a numeral glyph", "三体Ⅱ",
			[]Candidate{row("1", "三体", "刘慈欣", "2008"), row("2", "三体Ⅲ", "刘慈欣", "2010"), row("3", "三体Ⅱ", "刘慈欣", "2008")},
			"三体Ⅱ|刘慈欣", "三体Ⅲ|刘慈欣"},
		{"the first volume of a numbered series", "Dune 01",
			[]Candidate{row("1", "Dune 02", "Frank Herbert", "1969"), row("2", "Dune 01", "Frank Herbert", "1965")},
			"Dune 01|Frank Herbert", "Dune 02|Frank Herbert"},
		{"a Roman numeral", "Rocky II",
			[]Candidate{row("1", "Rocky III", "A. Writer", "1982"), row("2", "Rocky II", "A. Writer", "1979")},
			"Rocky II|A. Writer", "Rocky III|A. Writer"},
		{"a volume label in Chinese", "三体第2部",
			[]Candidate{row("1", "三体第3部", "刘慈欣", "2010"), row("2", "三体第2部", "刘慈欣", "2008")},
			"三体第2部|刘慈欣", "三体第3部|刘慈欣"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, _ := ranked(Query{Title: tc.query, Authors: []string{tc.in[0].Authors[0]}}, tc.in...)
			if names[0] != tc.first {
				t.Fatalf("first = %q, want %q: %v", names[0], tc.first, names)
			}
			notBefore(t, names, tc.first, tc.after)
		})
	}
}

func TestANumberThatIsTheTitleIsNotAVolume(t *testing.T) {
	for _, pair := range [][2]string{
		{"1984", "1985"}, {"Fahrenheit 451", "Fahrenheit 451"}, {"2001: A Space Odyssey", "2010: Odyssey Two"},
		{"Catch-22", "Catch-22"}, {"Book 1", "Book 2"}, {"Dune", "Dune Messiah"},
	} {
		if volumeConflict(pair[0], pair[1]) {
			t.Errorf("%q and %q were taken for volumes of one title", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"Dune 1", "Dune 2"}, {"三体Ⅰ", "三体Ⅱ"}, {"Windows 10", "Windows 11"}, {"Dune (Vol. 1)", "Dune (Vol. 3)"}} {
		if !volumeConflict(pair[0], pair[1]) {
			t.Errorf("%q and %q should be different volumes", pair[0], pair[1])
		}
	}
}

func TestASequelNeedsNoSpacesToBeASequel(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		ext       int
	}{
		{"Dune", "Dune Messiah", 1},
		{"Dune Messiah", "Dune", -1},
		{"Dune", "Dune: Messiah", 0}, // a subtitle: the main parts agree
		{"Dune", "Dune (Special Edition)", 0},
		{"Dune", "Dune Special Edition", 0}, // edition words do not make a sequel
		{"Dune", "Dunedin", 0},              // not a word of it
		{"三体", "三体Ⅱ", 1},
		{"三体", "三体黑暗森林", 1},
		{"三体", "三体全集", 0},
		{"三体", "三体：黑暗森林", 0},
		{"The Hobbit", "Hobbit", 0},
	} {
		if got := titleExtension(tc.want, tc.got); got != tc.ext {
			t.Errorf("titleExtension(%q, %q) = %d, want %d", tc.want, tc.got, got, tc.ext)
		}
	}
}

func TestAStudyGuideIsDemotedUnlessItIsWhatWasAsked(t *testing.T) {
	book := row("1", "The Great Gatsby", "F. Scott Fitzgerald", "1925")
	guide := row("2", "The Great Gatsby Study Guide", "SparkNotes", "2003")
	names, _ := ranked(Query{Title: "The Great Gatsby"}, guide, book)
	notBefore(t, names, "The Great Gatsby|F. Scott Fitzgerald", "The Great Gatsby Study Guide|SparkNotes")
	if names[0] != "The Great Gatsby|F. Scott Fitzgerald" {
		t.Errorf("the book is not first: %v", names)
	}
	if !derivativeTitle("Summary of Dune") || !derivativeTitle("Dune: A Workbook") || derivativeTitle("Dune") {
		t.Error("derivativeTitle misjudges a title")
	}

	names, _ = ranked(Query{Title: "The Great Gatsby Study Guide"}, book, guide)
	if names[0] != "The Great Gatsby Study Guide|SparkNotes" {
		t.Errorf("a study guide that was asked for is demoted: %v", names)
	}
}

func TestLanguageOnlyRanksAndUnknownIsNeutral(t *testing.T) {
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}, Language: "zh"}
	zh := row("1", "Dune", "Frank Herbert", "", "zh")
	en := row("2", "Dune", "Frank Herbert", "", "en")
	unknown := row("3", "Dune", "Frank Herbert", "")
	// Three editions of one title: the language the file declares, then one with
	// no language recorded, then the other language.
	scores := map[string]float64{}
	_, rows := ranked(q, en, unknown, zh)
	for _, c := range rows {
		lang := "unknown"
		if len(c.Languages) > 0 {
			lang = c.Languages[0]
		}
		scores[lang] = c.Score
	}
	if scores["zh"] < scores["en"] || scores["unknown"] < scores["en"] {
		t.Errorf("a result in another language is not ranked below: %v", scores)
	}
	if scores["unknown"] != scores["zh"] {
		t.Errorf("an unknown language was penalised: %v", scores)
	}
	for _, c := range rows {
		if len(c.Languages) > 0 && c.Languages[0] == "en" && !strings.Contains(strings.Join(c.Notes, ";"), "language") {
			t.Errorf("the row in another language says nothing: %v", c.Notes)
		}
	}
	// An ISBN match is not touched by the language or the year.
	withISBN := Query{Title: "Dune", Language: "zh", Year: "1990", ISBNs: []string{"9780441013593"}}
	hit := row("4", "Dune", "", "2005", "en")
	hit.Identifiers = append(hit.Identifiers, ebookmeta.Identifier{Scheme: "isbn", Value: "9780441013593"})
	if _, r := ranked(withISBN, hit); len(r) != 1 || r[0].Match != MatchISBN || r[0].Score != 1 {
		t.Errorf("soft evidence changed an ISBN match: %+v", r)
	}
}

func TestYearToleranceMissingYearAndWorkLevelRows(t *testing.T) {
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}, Year: "2005"}
	exact := row("1", "Dune", "Frank Herbert", "2005")
	near := row("2", "Dune", "Frank Herbert", "2006") // a reprint
	far := row("3", "Dune", "Frank Herbert", "1965")  // another edition decades off
	none := row("4", "Dune", "Frank Herbert", "")     // no year: neutral
	work := row("5", "Dune", "Frank Herbert", "1965") // a work-level row: its year is the work's
	work.EditionLevel = false
	work.Identifiers = []ebookmeta.Identifier{{Scheme: "openlibrary", Value: "OL1W"}}

	_, rows := ranked(q, far, near, none, exact, work)
	score := map[string]float64{}
	for _, c := range rows {
		id := c.Identifiers[0].Value
		score[id] = c.Score
	}
	if !(score["1"] >= score["2"] && score["2"] > score["3"]) {
		t.Errorf("a close year did not outrank a far one: %v", score)
	}
	if score["2"]-score["1"] < -0.05 {
		t.Errorf("a one-year gap was penalised like a far one: %v", score)
	}
	if score["4"] < score["3"] {
		t.Errorf("a missing year was penalised like a conflicting one: %v", score)
	}
	if notes := notesOf(rows, "Dune", "Frank Herbert"); strings.Contains(notes, "OL1W") {
		t.Errorf("unexpected: %s", notes)
	}
	for _, c := range rows {
		if c.Identifiers[0].Value == "5" && len(c.Notes) != 0 {
			t.Errorf("a work-level row was judged on its year: %v", c.Notes)
		}
	}
}

func TestAnotherEditionByISBNRanksBelowTheBookAndIsNeverPreselected(t *testing.T) {
	const mine, other = "9780441013593", "9780441172719"
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}, ISBNs: []string{mine}}
	same := row("1", "Dune", "Frank Herbert", "2005")
	same.Identifiers = append(same.Identifiers, ebookmeta.Identifier{Scheme: "isbn", Value: mine})
	another := row("2", "Dune", "Frank Herbert", "1990")
	another.Identifiers = append(another.Identifiers, ebookmeta.Identifier{Scheme: "isbn", Value: other})
	workLevel := Candidate{Title: "Dune", Authors: []string{"Frank Herbert"}, Sources: []string{"A"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "openlibrary", Value: "OL1W"}}}

	names, rows := ranked(q, another, workLevel, same)
	if names[0] != "Dune|Frank Herbert" || rows[0].Match != MatchISBN || rows[0].ISBN() != mine {
		t.Fatalf("the exact ISBN is not first: %+v", rows)
	}
	var otherRow Candidate
	for _, c := range rows {
		if c.ISBN() == other {
			otherRow = c
		}
	}
	if !strings.Contains(strings.Join(otherRow.Notes, ";"), "another edition") {
		t.Errorf("the other edition is not marked: %v", otherRow.Notes)
	}
	// Choosing it never selects its edition fields: what it would write is not this file's.
	cur := &ebookmeta.Metadata{Title: "Dune"}
	cur.SetIdentifier("isbn", mine)
	for _, ch := range Plan(cur, otherRow) {
		if editionField(ch.Field) && ch.DefaultSelected() {
			t.Errorf("%s of another edition starts selected", ch.Field)
		}
	}
}

func TestAuthorsAgreeByGivenNameInitialAndSurname(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		min, max  float64
	}{
		{"Frank Herbert", "Herbert, Frank", 1, 1},
		{"F. Herbert", "Frank Herbert", 0.9, 1},
		{"Frank Herbert", "Frank Patrick Herbert", 1, 1},
		{"Herbert", "Frank Herbert", 0.6, 0.8}, // a lone surname fits every Herbert
		{"Frank Herbert", "Brian Herbert", 0, 0.4},
		{"刘慈欣", "刘慈欣", 1, 1},
		{"Franz Kafka", "Frank Kafka", 0, 0.4},
	} {
		got := authorSimilarity([]string{tc.want}, []string{tc.got})
		if got < tc.min || got > tc.max {
			t.Errorf("authorSimilarity(%q, %q) = %.2f, want %.2f to %.2f", tc.want, tc.got, got, tc.min, tc.max)
		}
	}
}

func TestNotesAreDataOnTheCandidate(t *testing.T) {
	q := Query{Title: "Dune", Authors: []string{"Frank Herbert"}, ISBNs: []string{"9780441013593"}}
	hit := row("1", "Dune", "Frank Herbert", "2005")
	hit.Identifiers = append(hit.Identifiers, ebookmeta.Identifier{Scheme: "isbn", Value: "9780441013593"})
	_, rows := ranked(q, hit)
	if len(rows) != 1 || !strings.Contains(strings.Join(rows[0].Notes, ";"), "shares the file's ISBN") {
		t.Fatalf("notes = %v", rows[0].Notes)
	}
	// A hint is said to be one.
	q.Unconfirmed = []string{"9780441013593"}
	_, rows = ranked(q, hit)
	if !strings.Contains(strings.Join(rows[0].Notes, ";"), "hint") {
		t.Errorf("notes = %v", rows[0].Notes)
	}
}

// A conflict lowers a row's rank and says why; it does not take the row out of a
// list it was in. The other volume is shown, below the one that was asked for.
func TestAConflictLowersARowWithoutDroppingIt(t *testing.T) {
	q := Query{Title: "三体Ⅱ", Authors: []string{"刘慈欣"}}
	asked := row("1", "三体Ⅱ", "刘慈欣", "2008")
	other := row("2", "三体Ⅲ", "刘慈欣", "2010")
	names, rows := ranked(q, other, asked)
	before(t, names, "三体Ⅱ|刘慈欣", "三体Ⅲ|刘慈欣") // present, and after
	if got := notesOf(rows, "三体Ⅲ", "刘慈欣"); !strings.Contains(got, "another volume") {
		t.Errorf("the other volume does not say why it is lower: %q", got)
	}
	// And the floor holds for a row that was not in the list to begin with.
	if _, rows := ranked(Query{Title: "Dune"}, row("3", "Cooking for Dinner Parties", "A. Cook", "2010")); len(rows) != 0 {
		t.Errorf("an unrelated title was listed: %+v", rows)
	}
}
