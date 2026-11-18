package metasource

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	wereadBase       = "https://i.weread.qq.com"
	wereadGatewayAPI = "/api/agent/gateway"
	sourceWeRead     = "WeRead"

	// wereadSkillVersion is the gateway's client version, taken from the
	// SKILL.md of Tencent/WeChatReading (revision d98e66e). The gateway checks
	// it on every request.
	wereadSkillVersion = "1.0.5"
)

var (
	// ErrWeReadNoKey means the weread source is enabled but has no API key.
	ErrWeReadNoKey = errors.New("no API key (set ZLIB_WEREAD_API_KEY)")
	// ErrWeReadKeyMalformed means the configured key cannot be a WeRead key,
	// so it was not sent.
	ErrWeReadKeyMalformed = errors.New("the WeRead API key is malformed")
	// ErrWeReadKeyRejected means the gateway refused the key.
	ErrWeReadKeyRejected = errors.New("the WeRead API key was rejected")
	// ErrWeReadUpgrade means the gateway asked clients to upgrade. Its message
	// is written for an agent and is never shown or followed.
	ErrWeReadUpgrade = errors.New("the WeRead gateway reports a newer client version than this zlib supports; update zlib")
)

// wereadKeyRe is the shape of a WeRead API key: letters, digits, "-" and "_".
// It also keeps a configured value from putting anything else into a header.
var wereadKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// ValidWeReadKey reports whether s can be a WeRead API key.
func ValidWeReadKey(s string) bool { return wereadKeyRe.MatchString(s) }

// WeRead looks books up through the WeRead agent gateway, the API key
// interface Tencent publishes for agents (/store/search and /book/info). It is
// opt-in: it needs the user's own API key, which is bound to their account, so
// every title searched is sent to Tencent under that identity.
//
// It shares nothing with the importer in internal/weread, which logs in as a
// device and signs requests; this source sends a plain bearer key and an
// honest User-Agent. The key goes in the Authorization header only, never in a
// URL, a log or an error text, and the client refuses any redirect off the
// gateway host, so it cannot be sent anywhere else.
//
// The gateway has no lookup by ISBN: an ISBN used as a search keyword returns
// unrelated books. So a title is searched, each plausible hit is read with
// /book/info, and the ISBN the entry carries (often none) is what the
// matcher compares with the wanted one. A query with no title finds nothing.
type WeRead struct {
	base   string
	client *http.Client
	key    string
	limit  int // the lookup's result limit; 0 is the default
}

// NewWeRead returns the source for rt (nil for the default transport). key is
// the API key; without a valid one every lookup fails with a clear error.
func NewWeRead(rt http.RoundTripper, key string) *WeRead {
	return newWeRead(wereadBase, rt, key)
}

func newWeRead(base string, rt http.RoundTripper, key string) *WeRead {
	u, _ := url.Parse(base)
	return &WeRead{
		base:   base,
		key:    strings.TrimSpace(key),
		client: newHTTPClient(rt, 20*time.Second, hostPolicy{strings.ToLower(u.Host)}, u.Scheme),
	}
}

func (*WeRead) Name() string { return sourceWeRead }

// wereadSearch is the part of /store/search a lookup uses. Each hit is a
// bookInfo with a string bookId; results are split into groups (one per book
// in the measurement), and a group's scope can differ from the one requested.
type wereadSearch struct {
	Results []struct {
		Books []struct {
			BookInfo struct {
				BookID string `json:"bookId"`
				Title  string `json:"title"`
			} `json:"bookInfo"`
		} `json:"books"`
	} `json:"results"`
}

// wereadInfo is the part of /book/info a candidate uses. isbn can be an empty
// string and publishTime is a "2022-04-01 00:00:00" style string.
type wereadInfo struct {
	BookID      string `json:"bookId"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Publisher   string `json:"publisher"`
	PublishTime string `json:"publishTime"`
	ISBN        string `json:"isbn"`
	Intro       string `json:"intro"`
	Category    string `json:"category"`
}

func (s *WeRead) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	if s.key == "" {
		return nil, ErrWeReadNoKey
	}
	if !ValidWeReadKey(s.key) {
		return nil, ErrWeReadKeyMalformed
	}
	if len(foldText(q.Title)) == 0 {
		return nil, nil // nothing to search with: an ISBN is not a search term here
	}

	var res wereadSearch
	// The scope is always passed: 10 is e-books. A group's own scope is not
	// filtered on, since it comes back as 17 for the same request.
	err := s.call(ctx, map[string]any{"api_name": "/store/search", "keyword": mainTitle(q.Title), "scope": 10, "count": 10}, &res)
	if err != nil {
		return nil, err
	}
	type hit struct {
		id  string
		sim float64
	}
	var hits []hit
	seen := map[string]bool{}
	for _, g := range res.Results {
		for _, b := range g.Books {
			id := strings.TrimSpace(b.BookInfo.BookID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if sim := titleSimilarity(q.Title, clean(b.BookInfo.Title)); sim >= minTitleSimilarity {
				hits = append(hits, hit{id, sim})
			}
		}
	}
	// Best title first; equal titles keep the gateway's own order.
	slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.sim, a.sim) })
	// One /book/info request per hit, capped like Douban's detail requests.
	if n := detailCount(s.limit); len(hits) > n {
		hits = hits[:n]
	}

	var out []Candidate
	for _, h := range hits {
		var info wereadInfo
		if err := s.call(ctx, map[string]any{"api_name": "/book/info", "bookId": h.id}, &info); err != nil {
			if len(out) == 0 {
				return nil, err
			}
			break // keep what was read
		}
		if c := wereadCandidate(info, h.id); c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

// wereadCandidate maps a /book/info response to a candidate, or nil when it
// has no usable title. It is the one place that knows the response shape.
func wereadCandidate(info wereadInfo, id string) *Candidate {
	title := clean(info.Title)
	if title == "" {
		return nil
	}
	c := &Candidate{
		EditionLevel: true,
		Title:        title,
		Publisher:    clean(info.Publisher),
		Published:    normalizeDate(strings.TrimSpace(info.PublishTime)),
		Description:  ebookmeta.CleanText(stripHTML(info.Intro)),
	}
	for _, a := range strings.FieldsFunc(info.Author, func(r rune) bool { return r == '、' || r == '，' || r == '；' || r == ';' }) {
		if name := cleanAuthorForQuery(clean(a)); name != "" {
			c.Authors = append(c.Authors, name)
		}
	}
	c.Authors = cleanList(c.Authors)
	// The category is a path such as "Fiction-Science Fiction".
	c.Tags = cleanList(strings.Split(info.Category, "-"))
	if hasHan(title) {
		c.Languages = []string{"zh"}
	}
	// The ISBN is often empty. The WeRead book ID names the entry either way.
	c.Identifiers = appendIdentifier(c.Identifiers, "isbn", NormalizeISBN(info.ISBN))
	c.Identifiers = appendIdentifier(c.Identifiers, "weread", clean(cmp.Or(info.BookID, id)))
	return c
}

// call posts one gateway request: the API name and its parameters side by
// side in a flat JSON body, as the gateway requires.
func (s *WeRead) call(ctx context.Context, params map[string]any, v any) error {
	body := map[string]any{"skill_version": wereadSkillVersion}
	maps.Copy(body, params)
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+wereadGatewayAPI, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.key)
	resp, err := s.client.Do(req)
	if err != nil {
		return stripURL(err)
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, maxBodyBytes)
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrWeReadKeyRejected
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	var envelope struct {
		ErrCode     int             `json:"errcode"`
		UpgradeInfo json.RawMessage `json:"upgrade_info"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("unreadable response: %w", err)
	}
	// upgrade_info is text for an agent to act on. It is only noticed, never
	// echoed or followed.
	if len(envelope.UpgradeInfo) > 0 && string(envelope.UpgradeInfo) != "null" {
		return ErrWeReadUpgrade
	}
	if envelope.ErrCode != 0 {
		return fmt.Errorf("the gateway returned error code %d", envelope.ErrCode)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unreadable response: %w", err)
	}
	return nil
}
