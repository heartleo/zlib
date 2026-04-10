package cli

import (
	"fmt"
	"os"

	"github.com/heartleo/zlib/internal/config"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out from Z-Library",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.Remove(config.SessionPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("%s Logged out.\n", colorFaint(symbolSuccess))
		return nil
	},
}
