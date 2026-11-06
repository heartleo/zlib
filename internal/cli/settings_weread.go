package cli

import (
	"context"
	"errors"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// Actions on the WeRead account page.
const (
	wereadActionLogIn      = "Log in with WeChat"
	wereadActionLogInAgain = "Log in again"
	wereadActionLogOut     = "Log out"
)

type settingsWeReadTestMsg struct {
	generation int
	err        error
}

// settingsWeReadNameMsg reports a nickname refresh; the account row reloads
// from the saved login.
type settingsWeReadNameMsg struct{}

// editHasExtraAction reports whether the edit page offers Browse or Test.
func editHasExtraAction(key string) bool {
	return key == downloadDirEnvVar || key == zlib.EnvProxy || key == wereadProxyEnv
}

func (m settingsModel) wereadLoggedIn() bool {
	return m.value(wereadAccountKey).Saved != ""
}

func (m settingsModel) wereadAccountActions() []string {
	if m.wereadLoggedIn() {
		return []string{wereadActionLogInAgain, wereadActionLogOut}
	}
	return []string{wereadActionLogIn}
}

// openWeReadAccount shows the account page and, when logged in, refreshes
// the nickname in the background; a failed refresh keeps the saved one.
func (m *settingsModel) openWeReadAccount() tea.Cmd {
	if m.page != "weread-account" {
		m.returnPage, m.returnCursor = m.page, m.cursor
	}
	m.page = "weread-account"
	m.cursor = 0
	m.message = ""
	if !m.wereadLoggedIn() {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		if c, err := loadWeReadClient(); err == nil {
			_, _ = c.UpdateName(ctx)
		}
		return settingsWeReadNameMsg{}
	}
}

func (m settingsModel) runWeReadAccountAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case wereadActionLogIn, wereadActionLogInAgain:
		login := newWeReadLoginModel(newWeReadLoginClient())
		next, _ := login.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		login = next.(wereadLoginModel)
		m.wereadLogin = &login
		m.page = "weread-login"
		m.message = ""
		return m, login.Init()
	case wereadActionLogOut:
		m.logoutConfirm, m.logoutFocused = true, false
	}
	return m, nil
}

// updateWeReadLogin forwards messages to the embedded login page. Its
// tea.Quit ends only that page, never the settings program.
func (m settingsModel) updateWeReadLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
	}
	next, cmd := m.wereadLogin.Update(msg)
	login := next.(wereadLoginModel)
	if !login.finished() {
		m.wereadLogin = &login
		return m, cmd
	}
	m.wereadLogin = nil
	m.page = "weread-account"
	m.cursor = 0
	_ = m.reload()
	switch {
	case errors.Is(login.err, errCancelled):
		m.message = "Login cancelled"
	case login.err != nil:
		m.message = "Login failed: " + shellErrorText(login.err)
	default:
		m.message = "Logged in to WeRead as " + wereadAccountLabel(login.creds)
	}
	return m, nil
}

func (m settingsModel) updateLogoutConfirm(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "left", "right", "tab", "shift+tab":
		m.logoutFocused = !m.logoutFocused
		return m, nil
	case "y":
		m.logoutFocused = true
	case "enter":
	default:
		m.logoutConfirm = false
		return m, nil
	}
	m.logoutConfirm = false
	if !m.logoutFocused {
		return m, nil
	}
	removed, err := config.RemoveWeRead()
	_ = m.reload()
	m.cursor = 0
	switch {
	case err != nil:
		m.message = "Could not log out: " + err.Error()
	case removed:
		m.message = "Logged out of WeRead"
	default:
		m.message = "Not logged in to WeRead"
	}
	return m, nil
}

// testWeRead checks that WeRead answers through the proxy value being
// edited, without logging in.
func (m *settingsModel) testWeRead(proxy string) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.generation++
	generation := m.generation
	m.busy = true
	m.message = ""
	m.spinner = newLoadingSpinner()
	var opts []weread.Option
	if t := wereadTransport(proxy); t != nil {
		opts = append(opts, weread.WithTransport(t))
	}
	client := weread.NewClient(opts...)
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		return settingsWeReadTestMsg{generation, client.Ping(ctx)}
	})
}

// wereadImportRules are shown under the account actions. Size and count
// limits are left out: WeRead sets them on its server and the import reports
// its own message when one applies.
var wereadImportRules = [][2]string{
	{"Formats", "EPUB, PDF, MOBI, AZW3, TXT"},
}

// wereadImportRuleLines renders wereadImportRules under a section rule.
func wereadImportRuleLines(w int) []string {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	rows := make([][2]string, len(wereadImportRules))
	for i, row := range wereadImportRules {
		rows[i] = [2]string{row[0], ansi.Truncate(row[1], max(1, w-14), "…")}
	}
	lines := []string{"  " + sectionHeader("Import rules", "", max(1, w-4))}
	return append(lines, formatDetailRows(rows, "  ", func(s string) string { return muted.Render(s) })...)
}

func (m settingsModel) wereadAccountView(w, h int) tea.View {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	label := func(s string) string { return muted.Render(s) }
	account := m.value(wereadAccountKey)
	var head, intro []string
	if m.wereadLoggedIn() {
		head = append(head, lipgloss.NewStyle().Foreground(currentTheme.Success).Render("● Logged in"), "")
		rows := wereadAccountRows(m.wereadCreds)
		for i := range rows {
			rows[i][1] = ansi.Truncate(rows[i][1], max(1, w-16), "…")
			if rows[i][1] == wereadNoNickname {
				rows[i][1] = muted.Render(wereadNoNickname)
			}
		}
		head = append(head, formatDetailRows(rows, "  ", label)...)
	} else {
		head = append(head, muted.Render("○ "+account.Value))
		intro = []string{"",
			"  Scan a QR code with WeChat to let zlib import",
			"  books into your WeRead shelf.",
			"",
			label("  zlib signs in as the WeRead e-ink app. WeRead"),
			label("  may ask for verification in its own app.")}
	}
	actions := []string{""}
	for i, action := range m.rows() {
		actions = append(actions, styledListRow(action, i == m.cursor, w, currentTheme))
	}
	rules := append([]string{""}, wereadImportRuleLines(w)...)

	// Spend rows on the status, actions and key hints first; the intro and
	// the import rules are reference text that appears when it fits.
	// settingsCompactView adds the title, blank rows and the key hints.
	budget := h - 4
	if m.message != "" {
		budget -= 2
	}
	used := len(head) + len(actions)
	lines := head
	fits := used+len(intro) <= budget
	if fits {
		lines = append(lines, intro...)
		used += len(intro)
	}
	lines = append(lines, actions...)
	if fits && used+len(rules) <= budget {
		lines = append(lines, rules...)
	}
	controls := []keys.Binding{uiKey("enter", "select"), uiKey("↑/↓", "move"), uiKey("esc", "back")}
	v := settingsCompactView(w, h, "Settings / WeRead account", "", lines, m.message, controls)
	if m.logoutConfirm {
		v.Content = confirmationDialog(v.Content, w, h, "Log out of WeRead?", account.Value, "",
			"Imports need a new QR login afterwards.", "Log out", m.logoutFocused, true)
	}
	return v
}
