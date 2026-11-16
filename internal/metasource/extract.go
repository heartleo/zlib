package metasource

import (
	"regexp"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// Hint is what is known about a book from outside the file: the Z-Library
// record of a download.
type Hint struct {
	Title   string
	Authors []string
	ISBNs   []string // any form; an invalid one is ignored
}

// Embedded is the useful part of what a file carries: its own title, authors
// and ISBNs, and what it says about its edition. Placeholders such as
// "Unknown" are left out.
type Embedded struct {
	Title     string
	Authors   []string
	ISBNs     []string // the file's own, valid
	Publisher string
	Year      string
	Language  string
}

// Extraction is what is known about a book before it is searched, read from
// its file name first and then from what the file carries, and the query made
// of it. Each part of the query says where it came from.
type Extraction struct {
	FileName FileGuess
	Embedded Embedded
	Query    Query
	// TitleFrom and AuthorsFrom are "file name", "embedded" or "download
	// record", or "" when there is none.
	TitleFrom, AuthorsFrom string
	// AltTitle is the other title, when the name and the file offer two that
	// differ: the one the query did not use.
	AltTitle string
	// ISBNFrom says where each of Query.ISBNs came from, in the same order:
	// "embedded", "recorded by a download", "copyright page", "download
	// record" or "file name".
	ISBNFrom []string
}

// Extract builds the query for a book. The file name is read first: a Z-Library
// name is cleaner than the title inside the file, which often carries a
// marketing line or a series name. What the file itself says then fills what
// the name lacks and supplies the ISBN, the publisher and the year. scanned
// are ISBNs read from the book's text, and hint is the download's record.
//
// Only the file's own ISBN is confirmed. Every other ISBN is a hint, and a
// title from the name or the record never confirms one (Query.TitleGuessed),
// unless the file's own title agrees with it.
func Extract(fileName string, m *ebookmeta.Metadata, scanned []string, hint Hint) Extraction {
	x := Extraction{FileName: GuessFromFileName(fileName)}
	if m == nil {
		m = &ebookmeta.Metadata{}
	}
	x.Embedded = embeddedOf(m)
	fg, emb := x.FileName, x.Embedded
	fileTitle := ""
	if UsefulTitle(fg.Title) {
		fileTitle = fg.Title
	}
	hintTitle := ""
	if UsefulTitle(hint.Title) {
		hintTitle = clean(hint.Title)
	}

	// The title: a structured name first, then the file's own, then any name.
	var title string
	switch {
	case fg.Structured && fileTitle != "":
		title, x.TitleFrom = fileTitle, "file name"
	case emb.Title != "":
		title, x.TitleFrom = emb.Title, "embedded"
	case fileTitle != "":
		title, x.TitleFrom = fileTitle, "file name"
	case hintTitle != "":
		title, x.TitleFrom = hintTitle, "download record"
	}
	// The other title is on offer whenever its text differs, even when it only
	// adds a marketing line: the user may prefer it.
	switch x.TitleFrom {
	case "file name":
		if emb.Title != "" && !sameFolded(title, emb.Title) {
			x.AltTitle = emb.Title
		}
	case "embedded":
		if fileTitle != "" && !sameFolded(title, fileTitle) {
			x.AltTitle = fileTitle
		}
	}
	guessed := x.TitleFrom == "file name" || x.TitleFrom == "download record"
	if guessed && emb.Title != "" && titlesAgree(title, emb.Title) {
		guessed = false // the file's own title says the same
	}

	authors, from := pickAuthors(hasHan(title), fg.Authors, emb.Authors)
	if len(authors) == 0 && len(hint.Authors) > 0 {
		authors, from = cleanList(hint.Authors), "download record"
	}
	x.AuthorsFrom = from

	q := Query{Title: title, Authors: authors, TitleGuessed: guessed, Publisher: emb.Publisher, Year: emb.Year}
	add := func(isbn, source string, confirmed bool) {
		if isbn == "" || containsFold(q.ISBNs, isbn) {
			return
		}
		q.ISBNs = append(q.ISBNs, isbn)
		x.ISBNFrom = append(x.ISBNFrom, source)
		if !confirmed {
			q.Unconfirmed = append(q.Unconfirmed, isbn)
		}
	}
	add(NormalizeISBN(m.Identifier("isbn")), "embedded", true)
	add(NormalizeISBN(m.Identifier(SchemeZlibISBN)), "recorded by a download", false)
	for _, isbn := range scanned {
		add(NormalizeISBN(isbn), "copyright page", false)
	}
	for _, isbn := range hint.ISBNs {
		add(NormalizeISBN(isbn), "download record", false)
	}
	for _, isbn := range fg.ISBNs {
		add(isbn, "file name", false)
	}
	x.Query = q
	return x
}

// embeddedOf reads what is useful in a file's metadata. A placeholder title or
// author is dropped, authors are cleaned and split, and the year is the first
// four digits of the date.
func embeddedOf(m *ebookmeta.Metadata) Embedded {
	e := Embedded{Publisher: clean(m.Publisher)}
	if t := clean(m.Title); UsefulTitle(t) {
		e.Title = t
	}
	for _, a := range m.Authors {
		for _, name := range splitEmbeddedAuthor(a) {
			if !junkTitles[strings.ToLower(name)] && !containsFold(e.Authors, name) {
				e.Authors = append(e.Authors, name)
			}
		}
	}
	if isbn := NormalizeISBN(m.Identifier("isbn")); isbn != "" {
		e.ISBNs = []string{isbn}
	}
	if d := normalizeDate(m.Published); len(d) >= 4 {
		e.Year = d[:4]
	}
	if len(m.Languages) > 0 {
		e.Language = m.Languages[0]
	}
	return e
}

var embeddedAuthorSplitRe = regexp.MustCompile(`(?i)\s+and\s+|\s*[&、;；]\s*|\s{2,}`)

// splitEmbeddedAuthor cleans one embedded author value and splits it where it
// names several people ("A and B", "A & B"). A comma is kept: "Herbert, Frank"
// is one person.
func splitEmbeddedAuthor(s string) []string {
	var out []string
	for _, part := range embeddedAuthorSplitRe.Split(bracketed.ReplaceAllString(s, " "), -1) {
		if name := tidyAuthor(clean(part)); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// pickAuthors chooses between the authors a file name lists and the file's own.
// The file's own are cleaner when both name someone in common: a name often
// adds the translator. When the title is Chinese and only one side writes
// names in Chinese, that side is used, since that is how the Chinese sources
// list them. Otherwise the name's authors stand, as the file's may be
// unrelated.
func pickAuthors(titleHan bool, file, emb []string) ([]string, string) {
	switch {
	case len(file) == 0 && len(emb) == 0:
		return nil, ""
	case len(emb) == 0:
		return file, "file name"
	case len(file) == 0:
		return emb, "embedded"
	}
	if titleHan {
		switch fh, eh := anyHan(file), anyHan(emb); {
		case eh && !fh:
			return emb, "embedded"
		case fh && !eh:
			return file, "file name"
		}
	}
	if sharePerson(file, emb) {
		return emb, "embedded"
	}
	return file, "file name"
}

func anyHan(names []string) bool {
	for _, n := range names {
		if hasHan(n) {
			return true
		}
	}
	return false
}

// sharePerson reports whether two lists name the same person: the folded
// names are equal, or one holds the other (a full name against a surname).
func sharePerson(a, b []string) bool {
	for _, x := range a {
		kx := personKey(x)
		for _, y := range b {
			ky := personKey(y)
			if kx == "" || ky == "" {
				continue
			}
			if kx == ky || (len([]rune(kx)) >= 2 && len([]rune(ky)) >= 2 && (strings.Contains(kx, ky) || strings.Contains(ky, kx))) {
				return true
			}
		}
	}
	return false
}

// sameFolded reports whether two titles are the same text once case,
// punctuation and spaces are ignored.
func sameFolded(a, b string) bool { return string(foldText(a)) == string(foldText(b)) }

// titlesAgree reports whether two titles name the same book: they are close
// as titles, or one holds the other once punctuation is ignored, as a title
// with a marketing line after it holds the plain one.
func titlesAgree(a, b string) bool {
	if titleSimilarity(a, b) >= hintTitleSimilarity {
		return true
	}
	fa, fb := string(foldText(a)), string(foldText(b))
	if len([]rune(fa)) < 2 || len([]rune(fb)) < 2 {
		return fa != "" && fa == fb
	}
	return strings.Contains(fa, fb) || strings.Contains(fb, fa)
}
