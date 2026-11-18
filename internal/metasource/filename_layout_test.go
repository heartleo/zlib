package metasource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// nameAuthorTitle is FileNameFor in the layout it used to have.
func nameAuthorTitle(title string, authors []string, ext string) string {
	return FileNameWith(LayoutAuthorTitle, title, authors, ext)
}

func TestFileNameLayouts(t *testing.T) {
	authors := []string{"Neal Ford", "Mark Richards"}
	for layout, want := range map[NameLayout]string{
		LayoutTitleAuthor: "Architecture as Code - Neal Ford, Mark Richards.epub",
		LayoutAuthorTitle: "Neal Ford, Mark Richards - Architecture as Code.epub",
		LayoutTitleParen:  "Architecture as Code (Neal Ford, Mark Richards).epub",
	} {
		if got := FileNameWith(layout, "Architecture as Code", authors, ".epub"); got != want {
			t.Errorf("%s: %q, want %q", layout, got, want)
		}
		// With no author, or no title, every layout agrees.
		if got := FileNameWith(layout, "Dune", nil, ".epub"); got != "Dune.epub" {
			t.Errorf("%s with no author: %q", layout, got)
		}
		if got := FileNameWith(layout, "", authors, ".epub"); got != "" {
			t.Errorf("%s with no title: %q", layout, got)
		}
	}
	if FileNameFor("Dune", []string{"Frank Herbert"}, ".epub") != "Dune - Frank Herbert.epub" || DefaultNameLayout != LayoutTitleAuthor {
		t.Error("the default layout is not Title - Author")
	}
	if got := FileNameWith(LayoutTitleParen, "Dune (Chronicles, 1)", []string{"Frank Herbert"}, ".epub"); got != "Dune [Chronicles, 1] (Frank Herbert).epub" {
		t.Errorf("a title ending in a group stays apart from the authors: %q", got)
	}
}

func TestParseNameLayout(t *testing.T) {
	for in, want := range map[string]NameLayout{"": LayoutTitleAuthor, " Author-Title ": LayoutAuthorTitle, "title-paren": LayoutTitleParen} {
		if got, err := ParseNameLayout(in); err != nil || got != want {
			t.Errorf("ParseNameLayout(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseNameLayout("title_author"); err == nil {
		t.Error("an unknown layout was accepted")
	}
}

// A dashed name is read as the file says; with nothing to decide, as the
// user's layout does; and a title with a dash in it stays whole.
func TestDashedNamesAreReadEitherWay(t *testing.T) {
	meta := func(title string, authors ...string) *ebookmeta.Metadata {
		return &ebookmeta.Metadata{Title: title, Authors: authors}
	}
	for _, tc := range []struct {
		name        string
		file        string
		m           *ebookmeta.Metadata
		layout      NameLayout
		wantTitle   string
		wantAuthors []string
	}{
		{"the file's author is on the right", "Dune - Frank Herbert.epub", meta("", "Frank Herbert"), "", "Dune", []string{"Frank Herbert"}},
		{"the file's author is on the left", "Frank Herbert - Dune.epub", meta("", "Frank Herbert"), LayoutTitleAuthor, "Dune", []string{"Frank Herbert"}},
		{"the file's title is on the left", "Dune - Frank Herbert.epub", meta("Dune"), "", "Dune", []string{"Frank Herbert"}},
		{"nothing known, Title - Author layout", "Dune - Frank Herbert.epub", meta(""), LayoutTitleAuthor, "Dune", []string{"Frank Herbert"}},
		{"nothing known, Author - Title layout", "Frank Herbert - Dune.epub", meta(""), LayoutAuthorTitle, "Dune", []string{"Frank Herbert"}},
		{"nothing known, no layout: Author - Title", "Frank Herbert - Dune.epub", meta(""), "", "Dune", []string{"Frank Herbert"}},
		{"a real title with a dash", "Python - A Guide.epub", meta("Python - A Guide"), LayoutTitleAuthor, "Python - A Guide", nil},
	} {
		x := Extract(tc.file, tc.m, nil, Hint{NameLayout: tc.layout})
		if x.Query.Title != tc.wantTitle || !reflect.DeepEqual(x.Query.Authors, tc.wantAuthors) {
			t.Errorf("%s: title %q authors %q, want %q %q", tc.name, x.Query.Title, x.Query.Authors, tc.wantTitle, tc.wantAuthors)
		}
	}
}

// A custom pattern puts the title and the authors where it says, keeps its own
// text, and is held to what a file name can contain.
func TestCustomNamePatterns(t *testing.T) {
	authors := []string{"Frank Herbert"}
	for pattern, want := range map[string]string{
		"{title} [{authors}]": "Dune [Frank Herbert].epub",
		"{authors}, {title}":  "Frank Herbert, Dune.epub",
		"{title} _ {authors}": "Dune _ Frank Herbert.epub",
		"{title}":             "Dune.epub",
		"{title} (ebook)":     "Dune (ebook).epub",
		"{title} - {authors}": "Dune - Frank Herbert.epub", // a preset written out
		"{authors} - {title}": "Frank Herbert - Dune.epub",
		"{title} ({authors})": "Dune (Frank Herbert).epub",
	} {
		l, err := ParseNameLayout(pattern)
		if err != nil {
			t.Errorf("%q: %v", pattern, err)
			continue
		}
		if got := FileNameWith(l, "Dune", authors, ".epub"); got != want {
			t.Errorf("%q: %q, want %q", pattern, got, want)
		}
	}
	for pattern, want := range map[string]NameLayout{"{title} - {authors}": LayoutTitleAuthor, "{title} ({authors})": LayoutTitleParen} {
		if l, _ := ParseNameLayout(pattern); l != want {
			t.Errorf("%q is %q, want the preset %q", pattern, l, want)
		}
	}
	// With no author known, a pattern that names the authors gives the title.
	l, _ := ParseNameLayout("{title} [{authors}]")
	if got := FileNameWith(l, "Dune", nil, ".epub"); got != "Dune.epub" {
		t.Errorf("no authors: %q", got)
	}
	for _, bad := range []string{"title_author", "{authors}", "{title}{title}", "{title} {authors} {authors}", "{title} {year}", "{title}/{authors}", "{title} : {authors}", "{title} {", strings.Repeat("x", 60) + "{title}"} {
		if _, err := ParseNameLayout(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	// A custom layout that puts the title first reads a dashed name that way.
	if l, _ := ParseNameLayout("{title} - {authors} (x)"); !l.dashTitleFirst() || LayoutTitleParen.dashTitleFirst() || LayoutAuthorTitle.dashTitleFirst() {
		t.Error("dashTitleFirst is wrong")
	}
}
