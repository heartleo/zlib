package cli

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

const defaultSearchPageSize = 50

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search books with filters",
	Long:  "Search books by keyword. Without a query, open the search form.",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")

		if len(args) == 0 {
			if jsonOut {
				return fmt.Errorf("--json requires a search query (interactive mode has no JSON output)")
			}
			fullTitle, _ := cmd.Flags().GetBool("full-title")
			opts, err := searchOptsFromFlags(cmd)
			if err != nil {
				return err
			}
			// Check the session before the filter form opens, so a signed-out
			// user is not asked to fill it in first.
			c, err := loadClient()
			if err != nil {
				return err
			}
			selectedID, err := interactiveSearch(c, opts, fullTitle)
			if err != nil {
				return err
			}
			if selectedID == "" {
				return nil
			}
			dir, err := destDirFlag(cmd)
			if err != nil {
				return err
			}
			deliver := deliveryFlags(cmd)

			return runDownload(newDownloadModel(selectedID, dir, c), deliver, cmd)
		}

		query := strings.Join(args, " ")
		page, _ := cmd.Flags().GetInt("page")
		count, _ := cmd.Flags().GetInt("count")
		fullTitle, _ := cmd.Flags().GetBool("full-title")
		opts, err := searchOptsFromFlags(cmd)
		if err != nil {
			return err
		}

		c, err := loadClient()
		if err != nil {
			return err
		}
		result, err := c.Search(query, page, count, opts)
		if err != nil {
			return err
		}

		if jsonOut {
			result.Books = downloadableBooks(result.Books)
			return printJSON(result)
		}

		if len(result.Books) == 0 {
			lipgloss.Fprintf(os.Stdout, "%s No results found for %q\n", colorRed(symbolError), query)
			return nil
		}

		lipgloss.Fprintf(os.Stdout, "%s Found %s · Page %d / %d\n\n",
			colorGreen(symbolSuccess),
			colorBold(fmt.Sprintf("%d results", len(result.Books))),
			result.Page, result.TotalPages,
		)

		rows := make([][]string, 0, len(result.Books))
		for i, b := range result.Books {
			id := bookCommandID(b)
			if id == "" {
				id = "-"
			}
			authors := strings.Join(b.Authors, ", ")
			title := b.Name
			if !fullTitle {
				title = runewidth.Truncate(title, 40, "")
			}
			ext := strings.ToUpper(b.Extension)
			if ext == "" {
				ext = "-"
			}
			size := b.Size
			if size == "" {
				size = "-"
			}
			year := b.Year
			if year == "" || year == "0" {
				year = "-"
			}
			rating := b.Rating
			if rating == "" {
				rating = "-"
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), id,
				title,
				runewidth.Truncate(authors, 20, ""),
				year, ext, size, rating,
			})
		}

		t := table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(currentTheme.Accent)).
			Headers("#", "ID", "Title", "Authors", "Year", "Format", "Size", "Rating").
			Rows(rows...).
			StyleFunc(func(row, col int) lipgloss.Style {
				pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
				if row == table.HeaderRow {
					return pad.Bold(true).Foreground(currentTheme.Accent)
				}
				switch col {
				case 1: // ID
					return pad.Foreground(currentTheme.Link)
				case 2: // Title
					return pad.Foreground(currentTheme.Title)
				case 5: // Format
					return pad.Foreground(extLipglossColor(rows[row][5]))
				case 7: // Rating
					return pad.Foreground(currentTheme.Success)
				case 0, 3, 4, 6: // #, Authors, Year, Size
					return pad.Foreground(currentTheme.Muted)
				}
				return pad
			})

		lipgloss.Fprintln(os.Stdout, t)
		return nil
	},
}

// bookIDFromURL extracts the alphanumeric book ID from a URL like
// https://z-library.sk/book/2RAqApzDRL/title.html
func bookIDFromURL(u string) string {
	parts := strings.Split(u, "/")
	for i, p := range parts {
		if p == "book" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return u
}

// downloadableBooks rewrites each book's ID to the id:hash reference that
// `zlib download` accepts, so every --json book list can be piped into it. An
// empty list stays a JSON array rather than null.
func downloadableBooks(books []zlib.Book) []zlib.Book {
	if books == nil {
		return []zlib.Book{}
	}
	for i, b := range books {
		if id := bookCommandID(b); id != "" {
			books[i].ID = id
		}
	}
	return books
}

func bookCommandID(book zlib.Book) string {
	if book.ID != "" && book.Hash != "" {
		return book.ID + ":" + book.Hash
	}
	id := bookIDFromURL(book.URL)
	if id == book.URL {
		return ""
	}
	return id
}

func historyCommandID(item zlib.DownloadHistoryItem) string {
	if item.ID != "" && item.Hash != "" {
		return item.ID + ":" + item.Hash
	}
	id := bookIDFromURL(item.URL)
	if id == item.URL {
		return ""
	}
	return id
}

func searchOptsFromFlags(cmd *cobra.Command) (*zlib.SearchOptions, error) {
	exts, _ := cmd.Flags().GetStringArray("ext")
	formats, _ := cmd.Flags().GetStringArray("format")
	exts = append(exts, formats...)
	from, _ := cmd.Flags().GetInt("from-year")
	to, _ := cmd.Flags().GetInt("to-year")
	if !cmd.Flags().Changed("ext") && !cmd.Flags().Changed("format") {
		defaults, err := defaultSearchOptions()
		if err != nil {
			return nil, err
		}
		if defaults != nil {
			for _, ext := range defaults.Extensions {
				exts = append(exts, ext.String())
			}
		}
	}
	langs, _ := cmd.Flags().GetStringArray("language")
	exact, _ := cmd.Flags().GetBool("exact")
	orderRaw, _ := cmd.Flags().GetString("order")
	return searchOptions(searchFilterValues{
		Extensions: exts,
		Languages:  langs,
		FromYear:   from,
		ToYear:     to,
		Exact:      exact,
		Order:      orderRaw,
	})
}

func init() {
	searchCmd.Flags().IntP("page", "p", 1, "Page number")
	searchCmd.Flags().IntP("count", "n", defaultSearchPageSize, "Results per page")
	addDestDirFlag(searchCmd)
	addDeliveryFlags(searchCmd)
	searchCmd.Flags().StringArray("ext", nil, "Filter by file extension, e.g. epub or pdf (repeatable)")
	searchCmd.Flags().StringArray("format", nil, "Alias for --ext")
	searchCmd.Flags().Int("from-year", 0, "Earliest publication year")
	searchCmd.Flags().Int("to-year", 0, "Latest publication year")
	searchCmd.Flags().StringArray("language", nil, "Filter by language (repeatable)")
	searchCmd.Flags().Bool("exact", false, "Match the query exactly")
	searchCmd.Flags().String("order", "", "Sort: popular, bestmatch, date, titleA, title, year, filesize, filesizeA")
	searchCmd.Flags().Bool("full-title", false, "Show full titles in the results table")
	searchCmd.Flags().Bool("json", false, "Print results as JSON (requires a query)")
}
