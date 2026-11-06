package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/zlib"
)

func shellStep(m shellModel, msg tea.Msg) (shellModel, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(shellModel), cmd
}

// Errors stay until the next key press: a timer could clear one before it is
// read. The input and key hints keep their rows either way.
func TestShellErrorNoticeStaysUntilKeyPressWithoutReplacingInputOrHelp(t *testing.T) {
	m := newShellModel()
	m.signedIn = true
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 20, Height: 5}} {
		m, _ = shellStep(m, size)
		m.input.SetValue("/missing arg")
		m, cmd := shellStep(m, key("enter"))
		if cmd != nil {
			t.Fatal("an error notice must not schedule expiry")
		}
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) != size.Height {
			t.Fatalf("%dx%d: got %d rows", size.Width, size.Height, len(lines))
		}
		if !strings.Contains(strings.Join(lines[:len(lines)-2], "\n"), symbolError+" Unknown") {
			t.Fatalf("%dx%d: error notice missing above input: %q", size.Width, size.Height, lines)
		}
		if strings.Contains(strings.Join(lines[len(lines)-2:], "\n"), "Unknown command") || !strings.Contains(lines[len(lines)-2], "/missing") {
			t.Fatalf("%dx%d: notice replaced input or help: %q", size.Width, size.Height, lines[len(lines)-2:])
		}
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("%dx%d: row exceeds terminal width", size.Width, size.Height)
			}
		}
		m, _ = shellStep(m, shellNoticeExpired{version: m.noticeVersion})
		if m.status == "" {
			t.Fatal("a timer cleared the error notice")
		}
		m, _ = shellStep(m, key("x"))
		if m.status != "" || strings.Contains(ansi.Strip(m.View().Content), "Unknown command") {
			t.Fatal("the next key press did not dismiss the error notice")
		}
		m.input.SetValue("")
	}
}

func TestShellInfoAndSuccessNoticesExpire(t *testing.T) {
	m := newShellModel()
	m.signedIn = true
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.setSuccess("Logged out")
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, symbolSuccess+" Logged out") {
		t.Fatalf("success notice lacks its mark: %q", view)
	}
	oldVersion := m.noticeVersion
	m.setNotice("New feedback")
	m, _ = shellStep(m, shellNoticeExpired{version: oldVersion})
	if m.status != "New feedback" {
		t.Fatal("old timer cleared newer feedback")
	}
	m, _ = shellStep(m, shellNoticeExpired{version: m.noticeVersion})
	if m.status != "" || strings.Contains(ansi.Strip(m.View().Content), "New feedback") {
		t.Fatal("notice did not expire")
	}
}

// Feedback sits on the title row: row 1 of a list carries the page and the
// selected position, which a notice used to cover.
func TestShellNoticeKeepsListStatusVisible(t *testing.T) {
	for _, width := range []int{80, 32} {
		m := newShellModel()
		m.signedIn = true
		m, _ = shellStep(m, tea.WindowSizeMsg{Width: width, Height: 24})
		list := newBookFeedSelectModel("Recommended", nil)
		list.state = searchStateResults
		list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}, {ID: "2", Hash: "def", Name: "Second"}}
		m.open("list", list)
		m, _ = shellStep(m, tea.WindowSizeMsg{Width: width, Height: 24})
		before := strings.Split(ansi.Strip(m.View().Content), "\n")
		m.setError("Session expired. Use /login to sign in again.")
		after := strings.Split(ansi.Strip(m.View().Content), "\n")
		if strings.TrimSpace(before[1]) == "" || after[1] != before[1] {
			t.Fatalf("width %d: status row changed from %q to %q", width, before[1], after[1])
		}
		if !strings.Contains(after[0], symbolError) {
			t.Fatalf("width %d: notice not on the title row: %q", width, after[0])
		}
		if title := strings.TrimRight(before[0], " "); width >= 80 && (title == "" || !strings.HasPrefix(after[0], title)) {
			t.Fatalf("width %d: notice covered the title: %q", width, after[0])
		}
	}
}

func TestShellThemeSavedNoticeLeavesFooterAvailable(t *testing.T) {
	isolateConfigHome(t)
	m := newShellModel()
	m.signedIn = true
	picker := newThemePicker("solarized-dark", "")
	picker.accepted = true
	m.open("theme", picker)
	m, cmd := shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if cmd == nil || !strings.HasPrefix(m.status, "Theme saved:") {
		t.Fatal("theme save did not schedule transient feedback")
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if strings.Contains(strings.Join(lines[len(lines)-2:], "\n"), "Theme saved:") {
		t.Fatal("theme feedback occupies the input or help row")
	}
}

func TestShellNoticeAppearsOverCommandDialog(t *testing.T) {
	m := newShellModel()
	m.input.SetValue("/missing")
	m, _ = shellStep(m, key("enter"))
	if !m.paletteOpen() {
		t.Fatal("unknown command did not retain the command dialog")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Unknown command") || !strings.Contains(view, "No matching commands") {
		t.Fatal("dialog hid the transient command feedback")
	}
}

func TestShellDownloadReturnsToSelectedBook(t *testing.T) {
	isolateConfigHome(t)
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}, {ID: "2", Hash: "def", Name: "Second"}}
	list.cursor = 1
	list.client = zlib.NewClient()
	m.open("list", list)

	m, cmd := shellStep(m, key("enter"))
	if cmd != nil || !m.page.(searchSelectModel).confirmDownload {
		t.Fatal("list Enter did not open download confirmation")
	}
	m, _ = shellStep(m, key("enter"))
	if m.kind != "list" || m.page.(searchSelectModel).cursor != 1 {
		t.Fatal("default cancel did not restore selection")
	}

	m, _ = shellStep(m, key("enter"))
	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	event := cmd()
	if _, ok := event.(shellEvent); !ok {
		t.Fatal("list confirmation was not scoped to the shell")
	}
	m, _ = shellStep(m, event)
	if m.kind != "directory" {
		t.Fatalf("confirmed download opened %q", m.kind)
	}
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" {
		t.Fatalf("directory selection opened %q", m.kind)
	}
	savedPath, err := filepath.Abs("book.epub")
	if err != nil {
		t.Fatal(err)
	}
	m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: downloadDoneMsg{path: "book.epub", size: 42}})
	if cmd != nil || m.kind != "download" || m.page.(downloadModel).savedPath != savedPath {
		t.Fatal("completed download did not stay on its page with an absolute path")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Download complete") || !strings.Contains(view, "Saved to:") || !strings.Contains(view, savedPath) {
		t.Fatal("completion page omitted the full saved path")
	}
	m, cmd = shellStep(m, key("enter"))
	if cmd == nil {
		t.Fatal("Enter did not leave the completion page")
	}
	m, _ = shellStep(m, cmd())
	got := m.page.(searchSelectModel)
	if m.kind != "list" || got.cursor != 1 || m.status != "" {
		t.Fatalf("download did not restore selection: %q, %d, %q", m.kind, got.cursor, m.status)
	}
}

func TestShellDownloadKindleActionReturnsToCompletion(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(kindleSMTPPasswordEnv, "")
	path := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if cmd := m.open("download", download); cmd != nil {
		t.Fatal("reopening a completed download restarted the transfer")
	}
	chooseKindle := func() {
		var cmd tea.Cmd
		m, _ = shellStep(m, key("right"))
		m, cmd = shellStep(m, key("enter"))
		if cmd == nil {
			t.Fatal("Kindle action did not open confirmation")
		}
		m, _ = shellStep(m, cmd())
	}
	chooseKindle()
	if m.kind != "kindle-confirm" || m.deliveryReturn == nil || m.kindlePath != path {
		t.Fatal("Kindle confirmation lost the downloaded book")
	}
	m, cmd := shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" || m.deliveryReturn != nil || m.page.(downloadModel).savedPath != path {
		t.Fatal("cancel did not return to download completion")
	}
	chooseKindle()
	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" || !strings.Contains(ansi.Strip(m.View().Content), "Set up Kindle in /config kindle") {
		t.Fatal("missing Kindle settings did not return to download completion")
	}
	if err := config.SaveKindleConfig(zlib.KindleConfig{To: "reader@kindle.com", From: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPort: 587}); err != nil {
		t.Fatal(err)
	}
	chooseKindle()
	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "kindle-password" {
		t.Fatal("missing SMTP password did not open the masked setup form")
	}
	m, cmd = shellStep(m, key("esc"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" || m.page.(downloadModel).savedPath != path || os.Getenv(kindleSMTPPasswordEnv) != "" {
		t.Fatal("canceling password setup did not return to the saved file")
	}
	chooseKindle()
	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	form := m.page.(inlineForm)
	form.inputs[0].SetValue("fixture-password")
	form.fields[0].value = "fixture-password"
	m.page = form
	if strings.Contains(ansi.Strip(m.View().Content), "fixture-password") {
		t.Fatal("SMTP password is visible in the form")
	}
	m, cmd = shellStep(m, key("ctrl+s"))
	m, _ = shellStep(m, cmd())
	if m.kind != "kindle-send" || m.deliveryReturn == nil {
		t.Fatal("saved SMTP password did not continue to sender")
	}
	if os.Getenv(kindleSMTPPasswordEnv) != "fixture-password" {
		t.Fatal("saved SMTP password was not available to this process")
	}
	global, err := globalDotEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := readSettingsEnv(global)
	if err != nil || stored[kindleSMTPPasswordEnv] != "fixture-password" {
		t.Fatal("SMTP password was not saved for the next run")
	}
	sent := m.page.(kindleSendModel)
	sent.done = true
	m.page = sent
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "download" || !strings.Contains(ansi.Strip(m.View().Content), "Email sent to Kindle") {
		t.Fatal("Kindle result did not return to download completion")
	}
	chooseKindle()
	m, _ = shellStep(m, key("right"))
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	failed := m.page.(kindleSendModel)
	failed.err = errors.New("SMTP timeout")
	m.page = failed
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "kindle-error" || !strings.Contains(ansi.Strip(m.View().Content), "Check your Kindle inbox") {
		t.Fatal("failed send did not show the delivery warning")
	}
	m, cmd = shellStep(m, key("esc"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" || m.page.(downloadModel).savedPath != path || !strings.Contains(ansi.Strip(m.View().Content), "Kindle send failed") {
		t.Fatal("failed send did not return to the downloaded book")
	}
	m, cmd = shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "list" || m.deliveryReturn != nil || m.page.(searchSelectModel).cursor != 0 {
		t.Fatal("Back did not return to the selected list item")
	}
}

func TestShellKindlePasswordSaveFailureKeepsMaskedForm(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv(kindleSMTPPasswordEnv, "")
	global := filepath.Join(home, ".config", "zlib", ".env")
	if err := os.MkdirAll(global, 0700); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	form := newInlineForm("SMTP password", "Save and send", []formField{{label: "SMTP password", secret: true}})
	form.fields[0].value = "fixture-password"
	form.inputs[0].SetValue("fixture-password")
	form.accepted = true
	m.open("kindle-password", form)
	m.kindlePath = "book.epub"
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "kindle-password" || m.page.(inlineForm).accepted || !strings.Contains(m.page.(inlineForm).message, "Could not save") {
		t.Fatal("save failure left the password form")
	}
	if os.Getenv(kindleSMTPPasswordEnv) != "" || strings.Contains(ansi.Strip(m.View().Content), "fixture-password") {
		t.Fatal("save failure exposed or activated the password")
	}
}

func TestShellDownloadKindleRejectsUnsupportedFile(t *testing.T) {
	m := newShellModel()
	page := newDownloadModel("1:abc", ".", nil)
	page.state = stateComplete
	page.savedPath = filepath.Join(t.TempDir(), "book.fb2")
	m.open("download", page)
	if strings.Contains(ansi.Strip(m.View().Content), "Send to Kindle") {
		t.Fatal("unsupported file still offers Kindle delivery")
	}
	m, _ = shellStep(m, key("right"))
	if m.page.(downloadModel).completeAction != 0 {
		t.Fatal("unsupported file has a selectable Kindle action")
	}
	m, cmd := shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "" || m.deliveryReturn != nil {
		t.Fatal("unsupported file did not return from download completion")
	}
}

func TestShellDownloadFailureKeepsContextAndRetries(t *testing.T) {
	m := newShellModel()
	m.width, m.height = 80, 24
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}}
	m.open("list", list)
	m.previous = m.page
	m.download = newDownloadModel("1:abc", t.TempDir(), nil)
	m.open("download", m.download)
	failed := m.page.(downloadModel)
	failed.bookName = "First"
	failed.err = errors.New(`Get "https://signed.example/book?token=secret": timeout`)
	m.page = failed
	oldGeneration := m.generation
	m, _ = shellStep(m, shellEvent{generation: oldGeneration, msg: tea.QuitMsg{}})
	if m.kind != "download-error" || m.page == nil {
		t.Fatalf("download failure left its context: kind=%q", m.kind)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "First") || !strings.Contains(view, "timeout") || !strings.Contains(view, "r retry") || strings.Contains(view, "token=secret") {
		t.Fatalf("failure screen omitted context or exposed URL: %q", view)
	}
	m, cmd := shellStep(m, key("r"))
	if cmd == nil {
		t.Fatal("retry did not complete the error page")
	}
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "download" || m.generation <= oldGeneration || m.page.(downloadModel).dir != m.download.dir {
		t.Fatalf("retry lost download settings: kind=%q generation=%d", m.kind, m.generation)
	}
	m, _ = shellStep(m, shellEvent{generation: oldGeneration, msg: errMsg{errors.New("stale")}})
	if m.page.(downloadModel).err != nil {
		t.Fatal("late result replaced the retried download")
	}
}

func TestShellKindleFailureRequiresExplicitResend(t *testing.T) {
	m := newShellModel()
	m.width, m.height = 80, 24
	page := kindleSendModel{filePath: "book.epub", err: errors.New("SMTP connection closed")}
	m.open("kindle-send", page)
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "kindle-error" || !strings.Contains(ansi.Strip(m.View().Content), "Check your Kindle inbox") {
		t.Fatal("Kindle error did not stay visible with delivery warning")
	}
	m, cmd := shellStep(m, key("r"))
	if cmd != nil || m.kind != "kindle-error" {
		t.Fatal("Kindle failure restarted a possibly delivered send")
	}
	m, cmd = shellStep(m, key("esc"))
	if cmd == nil {
		t.Fatal("Kindle error did not allow exit")
	}
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.kind != "" {
		t.Fatalf("Kindle error did not return home: %q", m.kind)
	}
}

func TestShellUnknownCommandAndQuotedKindlePath(t *testing.T) {
	m := newShellModel()
	m.input.SetValue("/missing")
	m, _ = shellStep(m, key("enter"))
	if m.input.Value() != "/missing" || !strings.Contains(m.status, "Unknown command") {
		t.Fatal("unknown command should keep the draft")
	}
	path := filepath.Join(t.TempDir(), "book with spaces.epub")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.dispatch("/kindle send \"" + path + "\"")
	if m.kind != "kindle-confirm" || m.kindlePath != path {
		t.Fatalf("quoted path was not parsed: %q, %q", m.kind, m.kindlePath)
	}
}

func TestShellCommandPaletteAndReturn(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/fa")
	if got := m.matches(); len(got) != 1 || got[0].name != "favorites" {
		t.Fatalf("matches = %v", got)
	}
	m, _ = shellStep(m, key("enter"))
	// No persisted session is needed to verify matching: the command was resolved
	// before authentication and its value is retained for login recovery.
	if m.pending != "/favorites" && m.kind != "list" {
		t.Fatalf("slash prefix did not dispatch favorites: %q, %q", m.pending, m.kind)
	}
	m.home()
	m.signedIn = true
	m, _ = shellStep(m, key("/"))
	m, _ = shellStep(m, tea.PasteMsg{Content: "logout"})
	if m.kind != "" {
		t.Fatal("paste executed a command")
	}
	m, _ = shellStep(m, key("enter"))
	if m.kind != "logout" {
		t.Fatal("logout confirmation did not open")
	}
	confirm := m.page.(shellConfirm)
	if confirm.focused || confirm.confirmed {
		t.Fatal("logout must default to cancel")
	}
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if m.page != nil || m.kind != "" {
		t.Fatal("cancel did not return home")
	}
}

func isolateShellSession(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := config.SessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShellStartsOnLoginWithoutSession(t *testing.T) {
	isolateShellSession(t)
	m := newShellStartupModel()
	if m.signedIn || m.kind != "login" || m.typing {
		t.Fatalf("signed-out startup did not open login: kind=%q signedIn=%v", m.kind, m.signedIn)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Email") || !strings.Contains(view, "Password") || strings.Contains(view, "Sign in to Z-Library") {
		t.Fatal("startup does not show the login form directly")
	}
	m, cmd := shellStep(m, key("esc"))
	m, _ = shellStep(m, cmd())
	if m.kind != "" || m.page != nil || !m.typing {
		t.Fatal("cancel did not return to the signed-out home")
	}
}

func TestShellStartsHomeWithSession(t *testing.T) {
	path := isolateShellSession(t)
	if err := os.WriteFile(path, []byte(`{"cookies":{"session":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newShellStartupModel()
	if !m.signedIn || m.kind != "" || m.page != nil || !m.typing {
		t.Fatal("signed-in startup did not open the home screen")
	}
}

func TestShellLogoutOpensLogin(t *testing.T) {
	path := isolateShellSession(t)
	if err := os.WriteFile(path, []byte(`{"cookies":{"session":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	if !m.signedIn {
		t.Fatal("test session was not loaded")
	}
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Name: "Private book"}}
	m.open("list", list)
	m.pending = "/favorites"
	m.dispatch("/logout")
	confirm := m.page.(shellConfirm)
	confirm.confirmed = true
	m.page = confirm
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session remains after logout: %v", err)
	}
	if m.signedIn || m.kind != "login" || m.page == nil || m.previous != nil || m.pending != "" || m.input.Value() != "" || m.typing {
		t.Fatalf("logout kept account state: signedIn=%v kind=%q pending=%q", m.signedIn, m.kind, m.pending)
	}
	if m.status != "" {
		t.Fatalf("logout left a redundant footer status: %q", m.status)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Email") || !strings.Contains(view, "Password") || strings.Contains(view, "Private book") {
		t.Fatal("logout did not open the login form")
	}
}

func TestShellLogoutCancelKeepsSession(t *testing.T) {
	path := isolateShellSession(t)
	if err := os.WriteFile(path, []byte(`{"cookies":{"session":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Name: "First"}, {ID: "2", Name: "Second"}}
	list.cursor = 1
	m.open("list", list)
	m.dispatch("/logout")
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if !m.signedIn || m.kind != "list" || m.page.(searchSelectModel).cursor != 1 {
		t.Fatal("cancel did not restore the signed-in list")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cancel removed the saved session: %v", err)
	}
}

func TestShellLogoutFailureKeepsAccountState(t *testing.T) {
	path := isolateShellSession(t)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocker"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	m.signedIn = true
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Name: "First"}, {ID: "2", Name: "Second"}}
	list.cursor = 1
	m.open("list", list)
	m.dispatch("/logout")
	confirm := m.page.(shellConfirm)
	confirm.confirmed = true
	m.page = confirm
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: tea.QuitMsg{}})
	if !m.signedIn || m.kind != "list" || m.page.(searchSelectModel).cursor != 1 || !strings.Contains(m.status, "Could not log out") {
		t.Fatalf("logout failure lost active account or selection: signedIn=%v kind=%q status=%q", m.signedIn, m.kind, m.status)
	}
}

func TestShellLogoutWhenAlreadySignedOut(t *testing.T) {
	path := isolateShellSession(t)
	m := newShellModel()
	m.dispatch("/logout")
	if m.kind != "" || m.page != nil || m.status != "Not logged in" {
		t.Fatalf("signed-out logout opened a dialog: kind=%q status=%q", m.kind, m.status)
	}
	if err := os.WriteFile(path, []byte(`{"cookies":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.dispatch("/logout")
	if m.status != "Logged out" {
		t.Fatalf("stale session was not cleared: %q", m.status)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale session remains: %v", err)
	}
}

func TestShellPartialCommandEnterOpensSelectedScreen(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/th")
	m, _ = shellStep(m, key("enter"))
	if m.kind != "theme" {
		t.Fatalf("partial command opened %q, want theme", m.kind)
	}
}

func TestShellSignedOutHomeOpensLogin(t *testing.T) {
	m := newShellModel()
	m.signedIn = false
	m.input.SetValue("")
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Sign in to Z-Library") || !strings.Contains(view, "  Log in  ") || !strings.Contains(view, "Email and password") || !strings.Contains(view, "enter sign in") {
		t.Fatal("signed-out home has no clear sign-in action")
	}
	if strings.Contains(view, "/search") || strings.Contains(view, "/favorites") {
		t.Fatal("signed-out home shows account commands before login")
	}
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 20, Height: 5})
	if !strings.Contains(ansi.Strip(m.View().Content), "Log in") {
		t.Fatal("small signed-out home hides its sign-in action")
	}
	m, _ = shellStep(m, key("enter"))
	if m.kind != "login" {
		t.Fatalf("Enter on signed-out home opened %q", m.kind)
	}
}

func TestShellLoginSuccessReturnsHomeWithPendingDraft(t *testing.T) {
	m := newShellModel()
	m.signedIn = false
	m.kind = "login-wait"
	m.typing = false
	m.pending = "/favorites"
	form := newInlineForm("Log in", "Log in", []formField{{label: "Email"}, {label: "Password", secret: true}})
	m.loginForm = new(form)
	m, cmd := shellStep(m, shellEvent{generation: m.generation, msg: shellText{text: "Logged in"}})
	if cmd == nil || m.kind != "" || m.page != nil || !m.typing || !m.signedIn || m.previous != nil {
		t.Fatalf("login did not return home: kind=%q signedIn=%v", m.kind, m.signedIn)
	}
	if m.input.Value() != "/favorites" || m.pending != "" || !strings.Contains(m.status, "Press Enter") {
		t.Fatalf("pending command was lost: input=%q pending=%q status=%q", m.input.Value(), m.pending, m.status)
	}
}

func TestShellLoginSuccessHomeHasNoSessionLabel(t *testing.T) {
	m := newShellModel()
	m.signedIn = false
	m.kind = "login-wait"
	m.typing = false
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: shellText{text: "Logged in"}})
	if !m.signedIn || m.status != "" || strings.Contains(ansi.Strip(m.View().Content), "Signed in") {
		t.Fatal("login left a persistent session label on the home screen")
	}
}

func TestShellLoginFailureReturnsToForm(t *testing.T) {
	m := newShellModel()
	form := newInlineForm("Log in", "Log in", []formField{{label: "Email", value: "reader@example.com"}, {label: "Password", value: "secret", secret: true}})
	m.loginForm = new(form)
	m.kind = "login-wait"
	m.typing = false
	m, _ = shellStep(m, shellEvent{generation: m.generation, msg: shellText{err: errors.New("invalid credentials")}})
	if m.kind != "login" || m.loginForm != nil {
		t.Fatalf("login failure opened %q", m.kind)
	}
	got := m.page.(inlineForm)
	if got.message != "invalid credentials" || got.fields[0].value != "reader@example.com" || got.fields[1].value != "" || got.cursor != 1 {
		t.Fatalf("retry form lost its state: message=%q cursor=%d", got.message, got.cursor)
	}
}

// A slow mirror used to leave the user on a bare spinner for up to three
// minutes with no way out; "/" even opened a palette hidden behind it.
func TestShellLoginWaitCanBeCancelled(t *testing.T) {
	m := newShellModel()
	m.signedIn = false
	form := newInlineForm("Log in", "Log in", []formField{{label: "Email", value: "reader@example.com"}, {label: "Password", value: "secret", secret: true}})
	m.loginForm = new(form)
	m.kind = "login-wait"
	m.typing = false
	cancelled := false
	m.loginCancel = func() { cancelled = true }
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Signing in") || !strings.Contains(view, "cancel") {
		t.Fatalf("login wait does not say what it is doing or how to cancel:\n%s", view)
	}
	m, _ = shellStep(m, key("/"))
	if m.typing || m.kind != "login-wait" {
		t.Fatal(`"/" opened the palette during login`)
	}
	generation := m.generation
	m, _ = shellStep(m, key("esc"))
	if !cancelled || m.loginCancel != nil {
		t.Fatal("esc did not cancel the login request")
	}
	if m.kind != "login" || m.generation == generation {
		t.Fatalf("esc did not return to the form: kind=%q", m.kind)
	}
	got := m.page.(inlineForm)
	if got.message != "Login cancelled" || got.fields[0].value != "reader@example.com" || got.fields[1].value != "" {
		t.Fatalf("cancelled form state: message=%q", got.message)
	}
	// The abandoned login's result arrives under the old generation and must
	// not sign the user in.
	m, _ = shellStep(m, shellEvent{generation: generation, msg: shellText{text: "Logged in"}})
	if m.signedIn {
		t.Fatal("a cancelled login signed the user in")
	}
}

func TestShellLoginStopsBeforeSavingWhenCancelled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(zlib.EnvDomain, "https://z-lib.gd")
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // a cancelled context fails before any request leaves the machine
	if got := shellLogin(ctx, []formField{{value: "reader@example.com"}, {value: "secret"}}); got.err == nil {
		t.Fatal("shellLogin with a cancelled context succeeded")
	}
	if _, err := os.Stat(config.SessionPath()); !os.IsNotExist(err) {
		t.Fatalf("a cancelled login wrote a session: %v", err)
	}
}

func TestShellLoginFormHasOnlyEmailAndPassword(t *testing.T) {
	m := newShellModel()
	m.dispatch("/login")
	if m.kind != "login" {
		t.Fatalf("login opened %q", m.kind)
	}
	form := m.page.(inlineForm)
	if len(form.fields) != 2 || form.fields[0].label != "Email" || form.fields[1].label != "Password" || !form.fields[1].secret {
		t.Fatal("login form must contain only email and masked password")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "Cookie file") {
		t.Fatal("TUI login still offers Cookie file import")
	}
}

func TestShellDownloadIsNotASlashCommand(t *testing.T) {
	m := newShellModel()
	m.input.SetValue("/download 1:abc")
	m, _ = shellStep(m, key("enter"))
	if m.kind != "" || !strings.Contains(m.status, "Unknown command: /download") {
		t.Fatalf("download command was accepted: kind=%q status=%q", m.kind, m.status)
	}
	m.input.SetValue("/")
	m.status = ""
	if strings.Contains(ansi.Strip(m.View().Content), "/download") {
		t.Fatal("download is still shown in the command palette")
	}
}

func TestShellIgnoresOldResultsAndReturnsToSelection(t *testing.T) {
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}, {ID: "2", Hash: "def", Name: "Second"}}
	list.cursor = 1
	m.open("list", list)
	firstGeneration := m.generation
	m, _ = shellStep(m, shellEvent{generation: firstGeneration - 1, msg: searchResultMsg{books: []zlib.Book{{Name: "Stale"}}}})
	if m.page.(searchSelectModel).books[0].Name != "First" {
		t.Fatal("stale page result applied")
	}
	m.previous = m.page
	m.open("directory", newDownloadDirectoryModel("."))
	m.restore()
	got := m.page.(searchSelectModel)
	if got.cursor != 1 || got.books[1].Name != "Second" {
		t.Fatal("return lost list position")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "Stale") {
		t.Fatal("stale data rendered")
	}
}

func TestShellViewFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 16}, {20, 5}} {
		m := newShellModel()
		m, _ = shellStep(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.input.SetValue("/")
		view := m.View()
		if !view.AltScreen || len(strings.Split(view.Content, "\n")) > size[1] {
			t.Fatalf("shell exceeds height at %v", size)
		}
		for _, line := range strings.Split(view.Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("shell exceeds width at %v", size)
			}
		}
	}
}

func TestShellCommandsStayInPalette(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.signedIn = true
	home := ansi.Strip(m.View().Content)
	for _, command := range []string{"/search", "/favorites", "/recommended", "/history", "/config", "/kindle"} {
		if !strings.Contains(home, command) {
			t.Fatalf("home omits %s", command)
		}
	}
	if strings.Index(home, "█████") > strings.Index(home, "/search") {
		t.Fatal("commands are above the wordmark")
	}
	m.input.SetValue("/")
	palette := ansi.Strip(m.View().Content)
	if !strings.Contains(palette, "/search") || !strings.Contains(palette, "Search books with filters") || !strings.Contains(palette, "Commands") || strings.Contains(palette, " of 13") {
		t.Fatal("command dialog should show commands and descriptions without a count heading")
	}
	if strings.Count(palette, "/search") != 1 {
		t.Fatal("home shortcuts duplicate the active command palette")
	}
	if rows := m.paletteVisibleRows(m.height); rows != 5 {
		t.Fatalf("palette has %d visible rows, want 5", rows)
	}
	if !strings.Contains(palette, "> /search") || !strings.Contains(palette, "\n       │   Search books with filters") {
		t.Fatal("wide command dialog should show descriptions below command names")
	}
	if strings.Contains(palette, "/quit") {
		t.Fatal("palette should show a bounded set of commands")
	}
	m.selection = len(shellCommands) - 1
	if later := ansi.Strip(m.View().Content); !strings.Contains(later, "/quit") || strings.Contains(later, "/search") {
		t.Fatal("palette did not scroll to the selected command")
	}
	m.input.SetValue("/pop")
	if got := m.matches(); len(got) != 1 || got[0].name != "most-popular" {
		t.Fatalf("substring command match = %v", got)
	}
	m.input.SetValue("/help")
	m, _ = shellStep(m, key("enter"))
	if m.kind != "" || !strings.Contains(m.status, "Unknown command: /help") {
		t.Fatal("help should not be a TUI command")
	}
}

func TestShellHomeLogoAndShortcutsResponsive(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		large         bool
		columns       int
	}{
		{80, 24, true, 2},
		{40, 16, true, 1},
		{24, 12, false, 1},
	} {
		m := newShellModel()
		m.signedIn = true
		m.width, m.height = tc.width, tc.height
		view := ansi.Strip(m.View().Content)
		if strings.Contains(view, "Signed in") {
			t.Fatalf("%dx%d: home shows a redundant session row", tc.width, tc.height)
		}
		if !strings.Contains(view, "/search") || !strings.Contains(view, "/favorites") {
			t.Fatalf("%dx%d: primary commands are missing", tc.width, tc.height)
		}
		if tc.large && !strings.Contains(view, "█████") {
			t.Fatalf("%dx%d: large wordmark is missing", tc.width, tc.height)
		}
		if !tc.large && !strings.Contains(view, "ZLIB") {
			t.Fatalf("%dx%d: compact wordmark is missing", tc.width, tc.height)
		}
		if strings.Contains(view, "╱") || strings.Contains(view, "Z-Library") || strings.Contains(view, "™") {
			t.Fatalf("%dx%d: home shows strokes or brand text around the wordmark", tc.width, tc.height)
		}
		for _, row := range homeLogo(tc.width, tc.height) {
			plain := strings.TrimSpace(ansi.Strip(row))
			if strings.HasPrefix(plain, "╱") || strings.HasSuffix(plain, "╱") {
				t.Fatalf("%dx%d: decorative slashes surround the logo: %q", tc.width, tc.height, plain)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "/search") {
				both := strings.Contains(line, "/favorites")
				if both != (tc.columns == 2) {
					t.Fatalf("%dx%d: unexpected shortcut columns", tc.width, tc.height)
				}
			}
		}
		if tc.width >= 76 {
			commandStart := -1
			for _, line := range strings.Split(view, "\n") {
				for _, name := range []string{"/search", "/recommended", "/config"} {
					if start := strings.Index(line, name); start >= 0 {
						if commandStart >= 0 && commandStart != start {
							t.Fatal("shortcut columns do not align")
						}
						commandStart = start
					}
				}
			}
		}
	}
	logo := homeLogo(80, 24)
	if len(logo) != len(homeLetterforms[0]) {
		t.Fatalf("large wordmark has %d rows, want only the letterform rows", len(logo))
	}
	width := ansi.StringWidth(logo[0])
	for _, row := range logo {
		if ansi.StringWidth(row) != width {
			t.Fatal("wordmark rows have inconsistent width")
		}
	}
	if ramp := homeWordmark()[0]; strings.Count(ramp, "\x1b[") < 4 || ansi.Strip(ramp) != homeWordmarkRows()[0] {
		t.Fatal("wordmark is not painted column by column")
	}
	originalVersion := zlib.Version
	zlib.Version = "v0.0.9-0.20261015033749-d94e112583df+dirty"
	t.Cleanup(func() { zlib.Version = originalVersion })
	for _, size := range [][2]int{{80, 24}, {40, 16}, {24, 12}} {
		m := newShellModel()
		m.signedIn = true
		m.width, m.height = size[0], size[1]
		if view := ansi.Strip(m.View().Content); strings.Contains(view, "v0.0.9") {
			t.Fatalf("%dx%d: home shows the build version", size[0], size[1])
		}
	}
	if compactLogo := homeLogo(24, 12); len(compactLogo) != 1 || ansi.Strip(compactLogo[0]) != "ZLIB" {
		t.Fatalf("compact logo = %q, want only ZLIB", compactLogo)
	}
	for _, shortcut := range signedInShortcuts {
		if !slices.ContainsFunc(shellCommands, func(command shellCommand) bool { return command.name == shortcut.name }) {
			t.Fatalf("home shortcut /%s is not a shell command", shortcut.name)
		}
	}
}

func TestHomeWordmarkGradientVisibleInEveryTheme(t *testing.T) {
	for _, name := range themeOrder {
		theme, ok := namedTheme(name)
		if !ok {
			t.Fatalf("built-in theme %q is missing", name)
		}
		if span := colorDistance(theme.Accent, wordmarkRampEnd(theme)); span < wordmarkMinRampSpan {
			t.Fatalf("%s: wordmark gradient span %.1f reads as a flat color", name, span)
		}
	}
}

func TestShellCommandPaletteWrapsAndKeepsSelectionVisible(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/")
	m, _ = shellStep(m, key("up"))
	visible := m.paletteVisibleRows(m.height)
	if m.selection != len(shellCommands)-1 || m.paletteOffset != len(shellCommands)-visible {
		t.Fatalf("up from first did not wrap to last: selection=%d offset=%d", m.selection, m.paletteOffset)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "> /quit") {
		t.Fatalf("wrapped selection is outside the visible command list: %q", ansi.Strip(m.View().Content))
	}
	m, _ = shellStep(m, key("down"))
	if m.selection != 0 || m.paletteOffset != 0 {
		t.Fatalf("down from last did not wrap to first: selection=%d offset=%d", m.selection, m.paletteOffset)
	}
	for range visible - 1 {
		m, _ = shellStep(m, key("down"))
	}
	if m.paletteOffset != 0 {
		t.Fatal("palette scrolled before selection left the visible window")
	}
	m, _ = shellStep(m, key("down"))
	if m.paletteOffset != 1 {
		t.Fatalf("palette did not scroll at the visible edge: offset=%d", m.paletteOffset)
	}
	m.input.SetValue("/th")
	m.selection, m.paletteOffset = 0, 0
	m, _ = shellStep(m, key("up"))
	if m.selection != 0 || m.paletteOffset != 0 {
		t.Fatal("single filtered command should stay selected")
	}
}

func TestShellCommandDialogOverlaysPageAndKeepsFocus(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}, {ID: "2", Hash: "def", Name: "Second"}}
	m.open("list", list)
	m, _ = shellStep(m, key("/"))
	if !m.paletteOpen() || m.page.(searchSelectModel).height != 23 {
		t.Fatal("command dialog did not preserve the underlying list viewport")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "╭") || !strings.Contains(view, "Commands") || !strings.Contains(view, "> /search") {
		t.Fatal("commands did not open in a selected overlay")
	}
	m, _ = shellStep(m, key("down"))
	if m.selection != 1 || m.page.(searchSelectModel).cursor != 0 {
		t.Fatal("command navigation moved the book selection")
	}
	m, _ = shellStep(m, key("tab"))
	if m.paletteOpen() || m.input.Value() != "/favorites " || m.page.(searchSelectModel).height != 22 {
		t.Fatal("Tab did not complete into the inline command input")
	}
	m, _ = shellStep(m, key("esc"))
	if m.typing || m.page.(searchSelectModel).cursor != 0 || m.page.(searchSelectModel).height != 23 {
		t.Fatal("closing the command input lost the list selection")
	}
}

func TestShellCommandDialogFitsCompactTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 16}, {32, 12}, {24, 10}, {20, 5}} {
		m := newShellModel()
		m, _ = shellStep(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.input.SetValue("/")
		m, _ = shellStep(m, key("up"))
		view := m.View()
		lines := strings.Split(view.Content, "\n")
		if len(lines) != size[1] || !strings.Contains(ansi.Strip(view.Content), "/quit") {
			t.Fatalf("selected command is unavailable at %v", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("command dialog exceeds %v: %q", size, line)
			}
		}
	}
}

func TestShellInformationUsesFullScreenAndScrolls(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	m.openInfo("Available domains", strings.Join([]string{
		"domain 1", "domain 2", "domain 3", "domain 4", "domain 5", "domain 6",
		"domain 7", "domain 8", "domain 9", "domain 10", "domain 11", "domain 12",
	}, "\n"))
	view := m.View()
	if !view.AltScreen || len(strings.Split(view.Content, "\n")) != 16 || !strings.Contains(ansi.Strip(view.Content), "1–10 of 12") {
		t.Fatal("information page did not fill the viewport or show the scroll range")
	}
	m, _ = shellStep(m, key("end"))
	if m.infoOffset != 2 || !strings.Contains(ansi.Strip(m.View().Content), "domain 12") {
		t.Fatal("end did not reveal the final information row")
	}
	m, _ = shellStep(m, key("esc"))
	if m.kind != "" || m.status != "" {
		t.Fatal("closing information did not restore the home screen")
	}
}

func TestShellVersionIsNotACommand(t *testing.T) {
	m := newShellModel()
	m.input.SetValue("/ver")
	if len(m.matches()) != 0 {
		t.Fatal("version still appears in the command palette")
	}
	m.dispatch("/version")
	if m.kind != "" || !m.typing || m.status != "Unknown command: /version" {
		t.Fatal("version was accepted in the TUI")
	}
}

func TestShellHomeRequiresSlashForCommands(t *testing.T) {
	m := newShellModel()
	m.signedIn = true
	for _, event := range []tea.Msg{key("s"), key("ctrl+k"), tea.PasteMsg{Content: "/search books"}, key("enter")} {
		m, _ = shellStep(m, event)
		if m.input.Value() != "" || m.page != nil || m.paletteOpen() {
			t.Fatal("home accepted input before slash opened commands")
		}
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Search books or type") || strings.Contains(view, "enter search") || strings.Contains(view, "> ") || !strings.Contains(view, "/ commands") {
		t.Fatal("home still shows the search input or lacks the command hint")
	}
	m, _ = shellStep(m, key("/"))
	if !m.paletteOpen() {
		t.Fatal("slash did not open commands")
	}
	m, _ = shellStep(m, key("esc"))
	if !m.homeIdle() || m.paletteOpen() || m.footerHeight() != 1 {
		t.Fatal("Esc did not return to the home screen without an input")
	}
}

func TestShellListUsesAvailableRowsAndRestoresFocus(t *testing.T) {
	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "First"}}
	m.open("list", list)
	if got := m.page.(searchSelectModel).height; got != 23 {
		t.Fatalf("list content height = %d, want 23", got)
	}
	m, _ = shellStep(m, key("/"))
	if !m.typing || m.page.(searchSelectModel).height != 23 {
		t.Fatal("command dialog resized the list behind it")
	}
	m, _ = shellStep(m, key("esc"))
	if m.typing || m.page.(searchSelectModel).height != 23 || m.page.(searchSelectModel).cursor != 0 {
		t.Fatal("closing command input did not restore the list")
	}
	m.open("logout", shellConfirm{title: "Log out?", action: "Log out"})
	if got := m.page.(shellConfirm).height; got != 24 {
		t.Fatalf("dialog content height = %d, want full terminal", got)
	}
}

// The shell footer offered "ctrl+f refine" on every list, but only search
// results have filters to reopen; on the others the key did nothing.
func TestShellOffersRefineOnlyOnSearchResults(t *testing.T) {
	results := newSearchSelectModel("golang", nil, nil, false)
	results.state = searchStateResults
	results.books = []zlib.Book{{ID: "1", Hash: "a", Name: "Book"}}
	feed := newBookFeedSelectModel("Recommended", func() ([]zlib.Book, error) { return results.books, nil })
	feed.books = results.books
	for _, tc := range []struct {
		name string
		page tea.Model
		want bool
	}{
		{"search", results, true},
		{"recommended", feed, false},
		{"history", newHistorySelectModel(nil), false},
	} {
		m := newShellModel()
		m.width, m.height = 100, 30
		m.open("list", tc.page)
		if got := strings.Contains(ansi.Strip(m.footerView()), "ctrl+f refine"); got != tc.want {
			t.Errorf("%s: footer offers ctrl+f refine = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestShellSearchErrorCanRefineQuery(t *testing.T) {
	m := newShellModel()
	list := newSearchSelectModel("golang", nil, nil, false)
	list.state = searchStateResults
	list.err = errors.New("request timed out")
	list.retryPage = 1
	m.open("list", list)
	if !strings.Contains(ansi.Strip(m.View().Content), "ctrl+f search") {
		t.Fatalf("shell search error does not show its refine shortcut: %q", ansi.Strip(m.View().Content))
	}
	m, _ = shellStep(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	got := m.page.(searchSelectModel)
	if got.state != searchStateFilter || got.filter.input.Value() != "golang" {
		t.Fatal("Ctrl+F did not open the failed search query")
	}
}
