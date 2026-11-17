// Package ebookmeta reads and writes the metadata of e-book files (EPUB,
// FB2, DOCX, MOBI/AZW3 and PDF) using only the standard library. Formats are
// detected from file contents, not extensions.
package ebookmeta

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Metadata is the format-neutral view of a book's metadata. Every string is
// cleaned of control characters when read, so it is safe to print.
type Metadata struct {
	Title       string       `json:"title,omitempty"`
	TitleSort   string       `json:"title_sort,omitempty"`
	Authors     []string     `json:"authors,omitempty"`
	AuthorSort  string       `json:"author_sort,omitempty"`
	Publisher   string       `json:"publisher,omitempty"`
	Languages   []string     `json:"languages,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Series      string       `json:"series,omitempty"`
	SeriesIndex float64      `json:"series_index,omitempty"`
	Identifiers []Identifier `json:"identifiers,omitempty"`
	Published   string       `json:"published,omitempty"`
	Description string       `json:"description,omitempty"`
	// Rating is 0 (unset) to 5 stars; halves are allowed.
	Rating float64 `json:"rating,omitempty"`
	// Cover reports the embedded cover's media type, or "" when the file
	// has none or the format cannot carry one.
	Cover string `json:"cover,omitempty"`
	// UniqueScheme is the scheme of the identifier an EPUB package names as its
	// unique identifier. The package must keep one, so that identifier can be
	// given a new value but not removed. It is "" for other formats.
	UniqueScheme string `json:"-"`
}

// Identifier is a typed book identifier such as isbn:9780000000000.
type Identifier struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
}

// Identifier returns the value for scheme (case-insensitive) or "".
func (m *Metadata) Identifier(scheme string) string {
	for _, id := range m.Identifiers {
		if strings.EqualFold(id.Scheme, scheme) {
			return id.Value
		}
	}
	return ""
}

// SetIdentifier replaces scheme's value, or removes it when value is empty.
func (m *Metadata) SetIdentifier(scheme, value string) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	out := m.Identifiers[:0:0]
	for _, id := range m.Identifiers {
		if !strings.EqualFold(id.Scheme, scheme) {
			out = append(out, id)
		}
	}
	if value != "" {
		out = append(out, Identifier{Scheme: scheme, Value: value})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Scheme < out[j].Scheme })
	m.Identifiers = out
}

// Cover is image data to embed as a book's cover.
type Cover struct {
	Data      []byte
	MediaType string // image/jpeg, image/png, image/gif or image/webp
}

// Changes lists the fields to update. A nil field is left alone; a non-nil
// field replaces the old value, and an empty value clears it.
type Changes struct {
	Title       *string
	TitleSort   *string
	Authors     *[]string
	AuthorSort  *string
	Publisher   *string
	Languages   *[]string
	Tags        *[]string
	Series      *string
	SeriesIndex *float64
	// Identifiers maps a scheme to its new value; "" removes the scheme.
	Identifiers map[string]string
	Published   *string
	Description *string
	Rating      *float64
	Cover       *Cover
}

// Empty reports whether c changes nothing.
func (c *Changes) Empty() bool {
	return c.Title == nil && c.TitleSort == nil && c.Authors == nil &&
		c.AuthorSort == nil && c.Publisher == nil && c.Languages == nil &&
		c.Tags == nil && c.Series == nil && c.SeriesIndex == nil &&
		len(c.Identifiers) == 0 && c.Published == nil &&
		c.Description == nil && c.Rating == nil && c.Cover == nil
}

// Apply copies the set fields of c into m. Cover is not applied here; each
// format embeds the image itself.
func (c *Changes) Apply(m *Metadata) {
	setStr := func(dst *string, src *string) {
		if src != nil {
			*dst = cleanString(*src)
		}
	}
	setList := func(dst *[]string, src *[]string) {
		if src != nil {
			*dst = cleanList(*src)
		}
	}
	// A sort value describes the old title or authors; drop it when they
	// change unless a new one is given.
	if c.Title != nil && c.TitleSort == nil {
		m.TitleSort = ""
	}
	if c.Authors != nil && c.AuthorSort == nil {
		m.AuthorSort = ""
	}
	setStr(&m.Title, c.Title)
	setStr(&m.TitleSort, c.TitleSort)
	setList(&m.Authors, c.Authors)
	setStr(&m.AuthorSort, c.AuthorSort)
	setStr(&m.Publisher, c.Publisher)
	setList(&m.Languages, c.Languages)
	setList(&m.Tags, c.Tags)
	setStr(&m.Series, c.Series)
	if c.SeriesIndex != nil {
		m.SeriesIndex = *c.SeriesIndex
	}
	schemes := make([]string, 0, len(c.Identifiers))
	for s := range c.Identifiers {
		schemes = append(schemes, s)
	}
	sort.Strings(schemes)
	for _, s := range schemes {
		m.SetIdentifier(cleanString(s), cleanString(c.Identifiers[s]))
	}
	setStr(&m.Published, c.Published)
	if c.Description != nil {
		m.Description = cleanText(*c.Description)
	}
	if c.Rating != nil {
		m.Rating = clampRating(*c.Rating)
	}
}

// clean normalizes every field of m after a format reader filled it.
func (m *Metadata) clean() {
	m.Title = cleanString(m.Title)
	m.TitleSort = cleanString(m.TitleSort)
	m.Authors = cleanList(m.Authors)
	m.AuthorSort = cleanString(m.AuthorSort)
	m.Publisher = cleanString(m.Publisher)
	m.Languages = cleanList(m.Languages)
	m.Tags = cleanList(m.Tags)
	m.Series = cleanString(m.Series)
	if m.SeriesIndex < 0 {
		m.SeriesIndex = 0
	}
	ids := m.Identifiers
	m.Identifiers = nil
	for _, id := range ids {
		scheme, value := cleanString(id.Scheme), cleanString(id.Value)
		if scheme != "" && value != "" && m.Identifier(scheme) == "" {
			m.SetIdentifier(scheme, value)
		}
	}
	m.Published = cleanString(m.Published)
	m.Description = cleanText(m.Description)
	m.Rating = clampRating(m.Rating)
}

func clampRating(r float64) float64 {
	if r != r || r < 0 { // NaN or negative
		return 0
	}
	if r > 5 {
		return 5
	}
	return float64(int(r*2+0.5)) / 2
}

// cleanString drops control and bidi formatting characters and collapses
// whitespace runs, for one-line fields. strings.Map also replaces invalid
// UTF-8, which an 8-bit terminal could read as a C1 control.
func cleanString(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		case unicode.IsControl(r) || isBidiControl(r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// cleanText is cleanString for multi-line text: newlines survive.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case unicode.IsControl(r) || isBidiControl(r):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = cleanString(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func isBidiControl(r rune) bool {
	return r == '‎' || r == '‏' || r == '؜' ||
		(r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩')
}

// formatIndex renders a series index without a trailing ".0".
func formatIndex(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseIndex reads a series index, returning 0 for anything unusable.
func parseIndex(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f != f || f < 0 || f > 1e6 {
		return 0
	}
	return f
}

// CleanLine makes text from an untrusted source fit for a one-line field:
// control and bidirectional formatting characters are dropped and whitespace
// runs collapse.
func CleanLine(s string) string { return cleanString(s) }

// CleanText is CleanLine for multi-line text such as a description.
func CleanText(s string) string { return cleanText(s) }
