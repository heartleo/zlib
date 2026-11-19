package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The metadata tools expose what `zlib meta` and the /meta page do, in three
// steps an agent can take one at a time: read what a local file holds
// (read_metadata), look the book up and see what each match would change
// (lookup_metadata), and write the fields the user chose (edit_metadata). They
// share the CLI's code: the query is built by metasource.Extract, the sources
// follow the same configuration, and every typed value goes through the same
// validation as the editor.

const (
	// mcpCandidateDescription and mcpFileDescription cap descriptions in the
	// results, which an agent reads as tokens. The file keeps its own.
	mcpCandidateDescription = 600
	mcpFileDescription      = 2000
)

type mcpReadMetadataInput struct {
	FilePath string `json:"file_path" jsonschema:"Local path to an EPUB, FB2, DOCX, MOBI, AZW3 or PDF file (supports leading ~)."`
}

type mcpLookupMetadataInput struct {
	FilePath string   `json:"file_path,omitempty" jsonschema:"Local ebook to look up and compare the matches with. Its file name and metadata build the search."`
	Title    string   `json:"title,omitempty" jsonschema:"Title to search for, in place of the one read from the file."`
	Authors  []string `json:"authors,omitempty" jsonschema:"Authors to search for, in place of the ones read from the file."`
	ISBN     string   `json:"isbn,omitempty" jsonschema:"ISBN-10 or ISBN-13 to search for. An ISBN gives the exact edition."`
	Sources  []string `json:"sources,omitempty" jsonschema:"Catalogues to ask: openlibrary, google, goodreads, douban or weread. Leave unset to use the user's configuration. weread sends the title to Tencent under the user's account, so name it only when the user asked. goodreads is never asked unless the user turned it on, so leave it out unless the user wants it."`
}

type mcpEditMetadataInput struct {
	FilePath    string            `json:"file_path" jsonschema:"Local path to the ebook to change (supports leading ~). The file is rewritten in place."`
	Title       string            `json:"title,omitempty" jsonschema:"New title."`
	TitleSort   string            `json:"title_sort,omitempty" jsonschema:"Title as a library sorts it, such as 'Dune Messiah'."`
	Authors     []string          `json:"authors,omitempty" jsonschema:"New authors, replacing the file's. A name must not contain '&'."`
	AuthorSort  string            `json:"author_sort,omitempty" jsonschema:"Authors as a library sorts them, such as 'Herbert, Frank'."`
	Series      string            `json:"series,omitempty" jsonschema:"Series name. Keeps the file's position in it unless series_index is set."`
	SeriesIndex *float64          `json:"series_index,omitempty" jsonschema:"Position in the series, such as 2 or 2.5. The file needs a series name, or set series too."`
	Publisher   string            `json:"publisher,omitempty" jsonschema:"Publisher."`
	Published   string            `json:"published,omitempty" jsonschema:"Publication date: 2008, 2008-01 or 2008-01-05, and a real date."`
	Languages   []string          `json:"languages,omitempty" jsonschema:"Language codes, such as en or zh."`
	Tags        []string          `json:"tags,omitempty" jsonschema:"Tags, replacing the file's. A tag must not contain a comma."`
	Rating      *float64          `json:"rating,omitempty" jsonschema:"Rating from 0 to 5 stars, in halves."`
	Description string            `json:"description,omitempty" jsonschema:"Description; paragraphs are kept."`
	ISBN        string            `json:"isbn,omitempty" jsonschema:"One valid ISBN-10 or ISBN-13. Written as the isbn identifier."`
	Identifiers map[string]string `json:"identifiers,omitempty" jsonschema:"Catalogue identifiers to write, by scheme: google, openlibrary, goodreads, douban or weread. Copy them from a lookup match."`
	CoverURL    string            `json:"cover_url,omitempty" jsonschema:"A cover_url from a lookup match. It is downloaded from a trusted catalogue host and embedded. MOBI and AZW3 files can only replace a cover they already have."`
	Clear       []string          `json:"clear,omitempty" jsonschema:"Fields to empty: title_sort, authors, author_sort, series, publisher, published, languages, tags, rating, description or identifier:<scheme>. The title cannot be cleared."`
	Rename      bool              `json:"rename,omitempty" jsonschema:"Rename the file from its title and authors, in the user's file name format, after writing. Never replaces another file."`
	RenameTo    string            `json:"rename_to,omitempty" jsonschema:"Rename the file to this name. The extension is kept and characters a file system rejects are replaced. Use instead of rename."`
	DryRun      bool              `json:"dry_run,omitempty" jsonschema:"Check and report what would change without writing, downloading or renaming anything."`
}

// mcpFileMetadata is what a file holds, as an agent reads it: the editor's
// fields, with a long description cut.
type mcpFileMetadata struct {
	Title                string                 `json:"title,omitempty"`
	TitleSort            string                 `json:"title_sort,omitempty"`
	Authors              []string               `json:"authors,omitempty"`
	AuthorSort           string                 `json:"author_sort,omitempty"`
	Publisher            string                 `json:"publisher,omitempty"`
	Languages            []string               `json:"languages,omitempty"`
	Tags                 []string               `json:"tags,omitempty"`
	Series               string                 `json:"series,omitempty"`
	SeriesIndex          float64                `json:"series_index,omitempty"`
	Identifiers          []ebookmeta.Identifier `json:"identifiers,omitempty"`
	Published            string                 `json:"published,omitempty"`
	Description          string                 `json:"description,omitempty"`
	DescriptionTruncated bool                   `json:"description_truncated,omitempty" jsonschema:"True when the description was cut here; the file holds all of it."`
	Rating               float64                `json:"rating,omitempty"`
	Cover                string                 `json:"cover,omitempty" jsonschema:"Media type of the embedded cover, when there is one."`
}

func newMCPFileMetadata(m *ebookmeta.Metadata) mcpFileMetadata {
	out := mcpFileMetadata{
		Title: safeText(m.Title), TitleSort: safeText(m.TitleSort), Authors: safeList(m.Authors), AuthorSort: safeText(m.AuthorSort),
		Publisher: safeText(m.Publisher), Languages: safeList(m.Languages), Tags: safeList(m.Tags), Series: safeText(m.Series),
		SeriesIndex: m.SeriesIndex, Published: safeText(m.Published), Rating: m.Rating, Cover: m.Cover,
	}
	for _, id := range m.Identifiers {
		out.Identifiers = append(out.Identifiers, ebookmeta.Identifier{Scheme: safeText(id.Scheme), Value: safeText(id.Value)})
	}
	out.Description, out.DescriptionTruncated = cutText(m.Description, mcpFileDescription)
	return out
}

// safeText on every entry of a list.
func safeList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = safeText(s)
	}
	return out
}

// cutText shortens s to at most n runes, at a rune boundary, and says whether it
// did. Control characters are removed first.
func cutText(s string, n int) (string, bool) {
	s = strings.TrimSpace(safeText(s))
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return strings.TrimSpace(string(r[:n])) + "…", true
}

// mcpMetadataQuery is what a lookup searches for and where each part came from.
type mcpMetadataQuery struct {
	Title        string   `json:"title,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	ISBNs        []string `json:"isbns,omitempty"`
	Unconfirmed  []string `json:"unconfirmed_isbns,omitempty" jsonschema:"ISBNs nothing vouches for: a download's record, or the book's text. A match on one is a hint."`
	TitleFrom    string   `json:"title_from,omitempty" jsonschema:"file name, embedded, download record or edited."`
	AuthorsFrom  string   `json:"authors_from,omitempty"`
	TitleGuessed bool     `json:"title_guessed,omitempty" jsonschema:"True when the title came from a file name, not from the file or from you."`
}

func newMCPMetadataQuery(x metasource.Extraction) mcpMetadataQuery {
	q := x.Query
	return mcpMetadataQuery{
		Title: safeText(q.Title), Authors: safeList(q.Authors), ISBNs: q.ISBNs, Unconfirmed: q.Unconfirmed,
		TitleFrom: x.TitleFrom, AuthorsFrom: x.AuthorsFrom, TitleGuessed: q.TitleGuessed,
	}
}

type mcpReadMetadataResult struct {
	File              string           `json:"file" jsonschema:"Absolute path of the file."`
	Format            string           `json:"format"`
	Metadata          mcpFileMetadata  `json:"metadata"`
	Query             mcpMetadataQuery `json:"query" jsonschema:"What lookup_metadata would search for with only file_path."`
	SuggestedFileName string           `json:"suggested_file_name,omitempty" jsonschema:"The name rename would give the file."`
	CannotStore       []string         `json:"cannot_store,omitempty" jsonschema:"Fields this format cannot hold; edit_metadata skips them."`
}

// mcpPlannedChange is one field a match would change in the file.
type mcpPlannedChange struct {
	Field    string `json:"field" jsonschema:"title, authors, series, publisher, languages, tags, published, description, cover or identifier:<scheme>."`
	Old      string `json:"old,omitempty" jsonschema:"What the file holds, empty when the field is empty."`
	New      string `json:"new" jsonschema:"What the match says, as text."`
	Fill     bool   `json:"fill,omitempty" jsonschema:"True when the file has no value yet."`
	Selected bool   `json:"selected,omitempty" jsonschema:"True for the rows to take unless the user decides otherwise: fills from a confirmed match."`
	Note     string `json:"note,omitempty" jsonschema:"check edition: differs between editions, and this match is a guess. may be another book: the ISBN agrees but the title does not."`
}

type mcpMetadataCandidate struct {
	Rank                 int                    `json:"rank" jsonschema:"1 is the best match."`
	Title                string                 `json:"title"`
	Authors              []string               `json:"authors,omitempty"`
	Publisher            string                 `json:"publisher,omitempty"`
	Published            string                 `json:"published,omitempty"`
	Languages            []string               `json:"languages,omitempty"`
	Tags                 []string               `json:"tags,omitempty"`
	Series               string                 `json:"series,omitempty"`
	SeriesIndex          float64                `json:"series_index,omitempty"`
	Identifiers          []ebookmeta.Identifier `json:"identifiers,omitempty"`
	Description          string                 `json:"description,omitempty"`
	DescriptionTruncated bool                   `json:"description_truncated,omitempty"`
	CoverURL             string                 `json:"cover_url,omitempty" jsonschema:"Pass it as cover_url to edit_metadata to embed this cover."`
	Match                string                 `json:"match" jsonschema:"isbn: found by an ISBN. title: found by title and authors, a guess about the edition."`
	Score                float64                `json:"score" jsonschema:"0 to 1. An isbn match below 1 is not confirmed: its ISBN belongs to a book with another title, or only a hint."`
	Sources              []string               `json:"sources"`
	EditionLevel         bool                   `json:"edition_level,omitempty" jsonschema:"True when it describes one edition, so its publisher, date and ISBN are that edition's."`
	SuggestedFileName    string                 `json:"suggested_file_name,omitempty"`
	Changes              []mcpPlannedChange     `json:"changes,omitempty" jsonschema:"With file_path: what taking this match would change in the file."`
}

type mcpMetadataSourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type mcpLookupMetadataResult struct {
	File        string                   `json:"file,omitempty"`
	Format      string                   `json:"format,omitempty"`
	Current     *mcpFileMetadata         `json:"current,omitempty" jsonschema:"What the file holds, with file_path."`
	Query       mcpMetadataQuery         `json:"query"`
	Candidates  []mcpMetadataCandidate   `json:"candidates" jsonschema:"Ranked matches, best first; empty when nothing matched."`
	Errors      []mcpMetadataSourceError `json:"errors,omitempty" jsonschema:"Sources that failed; the others' matches are still listed."`
	CannotStore []string                 `json:"cannot_store,omitempty" jsonschema:"Fields the file's format cannot hold, left out of each match's changes."`
}

// mcpEditedField is one field edit_metadata changed or would change.
type mcpEditedField struct {
	Field string `json:"field"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`
}

type mcpEditMetadataResult struct {
	File        string           `json:"file" jsonschema:"Absolute path of the file, its new path when renamed."`
	Format      string           `json:"format"`
	Written     bool             `json:"written" jsonschema:"True when the file was rewritten."`
	DryRun      bool             `json:"dry_run,omitempty"`
	RenamedFrom string           `json:"renamed_from,omitempty"`
	Changes     []mcpEditedField `json:"changes" jsonschema:"The fields that changed, or would change with dry_run."`
	Skipped     []string         `json:"skipped,omitempty" jsonschema:"Fields this format cannot store, not written."`
	Metadata    mcpFileMetadata  `json:"metadata" jsonschema:"What the file holds now, or would hold with dry_run."`
}

// resolveMCPFile makes the path an agent gave usable: ~ expanded, confined to
// --allowed-dir, absolute, and an existing regular file.
func resolveMCPFile(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("file_path must not be empty")
	}
	path, err := expandTilde(raw)
	if err != nil {
		return "", err
	}
	if dir := mcpOptions(ctx).AllowedDir; dir != "" {
		if path, err = confineToDir(dir, path); err != nil {
			return "", err
		}
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %s", safeText(filepath.Base(path)), safeText(renameErrorText(err)))
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a file", safeText(filepath.Base(path)))
	}
	return path, nil
}

// cannotStore lists the editor's fields the format cannot hold.
func cannotStore(format ebookmeta.Format) []string {
	var out []string
	for _, f := range metasource.EditableFields {
		if !ebookmeta.CanStore(format, string(f)) {
			out = append(out, string(f))
		}
	}
	return out
}

func handleMCPReadMetadata(ctx context.Context, _ *mcp.CallToolRequest, in mcpReadMetadataInput) (*mcp.CallToolResult, mcpReadMetadataResult, error) {
	path, err := resolveMCPFile(ctx, in.FilePath)
	if err != nil {
		return nil, mcpReadMetadataResult{}, formatMCPError(err)
	}
	m, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return nil, mcpReadMetadataResult{}, formatMCPError(metaError(filepath.Base(path), err))
	}
	x := metasource.Extract(filepath.Base(path), m, scannedISBNs(path, m), metasource.Hint{NameLayout: metaNameLayout()})
	return nil, mcpReadMetadataResult{
		File: path, Format: string(format), Metadata: newMCPFileMetadata(m), Query: newMCPMetadataQuery(x),
		SuggestedFileName: metasource.FileNameWith(metaNameLayout(), x.Query.Title, x.Query.Authors, bookExt(path)),
		CannotStore:       cannotStore(format),
	}, nil
}

func handleMCPLookupMetadata(ctx context.Context, _ *mcp.CallToolRequest, in mcpLookupMetadataInput) (*mcp.CallToolResult, mcpLookupMetadataResult, error) {
	title, isbn := strings.TrimSpace(in.Title), strings.TrimSpace(in.ISBN)
	authors := cleanNames(in.Authors)
	if strings.TrimSpace(in.FilePath) == "" && title == "" && isbn == "" && len(authors) == 0 {
		return nil, mcpLookupMetadataResult{}, errors.New("give file_path, or a title, authors or isbn to search for")
	}
	var (
		out    mcpLookupMetadataResult
		cur    *ebookmeta.Metadata
		format ebookmeta.Format
		path   string
		x      metasource.Extraction
	)
	if strings.TrimSpace(in.FilePath) != "" {
		var err error
		if path, err = resolveMCPFile(ctx, in.FilePath); err != nil {
			return nil, out, formatMCPError(err)
		}
		if cur, format, err = ebookmeta.ReadFile(path); err != nil {
			return nil, out, formatMCPError(metaError(filepath.Base(path), err))
		}
		x = metasource.Extract(filepath.Base(path), cur, scannedISBNs(path, cur), metasource.Hint{NameLayout: metaNameLayout()})
		out.File, out.Format = path, string(format)
		current := newMCPFileMetadata(cur)
		out.Current = &current
		out.CannotStore = cannotStore(format)
	}
	var tp, ap, ip *string
	if title != "" {
		tp = &title
	}
	if len(authors) > 0 {
		joined := strings.Join(authors, "&")
		ap = &joined
	}
	if isbn != "" {
		ip = &isbn
	}
	if err := overrideExtractionQuery(&x, tp, ap, ip); err != nil {
		return nil, out, errors.New(strings.Replace(err.Error(), "--isbn", "isbn", 1))
	}
	out.Query = newMCPMetadataQuery(x)

	var sources []metasource.Source
	if len(in.Sources) > 0 {
		list, err := metasource.NormalizeSourceList(strings.Join(in.Sources, ","))
		if err != nil {
			return nil, out, fmt.Errorf("sources: %w", err)
		}
		if list == "" {
			return nil, out, errors.New("sources names none of openlibrary, google, goodreads, douban or weread")
		}
		sources = newMetaSourcesFor(strings.Split(list, ","))
	} else {
		sources = newMetaSourcesForBook(metaTransport(), metaBookIsChinese(cur, x.Query.Title, x.Query.Authors))
	}
	res, err := metasource.Lookup(ctx, sources, x.Query, metasource.Options{MaxResults: metaResultLimit()})
	switch {
	case errors.Is(err, metasource.ErrNoQuery):
		return nil, out, errors.New("there is nothing to search with: give a title, authors or an ISBN")
	case errors.Is(err, metasource.ErrAllSourcesFailed):
		return nil, out, formatMCPError(fmt.Errorf("%w: %s", err, res.ErrorSummary()))
	case err != nil:
		return nil, out, formatMCPError(err)
	}
	for _, e := range res.Errors {
		out.Errors = append(out.Errors, mcpMetadataSourceError{Source: e.Source, Error: formatMCPErrorText(e.Err)})
	}
	ext := ".epub"
	if path != "" {
		ext = bookExt(path)
	}
	out.Candidates = []mcpMetadataCandidate{}
	for i, c := range res.Candidates {
		mc := mcpMetadataCandidate{
			Rank: i + 1, Title: safeText(c.Title), Authors: safeList(c.Authors), Publisher: safeText(c.Publisher), Published: safeText(c.Published),
			Languages: safeList(c.Languages), Tags: safeList(c.Tags), Series: safeText(c.Series), SeriesIndex: c.SeriesIndex,
			CoverURL: c.CoverURL, Match: string(c.Match), Score: c.Score,
			Sources: c.Sources, EditionLevel: c.EditionLevel,
			SuggestedFileName: metasource.FileNameWith(metaNameLayout(), c.Title, c.Authors, ext),
		}
		for _, id := range c.Identifiers {
			mc.Identifiers = append(mc.Identifiers, ebookmeta.Identifier{Scheme: safeText(id.Scheme), Value: safeText(id.Value)})
		}
		mc.Description, mc.DescriptionTruncated = cutText(metasource.PlainText(c.Description), mcpCandidateDescription)
		if cur != nil {
			for _, ch := range metasource.Plan(cur, c) {
				if !canOfferChange(format, cur, ch) {
					continue
				}
				new, _ := cutText(ch.New, mcpCandidateDescription)
				old, _ := cutText(ch.Old, mcpCandidateDescription)
				mc.Changes = append(mc.Changes, mcpPlannedChange{
					Field: string(ch.Field), Old: old, New: new, Fill: ch.Fill, Selected: ch.DefaultSelected(), Note: ch.Note,
				})
			}
		}
		out.Candidates = append(out.Candidates, mc)
	}
	return nil, out, nil
}

// cleanNames trims the names and drops the empty ones.
func cleanNames(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// canOfferChange reports whether a file of this format can take the change: the
// format stores the field, and a MOBI cover can only be replaced, not added.
func canOfferChange(format ebookmeta.Format, cur *ebookmeta.Metadata, ch metasource.Change) bool {
	if !ebookmeta.CanStore(format, string(ch.Field)) {
		return false
	}
	return !(ch.Field == metasource.FieldCover && format == ebookmeta.FormatMOBI && cur.Cover == "")
}

// editRequest is the edit an agent asked for, checked and ready to apply.
type editRequest struct {
	changes *ebookmeta.Changes
	fields  []mcpEditedField
	cover   string // the cover_url to download, or ""
}

// buildMCPEdit turns the input into validated changes, through the editor's own
// parsing, so a value an agent sends is held to what the editor accepts.
func buildMCPEdit(in mcpEditMetadataInput, cur *ebookmeta.Metadata, format ebookmeta.Format) (editRequest, error) {
	var req editRequest
	var plan []metasource.Change
	seen := map[metasource.Field]bool{}
	add := func(f metasource.Field, text string) error {
		old := metasource.FileValue(cur, f)
		ch, err := (metasource.Change{Field: f, Old: old}).Edit(text)
		if err != nil {
			return fmt.Errorf("%s: %s", strings.ToLower(metaFieldLabel(f)), strings.ToLower(err.Error()[:1])+err.Error()[1:])
		}
		seen[f] = true
		if ch.New == old {
			return nil // the file already says it
		}
		plan = append(plan, ch)
		req.fields = append(req.fields, mcpEditedField{Field: string(f), Old: old, New: ch.New})
		return nil
	}
	list := func(name string, items []string, sep string) (string, error) {
		for _, s := range items {
			if strings.Contains(s, sep) {
				return "", fmt.Errorf("%s: %q contains %q, which separates entries; send them as separate entries without it", name, s, sep)
			}
		}
		return strings.Join(items, sep), nil
	}
	if in.Title != "" {
		if err := add(metasource.FieldTitle, in.Title); err != nil {
			return req, err
		}
	}
	if in.TitleSort != "" {
		if err := add(metasource.FieldTitleSort, in.TitleSort); err != nil {
			return req, err
		}
	}
	if len(in.Authors) > 0 {
		if _, err := list("authors", in.Authors, "&"); err != nil {
			return req, err
		}
		if err := add(metasource.FieldAuthors, strings.Join(in.Authors, " & ")); err != nil {
			return req, err
		}
	}
	if in.AuthorSort != "" {
		if err := add(metasource.FieldAuthorSort, in.AuthorSort); err != nil {
			return req, err
		}
	}
	if in.Series != "" || in.SeriesIndex != nil {
		name, index := in.Series, cur.SeriesIndex
		if name == "" {
			name = cur.Series
		}
		if in.SeriesIndex != nil {
			if *in.SeriesIndex < 0 || *in.SeriesIndex > 10000 {
				return req, errors.New("series_index: use a number from 0 to 10000")
			}
			index = *in.SeriesIndex
		}
		if name == "" {
			return req, errors.New("series_index: the file has no series, so name one with series")
		}
		text := name
		if index > 0 {
			text += " #" + strconv.FormatFloat(index, 'f', -1, 64)
		}
		if err := add(metasource.FieldSeries, text); err != nil {
			return req, err
		}
	}
	if in.Publisher != "" {
		if err := add(metasource.FieldPublisher, in.Publisher); err != nil {
			return req, err
		}
	}
	if in.Published != "" {
		if err := add(metasource.FieldPublished, in.Published); err != nil {
			return req, err
		}
	}
	if len(in.Languages) > 0 {
		if _, err := list("languages", in.Languages, ","); err != nil {
			return req, err
		}
		if err := add(metasource.FieldLanguages, strings.Join(in.Languages, ",")); err != nil {
			return req, err
		}
	}
	if len(in.Tags) > 0 {
		if _, err := list("tags", in.Tags, ","); err != nil {
			return req, err
		}
		if err := add(metasource.FieldTags, strings.Join(in.Tags, ",")); err != nil {
			return req, err
		}
	}
	if in.Rating != nil {
		if err := add(metasource.FieldRating, strconv.FormatFloat(*in.Rating, 'f', -1, 64)); err != nil {
			return req, err
		}
	}
	if in.Description != "" {
		if err := add(metasource.FieldDescription, in.Description); err != nil {
			return req, err
		}
	}
	if in.ISBN != "" {
		if err := add("identifier:isbn", in.ISBN); err != nil {
			return req, err
		}
	}
	for scheme, value := range in.Identifiers {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		value = strings.TrimSpace(ebookmeta.CleanText(value))
		switch {
		case scheme == "isbn":
			return req, errors.New("identifiers: set the ISBN with isbn, which checks it")
		case !metasource.WritableIdentifier(scheme):
			return req, fmt.Errorf("identifiers: %q cannot be written (use google, openlibrary, goodreads, douban or weread)", scheme)
		case value == "" || strings.ContainsAny(value, "\r\n"):
			return req, fmt.Errorf("identifiers: %s needs a one-line value; use clear to remove it", scheme)
		}
		f := metasource.Field("identifier:" + scheme)
		if seen[f] {
			continue
		}
		seen[f] = true
		old := cur.Identifier(scheme)
		req.fields = append(req.fields, mcpEditedField{Field: string(f), Old: old, New: value})
		if req.changes == nil {
			req.changes = &ebookmeta.Changes{}
		}
		if req.changes.Identifiers == nil {
			req.changes.Identifiers = map[string]string{}
		}
		req.changes.Identifiers[scheme] = value
	}
	for _, name := range in.Clear {
		f := metasource.Field(strings.ToLower(strings.TrimSpace(name)))
		if !metasource.Clearable(f) || f == "" {
			return req, fmt.Errorf("clear: %q cannot be cleared (the title and the cover cannot)", name)
		}
		if seen[f] {
			return req, fmt.Errorf("clear: %s is also being set", name)
		}
		if f == metasource.FieldCover || !slices.Contains(clearableMCPFields(cur), f) {
			return req, fmt.Errorf("clear: %q is not a field of this file (use title_sort, authors, author_sort, series, publisher, published, languages, tags, rating, description or identifier:<scheme>)", name)
		}
		old := metasource.FileValue(cur, f)
		if strings.HasPrefix(string(f), "identifier:") {
			old = cur.Identifier(strings.TrimPrefix(string(f), "identifier:"))
		}
		if old == "" {
			continue // already empty: nothing to do
		}
		ch, err := metasource.Clear(f, old)
		if err != nil {
			return req, fmt.Errorf("clear: %w", err)
		}
		seen[f] = true
		plan = append(plan, ch)
		req.fields = append(req.fields, mcpEditedField{Field: string(f), Old: old})
	}
	if in.CoverURL != "" {
		if !metasource.TrustedCoverURL(in.CoverURL) {
			return req, errors.New("cover_url: use the cover_url of a lookup match; only https URLs on a catalogue's cover host are fetched")
		}
		if !ebookmeta.CanStore(format, string(metasource.FieldCover)) || (format == ebookmeta.FormatMOBI && cur.Cover == "") {
			return req, fmt.Errorf("cover_url: a %s file cannot take this cover", strings.ToUpper(string(format)))
		}
		req.cover = in.CoverURL
		req.fields = append(req.fields, mcpEditedField{Field: "cover", Old: cur.Cover, New: "image from " + urlHost(in.CoverURL)})
	}
	built := metasource.BuildChanges(plan, func(metasource.Change) bool { return true })
	switch {
	case built == nil && req.changes == nil:
		req.changes = &ebookmeta.Changes{}
	case built != nil && req.changes != nil:
		if built.Identifiers == nil {
			built.Identifiers = map[string]string{}
		}
		for k, v := range req.changes.Identifiers {
			built.Identifiers[k] = v
		}
		req.changes = built
	case built != nil:
		req.changes = built
	}
	return req, nil
}

// clearableMCPFields are the fields clear may name for a file: the editor's, and
// the identifiers it holds.
func clearableMCPFields(cur *ebookmeta.Metadata) []metasource.Field {
	out := []metasource.Field{metasource.FieldTitleSort, metasource.FieldAuthors, metasource.FieldAuthorSort, metasource.FieldSeries,
		metasource.FieldPublisher, metasource.FieldPublished, metasource.FieldLanguages, metasource.FieldTags,
		metasource.FieldRating, metasource.FieldDescription}
	for _, id := range cur.Identifiers {
		out = append(out, metasource.Field("identifier:"+strings.ToLower(id.Scheme)))
	}
	return out
}

func urlHost(raw string) string {
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	host, _, _ := strings.Cut(raw, "/")
	return host
}

func handleMCPEditMetadata(ctx context.Context, _ *mcp.CallToolRequest, in mcpEditMetadataInput) (*mcp.CallToolResult, mcpEditMetadataResult, error) {
	policy := mcpOptions(ctx)
	if policy.ReadOnly || policy.DisableMetadataEdit {
		return nil, mcpEditMetadataResult{}, errors.New("metadata editing is disabled for this MCP server")
	}
	if in.Rename && in.RenameTo != "" {
		return nil, mcpEditMetadataResult{}, errors.New("use rename or rename_to, not both")
	}
	path, err := resolveMCPFile(ctx, in.FilePath)
	if err != nil {
		return nil, mcpEditMetadataResult{}, formatMCPError(err)
	}
	cur, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return nil, mcpEditMetadataResult{}, formatMCPError(metaError(filepath.Base(path), err))
	}
	req, err := buildMCPEdit(in, cur, format)
	if err != nil {
		return nil, mcpEditMetadataResult{}, err
	}
	// What the file would hold, for the rename and the report.
	predicted := *cur
	req.changes.Apply(&predicted)
	skipped := ebookmeta.DropUnsupported(format, req.changes)
	if len(skipped) > 0 {
		req.fields = slices.DeleteFunc(req.fields, func(f mcpEditedField) bool { return slices.Contains(skipped, f.Field) })
		predicted = *cur
		req.changes.Apply(&predicted)
	}
	hasWrite := !req.changes.Empty() || req.cover != ""
	if !hasWrite && !in.Rename && in.RenameTo == "" {
		return nil, mcpEditMetadataResult{}, errors.New("nothing to change: set at least one field, clear one, or rename")
	}

	// The name is settled, and checked, before anything is written, so a name that
	// cannot be used leaves the file untouched.
	newName := ""
	if in.Rename || in.RenameTo != "" {
		if newName, err = mcpRenameName(in, path, cur, &predicted); err != nil {
			return nil, mcpEditMetadataResult{}, err
		}
		if _, err := checkRename(path, newName); err != nil {
			return nil, mcpEditMetadataResult{}, fmt.Errorf("cannot rename %s to %s: %s", safeText(filepath.Base(path)), safeText(newName), safeText(renameErrorText(err)))
		}
	}
	out := mcpEditMetadataResult{File: path, Format: string(format), DryRun: in.DryRun, Changes: req.fields, Skipped: skipped}
	if out.Changes == nil {
		out.Changes = []mcpEditedField{}
	}
	if in.DryRun {
		if newName != "" && newName != filepath.Base(path) {
			out.File = filepath.Join(filepath.Dir(path), newName)
			out.RenamedFrom = path
		}
		out.Metadata = newMCPFileMetadata(&predicted)
		return nil, out, nil
	}

	if req.cover != "" {
		cov, err := metasource.FetchCover(ctx, metaTransport(), req.cover)
		if err != nil {
			return nil, mcpEditMetadataResult{}, fmt.Errorf("cover_url: nothing was written: %s", formatMCPErrorText(err))
		}
		req.changes.Cover = cov
	}
	if !req.changes.Empty() {
		if err := ebookmeta.WriteFile(path, req.changes); err != nil {
			return nil, mcpEditMetadataResult{}, formatMCPError(metaError(filepath.Base(path), err))
		}
		out.Written = true
	}
	if newName != "" {
		newPath, err := renameBook(path, newName)
		if err != nil {
			// The fields are written; say so, since the rename alone failed.
			return nil, mcpEditMetadataResult{}, fmt.Errorf("the fields were written but the file could not be renamed to %s: %s", safeText(newName), safeText(renameErrorText(err)))
		}
		if newPath != path {
			out.RenamedFrom, out.File = path, newPath
		}
	}
	after, _, err := ebookmeta.ReadFile(out.File)
	if err != nil {
		return nil, mcpEditMetadataResult{}, formatMCPError(metaError(filepath.Base(out.File), err))
	}
	out.Metadata = newMCPFileMetadata(after)
	return nil, out, nil
}

// mcpRenameName is the name rename or rename_to gives the file. rename uses the
// title and authors this call writes; when it writes neither, the ones a lookup
// would search for, since an embedded title often carries a marketing line (the
// same choice --rename makes).
func mcpRenameName(in mcpEditMetadataInput, path string, cur, predicted *ebookmeta.Metadata) (string, error) {
	ext := bookExt(path)
	if in.RenameTo != "" {
		name := metasource.SanitizeFileName(in.RenameTo, ext)
		if name == "" {
			return "", errors.New("rename_to needs a name")
		}
		return name, nil
	}
	title, authors := predicted.Title, predicted.Authors
	if in.Title == "" && len(in.Authors) == 0 {
		q := metasource.Extract(filepath.Base(path), cur, nil, metasource.Hint{NameLayout: metaNameLayout()}).Query
		title, authors = q.Title, q.Authors
	}
	name := metasource.FileNameWith(metaNameLayout(), title, authors, ext)
	if name == "" {
		return "", errors.New("the file has no title to name it by: set one with title, or use rename_to")
	}
	return name, nil
}
