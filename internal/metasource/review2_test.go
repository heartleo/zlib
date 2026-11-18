package metasource

import (
	"reflect"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func emb(title string, authors ...string) *ebookmeta.Metadata {
	return &ebookmeta.Metadata{Title: title, Authors: authors}
}

// A Z-Library name "X - Title (Author)" has its author in the group at the end;
// the dash in front is part of the title, so the name must not be read as
// "Author - Title" because the title after the dash agrees with the file's.
func TestAZLibraryNameWithADashStillKeepsItsAuthorGroup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		m           *ebookmeta.Metadata
		hint        Hint
		wantTitle   string
		wantAuthors []string
	}{
		{"Star Wars - Thrawn (Timothy Zahn).epub", emb("Thrawn", "Timothy Zahn"), Hint{}, "Star Wars - Thrawn", []string{"Timothy Zahn"}},
		{"Star Wars - Thrawn (Timothy Zahn).epub", emb(""), Hint{Title: "Thrawn (Star Wars)", Authors: []string{"Timothy Zahn"}}, "Star Wars - Thrawn", []string{"Timothy Zahn"}},
		{"Stephen King - On Writing (Stephen King).epub", emb("On Writing", "Stephen King"), Hint{}, "Stephen King - On Writing", []string{"Stephen King"}},
		{"鲁迅 - 呐喊 (鲁迅).epub", emb("呐喊", "鲁迅"), Hint{}, "鲁迅 - 呐喊", []string{"鲁迅"}},
	} {
		x := Extract(tc.name, tc.m, nil, tc.hint)
		if x.Query.Title != tc.wantTitle || !reflect.DeepEqual(x.Query.Authors, tc.wantAuthors) {
			t.Errorf("%s: title %q authors %q, want %q %q", tc.name, x.Query.Title, x.Query.Authors, tc.wantTitle, tc.wantAuthors)
		}
	}
}

// When only the title agrees with a dashed name, the words before the dash are
// not known to be authors, and the file's own authors stand.
func TestADashedNameAgreeingOnlyOnTheTitleDoesNotReplaceTheFilesAuthors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		m     *ebookmeta.Metadata
		title string
		want  []string
	}{
		{"Discworld - Mort.epub", emb("Mort", "Terry Pratchett"), "Mort", []string{"Terry Pratchett"}},
		{"Python - A Guide.epub", emb("A Guide", "Jo Doe"), "A Guide", []string{"Jo Doe"}},
	} {
		x := Extract(tc.name, tc.m, nil, Hint{})
		if x.Query.Title != tc.title || !reflect.DeepEqual(x.Query.Authors, tc.want) {
			t.Errorf("%s: title %q authors %q, want %q %q", tc.name, x.Query.Title, x.Query.Authors, tc.title, tc.want)
		}
	}
}

// A name this package built is read back, and built again, as the same name even
// when the file has no metadata to agree with it, in every layout: a second
// --rename changes nothing. A dashed name is read the way the user's layout says.
func TestABuiltNameSurvivesAFileWithNoMetadata(t *testing.T) {
	for _, layout := range NameLayouts {
		for _, tc := range []struct {
			title   string
			authors []string
		}{
			{"Dune", []string{"Frank Herbert"}},
			{"Dune: Messiah", []string{"Frank Herbert"}},
			{"Architecture as Code", []string{"Neal Ford", "Mark Richards"}},
			{"三体", []string{"刘慈欣"}},
			{"Dune (Dune Chronicles, Book 1)", []string{"Frank Herbert"}},
		} {
			name := FileNameWith(layout, tc.title, tc.authors, ".epub")
			x := Extract(name, &ebookmeta.Metadata{}, nil, Hint{NameLayout: layout})
			again := FileNameWith(layout, x.Query.Title, x.Query.Authors, ".epub")
			if again != name {
				t.Errorf("%s: %q -> read as %q %q -> %q", layout, name, x.Query.Title, x.Query.Authors, again)
			}
			if !reflect.DeepEqual(x.Query.Authors, tc.authors) {
				t.Errorf("%s: %q: authors read back as %q, want %q", layout, name, x.Query.Authors, tc.authors)
			}
		}
	}
	// A title that ends in a group would be read as the authors, so it is
	// written with square brackets.
	if got := nameAuthorTitle("Dune (Dune Chronicles, Book 1)", []string{"Frank Herbert"}, ".epub"); got != "Frank Herbert - Dune [Dune Chronicles, Book 1].epub" {
		t.Errorf("a title ending in a group: %q", got)
	}
}

// A dashed name that the file's own title and authors contradict is not read as
// "Author - Title".
func TestADashedNameThatTheFileContradictsIsATitle(t *testing.T) {
	x := Extract("Python - A Guide.epub", emb("Something Else", "Jo Smith"), nil, Hint{})
	if x.Query.Title != "Something Else" || !reflect.DeepEqual(x.Query.Authors, []string{"Jo Smith"}) {
		t.Errorf("query = %+v", x.Query)
	}
}

func TestATypedSubtitleKeepsItsTwoSpaces(t *testing.T) {
	if got := SanitizeFileName("Frank Herbert - Dune  Messiah", ".epub"); got != "Frank Herbert - Dune  Messiah.epub" {
		t.Errorf("SanitizeFileName = %q", got)
	}
	// What FileNameFor builds is a fixed point of SanitizeFileName.
	name := nameAuthorTitle("Dune: Messiah", []string{"Frank Herbert"}, ".epub")
	if got := SanitizeFileName(name, ".epub"); got != name {
		t.Errorf("%q became %q", name, got)
	}
}

func TestFileNameForEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		title   string
		authors []string
		want    string
	}{
		{"every author is flipped", "T", []string{"Pratchett, Terry", "Gaiman, Neil"}, "Terry Pratchett, Neil Gaiman - T.epub"},
		{"a joiner stays in Persian", "می‌خواهم", nil, "می‌خواهم.epub"},
		{"a zero-width space does not", "Du​ne", nil, "Dune.epub"},
		{"a console device", "CONIN$", nil, "_CONIN$.epub"},
		{"a line break between words", "Dune\nMessiah", nil, "Dune Messiah.epub"},
		{"a tab between words", "Dune\tMessiah", []string{"Frank\tHerbert"}, "Frank Herbert - Dune Messiah.epub"},
		{"a slash in a number-like title", "11/22/63", []string{"Stephen King"}, "Stephen King - 11-22-63.epub"},
		{"a placeholder title gives no name", "Unknown", nil, ""},
		{"a number alone gives no name", "9780441013593", nil, ""},
	} {
		if got := nameAuthorTitle(tc.title, tc.authors, ".epub"); got != tc.want {
			t.Errorf("%s: nameAuthorTitle(%q, %q) = %q, want %q", tc.name, tc.title, tc.authors, got, tc.want)
		}
	}
}
