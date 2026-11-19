// Package metasource looks up authoritative metadata for a book in public
// catalogues, ranks and merges what they return, and plans which fields to
// write into the file. It has no CLI dependencies and talks to a fixed set of
// hosts with a client that carries no credentials.
package metasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

var (
	// ErrRateLimited means a source refused the request for quota reasons.
	ErrRateLimited = errors.New("rate limited")
	// ErrNoQuery means the query has nothing to search with.
	ErrNoQuery = errors.New("nothing to look up: no title, author or ISBN")
	// ErrAllSourcesFailed means every source failed and none found anything.
	ErrAllSourcesFailed = errors.New("every metadata source failed")
	// ErrKeyRejected means a source refused the configured API key.
	ErrKeyRejected = errors.New("the API key was rejected (invalid, restricted, or the Books API is not enabled for it)")
	// ErrKeyMalformed means the configured API key cannot be a key at all, so
	// it was not sent.
	ErrKeyMalformed = errors.New("the configured API key is malformed")
)

// Query describes the book to look up.
type Query struct {
	Title   string   `json:"title,omitempty"`
	Authors []string `json:"authors,omitempty"`
	// ISBNs holds valid ISBN-13 values, see NormalizeISBN.
	ISBNs []string `json:"isbns,omitempty"`
	// Unconfirmed lists the ISBNs in ISBNs that nothing has vouched for: one
	// a download recorded from Z-Library's record, or one read from the
	// book's text. They are searched like any other, but only confirm a match
	// when the title agrees closely (hintTitleSimilarity).
	Unconfirmed []string `json:"unconfirmed_isbns,omitempty"`
	// TitleGuessed means the title was read from a file name or a download's
	// record, not from the file itself or from the user. A download's record
	// supplies both that name and a recorded ISBN, so such a title is no
	// independent evidence and cannot confirm an unconfirmed ISBN.
	TitleGuessed bool `json:"title_guessed,omitempty"`
	// Publisher and Year are what the file says about its edition. They do not
	// find a book; they only rank editions of one title a little.
	Publisher string `json:"publisher,omitempty"`
	Year      string `json:"year,omitempty"`
}

// AddUnconfirmedISBN adds isbn, a valid ISBN-13, to the query as an
// unconfirmed ISBN, unless the query already has it.
func (q *Query) AddUnconfirmedISBN(isbn string) {
	if isbn == "" || slices.Contains(q.ISBNs, isbn) {
		return
	}
	q.ISBNs = append(q.ISBNs, isbn)
	q.Unconfirmed = append(q.Unconfirmed, isbn)
}

// QueryFromMetadata builds a query from what a file already knows.
func QueryFromMetadata(m *ebookmeta.Metadata) Query {
	q := Query{Title: m.Title, Authors: m.Authors}
	if isbn := NormalizeISBN(m.Identifier("isbn")); isbn != "" {
		q.ISBNs = append(q.ISBNs, isbn)
	}
	// An ISBN a download recorded without confirming it is searched too. The
	// matcher flags a hit whose title disagrees, so a wrong one cannot pass
	// as exact.
	q.AddUnconfirmedISBN(NormalizeISBN(m.Identifier(SchemeZlibISBN)))
	return q
}

// Empty reports whether q has nothing to search with.
func (q Query) Empty() bool {
	return len(q.ISBNs) == 0 && len(foldText(q.Title)) == 0 && len(q.Authors) == 0
}

// Candidate is one book as a source describes it, or the merge of several.
type Candidate struct {
	// Sources names every source that contributed, in lookup order.
	Sources     []string               `json:"sources"`
	Title       string                 `json:"title"`
	Authors     []string               `json:"authors,omitempty"`
	Publisher   string                 `json:"publisher,omitempty"`
	Languages   []string               `json:"languages,omitempty"` // BCP 47
	Tags        []string               `json:"tags,omitempty"`
	Published   string                 `json:"published,omitempty"` // YYYY, YYYY-MM or YYYY-MM-DD
	Description string                 `json:"description,omitempty"`
	Identifiers []ebookmeta.Identifier `json:"identifiers,omitempty"`
	CoverURL    string                 `json:"cover_url,omitempty"`
	// Series is the series the book belongs to, and SeriesIndex its position
	// in it; 0 means the position is not known. A series belongs to the work,
	// so unlike a publisher or a date it does not depend on the edition.
	Series      string  `json:"series,omitempty"`
	SeriesIndex float64 `json:"series_index,omitempty"`

	// EditionLevel is true when the source described one specific edition,
	// so its ISBN belongs to this book. Work-level results (a title search
	// on Open Library) say nothing about which edition a file is.
	EditionLevel bool `json:"edition_level"`

	// Match says how the candidate relates to the query; Score is 0 to 1.
	Match Match   `json:"match"`
	Score float64 `json:"score"`

	coverRank int // lower is preferred
}

// Match classifies why a candidate was returned.
type Match string

const (
	MatchISBN  Match = "isbn"  // shares an ISBN with the query
	MatchTitle Match = "title" // matched on title and authors
)

// SeriesLabel is the series and its position written "Name #2", or "".
func (c *Candidate) SeriesLabel() string { return seriesText(c.Series, c.SeriesIndex) }

// ISBN returns the candidate's ISBN-13, or "".
func (c *Candidate) ISBN() string {
	for _, id := range c.Identifiers {
		if id.Scheme == "isbn" {
			return id.Value
		}
	}
	return ""
}

// Source is one catalogue.
type Source interface {
	Name() string
	// Identify returns candidates for q. Finding nothing is not an error.
	Identify(ctx context.Context, q Query) ([]Candidate, error)
}

// SourceError is the failure of one source, kept so a lookup can report it
// without failing as a whole.
type SourceError struct {
	Source string
	Err    error
}

func (e SourceError) Error() string { return e.Source + ": " + e.Err.Error() }
func (e SourceError) Unwrap() error { return e.Err }

const (
	userAgent    = "zlib-metadata-lookup (+https://github.com/heartleo/zlib)"
	maxBodyBytes = 4 << 20
)

// hostPolicy lists the hosts a client may reach. An entry starting with a
// dot matches that domain and its subdomains on the default port; any other
// entry matches one host:port exactly, so another port on the same host is
// a different server.
type hostPolicy []string

func (p hostPolicy) allows(u *url.URL) bool {
	host := strings.ToLower(u.Host)
	for _, h := range p {
		if strings.HasPrefix(h, ".") {
			name := strings.ToLower(u.Hostname())
			if u.Port() == "" && (strings.HasSuffix(name, h) || name == h[1:]) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// newHTTPClient returns a client with no cookie jar, a total timeout, and a
// redirect check that keeps every hop on an allowed host and on https (or on
// the scheme of base, for tests that serve plain http).
func newHTTPClient(rt http.RoundTripper, timeout time.Duration, policy hostPolicy, scheme string) *http.Client {
	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != scheme || !policy.allows(req.URL) {
				return fmt.Errorf("redirect to %s is not allowed", req.URL.Host)
			}
			return nil
		},
	}
}

// get fetches rawURL and decodes a JSON body into v.
func get(ctx context.Context, c *http.Client, rawURL string, v any) error {
	body, status, err := fetch(ctx, c, rawURL, "application/json", nil)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if status == http.StatusNotFound {
		return errNotFound
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unreadable response: %w", err)
	}
	return nil
}

var errNotFound = errors.New("not found")

// fetch reads at most maxBodyBytes of the response.
func fetch(ctx context.Context, c *http.Client, rawURL, accept string, extra http.Header) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	for k, v := range extra {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, stripURL(err)
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, maxBodyBytes)
	if err != nil {
		return nil, 0, err
	}
	return data, resp.StatusCode, nil
}

// readLimited reads r, failing when it holds more than limit bytes.
func readLimited(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, stripURL(err)
	}
	if len(data) > limit {
		return nil, errors.New("response too large")
	}
	return data, nil
}

// stripURL drops the request URL from a transport error: it carries the
// query, which is the book's title and authors.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

// clean makes server text safe to print and write.
func clean(s string) string { return ebookmeta.CleanLine(s) }

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = clean(s)
		k := strings.ToLower(s)
		if s != "" && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}
