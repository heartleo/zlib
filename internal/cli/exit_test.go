package cli

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

func TestExitCodeClassifiesCommandErrors(t *testing.T) {
	networkErr := &url.Error{Op: "Get", URL: "https://z-lib.gd/eapi/user/profile", Err: io.EOF}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, exitOK},
		{"generic", errors.New("invalid year range"), exitError},
		{"usage", &usageError{errors.New(`unknown flag: --bogus`)}, exitUsage},
		{"no session", errNotLoggedIn, exitAuth},
		{"library not logged in", zlib.ErrNotLoggedIn, exitAuth},
		{"session expired", fmt.Errorf("load favorites: %w", zlib.ErrSessionExpired), exitAuth},
		{"bot protection", &zlib.BotProtectionError{StatusCode: 513, URL: "https://z-lib.fm/"}, exitNetwork},
		{"no domain", fmt.Errorf("%w: none answered", zlib.ErrNoDomain), exitNetwork},
		{"network", networkErr, exitNetwork},
		{"kindle after download", &deliveryError{target: targetKindle, path: "/books/a.epub", err: fmt.Errorf("%w: %w", zlib.ErrSMTPSend, io.EOF)}, exitPartial},
		// A failed `zlib kindle send` must not read as a network outage: the
		// SMTP error names the server and the TLS mode itself.
		{"smtp", fmt.Errorf("%w: smtp.example.com:587: %w", zlib.ErrSMTPSend, io.EOF), exitError},
		{"cancelled", errCancelled, exitCancelled},
	}
	for _, tt := range tests {
		if got := exitCode(tt.err); got != tt.want {
			t.Errorf("%s: exitCode(%v) = %d, want %d", tt.name, tt.err, got, tt.want)
		}
	}
}

// The command tree is built per case: parsing a bad flag value leaves it set
// on the command, which would leak into other tests through rootCmd.
func TestCommandLineMistakesAreUsageErrors(t *testing.T) {
	newTree := func() *cobra.Command {
		root := &cobra.Command{Use: "zlib", SilenceErrors: true, SilenceUsage: true}
		download := &cobra.Command{Use: "download", Args: cobra.ExactArgs(1), RunE: func(*cobra.Command, []string) error { return nil }}
		download.Flags().Int("page", 1, "")
		root.AddCommand(download)
		markUsageErrors(root)
		return root
	}
	for _, args := range [][]string{
		{"download"},                       // missing argument
		{"download", "1:a", "2:b"},         // too many arguments
		{"download", "--bogus", "1:a"},     // unknown flag
		{"download", "--page", "x", "1:a"}, // invalid flag value
	} {
		root := newTree()
		root.SetArgs(args)
		err := root.Execute()
		var usage *usageError
		if !errors.As(err, &usage) || exitCode(err) != exitUsage {
			t.Errorf("zlib %s: error = %v, want a usage error", strings.Join(args, " "), err)
		}
	}
	root := newTree()
	root.SetArgs([]string{"serach"})
	if err := root.Execute(); err == nil || !isUnknownCommandError(err) {
		t.Fatalf("unknown command error = %v", err)
	}
}

// Without a session, search must fail before the filter form opens rather
// than after the user has filled it in.
func TestSearchWithoutQueryChecksSessionBeforeForm(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(zlib.EnvDomain, "")
	// RunE directly: executing rootCmd here changes how later tests see it.
	err := searchCmd.RunE(searchCmd, nil)
	if !errors.Is(err, errNotLoggedIn) {
		t.Fatalf("search error = %v, want errNotLoggedIn", err)
	}
	if got := ansi.Strip(formatCLIError(err)); got != "✗ Error: not logged in. Run `zlib login` first." {
		t.Fatalf("formatted = %q", got)
	}
}

func TestFormatCLIErrorNamesTheRemedy(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			"session expired",
			zlib.ErrSessionExpired,
			"✗ Error: the saved session expired or was revoked. Run `zlib login` to sign in again.",
		},
		{
			"mirror unreachable",
			&url.Error{Op: "Get", URL: "https://z-lib.gd/eapi/user/profile", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}},
			"✗ Error: network request failed (z-lib.gd). Check your connection, or run `zlib doctor` to find a reachable domain.",
		},
		{
			// zlib update talks to GitHub; switching mirrors would not help.
			"other host",
			&url.Error{Op: "Get", URL: "https://api.github.com/repos/heartleo/zlib/releases/latest", Err: io.EOF},
			"✗ Error: network request failed (api.github.com). Please check your connection and try again.",
		},
		{
			"diamwall",
			&zlib.BotProtectionError{StatusCode: 513, URL: "https://z-lib.fm/eapi/book/search"},
			"✗ Error: zlibrary: blocked by anti-bot protection (HTTP 513) at https://z-lib.fm/eapi/book/search. This mirror is behind DiamWall; run `zlib doctor` and set ZLIB_DOMAIN to a reachable domain.",
		},
		{
			"kindle after download",
			&deliveryError{target: targetKindle, path: "/books/a.epub", err: errors.New("kindle smtp password not set")},
			"✗ Error: book saved to /books/a.epub, but Send to Kindle failed: kindle smtp password not set",
		},
		{"cancelled", errCancelled, "✗ Cancelled"},
	}
	for _, tt := range tests {
		if got := ansi.Strip(formatCLIError(tt.err)); got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestShellErrorTextPointsToSlashLogin(t *testing.T) {
	if got := shellErrorText(errNotLoggedIn); !strings.Contains(got, "/login") || strings.Contains(got, "zlib login") {
		t.Fatalf("not logged in = %q", got)
	}
	if got := shellErrorText(fmt.Errorf("history: %w", zlib.ErrSessionExpired)); !strings.Contains(got, "/login") {
		t.Fatalf("session expired = %q", got)
	}
}

// Error text can quote a server; it must not reach the terminal as an escape
// sequence, in the CLI error line or in shell feedback.
func TestErrorOutputsDropControlCharacters(t *testing.T) {
	hostile := errors.New("zlibrary: EAPI search failed: limit\x1b]52;c;ZXZpbA==\x07\x1b[2J")
	if got := formatCLIError(hostile); strings.Contains(ansi.Strip(got), "\x1b") || strings.Contains(got, "\x07") || strings.Contains(got, "]52;c;ZXZpbA==\x07") {
		t.Fatalf("formatCLIError kept control characters: %q", got)
	}
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m.setError(hostile.Error())
	if view := m.View().Content; strings.Contains(view, "\x07") || strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x1b[2J") {
		t.Fatalf("shell notice kept control characters: %q", view)
	}
}
