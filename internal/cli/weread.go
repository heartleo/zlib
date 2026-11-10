package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
	"github.com/spf13/cobra"
)

const wereadLong = `Import local ebooks (EPUB, PDF, MOBI, AZW3, TXT) into your WeRead shelf.
Without a subcommand, pick a file in a terminal.

WeRead has no public upload API. zlib signs in as the WeRead e-ink app and
uses its unofficial endpoints, which may change or ask for verification in
the official app. TXT files can be up to 20 MiB and other formats up to
300 MiB. Without a paid membership, WeRead keeps at most 200 imported books on
the shelf and may apply further limits of its own.`

var wereadCmd = &cobra.Command{
	Use:   "weread",
	Short: "Import books into WeRead",
	Long:  wereadLong,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isInteractive() {
			return &usageError{errors.New("provide a file: zlib weread import <file>")}
		}
		return runStandaloneShellCommand("/weread")
	},
}

var wereadLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to WeRead by scanning a QR code with WeChat",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if isInteractive() {
			return runStandaloneShellCommand("/weread login")
		}
		return runWeReadLoginPlain(cmd.Context(), os.Stdout, os.Stderr)
	},
}

var wereadLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Delete the saved WeRead login",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, err := config.RemoveWeRead()
		if err != nil {
			return err
		}
		if !removed {
			lipgloss.Fprintln(os.Stdout, "Not logged in to WeRead.")
			return nil
		}
		lipgloss.Fprintf(os.Stdout, "%s Logged out of WeRead.\n", colorFaint(symbolSuccess))
		return nil
	},
}

var wereadImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import a local file into the WeRead shelf",
	Long:  "Import a local EPUB, PDF, MOBI, AZW3 or TXT file into the WeRead shelf. Without a file, open the file picker.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		if len(args) == 0 {
			if jsonOut || !isInteractive() {
				return &usageError{errors.New("provide a file: zlib weread import <file>")}
			}
			return runStandaloneShellCommand("/weread")
		}
		path, err := expandTilde(args[0])
		if err != nil {
			return err
		}
		if isInteractive() && !jsonOut {
			return runStandaloneShellCommand("/weread import " + path)
		}
		result, err := importToWeRead(cmd.Context(), path, os.Stderr)
		if err != nil {
			return err
		}
		if jsonOut {
			abs, _ := filepath.Abs(path)
			return printJSON(wereadImportOutput{Path: abs, Name: result.Name, BookID: result.BookID, AlreadyExisted: result.AlreadyExisted})
		}
		printWeReadImported(result)
		return nil
	},
}

var wereadWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the WeRead account that is logged in",
	Long:  "Fetch the logged-in account's nickname from WeRead, save it for display, and print it.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadWeReadClient()
		if err != nil {
			return err
		}
		if keysOnly, _ := cmd.Flags().GetBool("keys"); keysOnly {
			info, err := c.UserInfo(cmd.Context())
			if err != nil {
				return err
			}
			// Field names only, never values: safe to share for diagnosis.
			for _, k := range info.Keys {
				lipgloss.Fprintln(cmd.OutOrStdout(), safeText(k))
			}
			return nil
		}
		if _, err := c.UpdateName(cmd.Context()); err != nil {
			return err
		}
		creds := c.Credentials()
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			return printJSON(wereadWhoamiOutput{VID: creds.VID, Name: creds.Name})
		}
		printWeReadAccount(cmd.OutOrStdout(), "WeRead account", creds)
		if creds.Name == "" {
			lipgloss.Fprintf(cmd.OutOrStdout(), "%s WeRead returned no nickname. Run `zlib weread whoami --keys` and report the field names.\n", colorFaint(symbolInfo))
		}
		return nil
	},
}

// wereadWhoamiOutput is the `zlib weread whoami --json` document.
type wereadWhoamiOutput struct {
	VID  string `json:"vid"`
	Name string `json:"name"`
}

func init() {
	wereadImportCmd.Flags().Bool("json", false, "Print the imported book as JSON; progress goes to stderr")
	wereadWhoamiCmd.Flags().Bool("json", false, "Print the account as JSON")
	wereadWhoamiCmd.Flags().Bool("keys", false, "Print only the field names of WeRead's reply, without values")
	_ = wereadWhoamiCmd.Flags().MarkHidden("keys")
	wereadCmd.AddCommand(wereadLoginCmd, wereadLogoutCmd, wereadImportCmd, wereadWhoamiCmd)
	rootCmd.AddCommand(wereadCmd)
}

// wereadImportOutput is the `zlib weread import --json` document.
type wereadImportOutput struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	BookID string `json:"book_id"`
	// AlreadyExisted reports that WeRead already had the file and returned
	// the existing book rather than a new copy.
	AlreadyExisted bool `json:"already_existed"`
}

// wereadResult reports the Send to WeRead step of a download.
type wereadResult struct {
	Imported       bool   `json:"imported"`
	BookID         string `json:"book_id,omitempty"`
	AlreadyExisted bool   `json:"already_existed,omitempty"`
	Error          string `json:"error,omitempty"`
}

// newWeReadResult reports an import outcome for a download document.
func newWeReadResult(result weread.ImportResult, err error) *wereadResult {
	out := &wereadResult{Imported: err == nil, BookID: result.BookID, AlreadyExisted: result.AlreadyExisted}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

// wereadProxyEnv routes WeRead traffic. Empty uses the system proxy
// environment (HTTPS_PROXY, NO_PROXY); "direct" bypasses every proxy, for
// when a global proxy points abroad and WeRead's risk control objects.
// ZLIB_PROXY never applies to WeRead.
const (
	wereadProxyEnv    = "ZLIB_WEREAD_PROXY"
	wereadProxyDirect = "direct"
)

// validateWeReadProxy accepts empty, "direct" or a proxy URL.
func validateWeReadProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, wereadProxyDirect) {
		return wereadProxyDirect, nil
	}
	return validateDoctorProxy(raw)
}

// wereadTransport returns the transport for a ZLIB_WEREAD_PROXY value, or
// nil for the default. An invalid value fails every request with a clear
// error rather than silently using another route.
func wereadTransport(raw string) http.RoundTripper {
	raw, err := validateWeReadProxy(raw)
	if err == nil && raw == "" {
		return nil
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}
	t := base.Clone()
	switch {
	case err != nil:
		t.Proxy = func(*http.Request) (*url.URL, error) {
			return nil, fmt.Errorf("invalid %s: %w", wereadProxyEnv, err)
		}
	case raw == wereadProxyDirect:
		t.Proxy = nil
	default:
		u, _ := url.Parse(raw)
		t.Proxy = http.ProxyURL(u)
	}
	return t
}

// newWeReadClient builds every WeRead client with the configured proxy;
// tests replace it to stay offline.
var newWeReadClient = func(opts ...weread.Option) *weread.Client {
	if t := wereadTransport(os.Getenv(wereadProxyEnv)); t != nil {
		opts = append([]weread.Option{weread.WithTransport(t)}, opts...)
	}
	return weread.NewClient(opts...)
}

// loadWeReadClient builds a client from the saved login. Rotated tokens are
// written back as WeRead issues them.
func loadWeReadClient() (*weread.Client, error) {
	creds, err := config.LoadWeRead()
	if err != nil {
		return nil, err
	}
	return newWeReadClient(weread.WithCredentials(creds), weread.WithCredentialsSaver(config.WeReadSaver(creds))), nil
}

// newWeReadLoginClient keeps the device ID of an earlier login, so logging
// in again does not register another device.
func newWeReadLoginClient() *weread.Client {
	var prev weread.Credentials
	if creds, err := config.LoadWeRead(); err == nil {
		prev.DeviceID = creds.DeviceID
	}
	return newWeReadClient(weread.WithCredentials(prev))
}

// importToWeRead imports path without a UI, writing status lines to status
// (stderr). Ctrl+C cancels the upload.
func importToWeRead(ctx context.Context, path string, status io.Writer) (weread.ImportResult, error) {
	c, err := loadWeReadClient()
	if err != nil {
		return weread.ImportResult{}, err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	fmt.Fprintf(status, "Importing to WeRead: %s\n", bookCell(filepath.Base(path)))
	progress := wereadPlainProgress{w: status, bytes: plainProgress{w: status}, stage: -1}
	result, err := c.ImportBook(ctx, path, progress.report)
	if err != nil && ctx.Err() != nil && !errors.Is(err, weread.ErrImportUnconfirmed) {
		return result, errCancelled
	}
	return result, err
}

// sendDownloadedFileToWeRead imports a downloaded book. With report it shows
// the progress page on a terminal and prints the outcome on stdout; without
// it (--json) stdout stays free for the caller's document.
func sendDownloadedFileToWeRead(path string, report bool) (weread.ImportResult, error) {
	if !report || !isInteractive() {
		return importToWeRead(context.Background(), path, os.Stderr)
	}
	c, err := loadWeReadClient()
	if err != nil {
		return weread.ImportResult{}, err
	}
	final, err := tea.NewProgram(newWeReadImportModel(path, c)).Run()
	if err != nil {
		return weread.ImportResult{}, err
	}
	m := final.(wereadImportModel)
	if m.err != nil {
		return weread.ImportResult{}, m.err
	}
	printWeReadImported(m.result)
	return m.result, nil
}

// wereadPlainProgress prints one line per import stage, with upload
// progress in the same steps as a plain download.
type wereadPlainProgress struct {
	w     io.Writer
	bytes plainProgress
	stage weread.ImportStage
}

func (p *wereadPlainProgress) report(ip weread.ImportProgress) {
	if ip.Stage != p.stage {
		p.stage = ip.Stage
		switch ip.Stage {
		case weread.StagePrepare:
			fmt.Fprintln(p.w, "  Preparing the upload…")
		case weread.StageUpload:
			fmt.Fprintf(p.w, "  Uploading %s…\n", formatBytes(ip.Total))
		case weread.StageProcess:
			fmt.Fprintln(p.w, "  Waiting for WeRead to add the book…")
		}
	}
	if ip.Stage == weread.StageUpload && ip.Sent > 0 {
		p.bytes.report(ip.Sent, ip.Total)
	}
}

// wereadResultTitle words an import outcome as one line.
func wereadResultTitle(result weread.ImportResult) string {
	if result.AlreadyExisted {
		return "Already in WeRead"
	}
	return "Imported to WeRead"
}

// wereadResultNote tells the user where to find the book.
func wereadResultNote(result weread.ImportResult) string {
	if result.AlreadyExisted {
		return "WeRead already had this file and kept the existing copy. If it is not on your shelf, search for it in the WeRead app."
	}
	return "Open WeRead on your phone to find it on the shelf."
}

func printWeReadImported(result weread.ImportResult) {
	lipgloss.Fprintln(os.Stdout)
	lipgloss.Fprintf(os.Stdout, "%s %s: %s (book ID %s)\n",
		colorGreen(symbolSuccess), wereadResultTitle(result), colorCyan(bookCell(result.Name)), result.BookID)
	lipgloss.Fprintf(os.Stdout, "%s %s\n", colorFaint(symbolInfo), wereadResultNote(result))
}

// wereadImportNote describes a file before it is imported: its size and
// the account it goes to, or that a login comes first.
func wereadImportNote(size int64) string {
	creds, err := config.LoadWeRead()
	if err != nil || !creds.Valid() {
		return formatBytes(size) + " · log in with WeChat first"
	}
	return formatBytes(size) + " · to " + wereadAccountLabel(creds)
}

// runWeReadLoginPlain runs the QR login without a TUI: the QR code goes to
// out, progress to status.
func runWeReadLoginPlain(ctx context.Context, out, status io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	c := newWeReadLoginClient()
	qr, err := c.RequestQRCode(ctx)
	if err != nil {
		return err
	}
	art, err := renderQR(qr.URL)
	if err != nil {
		return err
	}
	lipgloss.Fprintln(out, art)
	lipgloss.Fprintln(out, "Scan this code with WeChat and confirm the WeRead login on your phone.")
	creds, err := finishWeReadLogin(ctx, c, qr, func() {
		fmt.Fprintln(status, "Scanned. Confirm the login in WeChat…")
	})
	if err != nil {
		if ctx.Err() != nil {
			return errCancelled
		}
		return err
	}
	lipgloss.Fprintln(out)
	printWeReadAccount(out, colorGreen(symbolSuccess)+" Logged in to WeRead", creds)
	return nil
}

// finishWeReadLogin waits for the QR confirmation, exchanges the code and
// saves the credentials.
func finishWeReadLogin(ctx context.Context, c *weread.Client, qr weread.QRCode, onScanned func()) (weread.Credentials, error) {
	code, err := c.WaitForConfirmation(ctx, qr.UUID, onScanned)
	if err != nil {
		return weread.Credentials{}, err
	}
	if _, err := c.Exchange(ctx, code); err != nil {
		return weread.Credentials{}, err
	}
	// The nickname is only for display; a login without it still works.
	_, _ = c.UpdateName(ctx)
	creds := c.Credentials()
	if err := config.SaveWeRead(creds); err != nil {
		return weread.Credentials{}, fmt.Errorf("save WeRead login: %w", err)
	}
	return creds, nil
}

// wereadAccountLabel names a WeRead login on one line: "Nickname (ID)", or
// the ID alone while the nickname is unknown.
func wereadAccountLabel(creds weread.Credentials) string {
	if name := safeText(creds.Name); name != "" {
		return name + " (" + creds.VID + ")"
	}
	return creds.VID
}

// wereadNoNickname stands in for a nickname WeRead has not returned yet.
const wereadNoNickname = "Not available yet"

// wereadAccountRows lists a login as label/value pairs for detail views.
func wereadAccountRows(creds weread.Credentials) [][2]string {
	name := safeText(creds.Name)
	if name == "" {
		name = wereadNoNickname
	}
	return [][2]string{
		{"Nickname", name},
		{"User ID", creds.VID},
		{"Saved to", tildePath(config.WeReadPath())},
	}
}

// formatDetailRows aligns label/value pairs in two columns, styling the
// labels with label (nil for plain text).
func formatDetailRows(rows [][2]string, indent string, label func(string) string) []string {
	width := 0
	for _, row := range rows {
		width = max(width, ansi.StringWidth(row[0]))
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		name := row[0] + strings.Repeat(" ", width-ansi.StringWidth(row[0]))
		if label != nil {
			name = label(name)
		}
		lines[i] = indent + name + "   " + row[1]
	}
	return lines
}

// printWeReadAccount prints a login's details under a heading.
func printWeReadAccount(out io.Writer, heading string, creds weread.Credentials) {
	lipgloss.Fprintln(out, heading)
	for _, line := range formatDetailRows(wereadAccountRows(creds), "  ", colorFaint) {
		lipgloss.Fprintln(out, line)
	}
}

// Ways each surface words signing in to WeRead.
const (
	wereadLoginCLI   = "Run `zlib weread login`"
	wereadLoginShell = "Use /weread login"
	wereadLoginAgent = "Ask the user to run `zlib weread login` in their terminal"
)

// wereadErrorHint returns the next step for a WeRead error, starting with
// loginStep when signing in is the fix, or "" when err needs no hint.
func wereadErrorHint(err error, loginStep string) string {
	switch {
	case errors.Is(err, weread.ErrImportUnconfirmed):
		return "Check the WeRead shelf on your phone before importing again; a retry can count as another import."
	case errors.Is(err, weread.ErrNotLoggedIn):
		return loginStep + " and scan the QR code with WeChat."
	case errors.Is(err, weread.ErrSessionExpired):
		return "WeRead no longer accepts the saved login. " + loginStep + " to sign in again, then repeat the import."
	case errors.Is(err, weread.ErrVerificationRequired):
		return "Open WeRead on your phone, complete any prompt there, then retry later."
	case errors.Is(err, weread.ErrImportLimit):
		return "Without a paid membership WeRead keeps at most 200 imported books; remove some from the shelf, or wait if the message names a time limit."
	case errors.Is(err, weread.ErrRateLimited):
		return "Wait a while before importing again."
	case errors.Is(err, weread.ErrImportParseFailed):
		return "Check that the file opens in an ebook reader, or convert it to EPUB."
	case errors.Is(err, weread.ErrFileTooLarge):
		return "Convert a large TXT file to EPUB, or compress or split the book."
	}
	return ""
}
