package metasource

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// NoteEdited marks a row whose new value the user typed in place of the
// catalogue's.
const NoteEdited = "edited"

// ErrNotEditable means a change has no value that can be edited by hand.
var ErrNotEditable = errors.New("this field cannot be edited here")

// Editable reports whether the change's value can be typed: every field the
// metadata editor shows except the cover (an image, chosen by path) and the
// catalogue identifiers other than the ISBN.
func (c Change) Editable() bool {
	switch c.Field {
	case FieldTitle, FieldAuthors, FieldPublisher, FieldLanguages, FieldTags, FieldPublished, "identifier:isbn",
		FieldSeries, FieldDescription, FieldTitleSort, FieldAuthorSort, FieldRating:
		return true
	}
	return false
}

// Edited reports whether the new value was typed by the user.
func (c Change) Edited() bool { return c.edited }

var (
	typedDateRe   = regexp.MustCompile(`^(\d{4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?$`)
	typedDateCnRe = regexp.MustCompile(`^(\d{4})年(?:(\d{1,2})月(?:(\d{1,2})[日号])?)?$`)
)

// parseTypedDate reads a date the user typed: a year, a year and month, or a
// full date, with "-" or the Chinese year, month and day characters. Unlike
// normalizeDate, which makes the best of what a catalogue sends, it accepts
// only text that is entirely a real date, so nothing the user typed is
// quietly dropped or invented.
func parseTypedDate(s string) (string, bool) {
	m := typedDateRe.FindStringSubmatch(s)
	if m == nil {
		m = typedDateCnRe.FindStringSubmatch(s)
	}
	if m == nil {
		return "", false
	}
	date := joinDate(m[1], m[2], m[3])
	// joinDate drops an impossible part; here that means the text was not a date.
	parts := 1
	for _, p := range m[2:] {
		if p != "" {
			parts++
		}
	}
	if date == "" || strings.Count(date, "-")+1 != parts {
		return "", false
	}
	return date, true
}

// Edit returns c with its new value replaced by text, read the way the field
// is written: authors separated by "&", languages and tags by commas, a date
// as a year, month or day, and an ISBN as one valid ISBN. Typed text is cleaned
// of control characters, and a value that does not read as the field's kind is
// refused with a message that says what is expected.
//
// Choosing a value by hand is an explicit decision, so the edited row starts
// selected and is marked NoteEdited. c itself is left as it was, so a failed
// or repeated edit starts from what the catalogue said.
func (c Change) Edit(text string) (Change, error) {
	if !c.Editable() {
		return c, ErrNotEditable
	}
	var shown string
	var apply func(*ebookmeta.Changes)
	switch c.Field {
	case FieldTitle:
		v := clean(text)
		if v == "" {
			return c, errors.New("The title cannot be empty")
		}
		shown, apply = v, func(ch *ebookmeta.Changes) { ch.Title = &v }
	case FieldPublisher:
		v := clean(text)
		if v == "" {
			return c, errors.New("The publisher cannot be empty")
		}
		shown, apply = v, func(ch *ebookmeta.Changes) { ch.Publisher = &v }
	case FieldAuthors:
		v := cleanList(strings.Split(text, "&"))
		if len(v) == 0 {
			return c, errors.New("Enter at least one author, separated by &")
		}
		shown, apply = strings.Join(v, " & "), func(ch *ebookmeta.Changes) { ch.Authors = &v }
	case FieldLanguages:
		var v []string
		for part := range strings.SplitSeq(text, ",") {
			if part = clean(part); part == "" {
				continue
			}
			code := normalizeLanguage(part)
			if code == "" {
				return c, fmt.Errorf("%q is not a language code: use codes such as en, zh or fr", part)
			}
			if !slices.Contains(v, code) {
				v = append(v, code)
			}
		}
		if len(v) == 0 {
			return c, errors.New("Enter at least one language code, such as en or zh")
		}
		shown, apply = strings.Join(v, ", "), func(ch *ebookmeta.Changes) { ch.Languages = &v }
	case FieldTags:
		v := cleanList(strings.Split(text, ","))
		if len(v) == 0 {
			return c, errors.New("Enter at least one tag, separated by commas")
		}
		shown, apply = strings.Join(v, ", "), func(ch *ebookmeta.Changes) { ch.Tags = &v }
	case FieldTitleSort, FieldAuthorSort:
		v := clean(text)
		if v == "" {
			return c, errors.New("Enter a value, or press x to clear the field")
		}
		f := c.Field
		shown, apply = v, func(ch *ebookmeta.Changes) {
			if f == FieldTitleSort {
				ch.TitleSort = &v
			} else {
				ch.AuthorSort = &v
			}
		}
	case FieldRating:
		v, err := strconv.ParseFloat(strings.TrimSpace(clean(text)), 64)
		if err != nil || v < 0 || v > 5 || v*2 != float64(int(v*2)) {
			return c, errors.New("The rating is 0 to 5 stars, in halves, such as 4 or 4.5")
		}
		shown, apply = strconv.FormatFloat(v, 'f', -1, 64), func(ch *ebookmeta.Changes) { ch.Rating = &v }
	case FieldSeries:
		name, index, err := parseSeries(text)
		if err != nil {
			return c, err
		}
		shown = seriesText(name, index)
		apply = func(ch *ebookmeta.Changes) { ch.Series, ch.SeriesIndex = &name, &index }
	case FieldDescription:
		// A description keeps its paragraphs: each line is cleaned, blank runs
		// collapse to one empty line.
		v := ebookmeta.CleanText(text)
		if strings.TrimSpace(v) == "" {
			return c, errors.New("The description cannot be empty: press x to clear it instead")
		}
		shown, apply = v, func(ch *ebookmeta.Changes) { ch.Description = &v }
	case FieldPublished:
		v, ok := parseTypedDate(clean(text))
		if !ok {
			// An ISO 8601 date and time, as Calibre writes it, reads as its date.
			if d := normalizeDate(clean(text)); d != "" && strings.HasPrefix(clean(text), d+"T") {
				v, ok = d, true
			}
		}
		if !ok {
			return c, errors.New("The date must look like 2008, 2008-01 or 2008-01-05, and be a real date")
		}
		shown, apply = v, func(ch *ebookmeta.Changes) { ch.Published = &v }
	case "identifier:isbn":
		v := NormalizeISBN(clean(text))
		if v == "" {
			return c, errors.New("That is not a valid ISBN: check the digits and the check digit")
		}
		shown = v
		apply = func(ch *ebookmeta.Changes) {
			if ch.Identifiers == nil {
				ch.Identifiers = map[string]string{}
			}
			ch.Identifiers["isbn"] = v
		}
	}
	c.New, c.apply, c.Note, c.preselect, c.edited = shown, apply, NoteEdited, true, true
	return c, nil
}
