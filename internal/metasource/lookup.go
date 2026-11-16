package metasource

import (
	"context"
	"errors"
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
	// MaxResults caps the candidates returned. Default 10.
	MaxResults int
}

// Result is the outcome of a lookup: ranked candidates plus what went wrong
// at each source. A source failing never fails the lookup.
type Result struct {
	Candidates []Candidate
	Errors     []SourceError
}

// Lookup asks every source in parallel and returns the merged, ranked
// candidates. It returns ErrNoQuery for an empty query and
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
	var all []Candidate
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
		for _, c := range a.cands {
			c.Sources = []string{name}
			all = append(all, c)
		}
	}
	res.Candidates = rankAndMerge(q, all, opt.MinScore, opt.MaxResults)
	if len(res.Candidates) == 0 && len(sources) > 0 && len(res.Errors) == len(sources) {
		return res, ErrAllSourcesFailed
	}
	return res, nil
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
	NameDouban      = "douban"
	NameWeRead      = "weread"
)

// DefaultSourceNames are the sources used when none are chosen. Douban and
// WeRead are not among them: Douban sends the title to a third party that
// needs a Referer, and WeRead sends it to Tencent under the user's own API
// key, so each is enabled on purpose.
var DefaultSourceNames = []string{NameOpenLibrary, NameGoogle}

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
}

// DefaultSources returns the default sources. googleKey is an optional Google
// Books API key; without one Google's shared quota applies.
func DefaultSources(rt http.RoundTripper, googleKey string) []Source {
	return NewSources(DefaultSourceNames, SourceConfig{Transport: rt, GoogleKey: googleKey})
}

// ParseSourceNames splits a comma-separated source list such as
// "douban,openlibrary". An empty list selects DefaultSourceNames.
func ParseSourceNames(list string) []string {
	var names []string
	for part := range strings.SplitSeq(list, ",") {
		if n := strings.ToLower(strings.TrimSpace(part)); n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return slices.Clone(DefaultSourceNames)
	}
	return names
}

// NewSources builds the named sources in order. An unknown name becomes a
// source that fails with a clear error, so a typo shows up in the lookup's
// error list instead of silently dropping a catalogue.
func NewSources(names []string, cfg SourceConfig) []Source {
	var out []Source
	for _, n := range names {
		switch n {
		case NameOpenLibrary:
			out = append(out, NewOpenLibrary(cfg.Transport))
		case NameGoogle:
			out = append(out, NewGoogleBooks(cfg.Transport, cfg.GoogleKey))
		case NameDouban:
			out = append(out, NewDouban(cfg.Transport))
		case NameWeRead:
			out = append(out, NewWeRead(cfg.WeReadTransport, cfg.WeReadKey))
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
	return nil, errors.New("unknown source (use openlibrary, google, douban or weread)")
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
