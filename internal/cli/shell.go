package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the terminal UI",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isInteractive() {
			return fmt.Errorf("zlib tui requires an interactive terminal")
		}
		return runShell()
	},
}

type shellEvent struct {
	generation int
	msg        tea.Msg
}
type shellText struct {
	text string
	err  error
}
type shellNoticeExpired struct{ version uint64 }
type shellModel struct {
	input                                                           textinput.Model
	page                                                            tea.Model
	previous                                                        tea.Model
	kind, status, pending, paletteDraft, infoTitle                  string
	width, height, generation, selection, paletteOffset, infoOffset int
	typing                                                          bool
	signedIn                                                        bool
	download                                                        downloadModel
	// deliveryReturn is the download completion page a Kindle or WeRead
	// delivery returns to.
	deliveryReturn  *downloadModel
	kindlePath      string
	wereadPath      string // file waiting for WeRead import, kept across login
	loginForm       *inlineForm
	loginCancel     context.CancelFunc
	spinner         spinner.Model
	noticeVersion   uint64
	noticeTone      noticeTone
	standalone      bool
	standaloneTitle string // info title when a standalone command ends with a message
	commandErr      error
	// resume is the command to rerun once the domain page closes, after the
	// user chose to change domain from a connection error.
	resume string
}

const shellNoticeTTL = 5 * time.Second

// noticeTone sets how shell feedback looks and how long it stays.
type noticeTone int

const (
	noticeInfo    noticeTone = iota // neutral guidance; expires
	noticeSuccess                   // ✓ in the success color; expires
	noticeError                     // ✗ in the error color; stays until the next key
)

// setNotice shows neutral feedback, such as a hint, for shellNoticeTTL.
func (m *shellModel) setNotice(message string) { m.notify(noticeInfo, message) }

// setSuccess confirms a finished action for shellNoticeTTL.
func (m *shellModel) setSuccess(message string) { m.notify(noticeSuccess, message) }

// setError reports a failure. It does not expire: a timer could clear it
// before it was read, so the next key press dismisses it instead.
func (m *shellModel) setError(message string) { m.notify(noticeError, message) }

func (m *shellModel) notify(tone noticeTone, message string) {
	m.status = message
	m.noticeTone = tone
	m.noticeVersion++
}

type shellCommand struct{ name, description string }

var shellCommands = []shellCommand{
	{"search", "Search books with filters"},
	{"favorites", "Show favorite books"},
	{"recommended", "Show recommended books"},
	{"most-popular", "Show most popular books"},
	{"history", "Show download history"},
	{"profile", "Show account profile and download quota"},
	{"config", "View or edit network, download, Kindle and WeRead settings"},
	{"theme", "Choose a color theme"},
	{"doctor", "Find working Z-Library domains"},
	{"login", "Log in with email and password"},
	{"logout", "Delete the saved session"},
	{"kindle", "Send a file to Kindle"},
	{"weread", "Import a file into WeRead"},
	{"quit", "Exit zlib"},
}

func newShellModel() shellModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Filter commands"
	input.Focus()
	session, _ := config.LoadSession()
	return shellModel{input: input, width: 80, height: 24, typing: true, signedIn: len(session.Cookies) > 0}
}
func newShellStartupModel() shellModel {
	m := newShellModel()
	if !m.signedIn {
		m.dispatch("/login")
	}
	return m
}
func runShell() error { _, err := runCoverProgram(newShellStartupModel()); return err }
func (m shellModel) Init() tea.Cmd {
	if m.page != nil {
		return shellCmd(m.generation, m.page.Init())
	}
	return textinput.Blink
}

// Adapt existing standalone pages without allowing their QuitMsg to end the shell.
// Every async response is scoped to the page instance that started it.
func shellCmd(generation int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			wrapped := make([]tea.Cmd, len(batch))
			for i, c := range batch {
				wrapped[i] = shellCmd(generation, c)
			}
			return tea.Batch(wrapped...)()
		}
		return shellEvent{generation, msg}
	}
}
func (m *shellModel) open(kind string, page tea.Model) tea.Cmd {
	if doctor, ok := m.page.(doctorPageModel); ok {
		doctor.stop()
	}
	if m.kind == "list" && m.page != nil && m.previous == nil {
		m.previous = m.page
	}
	if kind == "list" {
		if list, ok := page.(searchSelectModel); ok {
			list.inShell = true
			page = list
		} else if list, ok := page.(historySelectModel); ok {
			list.inShell = true
			page = list
		}
	}
	if profile, ok := page.(profilePageModel); ok {
		profile.inShell = true
		page = profile
	}
	if kind != "config" {
		m.resume = ""
	}
	if kind == "download" {
		if download, ok := page.(downloadModel); ok {
			download.stayOnComplete = true
			page = download
		}
	}
	m.generation++
	m.kind = kind
	m.page = page
	m.typing = false
	m.input.Blur()
	m.status = ""
	page, _ = page.Update(tea.WindowSizeMsg{Width: m.width, Height: m.contentHeight()})
	m.page = page
	return shellCmd(m.generation, page.Init())
}
func (m *shellModel) home() {
	if doctor, ok := m.page.(doctorPageModel); ok {
		doctor.stop()
	}
	m.generation++
	m.page = nil
	m.kind = ""
	m.infoTitle = ""
	m.infoOffset = 0
	m.typing = true
	m.paletteOffset = 0
	m.input.Focus()
}

func (m *shellModel) openInfo(title, content string) {
	if m.kind == "list" && m.page != nil {
		m.previous = m.page
	}
	m.home()
	m.kind = "info"
	m.typing = false
	m.input.Blur()
	m.infoTitle = title
	m.status = content
}
func (m *shellModel) restore() {
	if doctor, ok := m.page.(doctorPageModel); ok {
		doctor.stop()
	}
	old := m.previous
	m.previous = nil
	if old == nil {
		m.home()
		return
	}
	m.generation++
	m.page = old
	m.kind = "list"
	m.typing = false
	m.resizePage()
}

// returnToDownloadCompletion reopens the saved-file page with a delivery
// outcome in the given tone.
func (m *shellModel) returnToDownloadCompletion(tone noticeTone, message string) tea.Cmd {
	page := *m.deliveryReturn
	m.deliveryReturn = nil
	page.completeAction = 0
	page.sendToKindle = false
	page.sendToWeRead = false
	page.completeMessage, page.completeTone = message, tone
	return m.open("download", page)
}

func (m shellModel) contentHeight() int {
	return max(1, m.height-m.footerHeight())
}

func (m shellModel) homeIdle() bool {
	return m.page == nil && m.kind == "" && !strings.HasPrefix(m.input.Value(), "/")
}

func (m shellModel) footerHeight() int {
	if m.homeIdle() {
		return 1
	}
	if m.paletteOpen() {
		return 1
	}
	if m.typing {
		return 2
	}
	if m.page != nil && m.kind != "list" {
		return 0
	}
	return 1
}

func (m *shellModel) resizePage() {
	if m.page != nil {
		m.page, _ = m.page.Update(tea.WindowSizeMsg{Width: m.width, Height: m.contentHeight()})
	}
}

func (m shellModel) matches() []shellCommand {
	value, ok := strings.CutPrefix(m.input.Value(), "/")
	if !ok {
		return nil
	}
	if strings.Contains(value, " ") {
		return nil
	}
	var out []shellCommand
	for _, command := range shellCommands {
		if strings.HasPrefix(command.name, value) {
			out = append(out, command)
		}
	}
	if value != "" {
		for _, command := range shellCommands {
			if !strings.HasPrefix(command.name, value) && strings.Contains(command.name, value) {
				out = append(out, command)
			}
		}
	}
	return out
}
func (m shellModel) childInputActive() bool {
	switch page := m.page.(type) {
	case searchSelectModel:
		return page.state == searchStateFilter || page.confirmDownload
	case historySelectModel:
		return page.confirmDownload || page.state == historyStateConfirmDelete
	default:
		return false
	}
}
func (m shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if expired, ok := msg.(shellNoticeExpired); ok {
		if expired.version == m.noticeVersion && m.kind != "info" && m.noticeTone != noticeError {
			m.status = ""
		}
		return m, nil
	}
	if _, ok := msg.(tea.KeyPressMsg); ok && m.noticeTone == noticeError && m.status != "" && m.kind != "info" {
		m.status = ""
	}
	before := m.noticeVersion
	next, cmd := m.update(msg)
	m = next.(shellModel)
	if m.standalone && m.page == nil && m.kind == "" {
		if m.status != "" {
			m.openInfo(m.standaloneTitle, m.status)
			return m, nil
		}
		return m, tea.Quit
	}
	if m.noticeVersion != before && m.status != "" && m.kind != "info" && m.noticeTone != noticeError {
		version := m.noticeVersion
		expire := tea.Tick(shellNoticeTTL, func(time.Time) tea.Msg {
			return shellNoticeExpired{version: version}
		})
		return m, tea.Batch(cmd, expire)
	}
	return m, cmd
}

func (m shellModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(spinner.TickMsg); ok && m.kind == "login-wait" {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(tick)
		return m, cmd
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.input.SetWidth(max(1, m.width-4))
		m.resizePage()
		m.paletteOffset = min(m.paletteOffset, max(0, len(m.matches())-m.paletteVisibleRows(m.height)))
		return m, nil
	}
	if event, ok := msg.(shellEvent); ok {
		if event.generation != m.generation {
			return m, nil
		}
		if _, ok := event.msg.(tea.QuitMsg); ok {
			cmd := m.completed()
			return m, cmd
		}
		if result, ok := event.msg.(shellText); ok {
			loginFinished := m.kind == "login-wait"
			if loginFinished {
				m.stopLogin()
				if result.err != nil {
					if m.loginForm != nil {
						return m, m.reopenLoginForm(result.err.Error())
					}
					m.home()
					m.setError(result.err.Error())
					return m, nil
				}
				m.loginForm = nil
				m.signedIn = true
				m.previous = nil
				pending := m.pending
				m.pending = ""
				m.home()
				m.status = ""
				if pending != "" {
					m.input.SetValue(pending)
					m.input.CursorEnd()
					m.setNotice("Press Enter to continue.")
				}
				return m, nil
			}
			if result.err != nil {
				m.status = result.err.Error()
			} else {
				m.status = result.text
			}
			m.kind = "info"
			return m, nil
		}
		if m.page != nil {
			var cmd tea.Cmd
			m.page, cmd = m.page.Update(event.msg)
			return m, shellCmd(m.generation, cmd)
		}
		return m, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		// While signing in only cancel is meaningful: other keys used to open
		// an invisible command palette behind the spinner.
		if m.kind == "login-wait" {
			if s := k.String(); s == "esc" || s == "ctrl+c" {
				m.stopLogin()
				m.generation++ // drop the late result of the abandoned login
				if m.loginForm != nil {
					return m, m.reopenLoginForm("Login cancelled")
				}
				m.home()
			}
			return m, nil
		}
		if m.homeIdle() {
			switch k.String() {
			case "/":
				m.typing = true
				m.input.SetValue("/")
				m.selection, m.paletteOffset = 0, 0
				m.status = ""
				return m, m.input.Focus()
			case "esc", "ctrl+c":
			case "enter":
				if m.signedIn {
					return m, nil
				}
			default:
				return m, nil
			}
		}
		if m.typing {
			switch k.String() {
			case "esc":
				if m.paletteDraft != "" {
					m.input.SetValue(m.paletteDraft)
					m.input.CursorEnd()
					m.paletteDraft = ""
					m.selection = 0
					m.paletteOffset = 0
					return m, nil
				}
				m.input.SetValue("")
				m.selection = 0
				m.paletteOffset = 0
				if m.page != nil {
					m.typing = false
					m.input.Blur()
					m.resizePage()
				} else if m.previous != nil {
					m.restore()
				}
				return m, nil
			case "ctrl+c":
				if m.input.Value() != "" {
					m.input.SetValue("")
					return m, nil
				}
				if m.page == nil {
					return m, tea.Quit
				}
				m.typing = false
				m.resizePage()
				return m, nil
			case "ctrl+k":
				if !strings.HasPrefix(m.input.Value(), "/") {
					m.paletteDraft = m.input.Value()
				}
				m.input.SetValue("/")
				m.selection = 0
				m.paletteOffset = 0
				m.status = ""
				return m, nil
			case "up", "down":
				items := m.matches()
				if len(items) > 0 {
					m.selection = min(max(0, m.selection), len(items)-1)
					if k.String() == "up" {
						m.selection = (m.selection - 1 + len(items)) % len(items)
					} else {
						m.selection = (m.selection + 1) % len(items)
					}
					m.ensurePaletteVisible(len(items))
				}
				return m, nil
			case "tab":
				items := m.matches()
				if len(items) > 0 {
					m.input.SetValue("/" + items[min(m.selection, len(items)-1)].name + " ")
					m.input.CursorEnd()
					m.resizePage()
				}
				return m, nil
			case "enter":
				value := strings.TrimSpace(m.input.Value())
				if value == "" && !m.signedIn && m.page == nil && m.kind == "" {
					return m, m.dispatch("/login")
				}
				if strings.HasPrefix(value, "/") && !strings.Contains(value, " ") {
					items := m.matches()
					if len(items) > 0 && !slices.ContainsFunc(shellCommands, func(command shellCommand) bool { return command.name == strings.TrimPrefix(value, "/") }) {
						value = "/" + items[min(m.selection, len(items)-1)].name
					}
				}
				cmd := m.dispatch(value)
				return m, cmd
			}
		} else if m.kind == "info" {
			if k.String() == "esc" || k.String() == "q" || k.String() == "ctrl+c" {
				m.restore()
				m.status = ""
				return m, nil
			}
			lines := strings.Split(m.status, "\n")
			visible := max(1, m.height-6)
			switch k.String() {
			case "up":
				m.infoOffset--
			case "down":
				m.infoOffset++
			case "pgup":
				m.infoOffset -= visible
			case "pgdown":
				m.infoOffset += visible
			case "home":
				m.infoOffset = 0
			case "end":
				m.infoOffset = len(lines)
			}
			m.infoOffset = min(max(0, m.infoOffset), max(0, len(lines)-visible))
		}
		if !m.typing && (m.kind == "list" || m.kind == "info" || m.page == nil) && !m.childInputActive() {
			if !m.standalone && (k.String() == "/" || k.String() == "ctrl+k") {
				m.typing = true
				m.input.SetValue("/")
				m.selection = 0
				m.status = ""
				m.resizePage()
				return m, m.input.Focus()
			}
			if k.String() == "ctrl+f" && m.canRefine() {
				msg = tea.KeyPressMsg{Code: '/', Text: "/"}
			}
		}
	}
	if m.typing {
		if _, pasted := msg.(tea.PasteMsg); pasted && m.homeIdle() {
			return m, nil
		}
		var cmd tea.Cmd
		before := m.input.Value()
		wasPalette := m.paletteOpen()
		m.input, cmd = m.input.Update(msg)
		if m.homeIdle() {
			m.input.SetValue("")
		}
		if m.input.Value() != before {
			m.selection = 0
			m.paletteOffset = 0
			m.status = ""
			if wasPalette != m.paletteOpen() {
				m.resizePage()
			}
		}
		return m, cmd
	}
	if m.page != nil {
		var cmd tea.Cmd
		m.page, cmd = m.page.Update(msg)
		return m, shellCmd(m.generation, cmd)
	}
	return m, nil
}

func (m *shellModel) dispatch(value string) tea.Cmd {
	if value == "" {
		return nil
	}
	name, arg := "search", value
	if strings.HasPrefix(value, "/") {
		name, arg, _ = strings.Cut(strings.TrimPrefix(value, "/"), " ")
		arg = strings.TrimSpace(arg)
	}
	known := false
	for _, command := range shellCommands {
		if command.name == name {
			known = true
		}
	}
	if !known {
		m.setError("Unknown command: /" + name)
		return nil
	}
	if name == "quit" {
		if doctor, ok := m.page.(doctorPageModel); ok {
			doctor.stop()
		}
		return tea.Quit
	}
	m.input.SetValue("")
	m.selection = 0
	m.paletteOffset = 0
	m.paletteDraft = ""
	switch name {
	case "login":
		m.loginForm = nil
		form := newInlineForm("Log in", "Log in", []formField{{label: "Email", value: rememberedLoginEmail()}, {label: "Password", secret: true}})
		form.validate = func(fields []formField) error {
			if err := requiredField("email")(fields[0].value); err != nil {
				return err
			}
			return requiredField("password")(fields[1].value)
		}
		return m.open("login", form)
	case "logout":
		if !m.signedIn {
			removed, err := clearSavedSession()
			if err != nil {
				m.setError("Could not log out: " + err.Error())
			} else if removed {
				m.setSuccess("Logged out")
			} else {
				m.setNotice("Not logged in")
			}
			return nil
		}
		return m.open("logout", shellConfirm{title: "Log out?", detail: "Clear the saved session", action: "Log out"})
	case "theme":
		return m.open("theme", newThemePicker(configuredThemeName(), themeOverride()))
	case "config":
		store, err := newSettingsStore()
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		page := arg
		if page == "" {
			page = "home"
		}
		if page != "home" && page != "network" && page != "download" && page != "kindle" && page != "weread" && page != "domain" {
			m.setError("Use /config network, /config domain, /config download, /config kindle or /config weread")
			return nil
		}
		model, err := newSettingsModel(context.Background(), store, page)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.open("config", model)
	case "doctor":
		return m.open("doctor", newDoctorPageModel(zlib.KnownDomains(), os.Getenv(zlib.EnvProxy), doctorCurrentDomain(), nil))
	case "kindle":
		return m.openKindle(arg)
	case "weread":
		return m.openWeRead(arg)
	}
	c, err := loadClient()
	if err != nil {
		m.pending = value
		m.home()
		if m.signedIn {
			m.setError(shellErrorText(err))
		} else {
			m.setNotice("Sign in to continue. Press Enter to sign in.")
		}
		return nil
	}
	switch name {
	case "search":
		if arg == "" {
			return m.open("search-form", newSearchFilterForm("", nil))
		}
		return m.open("list", newSearchSelectModel(arg, c, nil, false))
	case "favorites":
		return m.open("list", newSavedBooksSelectModel(c.SavedBooks).withBookDetails(c))
	case "recommended":
		return m.open("list", newBookFeedSelectModel("Recommended", c.RecommendedBooks).withBookDetails(c))
	case "most-popular":
		return m.open("list", newBookFeedSelectModel("Most popular", c.MostPopularBooks).withBookDetails(c))
	case "history":
		return m.open("list", newHistorySelectModel(c))
	case "profile":
		return m.open("profile", newProfilePageModel(c))
	}
	return nil
}

func (m *shellModel) chooseDirectory() tea.Cmd {
	dir := os.Getenv(downloadDirEnvVar)
	if dir == "" {
		dir = "."
	}
	return m.open("directory", newDownloadDirectoryModel(dir))
}

// switchDomain opens the domain page for a page that hit a connection error.
// When it closes, completed reruns resume so the user lands back where they
// were, now talking to the chosen domain.
func (m *shellModel) switchDomain(resume string) tea.Cmd {
	store, err := newSettingsStore()
	if err != nil {
		m.setError(err.Error())
		m.restore()
		return nil
	}
	model, err := newSettingsModel(context.Background(), store, "domain")
	if err != nil {
		m.setError(err.Error())
		m.restore()
		return nil
	}
	cmd := m.open("config", model)
	// open saved the failed page as previous. It is flagged switchDomain, so
	// restoring it later would reopen this page and loop; the resumed command
	// replaces it instead.
	m.previous = nil
	m.resume = resume
	return cmd
}

func (m *shellModel) completed() tea.Cmd {
	if page, ok := m.page.(domainSwitcher); ok {
		if resume, wanted := page.domainSwitchRequest(); wanted {
			return m.switchDomain(resume)
		}
	}
	if m.kind == "config" && m.resume != "" {
		resume := m.resume
		m.resume, m.previous = "", nil
		m.home()
		return m.dispatch(resume)
	}
	switch page := m.page.(type) {
	case searchFilterForm:
		if !page.accepted {
			m.restore()
			return nil
		}
		c, err := loadClient()
		if err != nil {
			m.pending = "/search " + page.query
			m.home()
			m.setError(shellErrorText(err))
			return nil
		}
		return m.open("list", newSearchSelectModel(page.query, c, page.searchOptions(), false))
	case searchSelectModel:
		if page.selectedID != "" {
			id := page.selectedID
			page.selectedID = ""
			page.confirmDownload = false
			m.previous = page
			client := page.client
			if client == nil {
				var err error
				client, err = loadClient()
				if err != nil {
					m.setError(shellErrorText(err))
					m.restore()
					return nil
				}
			}
			m.download = newDownloadModel(id, "", client)
			return m.chooseDirectory()
		}
	case historySelectModel:
		if page.selected != nil {
			item := *page.selected
			page.selected = nil
			page.confirmDownload = false
			m.previous = page
			m.download = newDownloadModel(historyCommandID(item), "", page.client)
			return m.chooseDirectory()
		}
	case downloadDirectoryModel:
		if page.accepted {
			m.download.dir = page.dir
			return m.open("download", m.download)
		}
		m.restore()
		return nil
	case downloadModel:
		if page.err != nil {
			book := page.bookName
			if book == "" {
				book = page.id
			}
			title, detail := "Download failed", downloadFailureDetail(page.err)
			if f, ok := describeNetworkError(page.err); ok {
				title = f.title()
				detail = f.reason + " Press r to retry, or esc and use /config domain to pick another domain."
			}
			return m.open("download-error", shellErrorModel{title: title, subtitle: bookCell(book), detail: detail, retry: true})
		} else if page.state == stateCancelled {
			m.deliveryReturn = nil
			m.restore()
			m.setNotice("Download cancelled")
		} else if page.sendToKindle {
			page.sendToKindle = false
			page.completeAction = 0
			m.deliveryReturn = new(page)
			m.kindlePath = page.savedPath
			if !slices.Contains(kindleAllowedTypes, strings.ToLower(filepath.Ext(page.savedPath))) {
				return m.returnToDownloadCompletion(noticeError, "Kindle does not support this file type")
			}
			return m.open("kindle-confirm", shellConfirm{title: "Send to Kindle?", detail: filepath.Base(page.savedPath), action: "Send"})
		} else if page.sendToWeRead {
			page.sendToWeRead = false
			page.completeAction = 0
			m.deliveryReturn = new(page)
			confirm, err := wereadConfirmPage(page.savedPath)
			if err != nil {
				return m.returnToDownloadCompletion(noticeError, err.Error())
			}
			m.wereadPath = page.savedPath
			return m.open("weread-confirm", confirm)
		} else {
			m.deliveryReturn = nil
			m.restore()
		}
		return nil
	case shellErrorModel:
		if page.selectedRetry && page.retry {
			m.download = newDownloadModel(m.download.id, m.download.dir, m.download.client)
			return m.open("download", m.download)
		}
		if m.deliveryReturn != nil {
			if m.kind == "weread-error" {
				return m.returnToDownloadCompletion(noticeError, "WeRead import failed")
			}
			return m.returnToDownloadCompletion(noticeError, "Kindle send failed")
		}
		m.restore()
		return nil
	case themePickerModel:
		if page.accepted {
			cfg, _ := config.LoadConfig()
			cfg.Theme = page.selected()
			if err := config.SaveConfig(cfg); err != nil {
				m.setError(err.Error())
			} else {
				m.setSuccess("Theme saved: " + cfg.Theme)
			}
			resolveTheme()
		}
	case inlineForm:
		if !page.accepted {
			if m.kind == "kindle-password" && m.deliveryReturn != nil {
				return m.returnToDownloadCompletion(noticeInfo, "")
			}
			m.restore()
			return nil
		}
		switch m.kind {
		case "login":
			fields := page.fields
			m.loginForm = new(page)
			m.home()
			m.kind = "login-wait"
			m.typing = false
			m.status = ""
			m.spinner = newLoadingSpinner()
			ctx, cancel := context.WithCancel(context.Background())
			m.loginCancel = cancel
			return tea.Batch(m.spinner.Tick, shellCmd(m.generation, func() tea.Msg { return shellLogin(ctx, fields) }))
		case "kindle-config":
			return m.saveKindle(page.fields)
		case "kindle-password":
			if err := saveKindleSMTPPassword(page.fields[0].value); err != nil {
				page.accepted = false
				page.message = "Could not save SMTP password: " + err.Error()
				page.focus(0)
				m.page = page
				return nil
			}
			return m.sendKindle(m.kindlePath)
		}
	case sendFileModel:
		if page.accepted {
			if m.kind == "weread-file" {
				return m.importWeRead(page.path)
			}
			return m.sendKindle(page.path)
		}
	case wereadLoginModel:
		return m.wereadLoginDone(page)
	case wereadImportModel:
		return m.wereadImportDone(page)
	case kindleSendModel:
		if page.err != nil {
			m.commandErr = page.err
			return m.open("kindle-error", shellErrorModel{title: "Kindle send failed", subtitle: bookCell(page.filePath), detail: page.err.Error()})
		} else if m.deliveryReturn != nil {
			return m.returnToDownloadCompletion(noticeSuccess, "Email sent to Kindle")
		} else {
			path, _ := filepath.Abs(page.filePath)
			m.openInfo("Email sent to Kindle", "File\n"+bookCell(path)+"\n\nTo\n"+bookCell(page.cfg.To))
		}
		return nil
	case shellConfirm:
		if page.confirmed {
			switch m.kind {
			case "logout":
				_, err := clearSavedSession()
				if err != nil {
					m.restore()
					m.setError("Could not log out: " + err.Error())
					return nil
				}
				m.signedIn = false
				m.previous = nil
				m.pending = ""
				m.home()
				m.status = ""
				return m.dispatch("/login")
			case "kindle-confirm":
				return m.sendKindle(m.kindlePath)
			case "weread-confirm":
				return m.importWeRead(m.wereadPath)
			case "weread-logout":
				return m.logoutWeRead()
			}
		}
		if (m.kind == "kindle-confirm" || m.kind == "weread-confirm") && m.deliveryReturn != nil {
			return m.returnToDownloadCompletion(noticeInfo, "")
		}
	}
	m.restore()
	return nil
}

// canRefine reports whether Ctrl+F reopens search filters on the current page.
// Only search results have filters; Favorites, the feeds and History do not.
func (m shellModel) canRefine() bool {
	list, ok := m.page.(searchSelectModel)
	return ok && list.feed == nil && list.feedPage == nil
}

// stopLogin releases the context of an in-flight login, if any.
func (m *shellModel) stopLogin() {
	if m.loginCancel != nil {
		m.loginCancel()
		m.loginCancel = nil
	}
}

// reopenLoginForm returns to the login form with the email kept and the
// password cleared, showing message.
func (m *shellModel) reopenLoginForm(message string) tea.Cmd {
	form := *m.loginForm
	m.loginForm = nil
	form.accepted = false
	form.message = message
	form.fields[1].value = ""
	form.inputs[1].SetValue("")
	form.focus(1)
	return m.open("login", form)
}

func shellLogin(ctx context.Context, fields []formField) shellText {
	domain, err := resolveLoginDomain("")
	if err != nil {
		return shellText{err: err}
	}
	domain, err = resolveEAPIDomain(ctx, domain)
	if err != nil {
		return shellText{err: err}
	}
	c := zlib.NewClient()
	c.SetDomain(domain)
	if err = c.LoginEAPIContext(ctx, fields[0].value, fields[1].value); err != nil {
		return shellText{err: err}
	}
	// A login cancelled after the server answered must not sign in silently.
	if err = ctx.Err(); err != nil {
		return shellText{err: err}
	}
	err = config.SaveSession(config.Session{Cookies: c.Cookies(), Domain: c.Domain(), Mode: string(zlib.ClientModeEAPI)})
	if err == nil {
		_ = rememberLoginEmail(fields[0].value)
	}
	return shellText{text: "Logged in", err: err}
}

func (m shellModel) View() tea.View {
	if m.kind == "login-wait" {
		return screenView(m.width, m.height, "Log in", "", m.spinner.View()+" Signing in…", "", uiKey("esc", "cancel"))
	}
	if m.paletteOpen() {
		base := m.commandDialogView()
		if m.status != "" {
			return m.noticeView(base)
		}
		return base
	}
	if m.kind == "directory" {
		if directory, ok := m.page.(downloadDirectoryModel); ok {
			background := m.homeView(m.height)
			if m.previous != nil {
				background = m.previous.View().Content
			}
			return directory.dialogView(background)
		}
	}
	if m.kind == "info" && !m.typing {
		lines := strings.Split(safeText(m.status), "\n")
		visible := max(1, m.height-6)
		start := min(max(0, m.infoOffset), max(0, len(lines)-visible))
		end := min(len(lines), start+visible)
		status := ""
		if len(lines) > visible {
			status = fmt.Sprintf("%d–%d of %d", start+1, end, len(lines))
		}
		bindings := []keys.Binding{uiKey("↑/↓", "scroll")}
		if !m.standalone {
			bindings = append(bindings, uiKey("/", "commands"))
		}
		bindings = append(bindings, uiKey("esc", "back"))
		return screenView(m.width, m.height, m.infoTitle, "", strings.Join(lines[start:end], "\n"), status, bindings...)
	}
	available := m.contentHeight()
	contentRows := available
	var body string
	if m.page != nil {
		body = m.page.View().Content
		body = strings.ReplaceAll(body, "/ search", "Ctrl+F search")
	} else if m.kind == "info" || m.kind == "login-wait" {
		body = "zlib\n\n" + m.status
	} else {
		body = m.homeView(contentRows)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > contentRows {
		lines = lines[:contentRows]
	}
	for len(lines) < contentRows {
		lines = append(lines, "")
	}
	if m.typing && !m.homeIdle() {
		lines = append(lines, m.input.View())
	}
	if m.footerHeight() > 0 {
		lines = append(lines, m.footerView())
	}
	if len(lines) > m.height {
		lines = lines[:max(1, m.height)]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, m.width), "…")
	}
	base := fullScreenView(m.width, m.height, strings.Join(lines, "\n"))
	if m.status == "" || m.kind == "info" {
		return base
	}
	return m.noticeView(base)
}

func (m shellModel) noticeView(base tea.View) tea.View {
	width := max(1, m.width)
	message := strings.ReplaceAll(safeText(m.status), "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")
	style := lipgloss.NewStyle().Foreground(currentTheme.Title).Background(currentTheme.Surface).Padding(0, 1)
	switch m.noticeTone {
	case noticeSuccess:
		message = symbolSuccess + " " + message
		style = style.Foreground(currentTheme.Success)
	case noticeError:
		message = symbolError + " " + message
		style = style.Foreground(currentTheme.Error)
	}
	message = ansi.Truncate(message, max(1, width-6), "…")
	notice := style.Render(message)
	x := max(0, width-ansi.StringWidth(notice)-1)
	// Row 0 is the page title, left-aligned; row 1 is the list status line
	// (page and position), which feedback must not cover.
	y := 0
	return fullScreenView(m.width, m.height, lipgloss.NewCompositor(
		lipgloss.NewLayer(base.Content), lipgloss.NewLayer(notice).X(x).Y(y),
	).Render())
}

func (m shellModel) paletteVisibleRows(available int) int {
	if m.width >= 56 && available >= 18 {
		return min(8, max(1, (min(20, available-2)-9)/2))
	}
	return min(10, max(1, available-11))
}

func (m *shellModel) ensurePaletteVisible(count int) {
	rows := min(count, m.paletteVisibleRows(m.height))
	m.paletteOffset = min(max(0, m.paletteOffset), max(0, count-rows))
	if m.selection < m.paletteOffset {
		m.paletteOffset = m.selection
	} else if m.selection >= m.paletteOffset+rows {
		m.paletteOffset = m.selection - rows + 1
	}
}

func (m shellModel) footerView() string {
	h := help.New()
	h.SetWidth(max(1, m.width))
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	bindings := []keys.Binding{uiKey("esc", "back")}
	switch {
	case m.homeIdle():
		bindings = []keys.Binding{uiKey("/", "commands"), uiKey("ctrl+c", "quit")}
		if !m.signedIn {
			bindings = append([]keys.Binding{uiKey("enter", "sign in")}, bindings...)
		}
	case m.typing && strings.HasPrefix(m.input.Value(), "/"):
		bindings = []keys.Binding{uiKey("↑/↓", "cycle"), uiKey("tab", "complete"), uiKey("enter", "run"), uiKey("esc", "close")}
	case m.typing:
		if !m.signedIn && m.input.Value() == "" && m.page == nil && m.kind == "" {
			bindings = []keys.Binding{uiKey("enter", "sign in"), uiKey("/", "commands"), uiKey("esc", "clear")}
		} else {
			bindings = []keys.Binding{uiKey("enter", "search"), uiKey("/", "commands"), uiKey("esc", "clear")}
		}
	case m.kind == "list" && m.canRefine():
		bindings = []keys.Binding{uiKey("/", "commands"), uiKey("ctrl+f", "refine"), uiKey("esc", "back")}
	case m.kind == "list":
		bindings = []keys.Binding{uiKey("/", "commands"), uiKey("esc", "back")}
	case m.kind == "info" || m.page == nil && m.kind == "":
		bindings = []keys.Binding{uiKey("/", "commands"), uiKey("esc", "back")}
	}
	return h.ShortHelpView(bindings)
}
