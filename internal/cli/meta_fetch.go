package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

// errNoMetadataMatch means every source answered and none knew the book.
var errNoMetadataMatch = errors.New("no matching metadata found")

// googleBooksKeyEnv names the optional Google Books API key. It is read when
// the sources are built, never logged, and shown masked everywhere.
const googleBooksKeyEnv = "ZLIB_GOOGLE_BOOKS_KEY"

// wereadAPIKeyEnv names the WeRead API key the weread metadata source sends.
// It is the key Tencent issues for its agent gateway, not the WeRead login
// that imports books, and it is bound to the user's account. It is read when
// the sources are built and never logged or shown.
const wereadAPIKeyEnv = "ZLIB_WEREAD_API_KEY"

// metaSourcesEnv selects the lookup sources, a comma-separated list of
// openlibrary, google, douban and weread. Unset means openlibrary and google;
// douban and weread are opt-in.
const metaSourcesEnv = "ZLIB_METADATA_SOURCES"

// newMetaSources builds the lookup sources; tests replace it to stay
// offline. WeRead uses its own proxy setting (ZLIB_WEREAD_PROXY), like the
// importer, while the others use rt.
var newMetaSources = func(rt http.RoundTripper) []metasource.Source {
	return metasource.NewSources(metasource.ParseSourceNames(os.Getenv(metaSourcesEnv)), metaSourceConfig(rt))
}

// metaSourceConfig is what the sources need: the transports and the keys, read
// from the environment now.
func metaSourceConfig(rt http.RoundTripper) metasource.SourceConfig {
	return metasource.SourceConfig{
		Transport:       rt,
		WeReadTransport: wereadTransport(os.Getenv(wereadProxyEnv)),
		GoogleKey:       strings.TrimSpace(os.Getenv(googleBooksKeyEnv)),
		WeReadKey:       strings.TrimSpace(os.Getenv(wereadAPIKeyEnv)),
	}
}

// metaSourceError words a source failure for the screen, adding the next step
// when an API key is the problem.
func metaSourceError(e metasource.SourceError) string {
	text := safeText(e.Error())
	switch {
	case errors.Is(e, metasource.ErrKeyRejected) || errors.Is(e, metasource.ErrKeyMalformed):
		text += ". Check the key with `zlib config metadata`"
	case errors.Is(e, metasource.ErrWeReadKeyRejected) || errors.Is(e, metasource.ErrWeReadKeyMalformed):
		text += ". Check " + wereadAPIKeyEnv
	}
	return text
}

// metaTransport returns the transport for metadata lookups: ZLIB_PROXY when
// set, otherwise nil (the default transport, which follows HTTPS_PROXY).
// The Z-Library client is never used, so no session credentials can reach a
// catalogue. An invalid ZLIB_PROXY fails every request rather than falling
// back to a direct connection.
func metaTransport() http.RoundTripper {
	raw, err := validateDoctorProxy(os.Getenv(zlib.EnvProxy))
	if err == nil && raw == "" {
		return nil
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}
	t := base.Clone()
	if err != nil {
		t.Proxy = func(*http.Request) (*url.URL, error) {
			return nil, fmt.Errorf("invalid %s: %w", zlib.EnvProxy, err)
		}
		return t
	}
	u, _ := url.Parse(raw)
	t.Proxy = http.ProxyURL(u)
	return t
}

// metaFetchWriteFlags are the options that change or export a file; none
// may be combined with --fetch, which only reads.
var metaFetchWriteFlags = []string{
	"title-sort", "author-sort", "publisher", "language", "tags", "series", "index", "identifier",
	"rating", "date", "comments", "cover", "get-cover", "to-opf", "from-opf",
}

// metaFetchCheckFlags refuses an option that would change or export the file:
// --fetch only reads. It runs before anything opens, so the interactive page,
// which could not write that field, never silently ignores the option.
func metaFetchCheckFlags(cmd *cobra.Command) error {
	for _, name := range metaFetchWriteFlags {
		if cmd.Flags().Changed(name) {
			return &usageError{fmt.Errorf("--fetch only looks metadata up and writes nothing; it cannot be combined with --%s", name)}
		}
	}
	return nil
}

// metaFetchQuery builds the lookup query: what the file says, overridden by
// -t, -a and --isbn.
func metaFetchQuery(cmd *cobra.Command, m *ebookmeta.Metadata) (metasource.Query, error) {
	flags := cmd.Flags()
	if err := metaFetchCheckFlags(cmd); err != nil {
		return metasource.Query{}, err
	}
	q := metasource.QueryFromMetadata(m)
	if flags.Changed("title") {
		q.Title, _ = flags.GetString("title")
	}
	if flags.Changed("authors") {
		v, _ := flags.GetString("authors")
		q.Authors = nil
		for a := range strings.SplitSeq(v, "&") {
			if a = strings.TrimSpace(a); a != "" {
				q.Authors = append(q.Authors, a)
			}
		}
	}
	if flags.Changed("isbn") {
		v, _ := flags.GetString("isbn")
		isbn := metasource.NormalizeISBN(v)
		if isbn == "" {
			return metasource.Query{}, &usageError{fmt.Errorf("--isbn %q is not a valid ISBN", v)}
		}
		q.ISBNs = []string{isbn}
	}
	return q, nil
}

// addScannedISBNs searches the book's own text for an ISBN when the query has
// none, which is the usual case for a book downloaded before zlib recorded the
// Z-Library ISBN, or made from a typeset file without a metadata identifier.
// An ISBN found this way goes through the same title check as any other, so a
// stray number on the copyright page cannot pass as the book's own. A file the
// scan cannot read adds nothing.
func addScannedISBNs(q *metasource.Query, path string) {
	if len(q.ISBNs) > 0 {
		return
	}
	snippets, err := ebookmeta.ISBNSnippets(path)
	if err != nil {
		return
	}
	for _, isbn := range metasource.ISBNsFromSnippets(snippets) {
		q.AddUnconfirmedISBN(isbn)
	}
}

// metaFetchHasOverrides reports whether -t, -a or --isbn were given, which
// the interactive page does not take.
func metaFetchHasOverrides(cmd *cobra.Command) bool {
	f := cmd.Flags()
	return f.Changed("title") || f.Changed("authors") || f.Changed("isbn")
}

type metaFetchJSON struct {
	File       string                 `json:"file"`
	Query      metasource.Query       `json:"query"`
	Candidates []metasource.Candidate `json:"candidates"`
	Errors     []metaFetchErrorJSON   `json:"errors,omitempty"`
}

type metaFetchErrorJSON struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

func runMetaFetch(cmd *cobra.Command, path string) error {
	if err := metaFetchCheckFlags(cmd); err != nil {
		return err
	}
	// On a terminal, without --json or query overrides, the interactive page
	// does the same lookup and can apply a match.
	if asJSON, _ := cmd.Flags().GetBool("json"); !asJSON && metaIsInteractive() && !metaFetchHasOverrides(cmd) {
		return runStandaloneShellCommand(metaShellCommand(path))
	}
	m, _, err := ebookmeta.ReadFile(path)
	if err != nil {
		return metaError(path, err)
	}
	q, err := metaFetchQuery(cmd, m)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("isbn") {
		addScannedISBNs(&q, path)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := metasource.Lookup(ctx, newMetaSources(metaTransport()), q, metasource.Options{})
	if errors.Is(err, metasource.ErrNoQuery) {
		return &usageError{fmt.Errorf("%s has no title, author or ISBN to look up; pass -t, -a or --isbn", path)}
	}
	stderr := cmd.ErrOrStderr()
	warn := lipgloss.NewStyle().Foreground(currentTheme.Warning).Render("!")
	for _, e := range res.Errors {
		lipgloss.Fprintf(stderr, "%s %s\n", warn, metaSourceError(e))
	}
	if errors.Is(err, metasource.ErrAllSourcesFailed) {
		return fmt.Errorf("%w", err)
	}
	if err != nil {
		return err
	}
	if len(res.Candidates) == 0 {
		return errNoMetadataMatch
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		doc := metaFetchJSON{File: path, Query: q, Candidates: res.Candidates}
		for _, e := range res.Errors {
			doc.Errors = append(doc.Errors, metaFetchErrorJSON{Source: e.Source, Error: e.Err.Error()})
		}
		return printJSONTo(cmd.OutOrStdout(), doc)
	}
	printMetaCandidates(cmd.OutOrStdout(), res.Candidates)
	return nil
}

// metaMatchLabel words how a candidate matched.
func metaMatchLabel(c metasource.Candidate) string {
	if c.Match == metasource.MatchISBN {
		if c.Score < 1 {
			return "ISBN match, title differs"
		}
		return "ISBN match"
	}
	return "title match " + strconv.Itoa(int(c.Score*100+0.5)) + "%"
}

func printMetaCandidates(out io.Writer, cands []metasource.Candidate) {
	for i, c := range cands {
		lipgloss.Fprintf(out, "%s %s  %s\n", colorBold(strconv.Itoa(i+1)+"."), safeText(c.Title),
			colorFaint("["+metaMatchLabel(c)+" · "+safeText(strings.Join(c.Sources, ", "))+"]"))
		var rows [][2]string
		add := func(label, value string) {
			if value != "" {
				rows = append(rows, [2]string{label, safeText(value)})
			}
		}
		add("Authors", strings.Join(c.Authors, " & "))
		add("Publisher", c.Publisher)
		add("Published", c.Published)
		add("Languages", strings.Join(c.Languages, ", "))
		add("Tags", strings.Join(c.Tags, ", "))
		var ids []string
		for _, id := range c.Identifiers {
			ids = append(ids, id.Scheme+":"+id.Value)
		}
		add("Identifiers", strings.Join(ids, ", "))
		if c.Description != "" {
			first, _, _ := strings.Cut(c.Description, "\n")
			add("Description", runewidth.Truncate(first, 100, "…"))
		}
		for _, line := range formatDetailRows(rows, "   ", colorFaint) {
			lipgloss.Fprintln(out, line)
		}
		if i < len(cands)-1 {
			lipgloss.Fprintln(out)
		}
	}
}
