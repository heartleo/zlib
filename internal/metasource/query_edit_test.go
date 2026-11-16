package metasource

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQueryEdit(t *testing.T) {
	const a, b = "9787536692930", "9780140328721"
	old := Query{Title: "三体", Authors: []string{"刘慈欣"}, ISBNs: []string{a, b}, Unconfirmed: []string{b}}
	for _, tc := range []struct {
		name                  string
		title, authors, isbns string
		want                  Query
	}{
		{"nothing changed", "三体", "刘慈欣", a + ", " + b, old},
		{"only the title", "三体Ⅱ", "刘慈欣", a + ", " + b,
			Query{Title: "三体Ⅱ", Authors: []string{"刘慈欣"}, ISBNs: []string{a, b}, Unconfirmed: []string{b}}},
		{"text is cleaned and authors split", "  三体\t\x07 全集 ", " 刘慈欣 & 某人 & ", a,
			Query{Title: "三体 全集", Authors: []string{"刘慈欣", "某人"}, ISBNs: []string{a}}},
		{"a typed ISBN is confirmed", "三体", "刘慈欣", a + ", " + b + ", 9781593272203",
			Query{Title: "三体", Authors: []string{"刘慈欣"}, ISBNs: []string{a, b, "9781593272203"}, Unconfirmed: []string{b}}},
		{"an ISBN-10 reads as its ISBN-13", "三体", "刘慈欣", "7-5366-9293-5",
			Query{Title: "三体", Authors: []string{"刘慈欣"}, ISBNs: []string{a}}},
		{"removing the hint drops it", "三体", "刘慈欣", a,
			Query{Title: "三体", Authors: []string{"刘慈欣"}, ISBNs: []string{a}}},
		{"separators and repeats", "三体", "", a + ";" + a + "\n" + b,
			Query{Title: "三体", ISBNs: []string{a, b}, Unconfirmed: []string{b}}},
		{"an ISBN alone is a query", "", "", a, Query{ISBNs: []string{a}}},
		{"an author alone is a query", "", "刘慈欣", "", Query{Authors: []string{"刘慈欣"}}},
	} {
		got, err := ParseQueryEdit(old, tc.title, tc.authors, tc.isbns)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

// A hint that was confirmed by typing it again stays a hint only while the
// user leaves it alone: retyping a different ISBN is the user's own claim.
func TestParseQueryEditConfirmsOnlyWhatTheUserTyped(t *testing.T) {
	const hint = "9780140328721"
	old := Query{Title: "x", ISBNs: []string{hint}, Unconfirmed: []string{hint}}
	got, err := ParseQueryEdit(old, "x", "", "0-14-032872-6, 9787536692930")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Unconfirmed, []string{hint}) {
		t.Errorf("Unconfirmed = %v, want the untouched hint only", got.Unconfirmed)
	}
	if !reflect.DeepEqual(got.ISBNs, []string{hint, "9787536692930"}) {
		t.Errorf("ISBNs = %v", got.ISBNs)
	}
}

func TestParseQueryEditRefusesWhatCannotBeSearched(t *testing.T) {
	old := Query{Title: "三体"}
	for _, tc := range []struct {
		name                  string
		title, authors, isbns string
		contains              string
	}{
		{"empty", "", "", "", "title, an author or an ISBN"},
		{"blanks and control characters", " \t\x07", " & ", " , ", "title, an author or an ISBN"},
		{"bad check digit", "三体", "", "9787536692931", "9787536692931"},
		{"an ASIN", "三体", "", "B07S7CCQGL", "B07S7CCQGL"},
		{"one bad among good", "三体", "", "9787536692930, nope", "nope"},
	} {
		_, err := ParseQueryEdit(old, tc.title, tc.authors, tc.isbns)
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.contains) || strings.Contains(err.Error(), "\x07") {
			t.Errorf("%s: err = %q, want it to mention %q", tc.name, err, tc.contains)
		}
	}
}
