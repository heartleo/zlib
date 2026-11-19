package metasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	googleBase   = "https://www.googleapis.com"
	sourceGoogle = "Google Books"
)

// GoogleBooks queries the Google Books v1 API. Without an API key the quota
// is shared and a request may be refused with ErrRateLimited, which a lookup
// reports for this source only. A key gives the caller its own quota.
//
// The key is sent in the x-goog-api-key header, never in the URL, so it does
// not reach logs, error text or a Referer, and the client refuses redirects
// off the Google host, so it cannot be sent anywhere else.
type GoogleBooks struct {
	base   string
	client *http.Client
	key    string
	limit  int // the lookup's result limit; 0 is the default
}

// googleMaxResults is the most volumes one Google Books request returns.
const googleMaxResults = 40

// apiKeyRe is the shape of a Google API key: letters, digits, "-" and "_".
// It also keeps a configured value from smuggling anything into a header.
var apiKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{20,128}$`)

// ValidAPIKey reports whether s can be a Google API key.
func ValidAPIKey(s string) bool { return apiKeyRe.MatchString(s) }

// NewGoogleBooks returns the source for rt (nil for the default transport).
// key may be empty. A malformed key is not sent: every lookup then fails with
// ErrKeyMalformed, rather than quietly falling back to the shared quota.
func NewGoogleBooks(rt http.RoundTripper, key string) *GoogleBooks {
	return newGoogleBooks(googleBase, rt, key)
}

func newGoogleBooks(base string, rt http.RoundTripper, key string) *GoogleBooks {
	u, _ := url.Parse(base)
	return &GoogleBooks{base: base, key: strings.TrimSpace(key), client: newHTTPClient(rt, 20*time.Second, hostPolicy{strings.ToLower(u.Host)}, u.Scheme)}
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
	if s.key != "" && !ValidAPIKey(s.key) {
		return nil, ErrKeyMalformed
	}
	if len(q.ISBNs) > 0 {
		out, err := s.search(ctx, "isbn:"+q.ISBNs[0])
		if err != nil || len(out) > 0 {
			return out, err
		}
		// An ISBN Google does not know, such as one from a copyright page or
		// a download's record, must not stop a title search that would find
		// the book.
	}
	if len(foldText(q.Title)) == 0 {
		return nil, nil
	}
	terms := `intitle:"` + strings.ReplaceAll(mainTitle(q.Title), `"`, " ") + `"`
	if len(q.Authors) > 0 {
		if a := cleanAuthorForQuery(q.Authors[0]); a != "" {
			terms += ` inauthor:"` + strings.ReplaceAll(a, `"`, " ") + `"`
		}
	}
	return s.search(ctx, terms)
}

// search runs one volumes query and maps what it returns.
func (s *GoogleBooks) search(ctx context.Context, terms string) ([]Candidate, error) {
	u := s.base + "/books/v1/volumes?" + url.Values{
		"q":          {terms},
		"maxResults": {strconv.Itoa(fetchCount(s.limit, googleMaxResults))},
		"printType":  {"books"},
		"fields":     {"items(id,volumeInfo(title,subtitle,authors,publisher,publishedDate,description,categories,language,industryIdentifiers,imageLinks/thumbnail))"},
	}.Encode()
	var hdr http.Header
	if s.key != "" {
		hdr = http.Header{"X-Goog-Api-Key": {s.key}}
	}
	body, status, err := fetch(ctx, s.client, u, "application/json", hdr)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusTooManyRequests && s.key != "":
		return nil, fmt.Errorf("%w for this API key", ErrRateLimited)
	case status == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case s.key != "" && (status == http.StatusForbidden || status == http.StatusBadRequest && bytes.Contains(body, []byte("API_KEY_INVALID"))):
		return nil, ErrKeyRejected
	case status == http.StatusNotFound:
		return nil, nil
	case status != http.StatusOK:
		return nil, fmt.Errorf("unexpected HTTP status %d", status)
	}
	var res gbVolumes
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("unreadable response: %w", err)
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
