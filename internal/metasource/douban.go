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
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	doubanMobileBase = "https://m.douban.com"
	doubanBookBase   = "https://book.douban.com"
	sourceDouban     = "Douban"
)

// Douban looks books up on Douban Books, the catalogue that covers Chinese
// editions best. It is the default for a Chinese book (DefaultSourceNamesFor)
// and is asked about no other unless chosen.
//
// An ISBN is resolved through the book.douban.com/isbn/<isbn>/ redirect,
// which reads the subject ID from the Location header and downloads no page.
// A title is searched through the m.douban.com mobile search, and the best
// matches are read from the mobile detail API. Both mobile endpoints refuse
// requests without a Referer on m.douban.com, so one is sent: it is the only
// browser-like header, and there are no cookies, no login and no challenge
// solving. When Douban refuses a request the source reports an error and the
// lookup carries on with the other sources.
//
// A Douban subject is one edition, so its candidates are edition level.
type Douban struct {
	mobile, book string
	client       *http.Client // follows redirects within the allowed hosts
	noRedirect   *http.Client // returns the first response, for the ISBN hop
	limit        int          // the lookup's result limit; 0 is the default
}

// NewDouban returns the source for rt (nil for the default transport).
func NewDouban(rt http.RoundTripper) *Douban {
	return newDouban(doubanMobileBase, doubanBookBase, rt)
}

func newDouban(mobileBase, bookBase string, rt http.RoundTripper) *Douban {
	mu, _ := url.Parse(mobileBase)
	bu, _ := url.Parse(bookBase)
	policy := hostPolicy{strings.ToLower(mu.Host), strings.ToLower(bu.Host)}
	const timeout = 20 * time.Second
	return &Douban{
		mobile: mobileBase,
		book:   bookBase,
		client: newHTTPClient(rt, timeout, policy, mu.Scheme),
		noRedirect: &http.Client{
			Transport:     rt,
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (*Douban) Name() string { return sourceDouban }

var (
	doubanSubjectPath = regexp.MustCompile(`^/subject/(\d+)/?$`)
	doubanIDRe        = regexp.MustCompile(`^\d+$`)
)

func (s *Douban) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	if len(q.ISBNs) > 0 {
		id, err := s.subjectByISBN(ctx, q.ISBNs[0])
		if err != nil {
			return nil, err
		}
		if id != "" {
			c, err := s.detail(ctx, id)
			if err != nil {
				return nil, err
			}
			if c != nil {
				// Douban resolved this ISBN to the subject, so it is the
				// subject's ISBN whichever form its own page prints.
				c.Identifiers = append([]ebookmeta.Identifier{{Scheme: "isbn", Value: q.ISBNs[0]}}, c.Identifiers...)
				return []Candidate{*c}, nil
			}
		}
	}
	if len(foldText(q.Title)) == 0 {
		return nil, nil
	}
	return s.searchByTitle(ctx, q)
}

// subjectByISBN returns the subject ID Douban redirects an ISBN to, or "".
func (s *Douban) subjectByISBN(ctx context.Context, isbn string) (string, error) {
	u := s.book + "/isbn/" + url.PathEscape(isbn) + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", s.book+"/")
	resp, err := s.noRedirect.Do(req)
	if err != nil {
		return "", stripURL(err)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", ErrRateLimited
	case resp.StatusCode == http.StatusNotFound:
		return "", nil
	case resp.StatusCode < 300 || resp.StatusCode >= 400:
		if resp.StatusCode == http.StatusOK {
			return "", nil // no redirect: Douban does not know the ISBN
		}
		return "", fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", nil
	}
	// The target must stay on the book host and scheme, and be a subject.
	if loc.Host != req.URL.Host || loc.Scheme != req.URL.Scheme {
		return "", fmt.Errorf("redirect to %s is not allowed", loc.Host)
	}
	if m := doubanSubjectPath.FindStringSubmatch(loc.Path); m != nil {
		return m[1], nil
	}
	return "", nil
}

type doubanSearch struct {
	Banned string `json:"banned"`
	Items  []struct {
		TargetType string `json:"target_type"`
		Target     struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"target"`
	} `json:"items"`
}

func (s *Douban) searchByTitle(ctx context.Context, q Query) ([]Candidate, error) {
	title := mainTitle(q.Title)
	u := s.mobile + "/rexxar/api/v2/search/book?" + url.Values{
		"q":     {title},
		"start": {"0"},
		"count": {"10"},
	}.Encode()
	var res doubanSearch
	if err := s.getJSON(ctx, u, s.mobile+"/search/?"+url.Values{"query": {title}, "type": {"book"}}.Encode(), &res); err != nil {
		return nil, err
	}
	if strings.TrimSpace(res.Banned) != "" && len(res.Items) == 0 {
		return nil, errors.New("Douban refused this search keyword")
	}
	type hit struct {
		id  string
		sim float64
	}
	var hits []hit
	seen := map[string]bool{}
	for _, it := range res.Items {
		id := it.Target.ID
		t := clean(it.Target.Title)
		if it.TargetType != "book" || !doubanIDRe.MatchString(id) || t == "" || seen[id] {
			continue
		}
		seen[id] = true
		if sim := titleSimilarity(q.Title, t); sim >= minTitleSimilarity {
			hits = append(hits, hit{id, sim})
		}
	}
	// Stable by similarity, so equal titles keep Douban's own order.
	slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.sim, a.sim) })
	// One detail request per hit, capped so a lookup stays a modest number of
	// requests against a throttled service.
	if n := detailCount(s.limit); len(hits) > n {
		hits = hits[:n]
	}
	var out []Candidate
	for _, h := range hits {
		c, err := s.detail(ctx, h.id)
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			break // keep what was read
		}
		if c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

// doubanBook is the part of the mobile detail response a candidate uses. The
// list fields are lists of strings. The response has no ISBN, language or
// publisher key, and its tags list can be empty, so none of those are read.
// CoverURL is the large cover on one of Douban's image hosts.
type doubanBook struct {
	Title    string   `json:"title"`
	CoverURL string   `json:"cover_url"`
	Author   []string `json:"author"`
	Press    []string `json:"press"`
	PubDate  []string `json:"pubdate"`
	Intro    string   `json:"intro"`
}

// detail reads one subject, or returns nil when it has no usable title.
func (s *Douban) detail(ctx context.Context, id string) (*Candidate, error) {
	var b doubanBook
	if err := s.getJSON(ctx, s.mobile+"/rexxar/api/v2/book/"+id, s.mobile+"/book/subject/"+id+"/", &b); err != nil {
		return nil, err
	}
	title := clean(b.Title)
	if title == "" {
		return nil, nil
	}
	c := &Candidate{
		EditionLevel: true,
		Title:        title,
		Publisher:    firstNonEmpty(cleanList(b.Press)...),
		Description:  ebookmeta.CleanText(stripHTML(b.Intro)),
		CoverURL:     doubanCoverURL(b.CoverURL),
	}
	for _, a := range b.Author {
		if name := cleanAuthorForQuery(clean(a)); name != "" {
			c.Authors = append(c.Authors, name)
		}
	}
	c.Authors = cleanList(c.Authors)
	if d := cleanList(b.PubDate); len(d) > 0 {
		c.Published = normalizeDate(d[0])
	}
	// The response names no language; a Han-only title is Chinese.
	if hasHan(title) {
		c.Languages = []string{"zh"}
	}
	// The response has no ISBN, so a title hit names its edition by the
	// Douban subject ID alone. An ISBN lookup adds the ISBN it asked for.
	c.Identifiers = appendIdentifier(c.Identifiers, "douban", id)
	return c, nil
}

func (s *Douban) getJSON(ctx context.Context, rawURL, referer string, v any) error {
	body, status, err := fetch(ctx, s.client, rawURL, "application/json", http.Header{"Referer": {referer}})
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status == http.StatusNotFound:
		return errNotFound
	case status != http.StatusOK:
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unreadable response: %w", err)
	}
	return nil
}

// doubanCoverURL returns raw when it is an https URL on a Douban image host,
// and "" otherwise, so a response cannot point the cover fetch elsewhere.
func doubanCoverURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || coverReferer(u) == "" || !coverHosts.allows(u) {
		return ""
	}
	return u.String()
}

// hasHan reports whether s contains a Han ideograph and no kana or hangul, so
// a Japanese or Korean title is not tagged Chinese.
func hasHan(s string) bool {
	han := false
	for _, r := range s {
		switch {
		case r >= 0x3040 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7A3:
			return false
		case r >= 0x4E00 && r <= 0x9FFF, r >= 0x3400 && r <= 0x4DBF:
			han = true
		}
	}
	return han
}
