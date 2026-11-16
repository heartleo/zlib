package metasource

import (
	"reflect"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func isbnHit(title, isbn string) Candidate {
	return Candidate{Title: title, EditionLevel: true, Sources: []string{"Fake"}, Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}}
}

// With no title to compare, a hint ISBN has nothing to vouch for it: a result
// that merely carries the number is flagged, whatever its title says.
func TestAHintISBNWithNoTitleIsNotConfirmed(t *testing.T) {
	const isbn = "9780441013593"
	for name, tc := range map[string]struct {
		file string
		m    *ebookmeta.Metadata
		scan []string
	}{
		"an ISBN as the file name": {"9780441013593.epub", &ebookmeta.Metadata{}, nil},
		"a scanned ISBN":           {"x.pdf", &ebookmeta.Metadata{Title: "Unknown"}, []string{isbn}},
	} {
		x := Extract(tc.file, tc.m, tc.scan, Hint{})
		if x.Query.Title != "" || len(x.Query.Unconfirmed) != 1 {
			t.Fatalf("%s: query = %+v", name, x.Query)
		}
		got := rankAndMerge(x.Query, []Candidate{isbnHit("Totally Different Book", isbn)}, defaultMinScore, 10)
		if len(got) != 1 || got[0].Score >= 1 {
			t.Errorf("%s: a hint with no title was confirmed: %+v", name, got)
		}
		for _, ch := range Plan(tc.m, got[0]) {
			if ch.Field == "identifier:isbn" && ch.DefaultSelected() {
				t.Errorf("%s: the hint ISBN starts selected", name)
			}
		}
	}
	// An ISBN the user typed has no title to check either, and is confirmed.
	q := Query{ISBNs: []string{isbn}}
	if got := rankAndMerge(q, []Candidate{isbnHit("Dune", isbn)}, defaultMinScore, 10); len(got) != 1 || got[0].Score != 1 {
		t.Errorf("a typed ISBN was doubted: %+v", got)
	}
}

func TestFileNamesWithSiteListsAndYearsKeepTheirAuthors(t *testing.T) {
	for name, want := range map[string]FileGuess{
		"Dune (Frank Herbert) (z-library.sk, 1lib.sk, z-lib.sk).epub": {Title: "Dune", Authors: []string{"Frank Herbert"}},
		"Dune (Frank Herbert) (z-lib.gd).epub":                        {Title: "Dune", Authors: []string{"Frank Herbert"}},
		"Dune (Frank Herbert) (2005).epub":                            {Title: "Dune", Authors: []string{"Frank Herbert"}},
		"Dune (Frank Herbert) (Z-Library).epub":                       {Title: "Dune", Authors: []string{"Frank Herbert"}},
	} {
		g := GuessFromFileName(name)
		if g.Title != want.Title || !reflect.DeepEqual(g.Authors, want.Authors) {
			t.Errorf("%q: title %q authors %q, want %q %q", name, g.Title, g.Authors, want.Title, want.Authors)
		}
	}
}

func TestTranslatorGroupsNeverBecomeAuthors(t *testing.T) {
	for name, want := range map[string][]string{
		"三体 (刘慈欣著，张三译).epub":    {"刘慈欣"},
		"三体 (刘慈欣 著, 张三 译).epub": {"刘慈欣"},
		"三体 (刘慈欣 著  张三 译).epub": {"刘慈欣"},
		"三体 (张三译).epub":         nil, // only a translator: no author to guess
		"三体 (张三, 李四 译).epub":    nil, // no role word: an author cannot be told from a translator
		"三体 (刘慈欣, 张三 译).epub":   nil,
	} {
		if got := GuessFromFileName(name).Authors; !reflect.DeepEqual(got, want) {
			t.Errorf("%q: authors %q, want %q", name, got, want)
		}
	}
}

func TestANumberIsATitleWhenItIsShortAndNothingElse(t *testing.T) {
	for name, want := range map[string]string{
		"1984 (George Orwell).epub":  "1984",
		"2666 (Roberto Bolaño).epub": "2666",
		"9780441013593.epub":         "",
		"20231116.epub":              "",
		"12-34.epub":                 "",
	} {
		if got := GuessFromFileName(name).Title; got != want {
			t.Errorf("%q: title %q, want %q", name, got, want)
		}
	}
	if !UsefulTitle("1984") || UsefulTitle("20231116") || UsefulTitle("9780441013593") {
		t.Error("UsefulTitle judges numbers wrongly")
	}
	x := Extract("1984 (George Orwell).epub", &ebookmeta.Metadata{Title: "1984", Authors: []string{"George Orwell"}}, nil, Hint{})
	if x.Query.Title != "1984" || x.Embedded.Title != "1984" {
		t.Errorf("the title 1984 was dropped: %+v", x)
	}
}

// A file name that adds only a volume mark to the file's own title names another
// book, so it must not count as the file's own title agreeing.
func TestAVolumeMarkIsNotAMarketingLine(t *testing.T) {
	if titlesAgree("三体Ⅱ", "三体") || titlesAgree("三体", "三体2") || titlesAgree("Dune", "Dune II") {
		t.Error("a volume of a series agreed with the first book")
	}
	if !titlesAgree("Dune: Frank Herbert's classic masterpiece", "Dune") {
		t.Error("a marketing line no longer agrees")
	}
	const hint = "9787536692930"
	m := &ebookmeta.Metadata{Title: "三体"}
	m.SetIdentifier(SchemeZlibISBN, hint)
	x := Extract("三体Ⅱ (刘慈欣).epub", m, nil, Hint{})
	if !x.Query.TitleGuessed {
		t.Errorf("a sequel's name confirmed the first book's ISBN: %+v", x.Query)
	}
}
