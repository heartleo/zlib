package cli

import (
	"charm.land/lipgloss/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:               "zlib",
	Short:             "Search, download, and send Z-Library books to Kindle",
	Version:           zlib.Version,
	SilenceUsage:      true,
	SilenceErrors:     true,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the zlib version",
	Run: func(cmd *cobra.Command, args []string) {
		lipgloss.Fprintf(cmd.OutOrStdout(), "%s version %s\n", rootCmd.Name(), zlib.Version)
	},
}

// resolveClientDomain picks the domain an authenticated client should use.
// ZLIB_DOMAIN is current, explicit user intent, so it outranks the domain
// session.json cached back when the user logged in. An empty result means the
// client should keep the default it already resolved.
func resolveClientDomain(envDomain, sessionDomain string) string {
	if domain := strings.TrimSpace(envDomain); domain != "" {
		return domain
	}
	return strings.TrimSpace(sessionDomain)
}

// loadClient builds the authenticated client for a command. It is a variable
// only so the demo recorder (demo_test.go) can serve fixture data instead.
var loadClient = loadSessionClient

func loadSessionClient() (*zlib.Client, error) {
	session, err := config.LoadSession()
	if err != nil || len(session.Cookies) == 0 {
		return nil, errNotLoggedIn
	}
	c := zlib.NewClient()
	// SetDomain before SetCookies: SetCookies seats the jar against c.domain.
	domain := resolveClientDomain(os.Getenv(zlib.EnvDomain), session.Domain)
	if domain == "" {
		return nil, fmt.Errorf("domain is not configured; use login or config")
	}
	allowedDomain, err := zlib.ParseAllowedDomain(domain)
	if err != nil {
		return nil, fmt.Errorf("invalid domain: %w", err)
	}
	c.SetDomain(allowedDomain)
	c.SetCookies(session.Cookies)
	if dir, err := os.UserCacheDir(); err == nil {
		c.SetCoverCacheDir(filepath.Join(dir, "zlib", "covers"))
	}
	return c, nil
}

func Execute() {
	if err := loadDotEnv(); err != nil {
		lipgloss.Fprintln(os.Stderr, formatCLIError(err))
		os.Exit(1)
	}
	// A bad mirror entry must not lock the user out of every command: the
	// valid mirrors still apply, and only the warning reaches stderr.
	if err := loadUserMirrors(); err != nil {
		lipgloss.Fprintln(os.Stderr, colorFaint(symbolInfo)+" "+safeText(err.Error()))
	}
	resolveTheme()
	markUsageErrors(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		if isUnknownCommandError(err) {
			err = &usageError{err}
		}
		lipgloss.Fprintln(os.Stderr, formatCLIError(err))
		os.Exit(exitCode(err))
	}
}

func formatCLIError(err error) string {
	if err == nil {
		return ""
	}

	prefix := colorRed(symbolError + " Error:")
	if errors.Is(err, errCancelled) {
		return colorRed(symbolError) + " Cancelled"
	}
	var delivery *deliveryError
	if hint := wereadErrorHint(err, wereadLoginCLI); hint != "" {
		return prefix + " " + safeText(err.Error()) + ". " + hint
	}
	switch {
	// SMTP errors already name the server, port and TLS mode; a generic
	// network message would hide whether TLS, auth or the port is wrong.
	case errors.As(err, &delivery), errors.Is(err, zlib.ErrSMTPSend):
		return prefix + " " + safeText(err.Error())
	case errors.Is(err, errNotLoggedIn), errors.Is(err, zlib.ErrNotLoggedIn):
		return prefix + " not logged in. Run `zlib login` first."
	case errors.Is(err, zlib.ErrSessionExpired):
		return prefix + " the saved session expired or was revoked. Run `zlib login` to sign in again."
	case errors.Is(err, zlib.ErrBotProtection):
		return prefix + " " + safeText(err.Error()) + ". This mirror is behind DiamWall; run `zlib doctor` and set ZLIB_DOMAIN to a reachable domain."
	}
	if msg, ok := classifyNetworkError(err); ok {
		return prefix + " " + msg
	}

	return prefix + " " + safeText(err.Error())
}

// networkFailure words a network error, naming the host that failed. For a
// Z-Library mirror it points to `zlib doctor`, since switching domains is the
// usual fix; other hosts (GitHub for `zlib update`, a CDN) get no such hint.
func networkFailure(err error, problem string) string {
	host := failedHost(err)
	if host == "" {
		return problem + ". Please check your connection and try again."
	}
	if _, allowErr := zlib.ParseAllowedDomain("https://" + host); allowErr == nil {
		return fmt.Sprintf("%s (%s). Check your connection, or run `zlib doctor` to find a reachable domain.", problem, host)
	}
	return fmt.Sprintf("%s (%s). Please check your connection and try again.", problem, host)
}

// failedHost returns the host of the request that failed, when known.
func failedHost(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if u, parseErr := url.Parse(urlErr.URL); parseErr == nil {
			return u.Hostname()
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.Name
	}
	return ""
}

func classifyNetworkError(err error) (string, bool) {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if timeoutErr, ok := urlErr.Err.(interface{ Timeout() bool }); ok && timeoutErr.Timeout() {
			return networkFailure(err, "network request timed out"), true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return networkFailure(err, "network request timed out"), true
	}

	switch {
	case errors.Is(err, io.EOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH):
		return networkFailure(err, "network request failed"), true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return networkFailure(err, "could not resolve the server address"), true
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection aborted"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "server misbehaving"),
		strings.Contains(msg, "unexpected eof"),
		strings.Contains(msg, " eof"):
		return networkFailure(err, "network request failed"), true
	}

	return "", false
}

func init() {
	runewidth.DefaultCondition.EastAsianWidth = false
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			lipgloss.Fprintln(cmd.OutOrStdout(), "\n"+strings.Join(homeWordmark(), "\n")+"\n")
		}
		defaultHelp(cmd, args)
	})
	rootCmd.SetVersionTemplate("{{printf \"%s version %s\\n\" .Name .Version}}")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(recommendedCmd)
	rootCmd.AddCommand(mostPopularCmd)
	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(kindleCmd)
	rootCmd.AddCommand(themeCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
}
