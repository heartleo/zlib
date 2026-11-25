package metasource

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Options tunes a lookup. Zero values select the defaults.
type Options struct {
	// Timeout bounds the whole lookup. Default 30s.
	Timeout time.Duration
	// Grace is how long to wait for slower sources once one has produced a
	// result. Default 8s.
	Grace time.Duration
	// MinScore drops title matches below it. Default 0.45.
	MinScore float64
	// MaxResults caps the candidates returned for each source. Default 10.
	MaxResults int
}

// Result is the outcome of a lookup: ranked candidates, each from one source,
// plus what went wrong at each source. A source failing never fails the lookup.
type Result struct {
	Candidates []Candidate
	Errors     []SourceError
}

// Lookup asks every source in parallel and returns the candidates of all of
// them in one ranked list, each candidate from a single source: the answers of
// different sources are not merged. It returns ErrNoQuery for an empty query and
// ErrAllSourcesFailed when every source failed and nothing was found; a
// lookup that simply finds nothing returns an empty Result and no error.
func Lookup(ctx context.Context, sources []Source, q Query, opt Options) (Result, error) {
	if q.Empty() {
		return Result{}, ErrNoQuery
	}
	opt.Timeout = cmpDefault(opt.Timeout, 30*time.Second)
	opt.Grace = cmpDefault(opt.Grace, 8*time.Second)
	if opt.MinScore == 0 {
		opt.MinScore = defaultMinScore
	}
	if opt.MaxResults == 0 {
		opt.MaxResults = defaultMaxResults
	}

	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	type answer struct {
		i     int
		cands []Candidate
		err   error
	}
	ch := make(chan answer, len(sources))
	for i, s := range sources {
		go func() {
			c, err := s.Identify(ctx, q)
			ch <- answer{i, c, err}
		}()
	}

	answers := make([]answer, len(sources))
	var grace <-chan time.Time
	for done := 0; done < len(sources); {
		select {
		case a := <-ch:
			answers[a.i] = a
			done++
			if len(a.cands) > 0 && grace == nil {
				t := time.NewTimer(opt.Grace)
				defer t.Stop()
				grace = t.C
			}
		case <-grace:
			cancel() // stop waiting; the stragglers return promptly
			grace = nil
		}
	}

	var res Result
	// Each source's own answer is ranked on its own (the same book listed twice
	// by one source is one row), and the sources' rows are never joined: what a
	// source says is shown as that source says it.
	var rows []sourceRow
	for i, a := range answers { // source order, so equal runs rank equally
		name := sources[i].Name()
		if a.err != nil {
			err := a.err
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("did not answer in time")
			}
			res.Errors = append(res.Errors, SourceError{Source: name, Err: err})
			continue
		}
		own := make([]Candidate, len(a.cands))
		for j, c := range a.cands {
			c.Sources = []string{name}
			own[j] = cleanCandidate(c)
		}
		for _, c := range rankAndMerge(q, own, opt.MinScore, opt.MaxResults) {
			rows = append(rows, sourceRow{c, i})
		}
	}
	res.Candidates = orderSourceRows(q, rows)
	if len(res.Candidates) == 0 && len(sources) > 0 && len(res.Errors) == len(sources) {
		return res, ErrAllSourcesFailed
	}
	return res, nil
}

// sourceRow is one candidate of one source, and the position of that source in
// the lookup's list.
type sourceRow struct {
	c   Candidate
	src int
}

// orderSourceRows puts the rows of every source in one list, best first: an ISBN
// match before a title match, then by score and by how well the edition agrees
// with the file, and for equal rows by the order of the sources, so the same
// book from several sources stands in adjacent rows, one per source.
func orderSourceRows(q Query, rows []sourceRow) []Candidate {
	slices.SortStableFunc(rows, func(a, b sourceRow) int {
		if (a.c.Match == MatchISBN) != (b.c.Match == MatchISBN) {
			if a.c.Match == MatchISBN {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(b.c.Score, a.c.Score),
			cmp.Compare(editionBonus(q, b.c), editionBonus(q, a.c)),
			cmp.Compare(a.src, b.src),
			cmp.Compare(completeness(b.c), completeness(a.c)),
			strings.Compare(a.c.Title, b.c.Title),
		)
	})
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = r.c
	}
	return out
}

func cmpDefault(v, def time.Duration) time.Duration {
	if v == 0 {
		return def
	}
	return v
}

// Names of the built-in sources, as written in a source list.
const (
	NameOpenLibrary = "openlibrary"
	NameGoogle      = "google"
	NameGoodreads   = "goodreads"
	NameDouban      = "douban"
	NameWeRead      = "weread"
)

// DefaultSourceNames are the sources used for a book that is not Chinese when
// none are chosen and no Google Books key is set. See DefaultSourceNamesFor.
var DefaultSourceNames = []string{NameOpenLibrary}

// DefaultSourceNamesFor returns the sources used when none are chosen, by the
// language of the book: Douban for a Chinese book, which it covers best, and
// Open Library for any other, with Google Books added when a Google Books API
// key is set (without one Google answers from a shared quota that often
// refuses). WeRead is never a default: it sends the title to Tencent under the
// user's own API key, so it is enabled on purpose.
func DefaultSourceNamesFor(chinese, googleKey bool) []string {
	if chinese {
		return []string{NameDouban}
	}
	names := slices.Clone(DefaultSourceNames)
	if googleKey {
		names = append(names, NameGoogle)
	}
	return names
}

// ChineseBook reports whether a book is Chinese: its language is Chinese, or
// its title or first author is written in Han characters (a Chinese book is
// often tagged with the wrong language). Kana and hangul rule a title out, so
// a Japanese or Korean book is not Chinese.
func ChineseBook(languages []string, title string, authors []string) bool {
	for _, l := range languages {
		if normalizeLanguage(l) == "zh" || strings.HasPrefix(normalizeLanguage(l), "zh-") {
			return true
		}
	}
	if hasHan(title) {
		return true
	}
	return len(authors) > 0 && hasHan(authors[0])
}

// KnownSourceNames lists the built-in sources in the order they are offered.
var KnownSourceNames = []string{NameOpenLibrary, NameGoogle, NameGoodreads, NameDouban, NameWeRead}

// NormalizeSourceList checks a comma-separated source list and returns it in
// the canonical order, without duplicates. An unknown name is an error, so a
// list that is saved cannot hold a typo. An empty list stays empty.
func NormalizeSourceList(list string) (string, error) {
	var asked []string
	for part := range strings.SplitSeq(list, ",") {
		if n := strings.ToLower(strings.TrimSpace(part)); n != "" {
			if !slices.Contains(KnownSourceNames, n) {
				return "", fmt.Errorf("unknown source %q (use %s)", n, strings.Join(KnownSourceNames, ", "))
			}
			asked = append(asked, n)
		}
	}
	var out []string
	for _, n := range KnownSourceNames {
		if slices.Contains(asked, n) {
			out = append(out, n)
		}
	}
	return strings.Join(out, ","), nil
}

// SourceConfig carries what the built-in sources need. The zero value builds
// the sources on the default transport with no keys.
type SourceConfig struct {
	// Transport is used by Open Library, Google Books and Douban.
	Transport http.RoundTripper
	// WeReadTransport is used by WeRead, which has its own proxy setting.
	WeReadTransport http.RoundTripper
	// GoogleKey is an optional Google Books API key; without one Google's
	// shared quota applies.
	GoogleKey string
	// WeReadKey is the WeRead API key the weread source needs.
	WeReadKey string
	// MaxResults is the result limit the lookup will apply; the sources fetch
	// enough hits to fill it. Zero is DefaultMaxResults.
	MaxResults int
}

// DefaultSources returns the default sources for a book that is not Chinese.
// googleKey is an optional Google Books API key, which adds Google Books.
func DefaultSources(rt http.RoundTripper, googleKey string) []Source {
	return NewSources(DefaultSourceNamesFor(false, strings.TrimSpace(googleKey) != ""), SourceConfig{Transport: rt, GoogleKey: googleKey})
}

// ExplicitSourceNames splits a comma-separated source list such as
// "douban,openlibrary" into its names, or returns nil when it names none.
func ExplicitSourceNames(list string) []string {
	var names []string
	for part := range strings.SplitSeq(list, ",") {
		if n := strings.ToLower(strings.TrimSpace(part)); n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return names
}

// ParseSourceNames is ExplicitSourceNames, except that a list naming none
// selects DefaultSourceNames.
func ParseSourceNames(list string) []string {
	if names := ExplicitSourceNames(list); len(names) > 0 {
		return names
	}
	return slices.Clone(DefaultSourceNames)
}

// NewSources builds the named sources in order. An unknown name becomes a
// source that fails with a clear error, so a typo shows up in the lookup's
// error list instead of silently dropping a catalogue.
func NewSources(names []string, cfg SourceConfig) []Source {
	var out []Source
	for _, n := range names {
		switch n {
		case NameOpenLibrary:
			s := NewOpenLibrary(cfg.Transport)
			s.limit = cfg.MaxResults
			out = append(out, s)
		case NameGoogle:
			s := NewGoogleBooks(cfg.Transport, cfg.GoogleKey)
			s.limit = cfg.MaxResults
			out = append(out, s)
		case NameGoodreads:
			s := NewGoodreads(cfg.Transport)
			s.limit = cfg.MaxResults
			out = append(out, s)
		case NameDouban:
			s := NewDouban(cfg.Transport)
			s.limit = cfg.MaxResults
			out = append(out, s)
		case NameWeRead:
			s := NewWeRead(cfg.WeReadTransport, cfg.WeReadKey)
			s.limit = cfg.MaxResults
			out = append(out, s)
		default:
			out = append(out, unknownSource{name: n})
		}
	}
	return out
}

// unknownSource reports a source name that is not built in.
type unknownSource struct{ name string }

func (u unknownSource) Name() string { return u.name }
func (u unknownSource) Identify(context.Context, Query) ([]Candidate, error) {
	return nil, errors.New("unknown source (use openlibrary, google, goodreads, douban or weread)")
}

// ErrorSummary joins the per-source errors into one line.
func (r Result) ErrorSummary() string {
	parts := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		parts[i] = e.Error()
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}
