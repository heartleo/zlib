package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline test transport")
}

// offlineWeRead makes every WeRead client fail fast instead of reaching the
// network.
func offlineWeRead(t *testing.T) {
	t.Helper()
	prev := newWeReadClient
	t.Cleanup(func() { newWeReadClient = prev })
	newWeReadClient = func(opts ...weread.Option) *weread.Client {
		return weread.NewClient(append(opts, weread.WithHTTPClient(&http.Client{Transport: offlineTransport{}}))...)
	}
}

func writeTempFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDownloadCompleteOffersDeliveriesByFormat(t *testing.T) {
	tests := map[string][]string{
		"book.epub": {actionBack, actionSendToKindle, actionSendToWeRead},
		"book.mobi": {actionBack, actionSendToWeRead},
		"book.docx": {actionBack, actionSendToKindle},
		"book.fb2":  {actionBack},
	}
	for name, want := range tests {
		m := newDownloadModel("1:abc", "", nil)
		m.savedPath = name
		if got := m.completeActions(); !slices.Equal(got, want) {
			t.Errorf("%s actions = %v, want %v", name, got, want)
		}
	}

	m := newDownloadModel("1:abc", "", nil)
	m.state, m.stayOnComplete, m.savedPath = stateComplete, true, "book.epub"
	next, _ := m.Update(key("left")) // wraps from Back to the last action
	m = next.(downloadModel)
	next, cmd := m.Update(key("enter"))
	got := next.(downloadModel)
	if cmd == nil || !got.sendToWeRead || got.sendToKindle {
		t.Fatalf("WeRead action not chosen: weread=%v kindle=%v", got.sendToWeRead, got.sendToKindle)
	}
}

func TestShellDownloadWeReadActionLogsInFirstAndReturns(t *testing.T) {
	isolateConfigHome(t)
	offlineWeRead(t)
	path := writeTempFile(t, "book.epub")
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "Book"}}
	m.open("list", list)
	m.previous = m.page
	download := newDownloadModel("1:abc", filepath.Dir(path), nil)
	download.state = stateComplete
	download.savedPath = path
	download.bookName = "Book"
	m.open("download", download)

	m, _ = shellStep(m, key("left"))
	m, cmd := shellStep(m, key("enter"))
	if cmd == nil {
		t.Fatal("WeRead action did not leave the completion page")
	}
	m, _ = shellStep(m, cmd())
	if m.kind != "weread-confirm" || m.deliveryReturn == nil || m.wereadPath != path {
		t.Fatalf("kind = %q, want weread-confirm with the downloaded book", m.kind)
	}

	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "weread-login" || m.wereadPath != path {
		t.Fatalf("kind = %q, want the WeRead login page before importing", m.kind)
	}

	m, cmd = shellStep(m, key("esc"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" || m.deliveryReturn != nil || m.wereadPath != "" {
		t.Fatalf("kind = %q, want the completion page after cancelling login", m.kind)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "WeRead login cancelled") {
		t.Fatal("completion page did not report the cancelled login")
	}
}

func TestShellWeReadImportRejectsUnsupportedFile(t *testing.T) {
	m := newShellModel()
	m.dispatch("/weread import " + writeTempFile(t, "notes.docx"))
	if m.page != nil || !strings.Contains(m.status, "Unsupported file type") {
		t.Fatalf("status = %q, page = %T", m.status, m.page)
	}
	m.dispatch("/weread bogus")
	if m.status != wereadUsage {
		t.Fatalf("status = %q", m.status)
	}
}

func TestStandaloneWeReadErrorsUseWeReadTitle(t *testing.T) {
	m, err := newStandaloneShellCommand("/weread import missing.epub")
	if err == nil || !strings.Contains(err.Error(), "File not found") || m.standaloneTitle != "WeRead" {
		t.Fatalf("err = %v, title = %q", err, m.standaloneTitle)
	}
}

func TestWeReadErrorsMapToExitCodesAndHints(t *testing.T) {
	expired := fmt.Errorf("refresh: %w", weread.ErrSessionExpired)
	for err, want := range map[error]int{
		weread.ErrNotLoggedIn: exitAuth,
		expired:               exitAuth,
		&deliveryError{target: targetWeRead, path: "a.epub", err: weread.ErrNotLoggedIn}: exitPartial,
		weread.ErrVerificationRequired: exitError,
	} {
		if got := exitCode(err); got != want {
			t.Errorf("exitCode(%v) = %d, want %d", err, got, want)
		}
	}
	if msg := formatCLIError(weread.ErrNotLoggedIn); !strings.Contains(msg, "`zlib weread login`") {
		t.Errorf("CLI message lacks the login step: %q", msg)
	}
	if msg := shellErrorText(expired); !strings.Contains(msg, "Use /weread login") {
		t.Errorf("shell message lacks the login step: %q", msg)
	}
	if msg := formatMCPErrorText(weread.ErrNotLoggedIn); !strings.Contains(msg, "Ask the user to run `zlib weread login`") {
		t.Errorf("MCP message lacks the login step: %q", msg)
	}
	if msg := formatCLIError(weread.ErrVerificationRequired); !strings.Contains(msg, "Open WeRead on your phone") {
		t.Errorf("verification message lacks a next step: %q", msg)
	}
}

func TestRenderQRDrawsEvenRows(t *testing.T) {
	art, err := renderQR("https://open.weixin.qq.com/connect/confirm?uuid=0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(art, "\n")
	if len(lines) < 10 {
		t.Fatalf("QR has %d rows", len(lines))
	}
	width := ansi.StringWidth(lines[0])
	for _, line := range lines {
		if ansi.StringWidth(line) != width {
			t.Fatal("QR rows differ in width")
		}
	}
	if !strings.ContainsAny(ansi.Strip(art), "█▀▄") {
		t.Fatal("QR has no dark modules")
	}
}

// Decoding the half blocks back into modules must reproduce the encoder's
// matrix, quiet zone included.
func TestRenderQRMatchesModules(t *testing.T) {
	const content, quiet = "https://open.weixin.qq.com/connect/confirm?uuid=fixture", qrQuietSpec
	code, err := encodeQR(content)
	if err != nil {
		t.Fatal(err)
	}
	art, err := renderQR(content)
	if err != nil {
		t.Fatal(err)
	}
	for row, line := range strings.Split(ansi.Strip(art), "\n") {
		for col, r := range []rune(line) {
			top := r == '█' || r == '▀'
			bottom := r == '█' || r == '▄'
			x, y := col-quiet, 2*row-quiet
			for i, got := range []bool{top, bottom} {
				yy := y + i
				want := x >= 0 && yy >= 0 && x < code.Size && yy < code.Size && code.Black(x, yy)
				if got != want {
					t.Fatalf("module (%d,%d) = %v, want %v", x, yy, got, want)
				}
			}
		}
	}
}

func TestWeReadCredentialsRoundTrip(t *testing.T) {
	isolateConfigHome(t)
	if _, err := config.LoadWeRead(); !errors.Is(err, weread.ErrNotLoggedIn) {
		t.Fatalf("LoadWeRead without a file = %v", err)
	}
	creds := weread.Credentials{VID: "1", AccessToken: "a", RefreshToken: "r", DeviceID: "d"}
	if err := config.SaveWeRead(creds); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadWeRead()
	if err != nil || got != creds {
		t.Fatalf("LoadWeRead = %+v, %v", got, err)
	}
	if info, err := os.Stat(config.WeReadPath()); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	removed, err := config.RemoveWeRead()
	if !removed || err != nil {
		t.Fatalf("RemoveWeRead = %v, %v", removed, err)
	}
	if removed, _ := config.RemoveWeRead(); removed {
		t.Fatal("second RemoveWeRead reported a file")
	}
}

func TestImportToWeReadRequiresLogin(t *testing.T) {
	isolateConfigHome(t)
	offlineWeRead(t)
	_, err := importToWeRead(context.Background(), writeTempFile(t, "book.epub"), io.Discard)
	if !errors.Is(err, weread.ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestMCPImportToWeReadAsksForLoginAndHonorsAllowedDir(t *testing.T) {
	withDemoMCPFixtures(t)
	offlineWeRead(t)
	// The server confines paths to a real path, as resolveAllowedDir returns.
	// A raw t.TempDir differs from it on macOS (/var is a symlink to
	// /private/var) and on Windows (8.3 short names).
	allowed, err := resolveAllowedDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	book := filepath.Join(allowed, "book.epub")
	if err := os.WriteFile(book, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := connectMCPTestSession(t, mcpServerOptions{AllowedDir: allowed}, nil)
	call := func(path string) *mcp.CallToolResult {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "import_to_weread", Arguments: map[string]any{"file_path": path}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(book); !res.IsError || !strings.Contains(toolText(t, res), "zlib weread login") {
		t.Fatalf("import without login: error=%v %q", res.IsError, toolText(t, res))
	}
	outside := writeTempFile(t, "outside.epub")
	if res := call(outside); !res.IsError || !strings.Contains(toolText(t, res), "allowed directory") {
		t.Fatalf("import outside the allowed directory: error=%v %q", res.IsError, toolText(t, res))
	}
}

func TestWeReadFailureNamesCorrectService(t *testing.T) {
	m := shellModel{}
	m.wereadFailed("Import failed", "book.epub", weread.ErrImportUnconfirmed)
	page := m.page.(shellErrorModel)
	page.width, page.height = 80, 24
	view := ansi.Strip(page.View().Content)
	if strings.Contains(view, "Kindle") || !strings.Contains(view, "Send to WeRead") {
		t.Fatalf("wrong failure page: %s", view)
	}
	hint := wereadErrorHint(fmt.Errorf("%w: %w", weread.ErrImportUnconfirmed, weread.ErrRateLimited), wereadLoginCLI)
	if !strings.Contains(hint, "shelf") {
		t.Fatal("uncertainty warning lost")
	}
}
