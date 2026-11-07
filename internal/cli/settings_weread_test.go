package cli

import (
	"net/http"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
)

// cursorTo selects key on a settings list page.
func cursorTo(t *testing.T, m settingsModel, key string) settingsModel {
	t.Helper()
	for i, k := range settingsPageKeys(m.page) {
		if k == key {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("%s is not on page %s", key, m.page)
	return m
}

func TestSettingsHomeShowsWeReadSection(t *testing.T) {
	isolateConfigHome(t)
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "home")
	if err != nil {
		t.Fatal(err)
	}
	m, _ = settingsStep(t, m, tea.WindowSizeMsg{Width: 80, Height: 40})
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"KINDLE", "SMTP security", "WEREAD", "WeRead account", "Not logged in", "WeRead proxy", "System proxy environment"} {
		if !strings.Contains(view, want) {
			t.Fatalf("home page lacks %q:\n%s", want, view)
		}
	}
	// Kindle rows keep their own values after the refactor.
	if got := m.value(kindlePortKey).Value; got != "587" {
		t.Fatalf("Kindle port = %q", got)
	}
}

func TestSettingsWeReadAccountLoginCancelReturnsToAccount(t *testing.T) {
	isolateConfigHome(t)
	offlineWeRead(t)
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "home")
	m = cursorTo(t, m, wereadAccountKey)
	homeCursor := m.cursor

	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "weread-account" || strings.Join(m.rows(), ",") != wereadActionLogIn {
		t.Fatalf("page = %q, rows = %v", m.page, m.rows())
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Scan a QR code with WeChat") {
		t.Fatal("account page does not explain the login")
	}

	m, cmd := settingsStep(t, m, key("enter"))
	if m.page != "weread-login" || cmd == nil {
		t.Fatalf("page = %q, want the embedded login", m.page)
	}
	m, cmd = settingsStep(t, m, key("esc"))
	if m.page != "weread-account" || m.message != "Login cancelled" || cmd != nil {
		t.Fatalf("page = %q, message = %q, cmd = %v; cancelling must not quit settings", m.page, m.message, cmd)
	}
	m, _ = settingsStep(t, m, key("esc"))
	if m.page != "home" || m.cursor != homeCursor {
		t.Fatalf("page = %q, cursor = %d; want home at the account row", m.page, m.cursor)
	}
}

func TestSettingsWeReadLogOut(t *testing.T) {
	isolateConfigHome(t)
	if err := config.SaveWeRead(weread.Credentials{VID: "42", RefreshToken: "r", DeviceID: "d", Name: "Reader"}); err != nil {
		t.Fatal(err)
	}
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "weread")
	if got := m.value(wereadAccountKey).Value; got != "Reader (42)" {
		t.Fatalf("account = %q", got)
	}
	m, _ = settingsStep(t, m, key("enter"))
	if strings.Join(m.rows(), ",") != wereadActionLogInAgain+","+wereadActionLogOut {
		t.Fatalf("rows = %v", m.rows())
	}
	m, _ = settingsStep(t, m, key("down"))
	m, _ = settingsStep(t, m, key("enter"))
	if !m.logoutConfirm || !strings.Contains(ansi.Strip(m.View().Content), "Log out of WeRead?") {
		t.Fatal("log out did not ask for confirmation")
	}
	m, _ = settingsStep(t, m, key("enter")) // Cancel is focused first
	if _, err := os.Stat(config.WeReadPath()); err != nil || m.logoutConfirm {
		t.Fatal("cancelled logout removed the login")
	}
	m, _ = settingsStep(t, m, key("enter"))
	m, _ = settingsStep(t, m, key("right"))
	m, _ = settingsStep(t, m, key("enter"))
	if _, err := os.Stat(config.WeReadPath()); !os.IsNotExist(err) || m.message != "Logged out of WeRead" {
		t.Fatalf("logout: stat err = %v, message = %q", err, m.message)
	}
	if strings.Join(m.rows(), ",") != wereadActionLogIn {
		t.Fatalf("rows after logout = %v", m.rows())
	}
}

func TestSettingsWeReadAccountPageShowsDetails(t *testing.T) {
	isolateConfigHome(t)
	view := func() string {
		m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "weread")
		m.openWeReadAccount()
		next, _ := m.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
		return ansi.Strip(next.(settingsModel).View().Content)
	}
	if v := view(); !strings.Contains(v, "○ Not logged in") || !strings.Contains(v, "Scan a QR code") || strings.Contains(v, "User ID") {
		t.Fatalf("logged-out page:\n%s", v)
	}
	if err := config.SaveWeRead(weread.Credentials{VID: "1001", RefreshToken: "r", DeviceID: "d"}); err != nil {
		t.Fatal(err)
	}
	v := view()
	for _, want := range []string{"● Logged in", "Nickname   Not available yet", "User ID    1001", "Saved to   ~"} {
		if !strings.Contains(v, want) {
			t.Fatalf("page lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "user 1001") {
		t.Fatal("the ID is still prefixed with user")
	}
	if err := config.SaveWeRead(weread.Credentials{VID: "1001", RefreshToken: "r", DeviceID: "d", Name: "Reader"}); err != nil {
		t.Fatal(err)
	}
	if v := view(); !strings.Contains(v, "Nickname   Reader") {
		t.Fatalf("page lacks the nickname:\n%s", v)
	}
}

func TestSettingsWeReadProxySavesAndValidates(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(wereadProxyEnv, "")
	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "weread")
	m = cursorTo(t, m, wereadProxyEnv)
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || m.key != wereadProxyEnv {
		t.Fatalf("page = %q, key = %q", m.page, m.key)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "direct: connect without any proxy") || !strings.Contains(view, "Test") {
		t.Fatalf("proxy editor lacks guidance or Test:\n%s", view)
	}

	m.input.SetValue("ftp://example.com:21")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || !strings.Contains(m.message, "unsupported proxy scheme") {
		t.Fatalf("invalid proxy saved: page = %q, message = %q", m.page, m.message)
	}

	m.input.SetValue("DIRECT")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "weread" || m.message != "Saved" {
		t.Fatalf("page = %q, message = %q", m.page, m.message)
	}
	data, _ := os.ReadFile(s.global)
	if !strings.Contains(string(data), `ZLIB_WEREAD_PROXY="direct"`) {
		t.Fatalf("saved .env = %q", data)
	}
	if got := displaySetting(wereadProxyEnv, m.value(wereadProxyEnv).Value); got != "Direct (no proxy)" {
		t.Fatalf("display = %q", got)
	}
	if os.Getenv(wereadProxyEnv) != "direct" {
		t.Fatal("saved proxy did not apply to this process")
	}
}

func TestSettingsKindleSaveKeepsWeReadRows(t *testing.T) {
	isolateConfigHome(t)
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "kindle")
	m.edit(kindleToKey)
	m.input.SetValue("reader@kindle.com")
	m, _ = settingsStep(t, m, key("enter"))
	if m.value(kindleToKey).Value != "reader@kindle.com" || m.value(wereadAccountKey).Value != "Not logged in" ||
		m.value(wereadProxyEnv).Source == "" {
		t.Fatalf("values after Kindle save: %+v", m.values)
	}
}

func TestWeReadTransportFollowsProxySetting(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://i.weread.qq.com/", nil)
	if wereadTransport("") != nil {
		t.Fatal("empty setting should keep the default transport")
	}
	direct := wereadTransport("direct").(*http.Transport)
	if direct.Proxy != nil {
		t.Fatal("direct still uses a proxy")
	}
	proxied := wereadTransport("socks5://127.0.0.1:1080").(*http.Transport)
	if u, err := proxied.Proxy(req); err != nil || u.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy = %v, %v", u, err)
	}
	broken := wereadTransport("not a url").(*http.Transport)
	if _, err := broken.Proxy(req); err == nil || !strings.Contains(err.Error(), wereadProxyEnv) {
		t.Fatalf("invalid setting error = %v", err)
	}
}

func TestWeReadLoginOffersNewCodeAfterExpiry(t *testing.T) {
	offlineWeRead(t)
	m := newWeReadLoginModel(newWeReadClient())
	next, cmd := m.Update(errMsg{weread.ErrQRExpired})
	m = next.(wereadLoginModel)
	if cmd != nil || m.finished() || !strings.Contains(ansi.Strip(m.View().Content), "Press r for a new code") {
		t.Fatal("expired QR ended the page instead of offering a new code")
	}
	next, cmd = m.Update(key("r"))
	m = next.(wereadLoginModel)
	if cmd == nil || m.retry != "" || m.finished() {
		t.Fatal("r did not request a new code")
	}
	m.cancel()

	next, cmd = m.Update(errMsg{weread.ErrSessionExpired})
	if m = next.(wereadLoginModel); cmd == nil || !m.finished() {
		t.Fatal("a non-QR error must end the page")
	}
}

func TestWeReadLoginAsksForLargerTerminal(t *testing.T) {
	code, err := encodeQR("https://open.weixin.qq.com/connect/confirm?uuid=fixture")
	if err != nil {
		t.Fatal(err)
	}
	m := newWeReadLoginModel(nil)
	m.code = code
	next, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 12})
	if view := ansi.Strip(next.(wereadLoginModel).View().Content); !strings.Contains(view, "Enlarge the terminal") {
		t.Fatalf("small terminal view:\n%s", view)
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if view := ansi.Strip(next.(wereadLoginModel).View().Content); strings.Contains(view, "Enlarge") || !strings.Contains(view, "expires in 5 minutes") {
		t.Fatalf("large terminal view:\n%s", view)
	}
}

func TestFilePickerStartsInDownloadDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(downloadDirEnvVar, dir)
	if got := filePickerStartDir(); got != dir {
		t.Fatalf("start dir = %q, want %q", got, dir)
	}
	t.Setenv(downloadDirEnvVar, dir+"-missing")
	wd, _ := os.Getwd()
	if got := filePickerStartDir(); got != wd {
		t.Fatalf("start dir = %q, want the working directory", got)
	}
}

func TestWeReadLogoutDuringNameRefresh(t *testing.T) {
	isolateConfigHome(t)
	entered, release := make(chan struct{}), make(chan struct{})
	fakeWeReadAPI(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"name":"Reader"}`))
	})
	if err := config.SaveWeRead(weread.Credentials{VID: "42", AccessToken: "fixture", RefreshToken: "fixture", DeviceID: "fixture"}); err != nil {
		t.Fatal(err)
	}
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "weread")
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.openWeReadAccount()
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	<-entered
	m.logoutFocused = true
	m.updateLogoutConfirm("enter")
	close(release)
	<-done
	if _, err := os.Stat(config.WeReadPath()); !os.IsNotExist(err) {
		t.Fatal("background refresh restored login")
	}
}
