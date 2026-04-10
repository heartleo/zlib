package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	zlib "github.com/heartleo/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search for books",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()

		if len(args) == 0 {
			selectedID, err := interactiveSearch(c, searchOptsFromFlags(cmd))
			if err != nil {
				return err
			}
			if selectedID == "" {
				return nil
			}
			dir, _ := cmd.Flags().GetString("dir")
			sendToKindle, _ := cmd.Flags().GetBool("send-to-kindle")

			p := tea.NewProgram(newDownloadModel(selectedID, dir, c))
			result, err := p.Run()
			if err != nil {
				return err
			}
			dm := result.(downloadModel)
			if dm.state == stateCancelled {
				return nil
			}
			if dm.err != nil {
				return dm.err
			}
			fmt.Println()
			fmt.Printf("%s Saved to: %s (%d bytes)\n",
				colorGreen(symbolSuccess),
				colorGreen(tildePath(dm.savedPath)),
				dm.savedSize,
			)
			if sendToKindle {
				if err := sendDownloadedFileToKindle(dm.savedPath); err != nil {
					return fmt.Errorf("send to kindle failed: %w", err)
				}
			}
			return nil
		}

		query := strings.Join(args, " ")
		page, _ := cmd.Flags().GetInt("page")
		count, _ := cmd.Flags().GetInt("count")
		opts := searchOptsFromFlags(cmd)

		result, err := c.Search(query, page, count, opts)
		if err != nil {
			return err
		}

		if len(result.Books) == 0 {
			fmt.Printf("%s No results found for %q\n", colorRed(symbolError), query)
			return nil
		}

		fmt.Printf("%s Found %s · Page %d / %d\n\n",
			colorGreen(symbolSuccess),
			colorBold(fmt.Sprintf("%d results", len(result.Books))),
			result.Page, result.TotalPages,
		)

		rows := make([][]string, 0, len(result.Books))
		for i, b := range result.Books {
			id := bookIDFromURL(b.URL)
			if id == b.URL {
				id = "-"
			}
			authors := strings.Join(b.Authors, ", ")
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
				runewidth.Truncate(b.Name, 40, ""),
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

		fmt.Println(t)
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

func searchOptsFromFlags(cmd *cobra.Command) *zlib.SearchOptions {
	exts, _ := cmd.Flags().GetStringArray("ext")
	if len(exts) == 0 {
		return nil
	}
	var extensions []zlib.Extension
	for _, e := range exts {
		extensions = append(extensions, zlib.Extension(strings.ToUpper(e)))
	}
	return &zlib.SearchOptions{Extensions: extensions}
}

func init() {
	searchCmd.Flags().IntP("page", "p", 1, "Page number")
	searchCmd.Flags().IntP("count", "n", 50, "Results per page")
	searchCmd.Flags().StringP("dir", "d", ".", "Destination directory.")
	searchCmd.Flags().Bool("send-to-kindle", false, "Send the downloaded file to Kindle.")
	searchCmd.Flags().StringArray("ext", nil, "Filter by file extension (repeatable): epub, pdf, mobi, …")
}
