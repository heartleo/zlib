package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// googleBooksKeyAliasEnv is the plain name a Google Books key often has in an
// existing .env. It is read only when googleBooksKeyEnv holds nothing; config
// shows where it came from but saves and removes googleBooksKeyEnv.
const googleBooksKeyAliasEnv = "GOOGLE_BOOKS_API_KEY"

// googleBooksKey returns the configured Google Books key: googleBooksKeyEnv,
// else googleBooksKeyAliasEnv, else "".
func googleBooksKey() string {
	for _, name := range []string{googleBooksKeyEnv, googleBooksKeyAliasEnv} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// wereadAPIKeyEnv names the WeRead API key the weread metadata source sends.
// It is the key Tencent issues for its agent gateway, not the WeRead login
// that imports books, and it is bound to the user's account. It is read when
// the sources are built and never logged or shown.
const wereadAPIKeyEnv = "ZLIB_WEREAD_API_KEY"

// metaSourcesEnv selects the lookup sources, a comma-separated list of
// openlibrary, google, goodreads, douban and weread. Unset means the sources
// for the book's language (DefaultSourceNamesFor); goodreads, douban for an
// English book, and weread are opt-in.
const metaSourcesEnv = "ZLIB_METADATA_SOURCES"

// metaResultsEnv is how many matches a lookup lists, 1 to 50; unset means 10.
const metaResultsEnv = "ZLIB_METADATA_RESULTS"

// metaResultLimit is the configured result limit. A value that cannot be one
// (a hand-edited .env) counts as unset rather than failing the lookup.
func metaResultLimit() int {
	n, err := metasource.ParseMaxResults(os.Getenv(metaResultsEnv))
	if err != nil {
		return metasource.DefaultMaxResults
	}
	return n
}

// newMetaSources builds the lookup sources; tests replace it to stay
// offline. WeRead uses its own proxy setting (ZLIB_WEREAD_PROXY), like the
// importer, while the others use rt.
var newMetaSources = func(rt http.RoundTripper) []metasource.Source {
	return metasource.NewSources(configuredMetaSources(), metaSourceConfig(rt))
}

// metaSourcesFlag is the list --source gave for this run. It wins over
// ZLIB_METADATA_SOURCES, which in turn wins over the saved default (the
// environment is loaded from the .env files before the command runs).
var metaSourcesFlag string

// explicitMetaSources is the list the user chose: the --source flag, else
// ZLIB_METADATA_SOURCES. It is nil when neither names a source.
func explicitMetaSources() []string {
	if metaSourcesFlag != "" {
		return metasource.ExplicitSourceNames(metaSourcesFlag)
	}
	return metasource.ExplicitSourceNames(os.Getenv(metaSourcesEnv))
}

// metaSourceNamesFor is the list of sources a lookup of one book asks now: the
// user's choice, else the default for the book's language (Douban for a Chinese
// book, Open Library for another, plus Google Books when a key is set).
func metaSourceNamesFor(chinese bool) []string {
	if names := explicitMetaSources(); len(names) > 0 {
		return names
	}
	return metasource.DefaultSourceNamesFor(chinese, googleBooksKey() != "")
}

// configuredMetaSources is metaSourceNamesFor for a book that is not Chinese,
// which is what a lookup with no book to look at uses.
func configuredMetaSources() []string { return metaSourceNamesFor(false) }

// metaBookIsChinese reports whether the book a lookup is about is Chinese: by
// the language the file declares, or by the Han characters of the title or
// first author that will be searched.
func metaBookIsChinese(m *ebookmeta.Metadata, title string, authors []string) bool {
	var languages []string
	if m != nil {
		languages = m.Languages
	}
	return metasource.ChineseBook(languages, title, authors)
}

// newMetaSourcesForBook builds the sources for one book. A list the user chose
// is used as it is; otherwise the default for the book's language.
func newMetaSourcesForBook(rt http.RoundTripper, chinese bool) []metasource.Source {
	if len(explicitMetaSources()) > 0 {
		return newMetaSources(rt)
	}
	return newMetaSourcesFor(metaSourceNamesFor(chinese))
}

// applyMetaSourceFlag reads --source: it needs --fetch, and every name must be
// a built-in source, so a typo is a usage error instead of a silent failing
// source. It is the user's choice for this run only and is never saved.
func applyMetaSourceFlag(cmd *cobra.Command) error {
	metaSourcesFlag = ""
	if !cmd.Flags().Changed("source") {
		return nil
	}
	if fetch, _ := cmd.Flags().GetBool("fetch"); !fetch {
		return &usageError{errors.New("--source only applies with --fetch")}
	}
	names, _ := cmd.Flags().GetStringSlice("source")
	list, err := metasource.NormalizeSourceList(strings.Join(names, ","))
	if err != nil {
		return &usageError{fmt.Errorf("--source: %w", err)}
	}
	if list == "" {
		return &usageError{fmt.Errorf("--source needs at least one of %s", strings.Join(metasource.KnownSourceNames, ", "))}
	}
	metaSourcesFlag = list
	return nil
}

// metaSourceConfig is what the sources need: the transports and the keys, read
// from the environment now.
func metaSourceConfig(rt http.RoundTripper) metasource.SourceConfig {
	return metasource.SourceConfig{
		Transport:       rt,
		WeReadTransport: wereadTransport(os.Getenv(wereadProxyEnv)),
		GoogleKey:       googleBooksKey(),
		WeReadKey:       strings.TrimSpace(os.Getenv(wereadAPIKeyEnv)),
		MaxResults:      metaResultLimit(),
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
	"rating", "date", "comments", "cover", "get-cover", "to-opf", "from-opf", "rename", "rename-to",
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

// metaFetchQuery builds the lookup query the way the page does: the file name
// first, then what the file carries, then the copyright page, through
// metasource.Extract. -t, -a and --isbn are the user's own words and replace
// what was read: a title they give is theirs and no guess, and an ISBN they give
// is confirmed, like the file's own.
func metaFetchQuery(cmd *cobra.Command, path string, m *ebookmeta.Metadata) (metasource.Extraction, error) {
	flags := cmd.Flags()
	if err := metaFetchCheckFlags(cmd); err != nil {
		return metasource.Extraction{}, err
	}
	x := metasource.Extract(filepath.Base(path), m, scannedISBNs(path, m), metasource.Hint{NameLayout: metaNameLayout()})
	var title, authors, isbn *string
	if flags.Changed("title") {
		v, _ := flags.GetString("title")
		title = &v
	}
	if flags.Changed("authors") {
		v, _ := flags.GetString("authors")
		authors = &v
	}
	if flags.Changed("isbn") {
		v, _ := flags.GetString("isbn")
		isbn = &v
	}
	if err := overrideExtractionQuery(&x, title, authors, isbn); err != nil {
		return metasource.Extraction{}, &usageError{err}
	}
	return x, nil
}

// overrideExtractionQuery replaces what was read with what the user (or an
// agent) said to search for. A title they give is theirs and no guess; authors
// are separated by &; an ISBN they give is confirmed, like the file's own. A nil
// argument leaves that part as it was.
func overrideExtractionQuery(x *metasource.Extraction, title, authors, isbn *string) error {
	q := &x.Query
	if title != nil {
		q.Title = *title
		q.TitleGuessed = false
		x.TitleFrom = "edited"
	}
	if authors != nil {
		q.Authors = nil
		for a := range strings.SplitSeq(*authors, "&") {
			if a = strings.TrimSpace(a); a != "" {
				q.Authors = append(q.Authors, a)
			}
		}
		x.AuthorsFrom = "edited"
	}
	if isbn != nil {
		n := metasource.NormalizeISBN(*isbn)
		if n == "" {
			return fmt.Errorf("--isbn %q is not a valid ISBN", *isbn)
		}
		q.ISBNs, q.Unconfirmed, x.ISBNFrom = []string{n}, nil, []string{"typed"}
	}
	return nil
}

// metaFetchHasOverrides reports whether -t, -a or --isbn were given, which
// the interactive page does not take.
func metaFetchHasOverrides(cmd *cobra.Command) bool {
	f := cmd.Flags()
	return f.Changed("title") || f.Changed("authors") || f.Changed("isbn")
}

type metaFetchJSON struct {
	File string `json:"file"`
	// Current is the metadata the file holds now, before anything is looked up.
	Current *ebookmeta.Metadata `json:"current,omitempty"`
	Query   metasource.Query    `json:"query"`
	// Extraction says what the file name and the file offered and where each
	// part of the query came from.
	Extraction metaExtractionJSON   `json:"extraction"`
	Candidates []metaCandidateJSON  `json:"candidates"`
	Errors     []metaFetchErrorJSON `json:"errors,omitempty"`
}

// printCurrentMetadata shows what a file already holds under a heading, above
// the matches a lookup finds: the file name, and every value as `zlib meta` prints
// it, with the description cut to its first line.
func printCurrentMetadata(out io.Writer, path string, format ebookmeta.Format, m *ebookmeta.Metadata) {
	lipgloss.Fprintln(out, colorBold("In the file"))
	rows := [][2]string{{"Name", safeText(filepath.Base(path))}}
	rows = append(rows, metadataRows(format, m)...)
	if first, _, _ := strings.Cut(strings.TrimSpace(m.Description), "\n"); strings.TrimSpace(first) != "" {
		rows = append(rows, [2]string{"Description", runewidth.Truncate(oneLine(safeText(first)), 100, "…")})
	}
	for _, line := range formatDetailRows(rows, "  ", colorFaint) {
		lipgloss.Fprintln(out, line)
	}
	lipgloss.Fprintln(out)
}

// metaCandidateJSON is a candidate with the file name it would give the book:
// "Title - Authors.ext" (in the configured format) from the candidate's own
// title and authors, with the extension of the file that was looked up. Nothing is renamed by --fetch.
type metaCandidateJSON struct {
	metasource.Candidate
	SuggestedFileName string `json:"suggested_file_name,omitempty"`
}

// metaExtractionJSON is the extraction as the --json document shows it.
type metaExtractionJSON struct {
	FileName    metasource.FileGuess `json:"file_name"`
	Embedded    metasource.Embedded  `json:"embedded"`
	TitleFrom   string               `json:"title_from,omitempty"`
	AuthorsFrom string               `json:"authors_from,omitempty"`
	AltTitle    string               `json:"alt_title,omitempty"`
	ISBNFrom    []string             `json:"isbn_from,omitempty"`
	ZlibID      string               `json:"zlib_id,omitempty"`
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
	m, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return metaError(path, err)
	}
	x, err := metaFetchQuery(cmd, path, m)
	if err != nil {
		return err
	}
	q := x.Query
	asJSON, _ := cmd.Flags().GetBool("json")
	if !asJSON {
		// What the file already holds comes first, before any catalogue is asked.
		printCurrentMetadata(cmd.OutOrStdout(), path, format, m)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	sources := newMetaSourcesForBook(metaTransport(), metaBookIsChinese(m, q.Title, q.Authors))
	res, err := metasource.Lookup(ctx, sources, q, metasource.Options{MaxResults: metaResultLimit()})
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
		doc := metaFetchJSON{File: path, Current: m, Query: q, Extraction: metaExtractionJSON{
			FileName: x.FileName, Embedded: x.Embedded, TitleFrom: x.TitleFrom, AuthorsFrom: x.AuthorsFrom, AltTitle: x.AltTitle, ISBNFrom: x.ISBNFrom, ZlibID: x.ZlibID,
		}}
		for _, c := range res.Candidates {
			doc.Candidates = append(doc.Candidates, metaCandidateJSON{Candidate: c, SuggestedFileName: metasource.FileNameWith(metaNameLayout(), c.Title, c.Authors, bookExt(path))})
		}
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
			return "ISBN match, not confirmed"
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
		add("Series", c.SeriesLabel())
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
