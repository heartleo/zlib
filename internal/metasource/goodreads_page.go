package metasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// page reads the page of the Goodreads book id and returns the edition it
// describes. It returns a nil candidate and no error when Goodreads has no such
// book, errGoodreadsChallenge when the answer is a bot check or a page that holds
// no book, and ErrRateLimited when Goodreads says to slow down. Every caller
// treats an error as "use the suggestion instead".
func (s *Goodreads) page(ctx context.Context, id string) (*Candidate, error) {
	if !isDigits(id) {
		return nil, nil
	}
	u := s.base + "/book/show/" + id
	var body []byte
	var status int
	for attempt := 0; ; attempt++ {
		b, st, err := fetch(ctx, s.client, u, "text/html", s.uaHeader())
		if err != nil {
			return nil, err
		}
		if attempt == 0 && goodreadsTransient(st) {
			if err := sleepCtx(ctx, goodreadsRetryDelay); err != nil {
				return nil, err
			}
			continue
		}
		body, status = b, st
		break
	}
	switch {
	case status == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case status == http.StatusNotFound:
		return nil, nil
	case status == http.StatusAccepted, status == http.StatusUnauthorized, status == http.StatusForbidden:
		return nil, errGoodreadsChallenge
	case status != http.StatusOK:
		return nil, fmt.Errorf("unexpected HTTP status %d", status)
	}
	return mapGoodreadsPage(body, id)
}

// goodreadsNextData is the part of a book page's embedded data this source reads:
// a flat map of records, which refer to each other by key.
type goodreadsNextData struct {
	Props struct {
		PageProps struct {
			ApolloState map[string]json.RawMessage `json:"apolloState"`
		} `json:"pageProps"`
	} `json:"props"`
}

// goodreadsPageBook is one Book record. A number may arrive as text and a text
// as a number, so the fields that are numbers use flexString.
type goodreadsPageBook struct {
	LegacyID    flexString `json:"legacyId"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ImageURL    string     `json:"imageUrl"`
	Details     struct {
		Publisher       string     `json:"publisher"`
		ISBN            string     `json:"isbn"`
		ISBN13          string     `json:"isbn13"`
		PublicationTime flexString `json:"publicationTime"` // milliseconds since the epoch
		Language        struct {
			Name string `json:"name"`
		} `json:"language"`
	} `json:"details"`
	BookGenres []struct {
		Genre struct {
			Name string `json:"name"`
		} `json:"genre"`
	} `json:"bookGenres"`
	BookSeries []struct {
		UserPosition flexString `json:"userPosition"`
		Series       struct {
			Ref   string `json:"__ref"`
			Title string `json:"title"`
		} `json:"series"`
	} `json:"bookSeries"`
	Primary   goodreadsEdge   `json:"primaryContributorEdge"`
	Secondary []goodreadsEdge `json:"secondaryContributorEdges"`
}

// goodreadsEdge links a book to a contributor and says in what role.
type goodreadsEdge struct {
	Role string `json:"role"`
	Node struct {
		Ref string `json:"__ref"`
	} `json:"node"`
}

// mapGoodreadsPage reads a book page's embedded data and maps the Book record
// whose ID is id to an edition-level candidate.
func mapGoodreadsPage(body []byte, id string) (*Candidate, error) {
	i := bytes.Index(body, []byte(`<script id="__NEXT_DATA__"`))
	if i < 0 {
		return nil, errGoodreadsChallenge // a bot check, or a page of another kind
	}
	rest := body[i:]
	open := bytes.IndexByte(rest, '>')
	if open < 0 {
		return nil, errGoodreadsChallenge
	}
	end := bytes.Index(rest[open:], []byte("</script>"))
	if end < 0 {
		return nil, errGoodreadsChallenge
	}
	var data goodreadsNextData
	if err := json.Unmarshal(rest[open+1:open+end], &data); err != nil {
		return nil, fmt.Errorf("unreadable book page: %w", err)
	}
	state := data.Props.PageProps.ApolloState
	var book *goodreadsPageBook
	for key, raw := range state {
		if !strings.HasPrefix(key, "Book:") {
			continue
		}
		var b goodreadsPageBook
		if json.Unmarshal(raw, &b) == nil && string(b.LegacyID) == id && clean(b.Title) != "" {
			book = &b
			break
		}
	}
	if book == nil {
		return nil, errGoodreadsChallenge // the page holds no such book
	}

	title := clean(goodreadsSeriesSuffix.ReplaceAllString(book.Title, ""))
	lang := goodreadsLanguage(book.Details.Language.Name)
	c := &Candidate{
		EditionLevel: true,
		Title:        title,
		Authors:      goodreadsAuthors(state, book, lang == "zh"),
		Publisher:    clean(book.Details.Publisher),
		Published:    goodreadsDate(string(book.Details.PublicationTime)),
		Description:  ebookmeta.CleanText(stripHTML(book.Description)),
		CoverURL:     goodreadsCoverURL(book.ImageURL),
	}
	if lang != "" {
		c.Languages = []string{lang}
	}
	var genres []string
	for _, g := range book.BookGenres {
		genres = append(genres, g.Genre.Name)
	}
	c.Tags = cleanList(genres)
	if len(book.BookSeries) > 0 {
		first := book.BookSeries[0]
		name := first.Series.Title
		if first.Series.Ref != "" {
			var rec struct {
				Title string `json:"title"`
			}
			if raw, ok := state[first.Series.Ref]; ok && json.Unmarshal(raw, &rec) == nil && rec.Title != "" {
				name = rec.Title
			}
		}
		if name = clean(name); name != "" {
			index, err := strconv.ParseFloat(strings.TrimSpace(string(first.UserPosition)), 64)
			if err != nil || index < 0 || index > 10000 { // a range such as "1-3" is no position
				index = 0
			}
			c.Series, c.SeriesIndex = name, index
		}
	}
	if c.Series == "" { // a book of a series may carry it only in the title
		c.Series, c.SeriesIndex = goodreadsSeries(book.Title)
	}
	if isbn := NormalizeISBN(firstNonEmpty(strings.TrimSpace(book.Details.ISBN13), strings.TrimSpace(book.Details.ISBN))); isbn != "" {
		c.Identifiers = appendIdentifier(c.Identifiers, "isbn", isbn)
	}
	c.Identifiers = appendIdentifier(c.Identifiers, "goodreads", id)
	return c, nil
}

// isAuthorRole reports whether a contributor's role makes them an author. A role
// can name several things at once ("Author/Narrator" on an audiobook), so it is
// read by its parts.
func isAuthorRole(role string) bool {
	for _, part := range strings.FieldsFunc(role, func(r rune) bool { return r == '/' || r == ',' || r == ';' || r == '&' }) {
		if strings.EqualFold(strings.TrimSpace(part), "Author") {
			return true
		}
	}
	return false
}

// goodreadsAuthors are the contributors in the Author role. The translator, the
// narrator and the like are left out. A Chinese edition lists an author's name in
// Latin script and, right after it, the same name in Han script, as two
// contributors of one person: that pair is one author, and the Han name is kept.
// Every other name stays as it is, so a co-author with no Han form is not lost.
// Only a pair that is next to each other counts, and only for an edition in
// Chinese: where Goodreads gives no such shape, or the edition's language is
// missing or wrong, an author may be listed twice, which loses nothing.
func goodreadsAuthors(state map[string]json.RawMessage, b *goodreadsPageBook, chinese bool) []string {
	var names []string
	for i, e := range append([]goodreadsEdge{b.Primary}, b.Secondary...) {
		// The primary contributor with no role is the author; another with none is not.
		if e.Node.Ref == "" || (e.Role == "" && i > 0) || (e.Role != "" && !isAuthorRole(e.Role)) {
			continue
		}
		var rec struct {
			Name string `json:"name"`
		}
		if raw, ok := state[e.Node.Ref]; ok && json.Unmarshal(raw, &rec) == nil {
			if name := cleanAuthorForQuery(clean(rec.Name)); name != "" {
				names = append(names, name)
			}
		}
	}
	names = cleanList(names)
	if !chinese {
		return names
	}
	out := names[:0:0]
	for i, n := range names {
		if !hasHan(n) && i+1 < len(names) && hasHan(names[i+1]) {
			continue // the Latin form of the Han name that follows
		}
		out = append(out, n)
	}
	return out
}

// goodreadsDate writes a publication time given in milliseconds since the epoch
// as a date in UTC, or "" when it is not one.
func goodreadsDate(ms string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(ms), 64)
	if err != nil || f <= 0 {
		return ""
	}
	t := time.UnixMilli(int64(f)).UTC()
	if t.Year() < 1000 || t.Year() > time.Now().Year()+5 {
		return ""
	}
	return t.Format("2006-01-02")
}

// goodreadsLanguages maps the English language names Goodreads uses to codes. A
// name that is not here gives no language: it is not guessed.
var goodreadsLanguages = map[string]string{
	"english": "en", "chinese": "zh", "french": "fr", "german": "de", "spanish": "es", "italian": "it",
	"portuguese": "pt", "russian": "ru", "japanese": "ja", "korean": "ko", "arabic": "ar", "dutch": "nl",
	"swedish": "sv", "norwegian": "no", "danish": "da", "finnish": "fi", "polish": "pl", "turkish": "tr",
	"greek": "el", "hebrew": "he", "hindi": "hi", "czech": "cs", "hungarian": "hu", "latin": "la",
	"ukrainian": "uk", "vietnamese": "vi", "thai": "th", "indonesian": "id",
}

func goodreadsLanguage(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if code, ok := goodreadsLanguages[name]; ok {
		return code
	}
	if strings.Contains(name, "chinese") { // "Simplified Chinese" and the like
		return "zh"
	}
	return ""
}
