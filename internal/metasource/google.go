package metasource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	googleBase   = "https://www.googleapis.com"
	sourceGoogle = "Google Books"
)

// GoogleBooks queries the Google Books v1 API without a key. Without one the
// quota is shared and a request may be refused with ErrRateLimited, which a
// lookup reports for this source only.
type GoogleBooks struct {
	base   string
	client *http.Client
}

// NewGoogleBooks returns the source for rt (nil for the default transport).
func NewGoogleBooks(rt http.RoundTripper) *GoogleBooks {
	return newGoogleBooks(googleBase, rt)
}

func newGoogleBooks(base string, rt http.RoundTripper) *GoogleBooks {
	u, _ := url.Parse(base)
	return &GoogleBooks{base: base, client: newHTTPClient(rt, 20*time.Second, hostPolicy{strings.ToLower(u.Host)}, u.Scheme)}
}

func (*GoogleBooks) Name() string { return sourceGoogle }

type gbVolumes struct {
	Items []struct {
		ID         string `json:"id"`
		VolumeInfo struct {
			Title               string   `json:"title"`
			Subtitle            string   `json:"subtitle"`
			Authors             []string `json:"authors"`
			Publisher           string   `json:"publisher"`
			PublishedDate       string   `json:"publishedDate"`
			Description         string   `json:"description"`
			Categories          []string `json:"categories"`
			Language            string   `json:"language"`
			IndustryIdentifiers []struct {
				Type       string `json:"type"`
				Identifier string `json:"identifier"`
			} `json:"industryIdentifiers"`
			ImageLinks struct {
				Thumbnail string `json:"thumbnail"`
			} `json:"imageLinks"`
		} `json:"volumeInfo"`
	} `json:"items"`
}

func (s *GoogleBooks) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	var terms string
	switch {
	case len(q.ISBNs) > 0:
		terms = "isbn:" + q.ISBNs[0]
	case len(foldText(q.Title)) > 0:
		terms = `intitle:"` + strings.ReplaceAll(mainTitle(q.Title), `"`, " ") + `"`
		if len(q.Authors) > 0 {
			if a := cleanAuthorForQuery(q.Authors[0]); a != "" {
				terms += ` inauthor:"` + strings.ReplaceAll(a, `"`, " ") + `"`
			}
		}
	default:
		return nil, nil
	}
	u := s.base + "/books/v1/volumes?" + url.Values{
		"q":          {terms},
		"maxResults": {"10"},
		"printType":  {"books"},
		"fields":     {"items(id,volumeInfo(title,subtitle,authors,publisher,publishedDate,description,categories,language,industryIdentifiers,imageLinks/thumbnail))"},
	}.Encode()
	var res gbVolumes
	if err := get(ctx, s.client, u, &res); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Candidate
	for _, it := range res.Items {
		v := it.VolumeInfo
		if strings.TrimSpace(v.Title) == "" {
			continue
		}
		c := Candidate{
			EditionLevel: true,
			Title:        clean(v.Title),
			Authors:      cleanList(v.Authors),
			Publisher:    clean(v.Publisher),
			Published:    normalizeDate(v.PublishedDate),
			Description:  ebookmeta.CleanText(stripHTML(v.Description)),
			coverRank:    1, // small thumbnails: used only when no other cover exists
		}
		if sub := clean(v.Subtitle); sub != "" {
			c.Title += ": " + sub
		}
		if l := normalizeLanguage(v.Language); l != "" {
			c.Languages = []string{l}
		}
		for _, cat := range v.Categories {
			for _, part := range strings.Split(cat, "/") { // "Fiction / Science Fiction"
				c.Tags = append(c.Tags, part)
			}
		}
		c.Tags = cleanList(c.Tags)
		for _, id := range v.IndustryIdentifiers {
			if strings.HasPrefix(id.Type, "ISBN") && c.ISBN() == "" {
				c.Identifiers = appendIdentifier(c.Identifiers, "isbn", NormalizeISBN(id.Identifier))
			}
		}
		c.Identifiers = appendIdentifier(c.Identifiers, "google", clean(it.ID))
		if t := v.ImageLinks.Thumbnail; strings.HasPrefix(t, "http://") {
			c.CoverURL = "https://" + strings.TrimPrefix(t, "http://")
		} else {
			c.CoverURL = t
		}
		out = append(out, c)
	}
	return out, nil
}
