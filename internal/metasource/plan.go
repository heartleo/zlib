package metasource

import (
	"net/url"
	"slices"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// Field names a proposed change. Identifier changes are "identifier:<scheme>".
type Field string

const (
	FieldTitle       Field = "title"
	FieldAuthors     Field = "authors"
	FieldPublisher   Field = "publisher"
	FieldLanguages   Field = "languages"
	FieldTags        Field = "tags"
	FieldPublished   Field = "published"
	FieldDescription Field = "description"
	// FieldCover proposes the candidate's cover image. Its bytes are fetched
	// on demand with FetchCover, so a plan only carries the URL.
	FieldCover Field = "cover"
)

// writableIdentifiers are the schemes a lookup may add. A uuid names one
// particular file and is never taken from a catalogue.
var writableIdentifiers = []string{"isbn", "google", "openlibrary", "goodreads", "douban", "weread"}

// WritableIdentifier reports whether a lookup, or an agent acting on one, may
// write an identifier of this scheme. A uuid names one file and never can.
func WritableIdentifier(scheme string) bool {
	return slices.Contains(writableIdentifiers, strings.ToLower(strings.TrimSpace(scheme)))
}

// Change is one proposed edit to a file's metadata.
type Change struct {
	Field Field
	// Old and New are display text; Old is "" when the field is empty.
	Old, New string
	// Fill is true when the file has no value yet. Replacing what a file
	// already says always needs an explicit decision.
	Fill bool
	// Note warns about the row when the match is not certain: NoteCheckEdition
	// or NoteOtherBook. It is "" for a confirmed match.
	Note string

	preselect bool
	edited    bool
	apply     func(*ebookmeta.Changes)
	coverURL  string
}

// Notes on rows taken from a match that is not certain.
const (
	// NoteCheckEdition marks a field that differs between editions, taken
	// from a match found by title alone: it may describe another edition.
	NoteCheckEdition = "check edition"
	// NoteOtherBook marks every row of a match whose ISBN agrees but whose
	// title does not: it is probably another book.
	NoteOtherBook = "may be another book"
)

// CoverURL is where a FieldCover change gets its image, or "".
func (c Change) CoverURL() string { return c.coverURL }

// DefaultSelected reports whether the change is applied unless the user
// opts out. Only fills are, and only when the match is sure of the book (and,
// for edition-specific fields, of the edition): a title-only match leaves
// publisher, date, language and identifiers unselected, and a match whose
// ISBN agrees but is not confirmed (its title differs, or nothing independent
// vouches for a hint) selects nothing.
func (c Change) DefaultSelected() bool { return c.preselect }

// editionField reports whether a field differs between editions of a book.
func editionField(f Field) bool {
	return f == FieldPublisher || f == FieldPublished || f == FieldLanguages || strings.HasPrefix(string(f), "identifier:")
}

// Plan lists what cand would change in cur. Fields the candidate lacks, and
// fields that already agree, are left out. The file's own unique identifier
// and cover are never part of a plan.
func Plan(cur *ebookmeta.Metadata, cand Candidate) []Change {
	var out []Change
	add := func(f Field, old, new string, fill bool, apply func(*ebookmeta.Changes)) {
		out = append(out, Change{Field: f, Old: old, New: new, Fill: fill, apply: apply})
	}
	same := func(a, b string) bool { return string(foldText(a)) == string(foldText(b)) }

	if cand.Title != "" && !same(cur.Title, cand.Title) {
		t := cand.Title
		add(FieldTitle, cur.Title, t, cur.Title == "", func(c *ebookmeta.Changes) { c.Title = &t })
	}
	if len(cand.Authors) > 0 && !sameList(cur.Authors, cand.Authors, personKey) {
		a := slices.Clone(cand.Authors)
		add(FieldAuthors, strings.Join(cur.Authors, " & "), strings.Join(a, " & "), len(cur.Authors) == 0,
			func(c *ebookmeta.Changes) { c.Authors = &a })
	}
	// A candidate that names the file's own series without a position adds
	// nothing, and must not drop the position the file has.
	curSeries := seriesText(cur.Series, cur.SeriesIndex)
	if cand.Series != "" && !(same(cur.Series, cand.Series) && cand.SeriesIndex == 0) &&
		!same(curSeries, cand.SeriesLabel()) {
		name, index := cand.Series, cand.SeriesIndex
		add(FieldSeries, curSeries, cand.SeriesLabel(), cur.Series == "",
			func(c *ebookmeta.Changes) { c.Series, c.SeriesIndex = &name, &index })
	}
	if cand.Publisher != "" && !same(cur.Publisher, cand.Publisher) {
		p := cand.Publisher
		add(FieldPublisher, cur.Publisher, p, cur.Publisher == "", func(c *ebookmeta.Changes) { c.Publisher = &p })
	}
	if len(cand.Languages) > 0 && !sameList(cur.Languages, cand.Languages, strings.ToLower) {
		l := slices.Clone(cand.Languages)
		add(FieldLanguages, strings.Join(cur.Languages, ", "), strings.Join(l, ", "), len(cur.Languages) == 0,
			func(c *ebookmeta.Changes) { c.Languages = &l })
	}
	if len(cand.Tags) > 0 {
		// Tags are added to, not replaced: the proposal keeps the file's own.
		merged := slices.Clone(cur.Tags)
		for _, t := range cand.Tags {
			if !slices.ContainsFunc(merged, func(m string) bool { return strings.EqualFold(m, t) }) {
				merged = append(merged, t)
			}
		}
		if len(merged) > len(cur.Tags) {
			add(FieldTags, strings.Join(cur.Tags, ", "), strings.Join(merged, ", "), len(cur.Tags) == 0,
				func(c *ebookmeta.Changes) { c.Tags = &merged })
		}
	}
	// A work-level result only knows the year the work first appeared, which
	// says nothing about when this edition was published.
	if cand.Published != "" && cand.EditionLevel && cand.Published != cur.Published && !lessSpecificSame(cur.Published, cand.Published) {
		d := cand.Published
		add(FieldPublished, cur.Published, d, cur.Published == "", func(c *ebookmeta.Changes) { c.Published = &d })
	}
	if cand.Description != "" && !same(cur.Description, cand.Description) {
		d := cand.Description
		add(FieldDescription, cur.Description, d, cur.Description == "", func(c *ebookmeta.Changes) { c.Description = &d })
	}
	if cand.CoverURL != "" {
		host := "a catalogue"
		if u, err := url.Parse(cand.CoverURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		out = append(out, Change{Field: FieldCover, Old: cur.Cover, New: "image from " + host, Fill: cur.Cover == "", coverURL: cand.CoverURL})
	}
	for _, scheme := range writableIdentifiers {
		var value string
		for _, id := range cand.Identifiers {
			if id.Scheme == scheme {
				value = id.Value
			}
		}
		if value == "" || (scheme == "isbn" && !cand.EditionLevel) {
			continue
		}
		old := cur.Identifier(scheme)
		if scheme == "isbn" {
			old = NormalizeISBN(old)
			if old == "" && cur.Identifier("isbn") != "" {
				old = cur.Identifier("isbn") // not a valid ISBN, but the file's own
			}
		}
		if old == value {
			continue
		}
		s, v := scheme, value
		add(Field("identifier:"+scheme), cur.Identifier(scheme), v, old == "", func(c *ebookmeta.Changes) {
			if c.Identifiers == nil {
				c.Identifiers = map[string]string{}
			}
			c.Identifiers[s] = v
		})
	}

	// How sure the match is decides what starts selected. Only an ISBN match
	// whose title agrees is certain; anything else (including a candidate with
	// no match recorded) is treated as a guess. A written guess would also feed
	// later lookups as if it were an authoritative ISBN.
	confirmed := cand.Match == MatchISBN && cand.Score >= 1
	suspect := cand.Match == MatchISBN && cand.Score < 1
	for i := range out {
		ch := &out[i]
		ch.preselect = ch.Fill
		switch {
		case suspect:
			ch.preselect, ch.Note = false, NoteOtherBook
		case !confirmed && editionField(ch.Field):
			ch.preselect, ch.Note = false, NoteCheckEdition
		}
	}
	return out
}

// BuildChanges returns the ebookmeta changes for the selected entries of
// plan. It returns nil when nothing is selected. A cover change is left out:
// its image has to be downloaded first, so the caller fetches it from
// Change.CoverURL and sets Changes.Cover.
func BuildChanges(plan []Change, selected func(Change) bool) *ebookmeta.Changes {
	c := &ebookmeta.Changes{}
	for _, ch := range plan {
		if selected(ch) && ch.apply != nil {
			ch.apply(c)
		}
	}
	if c.Empty() {
		return nil
	}
	return c
}

func sameList(a, b []string, key func(string) string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if key(a[i]) != key(b[i]) {
			return false
		}
	}
	return true
}

// lessSpecificSame reports whether cand is only a less precise form of cur,
// such as "2003" against "2003-08-26".
func lessSpecificSame(cur, cand string) bool {
	return cur != "" && len(cand) < len(cur) && strings.HasPrefix(cur, cand)
}
