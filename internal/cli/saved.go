package cli

import (
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

var favoritesCmd = newSavedBooksCommand("favorites")

func newSavedBooksCommand(name string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name,
		Short: "Show favorite books",
		Args:  cobra.NoArgs,
		RunE:  runSavedBooks,
	}
	cmd.Flags().IntP("page", "p", 1, "Page number")
	cmd.Flags().Bool("json", false, "Print favorites as JSON")
	addDestDirFlag(cmd)
	addDeliveryFlags(cmd)
	return cmd
}

func runSavedBooks(cmd *cobra.Command, args []string) error {
	c, err := loadClient()
	if err != nil {
		return err
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	if !jsonOut && !cmd.Flags().Changed("page") && isInteractive() {
		p := tea.NewProgram(newSavedBooksSelectModel(c.SavedBooks).withBookDetails(c))
		result, err := p.Run()
		if err != nil {
			return err
		}
		selected := result.(searchSelectModel)
		if selected.err != nil {
			return selected.err
		}
		if selected.selectedID == "" {
			return nil
		}
		dir, err := destDirFlag(cmd)
		if err != nil {
			return err
		}
		deliver := deliveryFlags(cmd)
		return runDownload(newDownloadModel(selected.selectedID, dir, c), deliver, cmd)
	}

	page, _ := cmd.Flags().GetInt("page")
	books, err := c.SavedBooks(page)
	if err != nil {
		return err
	}
	if jsonOut {
		books.Books = downloadableBooks(books.Books)
		return printJSON(books)
	}
	if len(books.Books) == 0 {
		lipgloss.Fprintf(cmd.OutOrStdout(), "No favorites found on page %d.\n", books.Page)
		return nil
	}
	lipgloss.Fprintf(cmd.OutOrStdout(), "Favorites, page %d of %d\n\n", books.Page, books.TotalPages)
	printBookTable(cmd.OutOrStdout(), books.Books)
	return nil
}

func init() {

	rootCmd.AddCommand(favoritesCmd)
}
