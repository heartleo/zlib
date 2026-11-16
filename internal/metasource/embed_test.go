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
	alreadyHeld := &ebookmeta.Metadata{Title: "三体"}
	alreadyHeld.SetIdentifier(SchemeZlibISBN, "978-7-5366-9293-0")

	for _, tc := range []struct {
		name   string
		cur    *ebookmeta.Metadata
		title  string
		isbns  []string
		scheme string
	}{
		{"agreeing title is written as the ISBN", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{"", isbn}, "isbn"},
		{"subtitle on the record still agrees", &ebookmeta.Metadata{Title: "三体"}, "三体：地球往事", []string{isbn}, "isbn"},
		{"ISBN-10 is stored as ISBN-13", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{"7-5366-9293-5"}, "isbn"},
		{"disagreeing title is only a hint", &ebookmeta.Metadata{Title: "红楼梦"}, "三体", []string{isbn}, SchemeZlibISBN},
		{"a sequel is not the first volume", &ebookmeta.Metadata{Title: "三体"}, "三体Ⅱ", []string{isbn}, SchemeZlibISBN},
		{"no file title cannot confirm", &ebookmeta.Metadata{}, "三体", []string{isbn}, SchemeZlibISBN},
		{"no record title cannot confirm", &ebookmeta.Metadata{Title: "三体"}, "", []string{isbn}, SchemeZlibISBN},
		{"invalid ISBNs are skipped", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{"9787536692931", "abc"}, ""},
		// A record's identifier can list several editions' ISBNs, and a title
		// cannot say which edition the file is.
		{"one book's ISBN-13 and ISBN-10 are one edition", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{isbn, "7-5366-9293-5"}, "isbn"},
		{"several editions give only a hint", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{isbn, "7-5366-9293-5", "9780140328721,9781593272203"}, SchemeZlibISBN},
		{"a 978 and a 979 ISBN-13 are two editions", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{isbn, "9798666362525"}, SchemeZlibISBN},
		{"a repeated ISBN is still one edition", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{isbn, "9787536692930,9787536692930"}, "isbn"},
		{"an ASIN beside the ISBN is not an edition", &ebookmeta.Metadata{Title: "三体"}, "三体", []string{isbn, "B07S7CCQGL," + isbn}, "isbn"},
		{"an existing ISBN is never replaced", withISBN, "三体", []string{isbn}, ""},
		{"an already recorded hint is not rewritten", alreadyHeld, "三体", []string{isbn}, ""},
	} {
		scheme, c := PlanDownloadISBN(tc.cur, tc.title, tc.isbns...)
		if scheme != tc.scheme {
			t.Errorf("%s: scheme = %q, want %q", tc.name, scheme, tc.scheme)
			continue
		}
		if tc.scheme == "" {
			if c != nil {
				t.Errorf("%s: wrote %+v", tc.name, c)
			}
			continue
		}
		if c == nil || !reflect.DeepEqual(c.Identifiers, map[string]string{tc.scheme: isbn}) {
			t.Errorf("%s: changes = %+v", tc.name, c)
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
