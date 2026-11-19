package metasource

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// Fields the editor adds to those a match proposes.
const (
	// FieldSeries is a book's series and its position in it, written "Name #2".
	FieldSeries Field = "series"
	// FieldTitleSort and FieldAuthorSort are the forms a library sorts by, such
	// as "Dune Messiah" and "Herbert, Frank".
	FieldTitleSort  Field = "title_sort"
	FieldAuthorSort Field = "author_sort"
	// FieldRating is 0 to 5 stars, halves allowed.
	FieldRating Field = "rating"
)

// EditableFields are the fields the metadata editor shows, in its order: what
// a reader looks at first, then the edition, then the long text and the cover.
var EditableFields = []Field{
	FieldTitle, FieldTitleSort, FieldAuthors, FieldAuthorSort, FieldSeries, FieldPublisher, FieldPublished,
	FieldLanguages, FieldTags, "identifier:isbn", FieldRating, FieldDescription, FieldCover,
}

// FileValue is what cur holds for f, written the way the editor shows and
// edits it: authors joined with " & ", lists with ", ", a series as
// "Name #2". The cover is its media type. It is "" when the field is empty.
func FileValue(cur *ebookmeta.Metadata, f Field) string {
	switch f {
	case FieldTitle:
		return cur.Title
	case FieldTitleSort:
		return cur.TitleSort
	case FieldAuthorSort:
		return cur.AuthorSort
	case FieldRating:
		if cur.Rating <= 0 {
			return ""
		}
		return strconv.FormatFloat(cur.Rating, 'f', -1, 64)
	case FieldAuthors:
		return strings.Join(cur.Authors, " & ")
	case FieldSeries:
		return seriesText(cur.Series, cur.SeriesIndex)
	case FieldPublisher:
		return cur.Publisher
	case FieldPublished:
		return cur.Published
	case FieldLanguages:
		return strings.Join(cur.Languages, ", ")
	case FieldTags:
		return strings.Join(cur.Tags, ", ")
	case FieldDescription:
		return cur.Description
	case FieldCover:
		return cur.Cover
	}
	if scheme, ok := strings.CutPrefix(string(f), "identifier:"); ok {
		return cur.Identifier(scheme)
	}
	return ""
}

// MatchValue is what c says about f, in the same form as FileValue, or "".
// The cover is the catalogue's address for it.
func MatchValue(c Candidate, f Field) string {
	switch f {
	case FieldTitle:
		return c.Title
	case FieldAuthors:
		return strings.Join(c.Authors, " & ")
	case FieldSeries:
		return c.SeriesLabel()
	case FieldPublisher:
		return c.Publisher
	case FieldPublished:
		return c.Published
	case FieldLanguages:
		return strings.Join(c.Languages, ", ")
	case FieldTags:
		return strings.Join(c.Tags, ", ")
	case FieldDescription:
		return c.Description
	case FieldCover:
		return c.CoverURL
	}
	if scheme, ok := strings.CutPrefix(string(f), "identifier:"); ok {
		for _, id := range c.Identifiers {
			if id.Scheme == scheme {
				return id.Value
			}
		}
	}
	return ""
}

func seriesText(name string, index float64) string {
	if name == "" {
		return ""
	}
	if index > 0 {
		return name + " #" + strconv.FormatFloat(index, 'f', -1, 64)
	}
	return name
}

var seriesRe = regexp.MustCompile(`^(.*?)\s*#\s*([0-9]+(?:\.[0-9]+)?)\s*$`)

// parseSeries reads "Name #2" or "Name". The position is optional; a "#" with
// no number after it is part of the name.
func parseSeries(text string) (string, float64, error) {
	text = clean(text)
	if text == "" {
		return "", 0, errors.New("Enter a series name, and #2 for its position if you know it")
	}
	if m := seriesRe.FindStringSubmatch(text); m != nil && strings.TrimSpace(m[1]) != "" {
		n, err := strconv.ParseFloat(m[2], 64)
		if err != nil || n > 10000 {
			return "", 0, errors.New("The series position must be a number such as 2 or 2.5")
		}
		return strings.TrimSpace(m[1]), n, nil
	}
	return text, 0, nil
}

// Clearable reports whether f can be emptied in the editor. A book always
// keeps a title, and a cover can only be replaced.
func Clearable(f Field) bool {
	switch f {
	case FieldTitle, FieldCover:
		return false
	}
	return true
}

// Clear returns a change that empties f in a file that holds old. Clearing is
// an explicit decision, so the change starts selected.
func Clear(f Field, old string) (Change, error) {
	if !Clearable(f) {
		return Change{}, errors.New("This field cannot be cleared")
	}
	empty, none := "", []string{}
	var apply func(*ebookmeta.Changes)
	switch f {
	case FieldAuthors:
		apply = func(c *ebookmeta.Changes) { c.Authors = &none }
	case FieldSeries:
		zero := 0.0
		apply = func(c *ebookmeta.Changes) { c.Series, c.SeriesIndex = &empty, &zero }
	case FieldPublisher:
		apply = func(c *ebookmeta.Changes) { c.Publisher = &empty }
	case FieldTitleSort:
		apply = func(c *ebookmeta.Changes) { c.TitleSort = &empty }
	case FieldAuthorSort:
		apply = func(c *ebookmeta.Changes) { c.AuthorSort = &empty }
	case FieldRating:
		zero := 0.0
		apply = func(c *ebookmeta.Changes) { c.Rating = &zero }
	case FieldPublished:
		apply = func(c *ebookmeta.Changes) { c.Published = &empty }
	case FieldLanguages:
		apply = func(c *ebookmeta.Changes) { c.Languages = &none }
	case FieldTags:
		apply = func(c *ebookmeta.Changes) { c.Tags = &none }
	case FieldDescription:
		apply = func(c *ebookmeta.Changes) { c.Description = &empty }
	default:
		scheme, ok := strings.CutPrefix(string(f), "identifier:")
		if !ok {
			return Change{}, errors.New("This field cannot be cleared")
		}
		apply = func(c *ebookmeta.Changes) {
			if c.Identifiers == nil {
				c.Identifiers = map[string]string{}
			}
			c.Identifiers[scheme] = "" // "" removes the scheme
		}
	}
	return Change{Field: f, Old: old, New: "", apply: apply, preselect: true, edited: true, Note: NoteEdited}, nil
}

// ChangeFromCover is a change that writes cover, chosen by the user: an image
// file they named, or the bytes of a catalogue's cover already downloaded for
// the preview. label says what it is, for the editor's row.
func ChangeFromCover(old, label string, cover *ebookmeta.Cover) Change {
	return Change{Field: FieldCover, Old: old, New: label, Fill: old == "", preselect: true, edited: true, Note: NoteEdited,
		apply: func(c *ebookmeta.Changes) { c.Cover = cover }}
}
