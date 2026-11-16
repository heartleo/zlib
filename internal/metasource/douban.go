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

	// doubanDetailLimit caps the detail requests one title search makes, so a
	// lookup stays a handful of requests against a throttled service.
	doubanDetailLimit = 3
	doubanMaxTags     = 5
)

// Douban looks books up on Douban Books, the catalogue that covers Chinese
// editions best. It is opt-in: DefaultSources leaves it out.
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
			ID    any    `json:"id"`
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
		id := idString(it.Target.ID)
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
	if len(hits) > doubanDetailLimit {
		hits = hits[:doubanDetailLimit]
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

// doubanBook is the mobile detail response. List fields come as strings, lists
// of strings or lists of objects, so they are decoded by flexList.
type doubanBook struct {
	Title    string          `json:"title"`
	Author   json.RawMessage `json:"author"`
	Press    json.RawMessage `json:"press"`
	Pub      json.RawMessage `json:"publisher"`
	PubDate  json.RawMessage `json:"pubdate"`
	Intro    string          `json:"intro"`
	Summary  string          `json:"summary"`
	Tags     json.RawMessage `json:"tags"`
	ISBN     string          `json:"isbn"`
	Language string          `json:"language"`
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
		Publisher:    firstNonEmpty(append(flexList(b.Press), flexList(b.Pub)...)...),
		Description:  ebookmeta.CleanText(stripHTML(firstNonEmpty(b.Intro, b.Summary))),
	}
	for _, a := range flexList(b.Author) {
		if name := cleanAuthorForQuery(a); name != "" {
			c.Authors = append(c.Authors, name)
		}
	}
	c.Authors = cleanList(c.Authors)
	if d := flexList(b.PubDate); len(d) > 0 {
		c.Published = normalizeDate(d[0])
	}
	// Douban's tags are readers' labels; keep the first few and drop the ones
	// that only repeat the title or an author.
	titleKey := string(foldText(title))
	for _, t := range flexList(b.Tags) {
		k := string(foldText(t))
		if k == "" || k == titleKey {
			continue
		}
		dup := false
		for _, a := range c.Authors {
			if personKey(a) == k {
				dup = true
			}
		}
		if !dup {
			c.Tags = append(c.Tags, t)
		}
		if len(c.Tags) == doubanMaxTags {
			break
		}
	}
	c.Tags = cleanList(c.Tags)
	if l := normalizeLanguage(b.Language); l != "" {
		c.Languages = []string{l}
	} else if hasHan(title) {
		c.Languages = []string{"zh"}
	}
	// Not every response carries an ISBN; a malformed one is dropped.
	c.Identifiers = appendIdentifier(c.Identifiers, "isbn", NormalizeISBN(b.ISBN))
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

// flexList reads a JSON string, list of strings, or list of objects with a
// name, title or tag, into cleaned strings.
func flexList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return cleanList([]string{one})
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, s)
			continue
		}
		var o struct{ Name, Title, Tag string }
		if json.Unmarshal(it, &o) == nil {
			out = append(out, firstNonEmpty(o.Name, o.Title, o.Tag))
		}
	}
	return cleanList(out)
}

// idString renders a JSON id that may be a number or a string.
func idString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	}
	return ""
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
