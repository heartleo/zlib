package metasource

import (
	"fmt"
	"strings"
)

// NameLayout is the order in which a file name states the title and the
// authors.
type NameLayout string

const (
	// LayoutTitleAuthor is "Title - Authors": a folder sorts by title.
	LayoutTitleAuthor NameLayout = "title-author"
	// LayoutAuthorTitle is "Authors - Title": a folder sorts by author.
	LayoutAuthorTitle NameLayout = "author-title"
	// LayoutTitleParen is "Title (Authors)", the way Z-Library names its files.
	LayoutTitleParen NameLayout = "title-paren"

	// DefaultNameLayout is the layout when none is chosen.
	DefaultNameLayout = LayoutTitleAuthor
)

// NameLayouts lists the layouts in the order they are offered.
var NameLayouts = []NameLayout{LayoutTitleAuthor, LayoutAuthorTitle, LayoutTitleParen}

// Label is the layout as a pattern a person reads: "Title - Author".
func (l NameLayout) Label() string {
	switch l {
	case LayoutAuthorTitle:
		return "Author - Title"
	case LayoutTitleParen:
		return "Title (Author)"
	}
	return "Title - Author"
}

// Valid reports whether l is one of NameLayouts.
func (l NameLayout) Valid() bool {
	for _, v := range NameLayouts {
		if l == v {
			return true
		}
	}
	return false
}

// ParseNameLayout reads a layout name. An empty one is DefaultNameLayout.
func ParseNameLayout(s string) (NameLayout, error) {
	l := NameLayout(strings.ToLower(strings.TrimSpace(s)))
	if l == "" {
		return DefaultNameLayout, nil
	}
	if !l.Valid() {
		return "", fmt.Errorf("use title-author, author-title or title-paren")
	}
	return l, nil
}

// glueBytes is what the layout adds around the authors, for the length budget.
func (l NameLayout) glueBytes() int {
	if l == LayoutTitleParen {
		return len(" ()")
	}
	return len(" - ")
}

// join puts the title and the authors (not empty) together.
func (l NameLayout) join(title, authors string) string {
	switch l {
	case LayoutAuthorTitle:
		return authors + " - " + title
	case LayoutTitleParen:
		return title + " (" + authors + ")"
	}
	return title + " - " + authors
}
