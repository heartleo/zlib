package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/heartleo/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show download history",
	Long:  "Show download history. Without flags, opens an interactive browser with pagination and selection.",
	RunE: func(cmd *cobra.Command, args []string) error {
		page, _ := cmd.Flags().GetInt("page")
		downloadID, _ := cmd.Flags().GetString("download")
		dir, _ := cmd.Flags().GetString("dir")
		sendToKindle, _ := cmd.Flags().GetBool("send-to-kindle")
		formatFilter, _ := cmd.Flags().GetString("format")

		c := newClient()

		if downloadID != "" {
			item, err := findHistoryItemByBookID(c, downloadID)
			if err != nil {
				return err
			}
			if item.DownloadURL == "" {
				return fmt.Errorf("no history download URL available for book %s", downloadID)
			}

			bookName := item.Name
			if bookName == "" {
				bookName = downloadID
			}
			p := tea.NewProgram(newDirectDownloadModel(bookName, item.DownloadURL, dir, c))
			res, err := p.Run()
			if err != nil {
				return err
			}
			dm := res.(downloadModel)
			if dm.state == stateCancelled {
				return nil
			}
			if dm.err != nil {
				return dm.err
			}
			fmt.Println()
			fmt.Printf("%s Saved to: %s (%d bytes)\n", colorGreen(symbolSuccess), colorGreen(tildePath(dm.savedPath)), dm.savedSize)
			if sendToKindle {
				if err := sendDownloadedFileToKindle(dm.savedPath); err != nil {
					return fmt.Errorf("send to kindle failed: %w", err)
				}
			}
			return nil
		}

		// No flags: interactive mode
		if !cmd.Flags().Changed("page") && formatFilter == "" {
			p := tea.NewProgram(newHistorySelectModel(c))
			result, err := p.Run()
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
			bookName := item.Name
			if bookName == "" {
				bookName = bookIDFromURL(item.URL)
			}
			dp := tea.NewProgram(newDirectDownloadModel(bookName, item.DownloadURL, dir, c))
			res, err := dp.Run()
			if err != nil {
				return err
			}
			dm := res.(downloadModel)
			if dm.state == stateCancelled {
				return nil
			}
			if dm.err != nil {
				return dm.err
			}
			fmt.Println()
			fmt.Printf("%s Saved to: %s (%d bytes)\n", colorGreen(symbolSuccess), colorGreen(tildePath(dm.savedPath)), dm.savedSize)
			if sendToKindle {
				if err := sendDownloadedFileToKindle(dm.savedPath); err != nil {
					return fmt.Errorf("send to kindle failed: %w", err)
				}
			}
			return nil
		}

		result, err := c.DownloadHistory(page)
		if err != nil {
			return err
		}

		if len(result.Items) == 0 {
			fmt.Printf("No download history found on page %d.\n", result.Page)
			return nil
		}

		// Collect book IDs that need detail fetch for Extension/Size
		var missingIDs []string
		for _, item := range result.Items {
			if id := bookIDFromURL(item.URL); id != "" && id != item.URL && item.Extension == "" {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) > 0 {
			details := c.FetchBookDetails(missingIDs)
			for i, item := range result.Items {
				id := bookIDFromURL(item.URL)
				if d, ok := details[id]; ok {
					if result.Items[i].Extension == "" {
						result.Items[i].Extension = d.Extension
					}
					if result.Items[i].Size == "" {
						result.Items[i].Size = d.Size
					}
				}
			}
		}

		fmt.Printf("%s %s\n\n", colorBold("Download history page"), colorBold(fmt.Sprintf("%d", result.Page)))

		rows := make([][]string, 0, len(result.Items))
		for i, item := range result.Items {
			if formatFilter != "" && !strings.EqualFold(item.Extension, formatFilter) {
				continue
			}
			id := bookIDFromURL(item.URL)
			if id == item.URL {
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

		fmt.Println(t)

		if len(rows) == 0 && formatFilter != "" {
			fmt.Printf("No %s books found on page %d.\n", strings.ToUpper(formatFilter), result.Page)
		}
		return nil
	},
}

func init() {
	historyCmd.Flags().IntP("page", "p", 1, "Page number")
	historyCmd.Flags().StringP("download", "D", "", "Download a history book by book ID")
	historyCmd.Flags().StringP("dir", "d", ".", "Destination directory.")
	historyCmd.Flags().Bool("send-to-kindle", false, "Send the downloaded file to Kindle.")
	historyCmd.Flags().StringP("format", "f", "", "Filter by file format (e.g. epub, pdf)")
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
			if bookIDFromURL(item.URL) == bookID {
				return item, nil
			}
		}
	}

	return zlib.DownloadHistoryItem{}, fmt.Errorf("book %s not found in the first %d history pages", bookID, maxHistoryPages)
}
