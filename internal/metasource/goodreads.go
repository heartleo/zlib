package metasource

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	goodreadsBase      = "https://www.goodreads.com"
	sourceGoodreads    = "Goodreads"
	goodreadsCoverHost = "i.gr-assets.com"
	// goodreadsPageCoverHost serves the full-size cover a book page names.
	goodreadsPageCoverHost = "m.media-amazon.com"

	// goodreadsMaxPages is how many book pages one title search reads. A page is
	// large (about 0.9 MB) and Goodreads sheds load, so the rest of the hits stay
	// as the summary the suggestion list gives.
	goodreadsMaxPages = 3

	// goodreadsUserAgent is the User-Agent the source sends. It is the desktop
	// Chrome of the user who asked for this source, and using it instead of zlib's
	// own was their decision, as the WeRead source's is. Chrome reports only its
	// major version, so the string stays as it is until someone bumps it by hand.
	// NewGoodreads takes another with WithGoodreadsUserAgent; nothing else sets it.
	goodreadsUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"
)

// goodreadsRetryDelay is the pause before the one retry of a request that
// Goodreads answered with 502, 503 or 504. It is a variable so tests can
// shorten it.
var goodreadsRetryDelay = 1200 * time.Millisecond

// goodreadsPagePause is the wait between two book pages of one search, so a
// lookup does not ask for them in a burst. A variable, for tests.
var goodreadsPagePause = 600 * time.Millisecond

// Goodreads looks books up on Goodreads. It is opt-in: it is chosen by name
// and is never a default (DefaultSourceNamesFor).
//
// It reads three addresses, with no cookies and no login, and a User-Agent
// (see goodreadsUserAgent):
//
//   - /book/auto_complete?format=json&q=... returns up to a handful of books
//     as JSON: title, author, cover, page count and a blurb that is cut short.
//     It is searched with a title, a title and an author, or an ISBN.
//   - /book/isbn/<isbn> redirects to the book's page. The redirect is read and
//     not followed.
//   - /book/show/<id> is the book's page, whose embedded data holds the whole
//     record of one edition: authors, publisher, date, language, ISBNs,
//     categories, series, the full description and the cover.
//
// An ISBN is resolved to a book ID through the redirect and the page of that ID
// is read, which gives an edition-level candidate. A title search ranks the
// suggestions and reads the pages of the first few, so those rows are edition
// level too; the others stay as the suggestion list describes them, with a
// title, authors, a series and a cover. Goodreads sometimes answers a page with
// a bot check instead of the page (HTTP 202 and a script). The source does not
// answer it: it falls back to the suggestion for that book and reads no more
// pages in that lookup, so a lookup is never lost to it. A blurb that the
// suggestion list cut short is dropped, so it cannot replace a full description
// from another source with a fragment.
type Goodreads struct {
	base       string
	ua         string
	client     *http.Client // follows redirects within the allowed host
	noRedirect *http.Client // returns the first response, for the ISBN hop
	limit      int          // the lookup's result limit; 0 is the default
}

// GoodreadsOption changes how a Goodreads source is built.
type GoodreadsOption func(*Goodreads)

// WithGoodreadsUserAgent makes the source send ua instead of the default. An
// empty ua, or one with a line break in it, keeps the default.
func WithGoodreadsUserAgent(ua string) GoodreadsOption {
	return func(g *Goodreads) {
		if ua = strings.TrimSpace(ua); ua != "" && !strings.ContainsAny(ua, "\r\n") {
			g.ua = ua
		}
	}
}

// NewGoodreads returns the source for rt (nil for the default transport).
func NewGoodreads(rt http.RoundTripper, opts ...GoodreadsOption) *Goodreads {
	return newGoodreads(goodreadsBase, rt, opts...)
}

func newGoodreads(base string, rt http.RoundTripper, opts ...GoodreadsOption) *Goodreads {
	u, _ := url.Parse(base)
	const timeout = 20 * time.Second
	g := &Goodreads{
		base:   base,
		ua:     goodreadsUserAgent,
		client: newHTTPClient(rt, timeout, hostPolicy{strings.ToLower(u.Host)}, u.Scheme),
		noRedirect: &http.Client{
			Transport:     rt,
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (*Goodreads) Name() string { return sourceGoodreads }

// errGoodreadsChallenge is the bot check Goodreads serves in place of a page. It
// is reported as a failure of this source when nothing else could be read, and
// the lookup carries on with the others.
var errGoodreadsChallenge = errors.New("Goodreads answered with a bot check, which zlib does not solve")

// goodreadsBookPath matches the path a book redirects to: /book/show/<id>
// followed by a slug, as "-slug" or ".Slug", or by nothing.
var goodreadsBookPath = regexp.MustCompile(`^/book/show/(\d+)(?:[-.][^/]*)?/?$`)

func (s *Goodreads) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	if len(q.ISBNs) > 0 {
		c, err := s.byISBN(ctx, q.ISBNs[0])
		if err != nil {
			return nil, err
		}
		if c != nil {
			return []Candidate{*c}, nil
		}
	}
	if len(foldText(q.Title)) == 0 {
		return nil, nil
	}
	return s.byTitle(ctx, q)
}

// byISBN resolves isbn to a book ID, then returns that book from its page, or
// as the suggestion list describes it when the page cannot be read, or nil when
// Goodreads does not know the ISBN.
func (s *Goodreads) byISBN(ctx context.Context, isbn string) (*Candidate, error) {
	id, err := s.bookIDByISBN(ctx, isbn)
	if err != nil || id == "" {
		return nil, err
	}
	switch c, err := s.page(ctx, id); {
	case c != nil:
		// Goodreads resolved this ISBN to the book, so it is the book's ISBN
		// even when its page prints none.
		if c.ISBN() == "" {
			c.Identifiers = append([]ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}, c.Identifiers...)
		}
		return c, nil
	case err != nil && ctx.Err() != nil:
		return nil, ctx.Err()
	}
	return s.byISBNSuggestion(ctx, isbn, id)
}

// byISBNSuggestion is the book of id as the suggestion list for its ISBN
// describes it: what a lookup has when the page cannot be read.
func (s *Goodreads) byISBNSuggestion(ctx context.Context, isbn, id string) (*Candidate, error) {
	items, err := s.autocomplete(ctx, isbn)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.bookID() != id {
			continue
		}
		c := it.candidate()
		if c == nil {
			return nil, nil
		}
		// Goodreads resolved this ISBN to the book, so it is the book's
		// ISBN whichever form the book's own page prints.
		c.EditionLevel = true
		c.Identifiers = append([]ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}, c.Identifiers...)
		return c, nil
	}
	return nil, nil
}

// bookIDByISBN returns the book ID Goodreads redirects an ISBN to, or "".
func (s *Goodreads) bookIDByISBN(ctx context.Context, isbn string) (string, error) {
	u := s.base + "/book/isbn/" + url.PathEscape(isbn)
	status, loc, err := s.noRedirectStatus(ctx, u)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusTooManyRequests:
		return "", ErrRateLimited
	case status == http.StatusNotFound:
		return "", nil
	case status == http.StatusAccepted:
		return "", errGoodreadsChallenge
	case status == http.StatusOK:
		return "", nil // no redirect: Goodreads does not know the ISBN
	case status < 300 || status >= 400:
		return "", fmt.Errorf("unexpected HTTP status %d", status)
	}
	if loc == nil {
		return "", nil
	}
	// The target must stay on the same host and scheme, and be a book.
	base, _ := url.Parse(s.base)
	if loc.Host != base.Host || loc.Scheme != base.Scheme {
		return "", fmt.Errorf("redirect to %s is not allowed", loc.Host)
	}
	if m := goodreadsBookPath.FindStringSubmatch(loc.Path); m != nil {
		return m[1], nil
	}
	return "", nil
}

// noRedirectStatus makes one GET that is not redirected, retrying once when
// Goodreads is briefly unavailable, and returns its status and Location.
func (s *Goodreads) noRedirectStatus(ctx context.Context, rawURL string) (int, *url.URL, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("User-Agent", s.ua)
		resp, err := s.noRedirect.Do(req)
		if err != nil {
			return 0, nil, stripURL(err)
		}
		resp.Body.Close()
		if attempt == 0 && goodreadsTransient(resp.StatusCode) {
			if err := sleepCtx(ctx, goodreadsRetryDelay); err != nil {
				return 0, nil, err
			}
			continue
		}
		loc, _ := resp.Location()
		return resp.StatusCode, loc, nil
	}
}

// byTitle searches autocomplete for the main title, and for the title with
// the first author, and keeps the hits whose title is close to the one asked
// for. The lookup ranks and merges them.
func (s *Goodreads) byTitle(ctx context.Context, q Query) ([]Candidate, error) {
	queries := []string{mainTitle(q.Title)}
	if len(q.Authors) > 0 {
		if a := cleanAuthorForQuery(q.Authors[0]); a != "" {
			queries = append(queries, mainTitle(q.Title)+" "+a)
		}
	}
	type hit struct {
		c   Candidate
		sim float64
	}
	var hits []hit
	seen := map[string]bool{}
	var firstErr error
	for _, text := range queries {
		items, err := s.autocomplete(ctx, text)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, it := range items {
			id := it.bookID()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			c := it.candidate()
			if c == nil {
				continue
			}
			if sim := titleSimilarity(q.Title, c.Title); sim >= minTitleSimilarity {
				hits = append(hits, hit{*c, sim})
			}
		}
	}
	if len(hits) == 0 && firstErr != nil {
		return nil, firstErr
	}
	// Stable by similarity, so equal titles keep Goodreads' own order.
	slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.sim, a.sim) })
	if n := fetchCount(s.limit, MaxResultsCeiling); len(hits) > n {
		hits = hits[:n]
	}
	out := make([]Candidate, len(hits))
	for i, h := range hits {
		out[i] = h.c
	}
	s.readPages(ctx, out)
	return out, nil
}

// readPages replaces the first goodreadsMaxPages suggestions with the books'
// pages, one after another with a pause. The first page that cannot be read (a
// bot check, a rate limit, a failure) ends it: the suggestion stays for that
// book and the rest, so nothing already read is lost. A context that ends has
// the same effect, and no error is returned, because the lookup drops the
// candidates of a source that returns one.
func (s *Goodreads) readPages(ctx context.Context, out []Candidate) {
	for i := 0; i < len(out) && i < goodreadsMaxPages; i++ {
		if i > 0 && sleepCtx(ctx, goodreadsPagePause) != nil {
			return
		}
		c, err := s.page(ctx, goodreadsID(out[i]))
		if err != nil {
			return
		}
		if c != nil { // no page for this book: its suggestion stays
			out[i] = *c
		}
	}
}

// goodreadsID is the Goodreads book ID a candidate carries, or "".
func goodreadsID(c Candidate) string {
	for _, id := range c.Identifiers {
		if id.Scheme == "goodreads" {
			return id.Value
		}
	}
	return ""
}

// uaHeader is the User-Agent of this source, as the extra headers of a request.
func (s *Goodreads) uaHeader() http.Header { return http.Header{"User-Agent": {s.ua}} }

// autocomplete returns the books Goodreads suggests for text.
func (s *Goodreads) autocomplete(ctx context.Context, text string) ([]goodreadsItem, error) {
	u := s.base + "/book/auto_complete?" + url.Values{"format": {"json"}, "q": {text}}.Encode()
	var body []byte
	for attempt := 0; ; attempt++ {
		b, status, err := fetch(ctx, s.client, u, "application/json", s.uaHeader())
		if err != nil {
			return nil, err
		}
		if attempt == 0 && goodreadsTransient(status) {
			if err := sleepCtx(ctx, goodreadsRetryDelay); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case status == http.StatusTooManyRequests:
			return nil, ErrRateLimited
		case status == http.StatusAccepted:
			return nil, errGoodreadsChallenge
		case status == http.StatusNotFound:
			return nil, nil
		case status != http.StatusOK:
			return nil, fmt.Errorf("unexpected HTTP status %d", status)
		}
		body = b
		break
	}
	var items []goodreadsItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("unreadable response: %w", err)
	}
	return items, nil
}

// goodreadsTransient reports a status Goodreads sheds load with. It clears
// within seconds, so one retry usually succeeds. 500 is excluded: it is a
// fault, not load.
func goodreadsTransient(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// sleepCtx waits for d, or returns the context's error when it ends first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// goodreadsItem is one autocomplete hit. A book ID or a count may arrive as a
// string or as a number, and the author as a name or as an object, so each is
// read in either form: one change of shape must not drop every result.
type goodreadsItem struct {
	BookID      flexString      `json:"bookId"`
	BookURL     string          `json:"bookUrl"`
	Title       string          `json:"title"`
	TitleBare   string          `json:"bookTitleBare"`
	Author      goodreadsAuthor `json:"author"`
	ImageURL    string          `json:"imageUrl"`
	Description goodreadsBlurb  `json:"description"`
}

// bookID returns the numeric book ID, from the field or from the book's URL.
func (it goodreadsItem) bookID() string {
	if id := string(it.BookID); isDigits(id) {
		return id
	}
	if m := goodreadsBookPath.FindStringSubmatch(it.BookURL); m != nil {
		return m[1]
	}
	return ""
}

// goodreadsSeriesSuffix is the " (Series, #1)" Goodreads appends to the title
// of a book in a series. A range such as "#1-3", which a box set carries, does
// not match: it is not the position of one book.
var goodreadsSeriesSuffix = regexp.MustCompile(`\s*\(([^,()]+),\s*#(\d+(?:\.\d+)?)\)\s*$`)

// goodreadsSeries reads the series and the position from a hit's title, or
// returns "" and 0 when the title has no such suffix.
func goodreadsSeries(title string) (string, float64) {
	m := goodreadsSeriesSuffix.FindStringSubmatch(title)
	if m == nil {
		return "", 0
	}
	index, err := strconv.ParseFloat(m[2], 64)
	if err != nil || index > 10000 {
		return "", 0
	}
	return clean(m[1]), index
}

// candidate maps the hit to a work-level candidate, or nil when it has no
// usable title or ID.
func (it goodreadsItem) candidate() *Candidate {
	id := it.bookID()
	title := clean(it.TitleBare)
	if title == "" {
		title = clean(goodreadsSeriesSuffix.ReplaceAllString(it.Title, ""))
	}
	if id == "" || title == "" {
		return nil
	}
	c := &Candidate{
		Title:       title,
		Authors:     cleanList([]string{cleanAuthorForQuery(clean(string(it.Author)))}),
		Description: it.Description.text(),
		CoverURL:    goodreadsCoverURL(it.ImageURL),
	}
	c.Series, c.SeriesIndex = goodreadsSeries(it.Title)
	c.Identifiers = appendIdentifier(c.Identifiers, "goodreads", id)
	return c
}

// goodreadsThumb is the size token in an autocomplete cover URL, such as
// "._SY75_"; without it the URL is the full-size image.
var goodreadsThumb = regexp.MustCompile(`\._S[XY]\d+_\.`)

// goodreadsCoverURL returns the full-size cover for raw, or "" when raw is not
// an https URL on one of the two hosts Goodreads' covers come from (the
// suggestion list's and a book page's) or is the placeholder for a book with no
// cover, so a response cannot point the cover fetch elsewhere.
func goodreadsCoverURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Port() != "" ||
		!(strings.EqualFold(u.Hostname(), goodreadsCoverHost) || strings.EqualFold(u.Hostname(), goodreadsPageCoverHost)) ||
		strings.Contains(strings.ToLower(u.Path), "nophoto") {
		return ""
	}
	u.Path = goodreadsThumb.ReplaceAllString(u.Path, ".")
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// flexString reads a JSON string or number as text.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case string:
		*f = flexString(strings.TrimSpace(v))
	case float64:
		*f = flexString(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		*f = ""
	}
	return nil
}

// goodreadsAuthor reads an author given as a name or as an object with one.
type goodreadsAuthor string

func (a *goodreadsAuthor) UnmarshalJSON(b []byte) error {
	var v struct{ Name string }
	if err := json.Unmarshal(b, &v); err == nil {
		*a = goodreadsAuthor(v.Name)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		*a = ""
		return nil
	}
	*a = goodreadsAuthor(s)
	return nil
}

// goodreadsBlurb is the description of a hit: an object with HTML and a flag
// saying whether it was cut short, or plain text.
type goodreadsBlurb struct {
	HTML      string
	Truncated bool
}

func (d *goodreadsBlurb) UnmarshalJSON(b []byte) error {
	var v struct {
		HTML      string `json:"html"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(b, &v); err == nil {
		d.HTML, d.Truncated = v.HTML, v.Truncated
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		*d = goodreadsBlurb{}
		return nil
	}
	*d = goodreadsBlurb{HTML: s}
	return nil
}

// text returns the blurb as plain text, or "" when there is none or it was
// cut short.
func (d goodreadsBlurb) text() string {
	if d.Truncated {
		return ""
	}
	return ebookmeta.CleanText(stripHTML(d.HTML))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
