package metasource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	openLibraryBase = "https://openlibrary.org"
	sourceOL        = "Open Library"
)

// OpenLibrary queries openlibrary.org. A lookup by ISBN returns that
// edition; a lookup by title and author returns works, which say nothing
// about the edition, so those candidates carry no ISBN, publisher or
// edition-specific date.
type OpenLibrary struct {
	base   string
	client *http.Client
	limit  int // the lookup's result limit; 0 is the default
}

// openLibraryMaxResults is the most works one Open Library search returns here.
const openLibraryMaxResults = 100

// NewOpenLibrary returns the source for rt (nil for the default transport).
func NewOpenLibrary(rt http.RoundTripper) *OpenLibrary {
	return newOpenLibrary(openLibraryBase, rt)
}

func newOpenLibrary(base string, rt http.RoundTripper) *OpenLibrary {
	u, _ := url.Parse(base)
	return &OpenLibrary{base: base, client: newHTTPClient(rt, 20*time.Second, hostPolicy{strings.ToLower(u.Host)}, u.Scheme)}
}

func (*OpenLibrary) Name() string { return sourceOL }

func (s *OpenLibrary) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	if len(q.ISBNs) > 0 {
		out, err := s.byISBN(ctx, q.ISBNs)
		if err != nil || len(out) > 0 {
			return out, err
		}
	}
	if len(foldText(q.Title)) > 0 {
		return s.byTitle(ctx, q)
	}
	return nil, nil
}

type olEdition struct {
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Key         string `json:"key"`
	PublishDate string `json:"publish_date"`
	Authors     []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Publishers []struct {
		Name string `json:"name"`
	} `json:"publishers"`
	Subjects []struct {
		Name string `json:"name"`
	} `json:"subjects"`
	Identifiers struct {
		ISBN10      []string `json:"isbn_10"`
		ISBN13      []string `json:"isbn_13"`
		OpenLibrary []string `json:"openlibrary"`
		Goodreads   []string `json:"goodreads"`
	} `json:"identifiers"`
	Cover struct {
		Large  string `json:"large"`
		Medium string `json:"medium"`
	} `json:"cover"`
}

func (s *OpenLibrary) byISBN(ctx context.Context, isbns []string) ([]Candidate, error) {
	keys := make([]string, len(isbns))
	for i, isbn := range isbns {
		keys[i] = "ISBN:" + isbn
	}
	u := s.base + "/api/books?" + url.Values{
		"bibkeys": {strings.Join(keys, ",")}, "format": {"json"}, "jscmd": {"data"},
	}.Encode()
	var data map[string]olEdition
	if err := get(ctx, s.client, u, &data); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Candidate
	for _, key := range keys { // a map has no order; keep the query's
		e, ok := data[key]
		if !ok || strings.TrimSpace(e.Title) == "" {
			continue
		}
		c := Candidate{
			EditionLevel: true,
			Title:        clean(e.Title),
			Published:    normalizeDate(e.PublishDate),
			CoverURL:     firstNonEmpty(e.Cover.Large, e.Cover.Medium),
			coverRank:    0,
		}
		if sub := clean(e.Subtitle); sub != "" {
			c.Title += ": " + sub
		}
		for _, a := range e.Authors {
			c.Authors = append(c.Authors, a.Name)
		}
		if len(e.Publishers) > 0 {
			c.Publisher = clean(e.Publishers[0].Name)
		}
		for _, t := range e.Subjects {
			c.Tags = append(c.Tags, t.Name)
		}
		c.Authors, c.Tags = cleanList(c.Authors), cleanList(c.Tags)

		// The edition was found by this ISBN, so it is this edition's.
		c.Identifiers = appendIdentifier(c.Identifiers, "isbn", NormalizeISBN(strings.TrimPrefix(key, "ISBN:")))
		for _, id := range e.Identifiers.OpenLibrary {
			c.Identifiers = appendIdentifier(c.Identifiers, "openlibrary", clean(id))
			break
		}
		out = append(out, c)
	}
	return out, nil
}

type olSearch struct {
	Docs []struct {
		Key       string   `json:"key"`
		Title     string   `json:"title"`
		Subtitle  string   `json:"subtitle"`
		Authors   []string `json:"author_name"`
		FirstYear int      `json:"first_publish_year"`
		Subjects  []string `json:"subject"`
		Language  []string `json:"language"`
		CoverID   int      `json:"cover_i"`
	} `json:"docs"`
}

func (s *OpenLibrary) byTitle(ctx context.Context, q Query) ([]Candidate, error) {
	v := url.Values{
		"title":  {mainTitle(q.Title)},
		"limit":  {strconv.Itoa(fetchCount(s.limit, openLibraryMaxResults))},
		"fields": {"key,title,subtitle,author_name,first_publish_year,subject,language,cover_i"},
	}
	if len(q.Authors) > 0 {
		if k := personKey(q.Authors[0]); k != "" {
			v.Set("author", cleanAuthorForQuery(q.Authors[0]))
		}
	}
	var res olSearch
	if err := get(ctx, s.client, s.base+"/search.json?"+v.Encode(), &res); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Candidate
	for _, d := range res.Docs {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		c := Candidate{Title: clean(d.Title), Authors: cleanList(d.Authors), Tags: cleanList(d.Subjects), coverRank: 0}
		if sub := clean(d.Subtitle); sub != "" {
			c.Title += ": " + sub
		}
		if d.FirstYear > 0 {
			c.Published = strconv.Itoa(d.FirstYear)
		}
		if len(d.Language) == 1 {
			if l := normalizeLanguage(d.Language[0]); l != "" {
				c.Languages = []string{l}
			}
		}
		if d.CoverID > 0 {
			c.CoverURL = "https://covers.openlibrary.org/b/id/" + strconv.Itoa(d.CoverID) + "-L.jpg?default=false"
		}
		if id := strings.TrimPrefix(d.Key, "/works/"); id != "" && id != d.Key {
			c.Identifiers = appendIdentifier(c.Identifiers, "openlibrary-work", clean(id))
		}
		out = append(out, c)
	}
	return out, nil
}

// cleanAuthorForQuery drops tags such as "[美]" and role words, which a
// catalogue does not hold, and puts a "Last, First" name in reading order.
func cleanAuthorForQuery(name string) string {
	name = bracketed.ReplaceAllString(name, " ")
	name = roleSuffixes.ReplaceAllString(strings.TrimSpace(name), "")
	if before, after, ok := strings.Cut(name, ","); ok && !strings.Contains(after, ",") {
		name = after + " " + before
	}
	return strings.Join(strings.Fields(name), " ")
}

func appendIdentifier(ids []ebookmeta.Identifier, scheme, value string) []ebookmeta.Identifier {
	if value == "" {
		return ids
	}
	return append(ids, ebookmeta.Identifier{Scheme: scheme, Value: value})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
