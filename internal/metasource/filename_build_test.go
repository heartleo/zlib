package metasource

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestFileNameFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		title   string
		authors []string
		ext     string
		want    string
	}{
		{"author then title", "Dune", []string{"Frank Herbert"}, ".epub", "Frank Herbert - Dune.epub"},
		{"several authors", "Architecture as Code", []string{"Neal Ford", "Mark Richards"}, ".epub", "Neal Ford, Mark Richards - Architecture as Code.epub"},
		{"only three authors", "T", []string{"A One", "B Two", "C Three", "D Four"}, ".epub", "A One, B Two, C Three - T.epub"},
		{"no authors", "Dune", nil, ".epub", "Dune.epub"},
		{"no title", "", []string{"Frank Herbert"}, ".epub", ""},
		{"a blank title", " \t ", []string{"Frank Herbert"}, ".epub", ""},
		{"a subtitle is two spaces", "Dune: Messiah", []string{"Frank Herbert"}, ".epub", "Frank Herbert - Dune  Messiah.epub"},
		{"a dash in the title is a subtitle too", "Python - A Guide", []string{"Jo Doe"}, ".epub", "Jo Doe - Python  A Guide.epub"},
		{"a slash is not a path", "AC/DC", []string{"Jo Doe"}, ".epub", "Jo Doe - AC-DC.epub"},
		{"a backslash is not a path", `a\b`, nil, ".epub", "a-b.epub"},
		{"characters no system takes", `Who "Moved" <My> Cheese? | *`, nil, ".epub", "Who 'Moved' My Cheese.epub"},
		{"a bare colon", "Re:Zero", nil, ".epub", "Re-Zero.epub"},
		{"last name first", "Dune", []string{"Herbert, Frank"}, ".epub", "Frank Herbert - Dune.epub"},
		{"a list is not flipped", "T", []string{"Herbert, Frank, Smith"}, ".epub", "Herbert Frank Smith - T.epub"},
		{"a dot ends no name", "Dune...", nil, ".epub", "Dune.epub"},
		{"a dot starts no name", "..hidden", nil, ".epub", "hidden.epub"},
		{"a reserved Windows name", "CON", nil, ".epub", "_CON.epub"},
		{"a reserved Windows name in a path-like title", "nul.txt", nil, ".epub", "_nul.txt.epub"},
		{"controls and bidi marks", "Du\x00ne‮​", nil, ".epub", "Dune.epub"},
		{"the extension is lower case with a dot", "Dune", nil, "EPUB", "Dune.epub"},
		{"an odd extension is cleaned", "Dune", nil, ".ep/ub", "Dune.epub"},
		{"Chinese", "三体", []string{"刘慈欣"}, ".epub", "刘慈欣 - 三体.epub"},
		{"a full-width colon stays", "人性的弱点：增订全译版", []string{"戴尔·卡耐基"}, ".epub", "戴尔·卡耐基 - 人性的弱点：增订全译版.epub"},
		{"spaces are tidied", "  Dune   Messiah ", []string{" Frank   Herbert "}, ".azw3", "Frank Herbert - Dune Messiah.azw3"},
		{"an author with a dash", "T", []string{"Jean - Paul"}, ".epub", "Jean Paul - T.epub"},
	} {
		if got := nameAuthorTitle(tc.title, tc.authors, tc.ext); got != tc.want {
			t.Errorf("%s: nameAuthorTitle(%q, %q, %q) = %q, want %q", tc.name, tc.title, tc.authors, tc.ext, got, tc.want)
		}
	}
}

func TestFileNameForStaysWithinTheLimit(t *testing.T) {
	long := strings.Repeat("三体问题", 100)
	for name, authors := range map[string][]string{
		"no authors":   nil,
		"an author":    {"刘慈欣"},
		"huge authors": {strings.Repeat("Longname ", 60), "Second Author"},
	} {
		got := nameAuthorTitle(long, authors, ".epub")
		if len(got) > 255 || !utf8.ValidString(got) || !strings.HasSuffix(got, ".epub") || strings.HasPrefix(got, " - ") {
			t.Errorf("%s: %d bytes, valid=%v: %q", name, len(got), utf8.ValidString(got), got)
		}
		if strings.HasSuffix(strings.TrimSuffix(got, ".epub"), " ") {
			t.Errorf("%s: trailing space before the extension: %q", name, got)
		}
	}
	// A title that fits is never cut.
	if got := nameAuthorTitle("Dune", []string{"Frank Herbert"}, ".epub"); got != "Frank Herbert - Dune.epub" {
		t.Errorf("a short name changed: %q", got)
	}
	// The authors survive a long title.
	if got := nameAuthorTitle(long, []string{"刘慈欣"}, ".epub"); !strings.HasPrefix(got, "刘慈欣 - ") {
		t.Errorf("the author was cut: %q", got)
	}
}

// A name this package builds is read back as the same title and authors, so a
// renamed file is searched for what it is. The embedded metadata corroborates
// the order, as it does for the real files.
func TestABuiltNameIsReadBackAsItsTitleAndAuthors(t *testing.T) {
	for _, b := range realBooks() {
		if b.title == "" {
			continue
		}
		name := FileNameFor(b.title, b.authors, ".epub")
		if name == "" {
			t.Fatalf("%s: no name", b.name)
		}
		x := Extract(name, b.metadata(), nil, Hint{})
		if x.Query.Title != b.title || !reflect.DeepEqual(x.Query.Authors, b.authors) {
			t.Errorf("%s -> %q: read back title %q authors %q, want %q %q", b.name, name, x.Query.Title, x.Query.Authors, b.title, b.authors)
		}
	}
}

func TestADashedNameIsReadAsAuthorThenTitleOnlyWhenTheFileAgrees(t *testing.T) {
	g := GuessFromFileName("Frank Herbert - Dune.epub")
	if g.Title != "Frank Herbert - Dune" || g.Structured || !reflect.DeepEqual(g.DashAuthors, []string{"Frank Herbert"}) || g.DashTitle != "Dune" {
		t.Fatalf("guess = %+v", g)
	}
	for name, want := range map[string]string{"George Orwell - 1984.epub": "1984", "Roberto Bolano - 2666.epub": "2666"} {
		if g := GuessFromFileName(name); g.DashTitle != want || len(g.DashAuthors) != 1 {
			t.Errorf("%q: read as %+v, want the title %q", name, g, want)
		}
	}
	for _, name := range []string{"Jo Doe - 9780441013593.epub", "Jo Doe - 20231116.epub", "A - B - C.epub", "Dune.epub", "Dune - .epub", " - Dune.epub", "Dune-Messiah.epub"} {
		if g := GuessFromFileName(name); g.DashTitle != "" || len(g.DashAuthors) != 0 {
			t.Errorf("%q: read as a dashed name: %+v", name, g)
		}
	}

	meta := func(title string, authors ...string) *ebookmeta.Metadata {
		return &ebookmeta.Metadata{Title: title, Authors: authors}
	}
	for _, tc := range []struct {
		name        string
		file        string
		m           *ebookmeta.Metadata
		hint        Hint
		wantTitle   string
		wantAuthors []string
	}{
		{"the file's author agrees", "Frank Herbert - Dune.epub", meta("Anything", "Frank Herbert"), Hint{}, "Dune", []string{"Frank Herbert"}},
		{"the file's title agrees", "Frank Herbert - Dune.epub", meta("Dune"), Hint{}, "Dune", []string{"Frank Herbert"}},
		{"the download's author agrees", "Frank Herbert - Dune.epub", meta(""), Hint{Authors: []string{"Frank Herbert"}}, "Dune", []string{"Frank Herbert"}},
		{"nothing to contradict it: author then title", "Python - A Guide.epub", meta(""), Hint{}, "A Guide", []string{"Python"}},
		{"a real title with a dash", "Python - A Guide.epub", meta("Python - A Guide"), Hint{}, "Python - A Guide", nil},
		{"a Z-Library name keeps its author group", "Foo - Bar (Jo Doe).epub", meta("Foo - Bar", "Jo Doe"), Hint{}, "Foo - Bar", []string{"Jo Doe"}},
	} {
		x := Extract(tc.file, tc.m, nil, tc.hint)
		if x.Query.Title != tc.wantTitle || !reflect.DeepEqual(x.Query.Authors, tc.wantAuthors) {
			t.Errorf("%s: title %q authors %q, want %q %q", tc.name, x.Query.Title, x.Query.Authors, tc.wantTitle, tc.wantAuthors)
		}
	}
}

func TestSanitizeFileNameKeepsATypedDashAndTheExtension(t *testing.T) {
	for _, tc := range []struct{ in, ext, want string }{
		{"Frank Herbert - Dune", ".epub", "Frank Herbert - Dune.epub"},
		{"Frank Herbert - Dune.epub", ".epub", "Frank Herbert - Dune.epub"},
		{"Frank Herbert - Dune.EPUB", ".epub", "Frank Herbert - Dune.epub"},
		{"Dune: Messiah", ".epub", "Dune  Messiah.epub"},
		{`a/b\c?`, ".epub", "a-b-c.epub"},
		{"../../etc/passwd", ".epub", "-..-etc-passwd.epub"}, // no separator survives, and no leading dot
		{"  ", ".epub", ""},
		{"???", ".epub", ""},
		{"CON", ".epub", "_CON.epub"},
		{"Dune.", ".epub", "Dune.epub"},
	} {
		if got := SanitizeFileName(tc.in, tc.ext); got != tc.want {
			t.Errorf("SanitizeFileName(%q, %q) = %q, want %q", tc.in, tc.ext, got, tc.want)
		}
	}
}
