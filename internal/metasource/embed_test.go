package metasource

import (
	"reflect"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestPlanDownloadISBN(t *testing.T) {
	const isbn = "9787536692930"
	withISBN := &ebookmeta.Metadata{Title: "三体"}
	withISBN.SetIdentifier("isbn", "9780140328721")
	legacyHint := &ebookmeta.Metadata{Title: "三体"}
	legacyHint.SetIdentifier(SchemeZlibISBN, "978-7-5366-9293-0")

	for _, tc := range []struct {
		name string
		cur  *ebookmeta.Metadata
		isbn string
		want string // the ISBN written, "" for nothing
	}{
		// The record's ISBN-13 is the file's ISBN, whatever the titles say.
		{"an ISBN-13 is written", &ebookmeta.Metadata{Title: "三体"}, isbn, isbn},
		{"a file with a different title still gets it", &ebookmeta.Metadata{Title: "红楼梦"}, isbn, isbn},
		{"a file with no title still gets it", &ebookmeta.Metadata{}, isbn, isbn},
		{"hyphens and spaces are removed", &ebookmeta.Metadata{Title: "三体"}, " 978-7-5366-9293-0 ", isbn},
		{"an older hint does not stop the write", legacyHint, isbn, isbn},
		// Without an ISBN-13 nothing is written.
		{"an ISBN-10 alone is not enough", &ebookmeta.Metadata{Title: "三体"}, "7-5366-9293-5", ""},
		{"no ISBN", &ebookmeta.Metadata{Title: "三体"}, "", ""},
		{"a bad check digit", &ebookmeta.Metadata{Title: "三体"}, "9787536692931", ""},
		{"an ASIN", &ebookmeta.Metadata{Title: "三体"}, "B07S7CCQGL", ""},
		{"several values are not one ISBN-13", &ebookmeta.Metadata{Title: "三体"}, isbn + ",7-5366-9293-5", ""},
		// A file's own ISBN is never replaced.
		{"an existing ISBN is kept", withISBN, isbn, ""},
	} {
		c := PlanDownloadISBN(tc.cur, tc.isbn)
		if tc.want == "" {
			if c != nil {
				t.Errorf("%s: wrote %+v", tc.name, c)
			}
			continue
		}
		if c == nil || !reflect.DeepEqual(c.Identifiers, map[string]string{"isbn": tc.want}) {
			t.Errorf("%s: changes = %+v", tc.name, c)
		}
	}
}

func TestISBN13IsOnlyAnISBN13(t *testing.T) {
	for raw, want := range map[string]string{
		"9787536692930":       "9787536692930",
		"978-7-5366-9293-0":   "9787536692930",
		" 9787536692930 ":     "9787536692930",
		"7536692935":          "", // an ISBN-10 is not converted
		"9787536692931":       "", // bad check digit
		"978753669293":        "",
		"9787536692930,12345": "",
		"":                    "",
	} {
		if got := ISBN13(raw); got != want {
			t.Errorf("ISBN13(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestQueryFromMetadataSearchesTheRecordedHint(t *testing.T) {
	m := &ebookmeta.Metadata{Title: "三体"}
	m.SetIdentifier(SchemeZlibISBN, "9787536692930")
	if q := QueryFromMetadata(m); !reflect.DeepEqual(q.ISBNs, []string{"9787536692930"}) {
		t.Errorf("hint not searched: %+v", q)
	}
	m.SetIdentifier("isbn", "9787536692930")
	if q := QueryFromMetadata(m); !reflect.DeepEqual(q.ISBNs, []string{"9787536692930"}) {
		t.Errorf("same ISBN listed twice: %+v", q)
	}
}

// A hint that turns out right is promoted by the normal flow: an ISBN match
// whose title agrees is certain, so the plan fills the empty isbn and selects
// it. A hint for another book is flagged and selects nothing.
func TestRecordedHintIsConfirmedOrFlaggedByTheMatcher(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "三体"}
	cur.SetIdentifier(SchemeZlibISBN, "9787536692930")
	q := QueryFromMetadata(cur)
	cand := Candidate{Title: "三体", Authors: []string{"刘慈欣"}, EditionLevel: true, Sources: []string{"Douban"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9787536692930"}}}

	good := rankAndMerge(q, []Candidate{cand}, defaultMinScore, 10)
	if len(good) != 1 || good[0].Match != MatchISBN || good[0].Score != 1 {
		t.Fatalf("confirmed hint = %+v", good)
	}
	var found bool
	for _, ch := range Plan(cur, good[0]) {
		if ch.Field == "identifier:isbn" {
			found = true
			if !ch.DefaultSelected() {
				t.Error("a confirmed ISBN is not preselected")
			}
		}
	}
	if !found {
		t.Error("no isbn change proposed")
	}

	wrong := cand
	wrong.Title = "红楼梦"
	bad := rankAndMerge(q, []Candidate{wrong}, defaultMinScore, 10)
	if len(bad) != 1 || bad[0].Score != 0.5 {
		t.Fatalf("wrong hint = %+v", bad)
	}
	for _, ch := range Plan(cur, bad[0]) {
		if ch.DefaultSelected() {
			t.Errorf("%s preselected for a probable other book", ch.Field)
		}
	}
}

func TestISBNsFromSnippets(t *testing.T) {
	got := ISBNsFromSnippets([]string{
		"978-7-5366-9293-0 定价：38.00元", // ISBN-13 followed by a price
		"７-５３６６-９２９３-５\n38.00",        // full-width digits, ISBN-10
		" 978-7-5366-9293-0",          // duplicate of the first
		"978-7-5366-9293-1",           // bad check digit
		"020 8765 4321",               // a phone number
		"0-8044-2957-X",               // ISBN-10 with an X
		"9787536692930123456",         // longer run: the first 13 digits count
	})
	want := []string{"9787536692930", "9780804429573"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ISBNsFromSnippets = %v, want %v", got, want)
	}
}
