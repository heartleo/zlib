package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
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
			resolved, err := resolveEAPIDomain(cmd.Context(), domain)
			if err != nil {
				return err
			}
			if resolved != domain {
				fmt.Printf("%s %s redirects to %s; logging in there.\n", colorCyan(symbolInfo), domain, resolved)
				domain = resolved
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
			reportPersistedDomain(cmd.OutOrStdout(), cmd.ErrOrStderr(), domain)
			return nil
		}
		// Decide whether to prompt from the flags, before the remembered email
		// is filled in. Letting the prefill satisfy the check would make
		// `zlib login --password X` log into whatever account was remembered,
		// without ever showing the user which one.
		promptNeeded := email == "" || password == ""
		if email == "" {
			email = rememberedLoginEmail()
		}

		if promptNeeded {
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
		reportPersistedDomain(cmd.OutOrStdout(), cmd.ErrOrStderr(), c.Domain())
		return nil
	},
}

// reportPersistedDomain records the domain a login just used as ZLIB_DOMAIN in
// the config-dir .env and says so. A failure here is reported but not fatal:
// the login itself succeeded and the session already carries the domain.
func reportPersistedDomain(stdout, stderr io.Writer, domain string) {
	path, changed, err := persistDotEnvDomain(domain)
	if err != nil {
		fmt.Fprintf(stderr, "Could not record %s: %v\n", zlib.EnvDomain, err)
		return
	}
	if changed {
		fmt.Fprintf(stdout, "%s Wrote %s=%s to %s\n", colorCyan(symbolInfo), zlib.EnvDomain, domain, path)
	}
}

// eapiProbePath is the unauthenticated endpoint resolveEAPIDomain uses to
// decide whether an origin speaks the EAPI at all.
const eapiProbePath = "/eapi/info/domains"

// resolveEAPIDomain checks that domain answers the EAPI and follows a pure
// redirector to the origin that actually serves it.
//
// Adopting the final origin is not cosmetic. net/http turns a 302 POST into a
// GET and drops the body, so posting the login form at a redirector (zliba.ru
// 302s to zlib.bz) arrives at the target as a bare GET and comes back 404 --
// the failure reported in issue #18. Resolving up front is also what keeps the
// saved session domain on a host the allowlist accepts later.
func resolveEAPIDomain(ctx context.Context, domain string) (string, error) {
	check := zlib.ProbeDomainWithOptions(ctx, domain, zlib.DomainProbeOptions{Path: eapiProbePath})
	if check.Status != zlib.DomainStatusHealthy {
		return "", fmt.Errorf("%s is not usable for EAPI (%s): set %s to a domain reported healthy by `zlib doctor --eapi`, or pass --domain", domain, check.Status, zlib.EnvDomain)
	}

	requested, err := url.Parse(domain)
	if err != nil {
		return domain, nil
	}
	finalURL, err := url.Parse(check.FinalURL)
	if err != nil || finalURL.Host == "" || strings.EqualFold(finalURL.Host, requested.Host) {
		return domain, nil
	}
	// The probe left the requested host, so the adopted origin has to clear the
	// allowlist on its own. resolveLoginDomain only vetted what the user typed.
	resolved, err := zlib.ParseAllowedDomain(finalURL.Scheme + "://" + finalURL.Host)
	if err != nil {
		return "", fmt.Errorf("%s redirects to %s, which is not an allowed Z-Library domain: %w", domain, finalURL.Host, err)
	}
	return resolved, nil
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
