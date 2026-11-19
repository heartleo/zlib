package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/spf13/cobra"
)

const metaLong = `Show or change the metadata of a local e-book file.

Without options, print the metadata. Options that change a field rewrite the
file in place (atomically: a failed write leaves it untouched) and then
print the result. An empty value clears a field, for example --series "".

The format is detected from the file contents:
  EPUB  every field, including the cover
  FB2   title, authors, publisher, language, tags, series, ISBN, date,
        description, cover
  DOCX  title, authors, languages, tags, description
  MOBI, AZW3
        title, authors, publisher, languages, tags, ISBN, ASIN, date,
        description, cover (replacing an existing one)
  PDF   title, authors, tags, description

Fields a format cannot store are reported on stderr and skipped.

--rename names the file Title - Authors.ext (the order is set by zlib config
metadata filename) from the title and authors a lookup would search for (the file name first, then the file; the file's own when the
same command sets -t, -a or an OPF), after any change above. --rename-to NAME
gives it the name you type. The file stays in its directory and no other file
is ever replaced.

On a terminal, zlib meta with no file (or only --fetch) opens a file picker,
then a page that reads the file name and the file, lets you choose the sources
(Open Library, Google Books, Goodreads, Douban, WeRead; by default Douban for a Chinese
book, else Open Library, plus Google Books with a key) and searches when you
press Enter,
so you can review, edit and write its metadata. --fetch --source picks the
sources for one run.`

var metaCmd = &cobra.Command{
	Use:   "meta <file>",
	Short: "Show or change e-book metadata",
	Long:  metaLong,
	Example: `  zlib meta book.epub
  zlib meta book.epub -t "New Title" -a "Jane Doe & John Roe" --series Saga --index 2
  zlib meta book.epub --cover cover.jpg --isbn 9780000000002
  zlib meta book.azw3 --get-cover cover.jpg --to-opf book.opf
  zlib meta book.epub --json`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 || metaOpensPicker(cmd, args) {
			return nil
		}
		return &usageError{errors.New("provide one e-book file: zlib meta <file>")}
	},
	RunE: runMeta,
}

// metaIsInteractive is isInteractive, replaced in tests.
var metaIsInteractive = isInteractive

// metaOpensPicker reports whether `zlib meta` with no file should open the file
// picker: only on a terminal, and only with no option but --fetch, which also
// means "look the book up", and --source, which only says where. Any other option works on a file, so without one
// it stays a usage error, and so does a pipe, where no picker could run.
func metaOpensPicker(cmd *cobra.Command, args []string) bool {
	if len(args) != 0 || !metaIsInteractive() {
		return false
	}
	flags := cmd.Flags()
	if !flags.Changed("fetch") {
		return flags.NFlag() == 0
	}
	return flags.NFlag() == 1 || (flags.NFlag() == 2 && flags.Changed("source"))
}

func init() {
	addMetaFlags(metaCmd)
	rootCmd.AddCommand(metaCmd)
}

func addMetaFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP("title", "t", "", "Set the title")
	f.String("title-sort", "", "Set the title used for sorting")
	f.StringP("authors", "a", "", `Set the authors, separated by "&"`)
	f.String("author-sort", "", "Set the author name used for sorting")
	f.StringP("publisher", "p", "", "Set the publisher")
	f.StringP("language", "l", "", "Set the languages, comma-separated (e.g. en,fr)")
	f.String("tags", "", "Set the tags, comma-separated")
	f.StringP("series", "s", "", "Set the series")
	f.StringP("index", "i", "", "Set the position in the series, e.g. 2 or 2.5")
	f.StringArray("identifier", nil, `Set an identifier as scheme:value; "scheme:" removes it (repeatable)`)
	f.String("isbn", "", "Set the ISBN")
	f.StringP("rating", "r", "", "Set the rating, 0-5 stars")
	f.StringP("date", "d", "", "Set the publication date, e.g. 2024-03-04")
	f.StringP("comments", "c", "", "Set the description")
	f.String("cover", "", "Embed this JPEG, PNG, GIF or WebP image as the cover")
	f.String("get-cover", "", "Save the embedded cover to this file")
	f.String("to-opf", "", "Save the metadata to this OPF file")
	f.String("from-opf", "", "Apply the metadata in this OPF file")
	f.Bool("fetch", false, "Look the book up in online catalogues; on a terminal pick a match and apply it, otherwise list matches (-t, -a and --isbn set what to search for and list matches)")
	f.StringSlice("source", nil, "With --fetch, the catalogues to ask: openlibrary, google, goodreads, douban, weread (comma-separated or repeated); overrides "+metaSourcesEnv)
	f.Bool("rename", false, "Rename the file to \"Title - Authors.ext\" (or the format in ZLIB_FILENAME_FORMAT) from the title and authors a lookup would search for, after any change above")
	f.String("rename-to", "", "Rename the file to this name; the extension is kept and characters a file system rejects are replaced")
	f.Bool("json", false, "Print the metadata as JSON")
}

// metaJSON is the --json document: the metadata plus where it came from.
type metaJSON struct {
	File   string           `json:"file"`
	Format ebookmeta.Format `json:"format"`
	// RenamedFrom is the path the file had before --rename or --rename-to.
	RenamedFrom string `json:"renamed_from,omitempty"`
	*ebookmeta.Metadata
}

func runMeta(cmd *cobra.Command, args []string) error {
	if err := applyMetaSourceFlag(cmd); err != nil {
		return err
	}
	defer func() { metaSourcesFlag = "" }()
	if len(args) == 0 { // Args allowed it: a terminal and no other option
		return runStandaloneShellCommand("/meta")
	}
	path := args[0]
	if fetch, _ := cmd.Flags().GetBool("fetch"); fetch {
		return runMetaFetch(cmd, path)
	}
	if err := precheckRename(cmd, path); err != nil {
		return err
	}
	changes, err := metaChangesFromFlags(cmd)
	if err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	if !changes.Empty() {
		cur, format, err := ebookmeta.ReadFile(path)
		if err != nil {
			return metaError(path, err)
		}
		if err := checkSeriesIndex(cmd, cur, changes); err != nil {
			return err
		}
		if skipped := ebookmeta.DropUnsupported(format, changes); len(skipped) > 0 {
			lipgloss.Fprintf(stderr, "%s %s files cannot store %s; skipped.\n",
				lipgloss.NewStyle().Foreground(currentTheme.Warning).Render("!"), strings.ToUpper(string(format)), strings.Join(skipped, ", "))
		}
		if !changes.Empty() {
			if err := ebookmeta.WriteFile(path, changes); err != nil {
				return metaError(path, err)
			}
		}
	}

	m, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return metaError(path, err)
	}
	renamedFrom := ""
	if newPath, err := renameFromFlags(cmd, path, m); err != nil {
		return err
	} else if newPath != path {
		renamedFrom, path = path, newPath
		lipgloss.Fprintf(stderr, "%s Renamed to %s\n", colorFaint(symbolSuccess), safeText(filepath.Base(path)))
	}
	if out, _ := cmd.Flags().GetString("get-cover"); out != "" {
		cov, err := ebookmeta.CoverFile(path)
		if err != nil {
			return metaError(path, err)
		}
		if err := os.WriteFile(out, cov.Data, 0o644); err != nil {
			return err
		}
		lipgloss.Fprintf(stderr, "%s Cover saved to %s\n", colorFaint(symbolSuccess), safeText(out))
	}
	if out, _ := cmd.Flags().GetString("to-opf"); out != "" {
		data, err := ebookmeta.MarshalOPF(m)
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		lipgloss.Fprintf(stderr, "%s OPF saved to %s\n", colorFaint(symbolSuccess), safeText(out))
	}

	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return printJSONTo(cmd.OutOrStdout(), metaJSON{File: path, Format: format, RenamedFrom: renamedFrom, Metadata: m})
	}
	printMetadata(cmd.OutOrStdout(), format, m)
	return nil
}

func metaError(path string, err error) error {
	if errors.Is(err, ebookmeta.ErrUnsupportedFormat) {
		return fmt.Errorf("%s: %w (supported: %s)", path, err, ebookmeta.SupportedFormats)
	}
	return fmt.Errorf("%s: %w", path, err)
}

// metaChangesFromFlags collects the requested changes: --from-opf first,
// then individual flags on top.
func metaChangesFromFlags(cmd *cobra.Command) (*ebookmeta.Changes, error) {
	flags := cmd.Flags()
	c := &ebookmeta.Changes{}
	if p, _ := flags.GetString("from-opf"); p != "" {
		data, err := readLimited(p, 16<<20)
		if err != nil {
			return nil, err
		}
		m, err := ebookmeta.UnmarshalOPF(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		c = ebookmeta.ChangesFrom(m)
		// A uuid identifies one particular book file; copying it from
		// another book's OPF would give two books the same identity.
		delete(c.Identifiers, "uuid")
	}
	str := func(name string) *string {
		if !flags.Changed(name) {
			return nil
		}
		v, _ := flags.GetString(name)
		return &v
	}
	list := func(name, sep string) *[]string {
		v := str(name)
		if v == nil {
			return nil
		}
		parts := []string{}
		for p := range strings.SplitSeq(*v, sep) {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		return &parts
	}
	setStr := func(dst **string, name string) {
		if v := str(name); v != nil {
			*dst = v
		}
	}
	setList := func(dst **[]string, name, sep string) {
		if v := list(name, sep); v != nil {
			*dst = v
		}
	}
	setStr(&c.Title, "title")
	setStr(&c.TitleSort, "title-sort")
	setList(&c.Authors, "authors", "&")
	setStr(&c.AuthorSort, "author-sort")
	setStr(&c.Publisher, "publisher")
	setList(&c.Languages, "language", ",")
	setList(&c.Tags, "tags", ",")
	setStr(&c.Series, "series")
	setStr(&c.Published, "date")
	setStr(&c.Description, "comments")
	if v := str("index"); v != nil {
		f, err := parseMetaNumber(*v, "--index", math.Inf(1))
		if err != nil {
			return nil, err
		}
		c.SeriesIndex = &f
	}
	if v := str("rating"); v != nil {
		f, err := parseMetaNumber(*v, "--rating", 5)
		if err != nil {
			return nil, err
		}
		c.Rating = &f
	}
	ids, _ := flags.GetStringArray("identifier")
	for _, id := range ids {
		scheme, value, ok := strings.Cut(id, ":")
		scheme = strings.TrimSpace(scheme)
		if !ok || scheme == "" {
			return nil, &usageError{fmt.Errorf("--identifier %q: use scheme:value, e.g. isbn:9780000000002", id)}
		}
		scheme, value = strings.ToLower(scheme), strings.TrimSpace(value)
		if scheme == "isbn" && value != "" {
			n, err := metaISBN("--identifier", value)
			if err != nil {
				return nil, err
			}
			value = n
		}
		if c.Identifiers == nil {
			c.Identifiers = map[string]string{}
		}
		c.Identifiers[scheme] = value
	}
	if v := str("isbn"); v != nil {
		isbn := strings.TrimSpace(*v)
		if isbn != "" {
			n, err := metaISBN("--isbn", isbn)
			if err != nil {
				return nil, err
			}
			isbn = n
		}
		if c.Identifiers == nil {
			c.Identifiers = map[string]string{}
		}
		c.Identifiers["isbn"] = isbn
	}
	if p, _ := flags.GetString("cover"); p != "" {
		data, err := readLimited(p, 64<<20)
		if err != nil {
			return nil, err
		}
		if ebookmeta.SniffImage(data) == "" {
			return nil, &usageError{fmt.Errorf("--cover %s: not a JPEG, PNG, GIF or WebP image", p)}
		}
		c.Cover = &ebookmeta.Cover{Data: data}
	}
	return c, nil
}

// metaISBN returns s as a valid ISBN-13, the form the TUI editor and the MCP
// tool write, or a usage error naming the flag. An empty value is not passed
// here: it clears the identifier.
func metaISBN(flag, s string) (string, error) {
	n := metasource.NormalizeISBN(s)
	if n == "" {
		return "", &usageError{fmt.Errorf("%s %q is not a valid ISBN: check the digits and the check digit", flag, s)}
	}
	return n, nil
}

// checkSeriesIndex refuses --index when neither the file nor --series gives
// the book a series to number, as edit_metadata does. Without it the file would
// be rewritten with nothing changed.
func checkSeriesIndex(cmd *cobra.Command, cur *ebookmeta.Metadata, c *ebookmeta.Changes) error {
	if !cmd.Flags().Changed("index") {
		return nil
	}
	name := cur.Series
	if c.Series != nil {
		name = *c.Series
	}
	if name == "" {
		return &usageError{errors.New("--index: the file has no series, so name one with --series")}
	}
	return nil
}

// parseMetaNumber parses a non-negative number up to limit; "" means 0.
func parseMetaNumber(s, flag string, limit float64) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || f < 0 || f > limit {
		if math.IsInf(limit, 1) {
			return 0, &usageError{fmt.Errorf("%s %q: expected a number of 0 or more", flag, s)}
		}
		return 0, &usageError{fmt.Errorf("%s %q: expected a number from 0 to %g", flag, s, limit)}
	}
	return f, nil
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d MiB", path, limit>>20)
	}
	return data, nil
}

// metadataRows are the labelled values of a file's metadata, in the order the
// command prints them, with control characters removed. The description is not
// among them: it can run to many lines.
func metadataRows(format ebookmeta.Format, m *ebookmeta.Metadata) [][2]string {
	var rows [][2]string
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, safeText(value)})
		}
	}
	add("Format", strings.ToUpper(string(format)))
	add("Title", m.Title)
	add("Title sort", m.TitleSort)
	label := "Author"
	if len(m.Authors) > 1 {
		label = "Authors"
	}
	add(label, strings.Join(m.Authors, " & "))
	add("Author sort", m.AuthorSort)
	add("Publisher", m.Publisher)
	add("Languages", strings.Join(m.Languages, ", "))
	add("Tags", strings.Join(m.Tags, ", "))
	if m.Series != "" {
		s := m.Series
		if m.SeriesIndex > 0 {
			s += " #" + strconv.FormatFloat(m.SeriesIndex, 'f', -1, 64)
		}
		add("Series", s)
	}
	// The Z-Library record a download came from gets a row of its own.
	var ids []string
	for _, id := range m.Identifiers {
		if id.Scheme != metasource.SchemeZlibID {
			ids = append(ids, id.Scheme+":"+id.Value)
		}
	}
	add("Identifiers", strings.Join(ids, ", "))
	add("Z-Library ID", m.Identifier(metasource.SchemeZlibID))
	add("Published", m.Published)
	if m.Rating > 0 {
		add("Rating", strconv.FormatFloat(m.Rating, 'f', -1, 64)+"/5")
	}
	add("Cover", m.Cover)
	return rows
}

func printMetadata(out io.Writer, format ebookmeta.Format, m *ebookmeta.Metadata) {
	for _, line := range formatDetailRows(metadataRows(format, m), "", colorFaint) {
		lipgloss.Fprintln(out, line)
	}
	if m.Description != "" {
		lipgloss.Fprintln(out, colorFaint("Description"))
		for line := range strings.SplitSeq(safeText(m.Description), "\n") {
			lipgloss.Fprintln(out, "  "+line)
		}
	}
}
