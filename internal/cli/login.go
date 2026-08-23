package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
		cookieFile, _ := cmd.Flags().GetString("cookies")
		domain, _ := cmd.Flags().GetString("domain")
		eapi, _ := cmd.Flags().GetBool("eapi")
		domain, err := resolveLoginDomain(domain, eapi)
		if err != nil {
			return err
		}
		if eapi {
			check := zlib.ProbeDomainWithOptions(cmd.Context(), domain, zlib.DomainProbeOptions{Path: "/eapi/info/domains"})
			if check.Status != zlib.DomainStatusHealthy {
				return fmt.Errorf("ZLIB_DOMAIN=%s is not usable for EAPI (%s): set ZLIB_DOMAIN to a domain reported healthy by `zlib doctor --eapi`, or pass --domain", domain, check.Status)
			}
		}
		mode := ""
		if eapi {
			mode = string(zlib.ClientModeEAPI)
		}
		if cookieFile != "" {
			if email != "" || password != "" {
				return fmt.Errorf("--cookies cannot be combined with --email or --password")
			}
			count, err := importCookieSession(cookieFile, domain, time.Now(), mode)
			if err != nil {
				return fmt.Errorf("failed to import cookies: %w", err)
			}
			fmt.Printf("%s Imported %d cookies.\n", colorGreen(symbolSuccess), count)
			return nil
		}
		if email == "" {
			email = rememberedLoginEmail()
		}

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
		c.SetDomain(domain)
		var loginErr error
		if eapi {
			loginErr = c.LoginEAPI(email, password)
		} else {
			loginErr = c.Login(email, password)
		}
		if loginErr != nil {
			return fmt.Errorf("login failed: %w", loginErr)
		}

		if err := config.SaveSession(config.Session{
			Cookies: c.Cookies(),
			Domain:  c.Domain(),
			Mode:    mode,
		}); err != nil {
			return fmt.Errorf("failed to save session: %w", err)
		}
		if err := rememberLoginEmail(email); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Could not remember login email: %v\n", err)
		}

		fmt.Printf("%s Logged in successfully.\n", colorGreen(symbolSuccess))
		return nil
	},
}

func resolveLoginDomain(flagDomain string, eapi bool) (string, error) {
	if domain := strings.TrimSpace(flagDomain); domain != "" {
		allowed, err := zlib.ParseAllowedDomain(domain)
		if err != nil && eapi {
			return "", fmt.Errorf("EAPI requires an allowed Z-Library domain: %w", err)
		}
		return allowed, err
	}

	if domain := strings.TrimSpace(os.Getenv(zlib.EnvDomain)); domain != "" {
		allowed, err := zlib.ParseAllowedDomain(domain)
		if err != nil && eapi {
			return "", fmt.Errorf("EAPI requires an allowed Z-Library domain in %s: %w", zlib.EnvDomain, err)
		}
		return allowed, err
	}

	example := "https://z-lib.sk"
	if eapi {
		example = zlib.DefaultEAPIDomain
	}
	return "", fmt.Errorf("domain is not configured: pass --domain %s or set %s=%s (for example in ~/.config/zlib/.env)", example, zlib.EnvDomain, example)
}

// rememberedLoginEmail returns the most recently successful login email. It
// is a convenience for the TUI only; passwords are never persisted.
func rememberedLoginEmail() string {
	cfg, err := config.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.LastLoginEmail)
}

func rememberLoginEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}

	cfg, err := config.LoadConfig()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg.LastLoginEmail = email
	return config.SaveConfig(cfg)
}

func init() {
	loginCmd.Flags().String("email", "", "Z-Library email")
	loginCmd.Flags().String("password", "", "Z-Library password")
	loginCmd.Flags().String("cookies", "", "Import a Netscape-format cookie file")
	loginCmd.Flags().String("domain", "", "Login domain (otherwise ZLIB_DOMAIN)")
	loginCmd.Flags().Bool("eapi", false, "Use the unofficial mobile-app EAPI")
}
