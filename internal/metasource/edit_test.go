package metasource

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// planFor builds the plan for a candidate against an empty file, with every
// field the editor knows about.
func planFor(t *testing.T) map[Field]Change {
	t.Helper()
	cand := Candidate{
		Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Languages: []string{"zh"},
		Tags: []string{"科幻"}, Published: "2008-01", Description: "地球往事。", EditionLevel: true,
		CoverURL:    "https://covers.openlibrary.org/b/id/1-L.jpg",
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9787536692930"}, {Scheme: "weread", Value: "695233"}},
		Match:       MatchTitle, Score: 0.8,
	}
	out := map[Field]Change{}
	for _, ch := range Plan(&ebookmeta.Metadata{}, cand) {
		out[ch.Field] = ch
	}
	return out
}

func apply(ch Change) *ebookmeta.Changes {
	return BuildChanges([]Change{ch}, func(Change) bool { return true })
}

func TestChangeEditValidatesAndRewritesEachField(t *testing.T) {
	plan := planFor(t)
	for _, tc := range []struct {
		field Field
		text  string
		want  string // the displayed new value
		check func(*ebookmeta.Changes) bool
	}{
		// Control characters are dropped and whitespace collapsed, so typed or
		// pasted text cannot carry a terminal escape into the file or the screen.
		{FieldTitle, "  三体\t\x07 \x1b全集  ", "三体 全集", func(c *ebookmeta.Changes) bool { return *c.Title == "三体 全集" }},
		{FieldPublisher, "人民文学出版社", "人民文学出版社", func(c *ebookmeta.Changes) bool { return *c.Publisher == "人民文学出版社" }},
		{FieldAuthors, "刘慈欣 & 某人 &  ", "刘慈欣 & 某人", func(c *ebookmeta.Changes) bool { return reflect.DeepEqual(*c.Authors, []string{"刘慈欣", "某人"}) }},
		{FieldLanguages, "zh, EN", "zh, en", func(c *ebookmeta.Changes) bool { return reflect.DeepEqual(*c.Languages, []string{"zh", "en"}) }},
		{FieldLanguages, "chi", "zh", func(c *ebookmeta.Changes) bool { return reflect.DeepEqual(*c.Languages, []string{"zh"}) }},
		{FieldTags, "科幻, 小说,科幻", "科幻, 小说", func(c *ebookmeta.Changes) bool { return reflect.DeepEqual(*c.Tags, []string{"科幻", "小说"}) }},
		{FieldPublished, "2008年1月5日", "2008-01-05", func(c *ebookmeta.Changes) bool { return *c.Published == "2008-01-05" }},
		{FieldPublished, "2008", "2008", func(c *ebookmeta.Changes) bool { return *c.Published == "2008" }},
		{"identifier:isbn", "978-7-5366-9293-0", "9787536692930", func(c *ebookmeta.Changes) bool { return c.Identifiers["isbn"] == "9787536692930" }},
		{"identifier:isbn", "7-5366-9293-5", "9787536692930", func(c *ebookmeta.Changes) bool { return c.Identifiers["isbn"] == "9787536692930" }},
	} {
		ch, ok := plan[tc.field]
		if !ok {
			t.Fatalf("%s: no such row in the plan", tc.field)
		}
		got, err := ch.Edit(tc.text)
		if err != nil {
			t.Errorf("%s %q: %v", tc.field, tc.text, err)
			continue
		}
		if got.New != tc.want {
			t.Errorf("%s %q: New = %q, want %q", tc.field, tc.text, got.New, tc.want)
		}
		if c := apply(got); c == nil || !tc.check(c) {
			t.Errorf("%s %q: the write does not carry the edited value: %+v", tc.field, tc.text, c)
		}
		// Editing is an explicit choice: the row is selected and says so.
		if !got.DefaultSelected() || !got.Edited() || got.Note != NoteEdited {
			t.Errorf("%s: selected=%v edited=%v note=%q", tc.field, got.DefaultSelected(), got.Edited(), got.Note)
		}
		if got.Field != ch.Field || got.Old != ch.Old || got.Fill != ch.Fill {
			t.Errorf("%s: an edit changed more than the new value: %+v", tc.field, got)
		}
	}
}

func TestChangeEditRejectsInvalidText(t *testing.T) {
	plan := planFor(t)
	for _, tc := range []struct {
		field Field
		text  string
	}{
		{FieldTitle, "   "},
		{FieldTitle, "\x1b\x00\x07 \t"},
		{FieldPublisher, ""},
		{FieldAuthors, " & & "},
		{FieldLanguages, "klingon!"},
		{FieldLanguages, ""},
		{FieldTags, " , ,"},
		{FieldPublished, "last spring"},
		{FieldPublished, ""},
		{"identifier:isbn", "9787536692931"}, // bad check digit
		{"identifier:isbn", "B07S7CCQGL"},    // an ASIN
		{"identifier:isbn", "9787536692930,9780140328721"},
	} {
		if _, err := plan[tc.field].Edit(tc.text); err == nil {
			t.Errorf("%s %q was accepted", tc.field, tc.text)
		} else if strings.TrimSpace(err.Error()) == "" || strings.Contains(err.Error(), "\x1b") {
			t.Errorf("%s %q: unusable error %q", tc.field, tc.text, err)
		}
	}
}

func TestChangeEditOnlyOffersWhatThePlanHolds(t *testing.T) {
	plan := planFor(t)
	for _, f := range []Field{FieldCover, "identifier:weread"} { // the description is editable since the field editor
		ch, ok := plan[f]
		if !ok {
			t.Fatalf("%s: no such row in the plan", f)
		}
		if ch.Editable() {
			t.Errorf("%s is editable", f)
		}
		if _, err := ch.Edit("anything"); !errors.Is(err, ErrNotEditable) {
			t.Errorf("%s: err = %v, want ErrNotEditable", f, err)
		}
	}
	for _, f := range []Field{FieldTitle, FieldAuthors, FieldPublisher, FieldLanguages, FieldTags, FieldPublished, "identifier:isbn", FieldDescription} {
		if !plan[f].Editable() {
			t.Errorf("%s is not editable", f)
		}
	}
}

// Editing keeps the original row intact, so a failed or repeated edit starts
// from what the catalogue said.
func TestChangeEditLeavesTheOriginalAlone(t *testing.T) {
	plan := planFor(t)
	orig := plan[FieldTitle]
	if _, err := orig.Edit("Changed"); err != nil {
		t.Fatal(err)
	}
	if orig.New != "三体" || orig.Edited() {
		t.Errorf("the original changed: %+v", orig)
	}
	if c := apply(orig); c == nil || *c.Title != "三体" {
		t.Errorf("the original now writes %+v", c)
	}
	again, err := plan[FieldTitle].Edit("三体")
	if err != nil || again.New != "三体" || !again.Edited() {
		t.Errorf("an edit back to the same text = %+v, %v", again, err)
	}
}

// A date is read strictly when typed: what the user wrote is what is written,
// or the text is refused with the format, never quietly cut to a year.
func TestChangeEditDateIsStrict(t *testing.T) {
	plan := planFor(t)
	for text, want := range map[string]string{
		"2008": "2008", "2008-01": "2008-01", "2008-1": "2008-01", "2008-01-05": "2008-01-05", "2008-1-5": "2008-01-05",
		" 2008-01-05 ": "2008-01-05", "2008年1月": "2008-01", "2008年1月5日": "2008-01-05", "2008-02-29": "2008-02-29",
	} {
		got, err := plan[FieldPublished].Edit(text)
		if err != nil || got.New != want {
			t.Errorf("%q: New = %q, err = %v, want %q", text, got.New, err, want)
		}
	}
	for _, text := range []string{
		"2008-00-05", "2008-1-00", "2008-02-31", "2009-02-29", "2008-13-05", "2008-04-31", "12/25/2008",
		"Jan 2008", "2008-01-05 and more", "0999", "20080105",
	} {
		if got, err := plan[FieldPublished].Edit(text); err == nil {
			t.Errorf("%q was accepted as %q", text, got.New)
		}
	}
}

// Dates a catalogue sends are never invented either: an impossible month or
// day is dropped to the precision that is real.
func TestNormalizeDateDropsImpossibleParts(t *testing.T) {
	for in, want := range map[string]string{
		"2008-00-05": "2008", "2008-1-00": "2008-01", "2008-02-31": "2008-02", "2009-02-29": "2009-02",
		"2008-13-05": "2008", "2008-04-31": "2008-04", "2008-02-29": "2008-02-29", "2008-12-31": "2008-12-31",
	} {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, want %q", in, got, want)
		}
	}
}
