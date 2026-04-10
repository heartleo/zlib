package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/heartleo/zlib"
	"github.com/heartleo/zlib/internal/config"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Z-Library",
	Long:  `Log in to Z-Library and save session cookies`,
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")

		if email == "" || password == "" {
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Email").
						Value(&email),
					huh.NewInput().
						Title("Password").
						EchoMode(huh.EchoModePassword).
						Value(&password),
				),
			).WithTheme(huhTheme())
			if err := form.Run(); err != nil {
				return err
			}
		}

		c := zlib.NewClient()
		if err := c.Login(email, password); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		if err := config.SaveSession(config.Session{
			Cookies: c.Cookies(),
			Domain:  c.Domain(),
		}); err != nil {
			return fmt.Errorf("failed to save session: %w", err)
		}

		fmt.Printf("%s Logged in successfully.\n", colorGreen(symbolSuccess))
		return nil
	},
}

func init() {
	loginCmd.Flags().String("email", "", "Z-Library email")
	loginCmd.Flags().String("password", "", "Z-Library password")
}
