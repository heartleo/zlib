package cli

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show download history",
	Long:  "Without flags, browse download history interactively. Use --json to print it, --download to download a book, or --delete to remove an entry.",
	RunE: func(cmd *cobra.Command, args []string) error {
		page, _ := cmd.Flags().GetInt("page")
		downloadID, _ := cmd.Flags().GetString("download")
		dir, err := destDirFlag(cmd)
		if err != nil {
			return err
		}
		deliver := deliveryFlags(cmd)
		formatFilter, _ := cmd.Flags().GetString("format")
		jsonOut, _ := cmd.Flags().GetBool("json")
		deleteID, _ := cmd.Flags().GetString("delete")

		c, err := loadClient()
		if err != nil {
			return err
		}
		if deleteID != "" {
			if err := c.DeleteDownloadHistoryContext(cmd.Context(), deleteID); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(map[string]any{"success": true, "id": deleteID})
			}
			lipgloss.Fprintln(cmd.OutOrStdout(), "History entry deleted.")
			return nil
		}

		if downloadID != "" {
			item, err := findHistoryItemByBookID(c, downloadID)
			if err != nil {
				return err
			}
			if item.DownloadURL == "" {
				return runDownload(newDownloadModel(historyCommandID(item), dir, c), deliver, cmd)
			}

			bookName := item.Name
			if bookName == "" {
				bookName = downloadID
			}
			return runDownload(historyDirectDownload(item, bookName, dir, c), deliver, cmd)
		}

		// No flags: interactive mode (--json always forces the static path)
		if !cmd.Flags().Changed("page") && formatFilter == "" && !jsonOut && isInteractive() {
			result, err := runCoverProgram(newHistorySelectModel(c))
			if err != nil {
				return err
			}
			hm := result.(historySelectModel)
			if hm.err != nil {
				return hm.err
			}
			if hm.selected == nil {
				return nil
			}
			item := hm.selected
			if item.DownloadURL == "" {
				return runDownload(newDownloadModel(historyCommandID(*item), dir, c), deliver, cmd)
			}
			bookName := item.Name
			if bookName == "" {
				bookName = bookIDFromURL(item.URL)
			}
			return runDownload(historyDirectDownload(*item, bookName, dir, c), deliver, cmd)
		}

		result, err := loadHistoryPage(cmd.Context(), c, page, formatFilter)
		if err != nil {
			return err
		}

		if len(result.Items) == 0 && !jsonOut && formatFilter == "" {
			lipgloss.Fprintf(os.Stdout, "No download history found on page %d.\n", result.Page)
			return nil
		}

		if jsonOut {
			return printJSON(result)
		}

		lipgloss.Fprintf(os.Stdout, "%s %s\n\n", colorBold("Download history page"), colorBold(fmt.Sprintf("%d", result.Page)))

		rows := make([][]string, 0, len(result.Items))
		for i, item := range result.Items {
			id := item.ID
			if id == "" {
				id = "-"
			}
			ext := item.Extension
			if ext == "" {
				ext = "-"
			}
			size := item.Size
			if size == "" {
				size = "-"
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), id,
				runewidth.Truncate(item.Name, 50, ""),
				ext, size, item.Date,
			})
		}

		t := table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(currentTheme.Accent)).
			Headers("#", "ID", "Title", "Format", "Size", "Date").
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
				case 3: // Format
					return pad.Foreground(extLipglossColor(rows[row][3]))
				case 0, 4, 5: // #, Size, Date
					return pad.Foreground(currentTheme.Muted)
				}
				return pad
			})

		lipgloss.Fprintln(os.Stdout, t)

		if len(rows) == 0 && formatFilter != "" {
			lipgloss.Fprintf(os.Stdout, "No %s books found on page %d.\n", strings.ToUpper(formatFilter), result.Page)
		}
		return nil
	},
}

func init() {
	historyCmd.Flags().IntP("page", "p", 1, "Page number")
	historyCmd.Flags().StringP("download", "D", "", "Download a book from history by ID")
	addDestDirFlag(historyCmd)
	addDeliveryFlags(historyCmd)
	historyCmd.Flags().StringP("format", "f", "", "Filter by file format, e.g. epub")
	historyCmd.Flags().Bool("json", false, "Print history as JSON")
	historyCmd.Flags().String("delete", "", "Delete a history entry on Z-Library by ID or id:hash; local files stay")
	historyCmd.MarkFlagsMutuallyExclusive("delete", "download")
	historyCmd.MarkFlagsMutuallyExclusive("delete", "page")
	historyCmd.MarkFlagsMutuallyExclusive("delete", "format")
	historyCmd.MarkFlagsMutuallyExclusive("delete", "send-to-kindle")
	historyCmd.MarkFlagsMutuallyExclusive("delete", "send-to-weread")
	rootCmd.AddCommand(historyCmd)
}

func findHistoryItemByBookID(c *zlib.Client, bookID string) (zlib.DownloadHistoryItem, error) {
	const maxHistoryPages = 20

	for page := 1; page <= maxHistoryPages; page++ {
		result, err := c.DownloadHistory(page)
		if err != nil {
			return zlib.DownloadHistoryItem{}, fmt.Errorf("failed to load history page %d: %w", page, err)
		}
		if len(result.Items) == 0 {
			break
		}

		for _, item := range result.Items {
			if historyCommandID(item) == bookID {
				return item, nil
			}
		}
	}

	return zlib.DownloadHistoryItem{}, fmt.Errorf("book %s not found in the first %d history pages", bookID, maxHistoryPages)
}

// historyDirectDownload downloads a history entry from its known URL. It keeps
// the entry's id:hash reference so `history --download --json` reports it.
func historyDirectDownload(item zlib.DownloadHistoryItem, bookName, dir string, c *zlib.Client) downloadModel {
	m := newDirectDownloadModel(bookName, item.DownloadURL, dir, c)
	m.id = historyCommandID(item)
	return m
}
