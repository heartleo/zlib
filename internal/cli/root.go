package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/heartleo/zlib"
	"github.com/heartleo/zlib/internal/config"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:               "zlib",
	Short:             "A terminal client for Z-Library.",
	Version:           zlib.Version,
	SilenceUsage:      true,
	SilenceErrors:     true,
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s version %s\n", rootCmd.Name(), zlib.Version)
	},
}

// newClient loads session and returns an authenticated client.
func newClient() *zlib.Client {
	session, err := config.LoadSession()
	if err != nil || len(session.Cookies) == 0 {
		fmt.Fprintln(os.Stderr, "Not logged in. Run: zlib login")
		os.Exit(1)
	}
	c := zlib.NewClient()
	if session.Domain != "" {
		c.SetDomain(session.Domain)
	}
	c.SetCookies(session.Cookies)
	return c
}

func Execute() {
	if err := loadDotEnv(); err != nil {
		fmt.Fprintln(os.Stderr, formatCLIError(err))
		os.Exit(1)
	}
	resolveTheme()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, formatCLIError(err))
		os.Exit(1)
	}
}

func formatCLIError(err error) string {
	if err == nil {
		return ""
	}

	prefix := colorRed(symbolError + " Error:")
	if msg, ok := classifyNetworkError(err); ok {
		return prefix + " " + msg
	}

	return prefix + " " + err.Error()
}

func classifyNetworkError(err error) (string, bool) {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if timeoutErr, ok := urlErr.Err.(interface{ Timeout() bool }); ok && timeoutErr.Timeout() {
			return "network request timed out. Please check your connection and try again.", true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network request timed out. Please check your connection and try again.", true
	}

	switch {
	case errors.Is(err, io.EOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH):
		return "network request failed. Please check your connection and try again.", true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "could not resolve the server address. Please check your network or DNS settings and try again.", true
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
		return "network request failed. Please check your connection and try again.", true
	}

	return "", false
}

func init() {
	runewidth.DefaultCondition.EastAsianWidth = false
	rootCmd.SetVersionTemplate("{{printf \"%s version %s\\n\" .Name .Version}}")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(kindleCmd)
	rootCmd.AddCommand(themeCmd)
	rootCmd.AddCommand(versionCmd)
}
