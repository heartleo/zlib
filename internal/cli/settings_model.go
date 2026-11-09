package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

const (
	kindleToKey       = "KINDLE_TO"
	kindleFromKey     = "KINDLE_FROM"
	kindleHostKey     = "KINDLE_SMTP_HOST"
	kindlePortKey     = "KINDLE_SMTP_PORT"
	kindleSecurityKey = "KINDLE_SMTP_SECURITY"
)

var kindleSettingKeys = []string{kindleToKey, kindleFromKey, kindleHostKey, kindlePortKey, kindleSecurityKey}

// wereadAccountKey is the WeRead login row. It is not an editable value:
// Enter opens the account page.
const wereadAccountKey = "WEREAD_ACCOUNT"

// settingsSection is one group of rows on the home page and its own page.
type settingsSection struct {
	title, page string
	keys        []string
}

var settingsSections = []settingsSection{
	{"NETWORK", "network", []string{zlib.EnvDomain, zlib.EnvProxy}},
	{"DOWNLOADS", "download", []string{downloadDirEnvVar, zlib.EnvRetries}},
	{"SEARCH", "search", []string{searchExtensionsEnv}},
	{"KINDLE", "kindle", kindleSettingKeys},
	{"WEREAD", "weread", []string{wereadAccountKey, wereadProxyEnv}},
}

var settingLabels = map[string]string{
	zlib.EnvDomain:      "Domain",
	zlib.EnvProxy:       "Proxy URL",
	downloadDirEnvVar:   "Default directory",
	searchExtensionsEnv: "Default formats",
	zlib.EnvRetries:     "Download retries",
	kindleToKey:         "Kindle address",
	kindleFromKey:       "From address",
	kindleHostKey:       "SMTP host",
	kindlePortKey:       "SMTP port",
	kindleSecurityKey:   "SMTP security",
	wereadAccountKey:    "WeRead account",
	wereadProxyEnv:      "WeRead proxy",
}

// settingsPageKeys lists the setting rows of a list page in display order.
func settingsPageKeys(page string) []string {
	var keys []string
	for _, section := range settingsSections {
		if page == "home" {
			keys = append(keys, section.keys...)
		} else if section.page == page {
			return section.keys
		}
	}
	return keys
}

func isSettingsListPage(page string) bool {
	return page == "home" || page == "network" || page == "download" || page == "search" || page == "kindle" || page == "weread"
}

// wereadAccount loads the saved WeRead login and its settings row: the
// account label when logged in, otherwise the reason there is none.
func wereadAccount() (weread.Credentials, []settingValue) {
	v := settingValue{Key: wereadAccountKey, Value: "Not logged in", Source: "Default"}
	creds, err := config.LoadWeRead()
	switch {
	case err == nil:
		v.Value, v.Saved, v.Source = wereadAccountLabel(creds), creds.VID, "WeRead login"
	case !errors.Is(err, weread.ErrNotLoggedIn):
		v.Value, v.Source = "Saved login is unreadable", "WeRead login"
	}
	if err != nil {
		creds = weread.Credentials{}
	}
	return creds, []settingValue{v}
}

type settingsProbeMsg struct {
	generation int
	checks     map[string]zlib.DomainCheck
}

// settingsDomainCheckMsg is one domain's finished check, sent while the rest
// are still running. next reads the following result.
type settingsDomainCheckMsg struct {
	generation int
	domain     string
	check      zlib.DomainCheck
	next       tea.Cmd
}
type settingsModel struct {
	store          settingsStore
	values         []settingValue
	kindle         zlib.KindleConfig
	page           string
	cursor         int
	width, height  int
	input          textinput.Model
	key            string
	message        string
	domains        []string
	busy           bool
	spinner        spinner.Model
	generation     int
	cancel         context.CancelFunc
	ctx            context.Context
	probeReturn    string
	directory      string
	directories    []string
	discard        bool
	discardFocused bool
	editAction     int
	formats        []zlib.Extension
	formatCursor   int
	original       string
	returnPage     string
	returnCursor   int
	// Domain page state (settings_domains.go).
	checks        map[string]zlib.DomainCheck
	pendingCheck  zlib.DomainCheck
	mirrorConfirm string
	mirrorTarget  string
	mirrorFocused bool
	// mirrorCursor is the domain row to restore when the add-mirror form
	// closes; returnCursor still belongs to the page that opened the list.
	mirrorCursor int
	// WeRead account page state.
	wereadLogin   *wereadLoginModel
	logoutConfirm bool
	logoutFocused bool
	// wereadCreds is the saved WeRead login, zero when there is none.
	wereadCreds weread.Credentials
}

func newSettingsModel(ctx context.Context, store settingsStore, page string) (settingsModel, error) {
	values, err := store.values()
	if err != nil {
		return settingsModel{}, err
	}
	kindle, err := config.LoadKindleConfigFrom(store.kindle)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return settingsModel{}, err
	}
	values = append(values, kindleSettingValues(kindle)...)
	wereadCreds, wereadValues := wereadAccount()
	values = append(values, wereadValues...)
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.Prompt = "> "
	input.SetWidth(48)
	styles := input.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Title)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Cursor.Color = currentTheme.Accent
	input.SetStyles(styles)
	m := settingsModel{store: store, values: values, kindle: kindle, wereadCreds: wereadCreds, page: page, input: input, ctx: ctx, width: 80, height: 24}
	if page == "domain" {
		m.domains = m.domainCandidates()
		m.cursor = m.currentDomainIndex()
	}
	return m, nil
}

func kindleSettingValues(cfg zlib.KindleConfig) []settingValue {
	values := []settingValue{
		{Key: kindleToKey, Value: cfg.To, Saved: cfg.To},
		{Key: kindleFromKey, Value: cfg.From, Saved: cfg.From},
		{Key: kindleHostKey, Value: cfg.SMTPHost, Saved: cfg.SMTPHost},
		{Key: kindlePortKey, Saved: strconv.Itoa(cfg.SMTPPort)},
		{Key: kindleSecurityKey, Saved: cfg.SMTPSecurity},
	}
	if cfg.SMTPPort > 0 {
		values[3].Value = strconv.Itoa(cfg.SMTPPort)
	} else {
		values[3].Value, values[3].Saved = strconv.Itoa(defaultKindleSMTPPort), ""
		cfg.SMTPPort = defaultKindleSMTPPort
	}
	// Without a saved mode, show the one the port implies.
	values[4].Value = cfg.Security()
	if cfg.SMTPHost == "" {
		values[2].Value = defaultKindleSMTPHost
	}
	for i := range values {
		values[i].Source = "Kindle configuration"
		if values[i].Saved == "" {
			values[i].Source = "Default"
		}
	}
	return values
}

func isKindleSetting(key string) bool {
	switch key {
	case kindleToKey, kindleFromKey, kindleHostKey, kindlePortKey, kindleSecurityKey:
		return true
	}
	return false
}

// Init starts the domain check when the model opens on the domain page. The
// check runs from Update, because Init cannot keep changes to the model.
func (m settingsModel) Init() tea.Cmd {
	if m.page == "domain" {
		return func() tea.Msg { return settingsStartProbeMsg{} }
	}
	return nil
}
func (m settingsModel) value(key string) settingValue {
	for _, v := range m.values {
		if v.Key == key {
			return v
		}
	}
	return settingValue{Key: key}
}
func (m settingsModel) rows() []string {
	switch m.page {
	case "domain":
		return m.domains
	case "browse":
		return append([]string{".."}, m.directories...)
	case "weread-account":
		return m.wereadAccountActions()
	}
	keys := settingsPageKeys(m.page)
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = settingLabels[key]
	}
	return labels
}

// reload rereads every setting after a save.
func (m *settingsModel) reload() error {
	values, err := m.store.values()
	if err != nil {
		return err
	}
	values = append(values, kindleSettingValues(m.kindle)...)
	creds, wereadValues := wereadAccount()
	m.wereadCreds = creds
	m.values = append(values, wereadValues...)
	return nil
}

func (m *settingsModel) edit(key string) tea.Cmd {
	m.returnPage = m.page
	m.returnCursor = m.cursor
	m.key = key
	m.page = "edit"
	m.message = ""
	m.discard = false
	m.editAction = 0
	v := m.value(key)
	value := v.Value
	if (v.Source == "Default" && !isKindleSetting(key)) || (key == zlib.EnvDomain && v.Source == "Saved session") {
		value = v.Saved
	}
	m.original = value
	if key == searchExtensionsEnv {
		m.page = "formats"
		m.formats, _ = parseDefaultSearchExtensions(value)
		m.formatCursor = 0
		m.input.Blur()
		return nil
	}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Placeholder = ""
	if key == zlib.EnvProxy {
		m.input.Placeholder = "http://host:port"
	} else if key == wereadProxyEnv {
		m.input.Placeholder = "http://host:port or direct"
	}
	m.input.SetWidth(max(1, min(56, m.width-ansi.StringWidth(m.input.Prompt)-4)))
	m.input.EchoMode = textinput.EchoNormal
	return m.input.Focus()
}

func (m *settingsModel) back() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.generation++
	m.busy = false
	m.cursor = 0
	m.message = ""
	switch m.page {
	case "domain":
		m.page = m.returnPage
		if m.page == "" {
			m.page = "network"
		}
		m.cursor = m.returnCursor
	case "edit", "formats":
		m.page = m.returnPage
		if m.page == "" {
			m.page = "home"
		}
		m.cursor = m.returnCursor
	case "browse":
		m.page = "edit"
		m.editAction = 0
		m.input.Focus()
	case "mirror":
		m.page = "domain"
		m.cursor = m.mirrorCursor
		m.input.Blur()
	case "weread-account":
		m.page = m.returnPage
		if m.page == "" || m.page == "weread-account" {
			m.page = "home"
		}
		m.cursor = m.returnCursor
	default:
		m.page = "home"
	}
}

func (m *settingsModel) probe(proxy string, page string) tea.Cmd {
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
	m.probeReturn = page
	candidates := m.domainCandidates()
	if page == "domain" {
		m.domains = candidates
	}
	ch := streamDomainChecks(ctx, candidates, proxy)
	return tea.Batch(m.spinner.Tick, nextDomainCheck(ctx, generation, candidates, ch, map[string]zlib.DomainCheck{}))
}

// nextDomainCheck reads one finished probe and hands the page a message for it
// together with the command that reads the next, so each row fills in as its
// check completes. When the stream closes it sends the complete settingsProbeMsg
// (empty if the probe was cancelled). The collected map is only touched by the
// one outstanding command at a time.
func nextDomainCheck(ctx context.Context, generation int, candidates []string, ch <-chan domainCheckEvent, collected map[string]zlib.DomainCheck) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			if ctx.Err() != nil {
				collected = map[string]zlib.DomainCheck{}
			}
			return settingsProbeMsg{generation, collected}
		}
		domain := candidates[event.index]
		collected[domain] = event.check
		return settingsDomainCheckMsg{
			generation: generation, domain: domain, check: event.check,
			next: nextDomainCheck(ctx, generation, candidates, ch, collected),
		}
	}
}

func (m *settingsModel) effectiveProxy() string {
	v := m.value(zlib.EnvProxy)
	if v.Source == "Default" {
		return ""
	}
	return v.Value
}

func (m *settingsModel) save(value string) {
	if isKindleSetting(m.key) {
		m.saveKindle(value)
		return
	}
	if err := m.store.save(m.key, value); err != nil {
		m.message = "Could not save: " + err.Error()
		return
	}
	if err := m.reload(); err != nil {
		m.message = "Saved; could not reload settings: " + err.Error()
		return
	}
	v := m.value(m.key)
	// Update only this process's derived settings. Preserve explicit overrides.
	if v.Override != "" {
		// Explicit process and working-directory settings keep precedence.
	} else if v.Source == "Global configuration" {
		_ = os.Setenv(m.key, v.Value)
	} else if v.Source == "Default" || v.Source == "Saved session" {
		_ = os.Unsetenv(m.key)
	}
	m.back()
	m.message = "Saved"
	if v.Override != "" {
		m.message = "Saved globally; overridden by " + v.Override
	}
	m.domains = nil
}

func (m *settingsModel) saveKindle(value string) {
	if strings.ContainsAny(value, "\r\n\x00") {
		m.message = "Enter a single-line value"
		return
	}
	value = strings.TrimSpace(value)
	cfg := m.kindle
	switch m.key {
	case kindleToKey:
		cfg.To = value
	case kindleFromKey:
		cfg.From = value
	case kindleHostKey:
		cfg.SMTPHost = value
	case kindlePortKey:
		if value == "" {
			cfg.SMTPPort = 0
		} else {
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				m.message = "Enter a port from 1 to 65535"
				return
			}
			cfg.SMTPPort = port
		}
	case kindleSecurityKey:
		// Empty resets to the port's standard mode.
		value = strings.ToLower(value)
		if value != "" && value != zlib.SMTPSecurityTLS && value != zlib.SMTPSecuritySTARTTLS {
			m.message = `Enter "tls" (SSL/TLS) or "starttls"`
			return
		}
		cfg.SMTPSecurity = value
	}
	if cfg.SMTPHost == "" {
		cfg.SMTPHost = defaultKindleSMTPHost
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = defaultKindleSMTPPort
	}
	if err := config.SaveKindleConfigTo(m.store.kindle, cfg); err != nil {
		m.message = "Could not save: " + err.Error()
		return
	}
	m.kindle = cfg
	if err := m.reload(); err != nil {
		m.message = "Saved; could not reload settings: " + err.Error()
		return
	}
	m.back()
	m.message = "Saved"
}

func (m *settingsModel) browse(path string) {
	p, err := expandTilde(path)
	if err == nil {
		p, err = filepath.Abs(p)
	}
	if err != nil {
		m.message = "Invalid directory"
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		m.message = "Cannot open directory"
		return
	}
	m.directory = p
	m.directories = nil
	for _, entry := range entries {
		if entry.IsDir() {
			m.directories = append(m.directories, entry.Name())
		}
	}
	m.page = "browse"
	m.cursor = 0
	m.message = ""
}

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.page == "weread-login" && m.wereadLogin != nil {
		return m.updateWeReadLogin(msg)
	}
	switch msg := msg.(type) {
	case settingsWeReadNameMsg:
		_ = m.reload()
		return m, nil
	case settingsWeReadTestMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		m.message = "WeRead reachable through this setting"
		if msg.err != nil {
			m.message = safeText(msg.err.Error())
		}
		return m, nil
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, min(56, m.width-ansi.StringWidth(m.input.Prompt)-4)))
		return m, nil
	case settingsDomainCheckMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		// The proxy test only counts results; the domain page shows each row now.
		if m.probeReturn != "edit" {
			checks := make(map[string]zlib.DomainCheck, len(m.checks)+1)
			maps.Copy(checks, m.checks)
			checks[msg.domain] = msg.check
			m.checks = checks
		}
		return m, msg.next
	case settingsProbeMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		available := 0
		for _, check := range msg.checks {
			if _, ok := usableOrigin(check); ok {
				available++
			}
		}
		if m.probeReturn == "edit" {
			m.message = fmt.Sprintf("Proxy test: %d available domains", available)
			return m, nil
		}
		m.checks = msg.checks
		m.sortDomains()
		if m.page == "domain" {
			m.message = fmt.Sprintf("%d of %d domains available", available, len(m.domains))
		}
		return m, nil
	case settingsMirrorCheckMsg:
		return m.handleMirrorCheck(msg)
	case settingsStartProbeMsg:
		if m.page == "domain" {
			return m, m.probe(m.effectiveProxy(), "domain")
		}
		return m, nil
	case tea.KeyPressMsg:
		k := msg.String()
		if m.page == "formats" {
			return m.updateFormats(k)
		}
		if k == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.logoutConfirm {
			return m.updateLogoutConfirm(k)
		}
		if m.discard {
			if k == "left" || k == "right" || k == "tab" {
				m.discardFocused = !m.discardFocused
				return m, nil
			}
			if k == "y" || (k == "enter" && m.discardFocused) {
				m.discard = false
				m.back()
			} else if k == "n" || k == "esc" || k == "enter" {
				m.discard = false
			}
			return m, nil
		}
		if m.mirrorConfirm != "" || m.page == "mirror" || m.page == "domain" {
			next, cmd, handled := m.updateDomainKeys(msg)
			if handled {
				return next, cmd
			}
		}
		if k == "esc" {
			if m.page == "home" {
				return m, tea.Quit
			}
			if m.page == "edit" && m.input.Value() != m.original {
				m.discard = true
				m.discardFocused = false
				return m, nil
			}
			m.back()
			return m, nil
		}
		if m.page == "edit" {
			if k == "tab" || k == "shift+tab" {
				delta := 1
				if k == "shift+tab" {
					delta = -1
				}
				m.editAction = (m.editAction + delta + 5) % 5
				if !editHasExtraAction(m.key) && m.editAction == 1 {
					m.editAction = (m.editAction + delta + 5) % 5
				}
				if m.editAction == 0 {
					return m, m.input.Focus()
				}
				m.input.Blur()
				return m, nil
			}
			if k == "enter" && m.editAction != 0 {
				switch m.editAction {
				case 1:
					if m.key == downloadDirEnvVar {
						k = "ctrl+b"
					} else if m.key == zlib.EnvProxy || m.key == wereadProxyEnv {
						k = "ctrl+t"
					} else {
						return m, nil
					}
				case 2:
					k = "ctrl+r"
				case 3:
					if m.input.Value() != m.original {
						m.discard = true
						m.discardFocused = false
					} else {
						m.back()
					}
					return m, nil
				}
			}
			switch k {
			case "enter":
				m.save(m.input.Value())
				return m, nil
			case "ctrl+r":
				m.input.SetValue("")
				m.message = "Default selected; Enter saves"
				return m, nil
			case "ctrl+b":
				if m.key == downloadDirEnvVar {
					p := m.input.Value()
					if p == "" {
						p, _ = os.Getwd()
					}
					m.browse(p)
				}
				return m, nil
			case "ctrl+t":
				if m.key == wereadProxyEnv {
					v, err := validateSetting(m.key, m.input.Value())
					if err != nil {
						m.message = err.Error()
						return m, nil
					}
					return m, m.testWeRead(v)
				}
				if m.key == zlib.EnvProxy {
					v, err := validateSetting(m.key, m.input.Value())
					if err != nil {
						m.message = err.Error()
						return m, nil
					}
					if v == "" {
						m.message = "Enter a proxy URL to test"
						return m, nil
					}
					return m, m.probe(v, "edit")
				}
				return m, nil
			}
			if m.editAction != 0 {
				return m, nil
			}
			var cmd tea.Cmd
			before := m.input.Value()
			m.input, cmd = m.input.Update(msg)
			if m.busy && m.input.Value() != before {
				if m.cancel != nil {
					m.cancel()
				}
				m.generation++
				m.busy = false
				m.message = "Proxy changed; test again"
			}
			return m, cmd
		}
		if k == "q" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		rows := m.rows()
		count := len(rows)
		switch k {
		case "up", "k", "shift+tab":
			m.cursor = max(0, m.cursor-1)
		case "down", "j", "tab":
			m.cursor = min(max(0, count-1), m.cursor+1)
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = max(0, count-1)
		case "pgdown":
			m.cursor = min(max(0, count-1), m.cursor+max(1, m.height-10))
		case "pgup":
			m.cursor = max(0, m.cursor-max(1, m.height-10))
		case "space":
			if m.page == "browse" {
				m.input.SetValue(m.directory)
				m.page = "edit"
				m.editAction = 0
				return m, m.input.Focus()
			}
		case "enter":
			if m.busy {
				return m, nil
			}
			switch m.page {
			case "home", "network", "download", "kindle", "weread":
				keys := settingsPageKeys(m.page)
				if m.cursor >= len(keys) {
					return m, nil
				}
				switch key := keys[m.cursor]; key {
				case zlib.EnvDomain:
					m.returnPage = m.page
					m.returnCursor = m.cursor
					return m, m.openDomains()
				case wereadAccountKey:
					return m, m.openWeReadAccount()
				default:
					return m, m.edit(key)
				}
			case "weread-account":
				return m.runWeReadAccountAction(rows[m.cursor])
			case "browse":
				if count > 0 {
					m.browse(filepath.Join(m.directory, rows[m.cursor]))
				}
			}
		}
		return m, nil
	}
	if m.page == "edit" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m settingsModel) settingRows(width, available int) []string {
	keys := settingsPageKeys(m.page)
	labelWidth := min(18, max(12, width-14))
	valueWidth := max(1, width-labelWidth-4)
	section := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	lines := make([]string, 0, len(keys)+2*len(settingsSections))
	selected := 0
	sectionStarts := map[string]string{}
	for _, s := range settingsSections {
		sectionStarts[s.keys[0]] = s.title
	}
	for i, key := range keys {
		if group, ok := sectionStarts[key]; ok && m.page == "home" {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, section.Render("  "+group))
		}
		label := settingLabels[key]
		v := m.value(key)
		value := displaySetting(v.Key, v.Value)
		if value == "" {
			if v.Key == kindleToKey || v.Key == kindleFromKey {
				value = "Add address"
			} else {
				value = "Automatic"
			}
		}
		name := ansi.Truncate(label, labelWidth, "…")
		row := name + strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(name))+2)
		row += ansi.Truncate(value, valueWidth, "…")
		if i == m.cursor {
			selected = len(lines)
		}
		lines = append(lines, styledListRow(row, i == m.cursor, width, currentTheme))
	}
	if len(lines) <= available {
		return lines
	}
	from := max(0, selected-available+1)
	return lines[from:min(len(lines), from+available)]
}

func (m settingsModel) selectedSetting() settingValue {
	keys := settingsPageKeys(m.page)
	if m.cursor >= 0 && m.cursor < len(keys) {
		return m.value(keys[m.cursor])
	}
	return settingValue{}
}

func (m settingsModel) settingsListView(width, height int, title string) tea.View {
	if height < 7 {
		return fullScreenView(width, height, title+"\nEnlarge terminal")
	}
	helpModel := help.New()
	helpModel.SetWidth(width)
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	enter := "edit"
	if key := m.selectedSetting().Key; key == wereadAccountKey || key == zlib.EnvDomain {
		enter = "open"
	}
	footer := helpModel.ShortHelpView([]keys.Binding{uiKey("esc", "back"), uiKey("enter", enter), uiKey("↑/↓", "select")})
	lines := []string{lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render(title), ""}
	reserved := 1
	if m.message != "" || m.selectedSetting().Override != "" {
		reserved = 2
	}
	lines = append(lines, m.settingRows(width, max(1, height-3-reserved))...)
	if m.message != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(m.message))
	} else if override := m.selectedSetting().Override; override != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(currentTheme.Muted).Render("Using "+override))
	}
	lines = append(lines, "", footer)
	return fullScreenView(width, height, strings.Join(lines, "\n"))
}

func settingsCompactView(width, height int, title, subtitle string, lines []string, status string, controls []keys.Binding) tea.View {
	if height < 7 {
		return fullScreenView(width, height, title+"\nEnlarge terminal")
	}
	helpModel := help.New()
	helpModel.SetWidth(width)
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	content := []string{lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render(title)}
	if subtitle != "" {
		content = append(content, lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(subtitle))
	}
	content = append(content, "")
	content = append(content, lines...)
	if status != "" {
		content = append(content, "", lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(status))
	}
	content = append(content, "", helpModel.ShortHelpView(controls))
	return fullScreenView(width, height, strings.Join(content, "\n"))
}

func (m settingsModel) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	if m.page == "formats" {
		return m.formatsView(w, h)
	}
	if m.page == "domain" || m.page == "mirror" {
		return m.domainView(w, h)
	}
	if m.page == "weread-login" && m.wereadLogin != nil {
		return m.wereadLogin.View()
	}
	if m.page == "weread-account" && !m.busy {
		return m.wereadAccountView(w, h)
	}
	titles := map[string]string{"home": "Settings", "network": "Settings / Network", "download": "Settings / Downloads", "search": "Settings / Search", "kindle": "Settings / Kindle", "weread": "Settings / WeRead", "weread-account": "Settings / WeRead account", "browse": "Settings / Choose directory", "edit": "Settings / Edit"}
	if m.busy {
		return screenView(w, h, titles[m.page], "", m.spinner.View(), "", uiKey("esc", "back"))
	}
	subtitle := ""
	status := m.message
	controls := []keys.Binding{uiKey("esc", "back"), uiKey("enter", "open"), uiKey("↑/↓", "select"), uiKey("q", "quit")}
	var lines []string
	if isSettingsListPage(m.page) {
		return m.settingsListView(w, h, titles[m.page])
	} else if m.page == "edit" {
		v := m.value(m.key)
		labels := map[string]string{zlib.EnvProxy: "Proxy URL", downloadDirEnvVar: "Download directory", zlib.EnvRetries: "Download retries", kindleToKey: "Kindle address", kindleFromKey: "From address", kindleHostKey: "SMTP host", kindlePortKey: "SMTP port", kindleSecurityKey: "SMTP security", wereadProxyEnv: "WeRead proxy"}
		if v.Override != "" {
			subtitle = "Using " + v.Override
		}
		lines = append(lines, labels[m.key], m.input.View(), "")
		if m.key == kindleSecurityKey {
			lines = append(lines, "tls: SSL/TLS on connect (port 465)", "starttls: upgrade after connect (port 587)", "Empty: follow the port", "")
		}
		if isKindleSetting(m.key) {
			lines = append(lines, "SMTP password: ZLIB_SMTP_PWD", "")
		}
		if m.key == wereadProxyEnv {
			lines = append(lines, "Empty: system proxy (HTTPS_PROXY)", "direct: connect without any proxy", "Proxy URL: http, https, socks5 or socks5h", "The Z-Library proxy (ZLIB_PROXY) never applies", "")
		}
		actions := []string{"", "Reset", "Cancel", "Save"}
		if m.key == downloadDirEnvVar {
			actions[0] = "Browse"
		} else if m.key == zlib.EnvProxy || m.key == wereadProxyEnv {
			actions[0] = "Test"
		}
		lines = append(lines, actionButtonRows(actions, m.editAction, w)...)
		controls = []keys.Binding{uiKey("esc", "cancel"), uiKey("enter", "save"), uiKey("tab", "actions")}
		if m.key == downloadDirEnvVar {
			controls = append(controls, uiKey("ctrl+b", "browse"))
		}
		if m.key == zlib.EnvProxy || m.key == wereadProxyEnv {
			controls = append(controls, uiKey("ctrl+t", "test"))
		}
	} else {
		rows := m.rows()
		values := make([]string, len(rows))
		labelNeed, valueNeed := 0, 0
		for i, label := range rows {
			switch m.page {
			case "network":
				values[i] = displaySetting(m.values[i].Key, m.values[i].Value)
			case "download":
				values[i] = displaySetting(m.values[i+2].Key, m.values[i+2].Value)
			}
			labelNeed = max(labelNeed, ansi.StringWidth(bookCell(label)))
			valueNeed = max(valueNeed, ansi.StringWidth(bookCell(values[i])))
		}
		// The shared allocator reserves a leading column gap; compensate here
		// because settings have no numeric column.
		lw, vw, gap := bookColumnWidths(w+3, 0, labelNeed, valueNeed)
		if gap == 2 {
			lw, vw, gap = bookColumnWidths(w+2, 0, labelNeed, valueNeed)
		}
		visible := max(1, h-10)
		start := max(0, m.cursor-visible+1)
		for i := start; i < min(len(rows), start+visible); i++ {
			label := rows[i]
			if values[i] != "" && vw > 0 {
				label = ansi.Truncate(bookCell(label), lw, "…")
				label += strings.Repeat(" ", max(0, lw-ansi.StringWidth(label))+gap) + ansi.Truncate(bookCell(values[i]), vw, "…")
			}
			lines = append(lines, styledListRow(label, i == m.cursor, w, currentTheme))
		}
		if m.page == "browse" {
			subtitle = bookCell(m.directory)
			controls = []keys.Binding{uiKey("esc", "back"), uiKey("space", "use folder"), uiKey("enter", "open")}
		}
		if m.page == "network" || m.page == "download" {
			i := m.cursor
			if m.page == "download" {
				i += 2
			}
			v := m.values[i]
			lines = append(lines, "", "Source: "+v.Source, "Saved: "+displaySetting(v.Key, v.Saved))
		}
	}
	v := screenView(w, h, titles[m.page], subtitle, strings.Join(lines, "\n"), status, controls...)
	if m.page == "edit" {
		v = settingsCompactView(w, h, titles[m.page], subtitle, lines, status, controls)
	}
	if m.discard {
		v.Content = confirmationDialog(v.Content, w, h, "Discard changes?", m.key, "", "Unsaved changes will be lost.", "Discard", m.discardFocused, true)
	}
	return v
}
