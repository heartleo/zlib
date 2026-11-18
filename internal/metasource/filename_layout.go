package metasource

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// NameLayout is the order in which a file name states the title and the
// authors: one of the presets, or a custom pattern such as "{title} [{authors}]",
// in which {title} and {authors} are replaced and any other text is kept.
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

	tokenTitle   = "{title}"
	tokenAuthors = "{authors}"

	// maxPatternText bounds the text a custom pattern keeps around its fields.
	maxPatternText = 40
)

// NameLayouts lists the presets in the order they are offered.
var NameLayouts = []NameLayout{LayoutTitleAuthor, LayoutAuthorTitle, LayoutTitleParen}

// presetPatterns is each preset written as a pattern.
var presetPatterns = map[NameLayout]string{
	LayoutTitleAuthor: tokenTitle + " - " + tokenAuthors,
	LayoutAuthorTitle: tokenAuthors + " - " + tokenTitle,
	LayoutTitleParen:  tokenTitle + " (" + tokenAuthors + ")",
}

// pattern is the layout as a pattern.
func (l NameLayout) pattern() string {
	if p, ok := presetPatterns[l]; ok {
		return p
	}
	return string(l)
}

// IsPreset reports whether l is one of NameLayouts.
func (l NameLayout) IsPreset() bool {
	_, ok := presetPatterns[l]
	return ok
}

// Label is the layout as a pattern a person reads: "Title - Author" for a
// preset, the pattern itself for a custom layout.
func (l NameLayout) Label() string {
	switch l {
	case LayoutAuthorTitle:
		return "Author - Title"
	case LayoutTitleParen:
		return "Title (Author)"
	case LayoutTitleAuthor:
		return "Title - Author"
	}
	return string(l)
}

// Valid reports whether l can build names: a preset or a good custom pattern.
func (l NameLayout) Valid() bool {
	return l.IsPreset() || checkPattern(string(l)) == nil
}

// titleFirst reports whether the layout writes the title before the authors.
func (l NameLayout) titleFirst() bool {
	p := l.pattern()
	t, a := strings.Index(p, tokenTitle), strings.Index(p, tokenAuthors)
	return t >= 0 && (a < 0 || t < a)
}

// dashTitleFirst reports whether the layout writes a dashed name with the title
// first, which is how an unexplained "A - B" is read for its user.
func (l NameLayout) dashTitleFirst() bool {
	return strings.Contains(l.pattern(), " - ") && l.titleFirst()
}

// usesAuthors reports whether the layout names the authors at all.
func (l NameLayout) usesAuthors() bool { return strings.Contains(l.pattern(), tokenAuthors) }

// ParseNameLayout reads a layout: a preset name, in any case, or a custom
// pattern. An empty one is DefaultNameLayout. A pattern that spells a preset is
// that preset.
func ParseNameLayout(s string) (NameLayout, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultNameLayout, nil
	}
	if l := NameLayout(strings.ToLower(s)); l.IsPreset() {
		return l, nil
	}
	if !strings.Contains(s, "{") && !strings.Contains(s, "}") {
		return "", errors.New("use title-author, author-title or title-paren, or a pattern with {title} and {authors}")
	}
	if err := checkPattern(s); err != nil {
		return "", err
	}
	for l, p := range presetPatterns {
		if s == p {
			return l, nil
		}
	}
	return NameLayout(s), nil
}

// checkPattern says why a custom pattern cannot be used, or nil. It needs
// {title} once, may have {authors} once, and keeps only short plain text between
// them: none of the characters a file name cannot hold.
func checkPattern(p string) error {
	if strings.Count(p, tokenTitle) != 1 {
		return fmt.Errorf("a pattern needs %s once", tokenTitle)
	}
	if strings.Count(p, tokenAuthors) > 1 {
		return fmt.Errorf("use %s once at most", tokenAuthors)
	}
	text := strings.NewReplacer(tokenTitle, "", tokenAuthors, "").Replace(p)
	if strings.ContainsAny(text, "{}") {
		return fmt.Errorf("only %s and %s can be written in braces", tokenTitle, tokenAuthors)
	}
	if strings.ContainsAny(text, `/\:"<>|?*`) {
		return errors.New(`the text around the fields cannot contain / \ : " < > | ? *`)
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return errors.New("the text around the fields cannot contain control characters")
		}
	}
	if len([]rune(text)) > maxPatternText {
		return fmt.Errorf("keep the text around the fields to %d characters", maxPatternText)
	}
	return nil
}

// glueBytes is what the layout adds around the authors, for the length budget.
func (l NameLayout) glueBytes() int {
	return len(strings.NewReplacer(tokenTitle, "", tokenAuthors, "").Replace(l.pattern()))
}

// join puts the title and the authors (not empty) together.
func (l NameLayout) join(title, authors string) string {
	return strings.NewReplacer(tokenTitle, title, tokenAuthors, authors).Replace(l.pattern())
}
