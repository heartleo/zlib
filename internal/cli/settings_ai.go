package cli

import (
	"context"
	"strings"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
	"github.com/heartleo/zlib/internal/config"
)

// aiModelKey is the AI model row. Like WEREAD_ACCOUNT it is not an editable
// value: Enter opens the AI model page, and the settings live in ai.json, not
// in the environment.
const aiModelKey = "AI_MODEL"

// The AI settings file and the model clients; tests replace them to stay
// offline and out of the real config directory.
var (
	loadAISettings   = config.LoadAI
	saveAISettings   = config.SaveAI
	removeAISettings = config.RemoveAI
	newAIClient      = func(s aimatch.Settings) (*aimatch.Client, error) { return aimatch.NewClient(s, metaTransport()) }
	newAILister      = func(s aimatch.Settings) (*aimatch.Client, error) { return aimatch.NewLister(s, metaTransport()) }
)

// aiSummary is the model and whether the connection test still holds.
func aiSummary(s aimatch.Settings) string {
	if s.Verified() {
		return s.Model + " (verified)"
	}
	return s.Model + " (not verified)"
}

// aiSettingValue is the settings row for the AI model. It never holds the key.
func aiSettingValue() settingValue {
	v := settingValue{Key: aiModelKey, Value: "Not configured", Source: "Default"}
	s, err := loadAISettings()
	switch {
	case err != nil:
		v.Value, v.Source = "Saved settings are unreadable", "AI configuration"
	case s.Configured():
		v.Value, v.Saved, v.Source = aiSummary(s), s.Model, "AI configuration"
	}
	return v
}

type aiMode int

const (
	aiRows aiMode = iota
	aiEdit
	aiPick
	aiConfirmRemove
)

// aiRow is a row of the AI model page.
type aiRow int

const (
	aiRowProvider aiRow = iota
	aiRowProtocol
	aiRowURL
	aiRowKey
	aiRowModel
	aiRowSearch
	aiRowTest
	aiRowRemove
	aiRowCount
)

var aiRowLabels = [aiRowCount]string{"Provider", "Protocol", "Base URL", "API key", "Model", "Web search", "Test connection", "Remove settings"}

const aiCustomProvider = "Custom endpoint"

type (
	aiModelsMsg struct {
		gen int
		ids []string
		err error
	}
	aiTestMsg struct {
		gen int
		err error
	}
)

// aiPageModel is the AI model page: provider, endpoint, key, model, web search
// and the connection test. Every edit is saved at once; the saved verification
// goes stale by itself (Settings.Verified), so AI Match stays off until a test
// passes for exactly these settings.
type aiPageModel struct {
	ctx           context.Context
	s             aimatch.Settings
	mode          aiMode
	cursor        int
	input         textinput.Model
	editRow       aiRow
	resetKey      bool
	pickKind      aiRow
	pickAll       []string
	pickCursor    int
	spinner       spinner.Model
	busy          bool
	gen           int
	cancel        context.CancelFunc
	message       string
	width, height int
	done          bool
}

func newAIPage(ctx context.Context, width, height int) aiPageModel {
	s, err := loadAISettings()
	m := aiPageModel{ctx: ctx, s: s, width: width, height: height, spinner: newLoadingSpinner()}
	if err != nil {
		m.message = "Saved AI settings are unreadable: " + safeText(err.Error())
		m.s = aimatch.Settings{}
	}
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.Prompt = "> "
	styles := input.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Title)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Cursor.Color = currentTheme.Accent
	input.SetStyles(styles)
	m.input = input
	m.resize()
	return m
}

func (m *aiPageModel) resize() {
	m.input.SetWidth(max(1, min(56, m.width-ansi.StringWidth(m.input.Prompt)-4)))
}

// provider names the preset the settings match, else the custom endpoint.
func (m aiPageModel) provider() string {
	for _, p := range aimatch.Providers {
		if p.BaseURL == m.s.BaseURL && p.Protocol == m.s.Protocol {
			return p.Name
		}
	}
	if m.s.BaseURL == "" {
		return "Not set"
	}
	return aiCustomProvider
}

// save writes the settings; a failure is shown and the page goes on.
func (m *aiPageModel) save() {
	if err := saveAISettings(m.s); err != nil {
		m.message = "Could not save: " + safeText(err.Error())
	}
}

func (m *aiPageModel) stopWork() {
	m.gen++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.busy = false
}

func (m aiPageModel) update(msg tea.Msg) (aiPageModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case aiModelsMsg:
		if msg.gen != m.gen || m.mode != aiPick {
			return m, nil
		}
		m.busy, m.cancel = false, nil
		if msg.err != nil {
			m.message = "Could not list models (" + safeText(msg.err.Error()) + "). Type a model name to use it."
			return m, nil
		}
		m.pickAll, m.message = msg.ids, ""
	case aiTestMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.busy, m.cancel = false, nil
		if msg.err != nil {
			m.message = "✗ " + safeText(msg.err.Error())
			return m, nil
		}
		m.s.MarkVerified()
		m.save()
		if m.message == "" {
			m.message = "✓ The connection works. AI Match is available in the metadata lookup."
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	default:
		if m.mode == aiEdit || m.mode == aiPick {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg) // a paste
			return m, cmd
		}
	}
	return m, nil
}

func (m aiPageModel) key(msg tea.KeyPressMsg) (aiPageModel, tea.Cmd) {
	k := msg.String()
	if m.busy {
		if k == "esc" || k == "ctrl+c" {
			m.stopWork()
			if m.mode == aiPick {
				m.mode = aiRows
			}
			m.message = "Cancelled"
		}
		return m, nil
	}
	switch m.mode {
	case aiEdit:
		return m.editKey(msg, k)
	case aiPick:
		return m.pickKey(msg, k)
	case aiConfirmRemove:
		if k == "y" {
			if _, err := removeAISettings(); err != nil {
				m.message = "Could not remove: " + safeText(err.Error())
			} else {
				m.s, m.message = aimatch.Settings{}, "AI settings removed"
			}
		}
		m.mode = aiRows
		return m, nil
	}
	switch k {
	case "esc", "ctrl+c", "q":
		m.done = true
	case "up", "k", "shift+tab", "down", "j", "tab", "home", "end":
		m.cursor = listCursor(k, m.cursor, int(aiRowCount), 1)
	case "enter", "space":
		return m.activate()
	}
	return m, nil
}

func (m aiPageModel) activate() (aiPageModel, tea.Cmd) {
	m.message = ""
	switch aiRow(m.cursor) {
	case aiRowProvider:
		m.pickKind = aiRowProvider
		m.pickAll = []string{aiCustomProvider}
		for _, p := range aimatch.Providers {
			m.pickAll = append(m.pickAll, p.Name)
		}
		return m.openPick(), nil
	case aiRowProtocol:
		if m.s.Protocol == aimatch.ProtocolOpenAI {
			m.s.Protocol = aimatch.ProtocolAnthropic
		} else {
			m.s.Protocol = aimatch.ProtocolOpenAI
		}
		m.s.Model = ""
		m.save()
		m.message = "Protocol changed; pick a model again"
	case aiRowURL:
		return m.openEdit(aiRowURL, m.s.BaseURL, false)
	case aiRowKey:
		return m.openEdit(aiRowKey, "", true)
	case aiRowModel:
		m.pickKind, m.pickAll = aiRowModel, nil
		m = m.openPick()
		if err := m.s.ValidateEndpoint(); err != nil {
			m.message = safeText(err.Error()) + ". Type a model name to use it."
			return m, nil
		}
		return m.discover()
	case aiRowSearch:
		m.s.WebSearch = !m.s.WebSearch
		m.save()
		m.message = "Web search changed; run the connection test again"
	case aiRowTest:
		return m.test()
	case aiRowRemove:
		m.mode = aiConfirmRemove
	}
	return m, nil
}

func (m aiPageModel) openEdit(row aiRow, value string, secret bool) (aiPageModel, tea.Cmd) {
	m.mode, m.editRow, m.resetKey = aiEdit, row, false
	m.input.EchoMode = textinput.EchoNormal
	if secret {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.SetValue(value)
	m.input.Placeholder = ""
	return m, m.input.Focus()
}

func (m aiPageModel) editKey(msg tea.KeyPressMsg, k string) (aiPageModel, tea.Cmd) {
	switch k {
	case "esc", "ctrl+c":
		m.mode, m.message = aiRows, ""
		m.input.Blur()
		return m, nil
	case "ctrl+r":
		if m.editRow == aiRowKey {
			m.input.SetValue("")
			m.resetKey, m.message = true, "Key removal selected; Enter saves"
		}
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		switch m.editRow {
		case aiRowURL:
			v = strings.TrimRight(v, "/")
			if err := aimatch.ValidateBaseURL(v); err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.s.BaseURL = v
		case aiRowKey:
			if v == "" && !m.resetKey {
				m.message = "Paste a key to save, or press Ctrl+R to remove the saved one"
				return m, nil
			}
			m.s.APIKey = v
		}
		m.save()
		m.mode, m.message = aiRows, ""
		m.input.Blur()
		return m, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.resetKey = false
	}
	return m, cmd
}

func (m aiPageModel) openPick() aiPageModel {
	m.mode, m.pickCursor, m.message = aiPick, 0, ""
	m.input.EchoMode = textinput.EchoNormal
	m.input.SetValue("")
	m.input.Placeholder = "type to filter"
	if m.pickKind == aiRowModel {
		m.input.Placeholder = "type to filter, or a model name"
	}
	m.input.Focus()
	return m
}

// pickRows are the rows the filter leaves, then a row that uses the typed text
// as it is, so a model the endpoint does not list can still be chosen.
func (m aiPageModel) pickRows() (labels, values []string) {
	text := strings.TrimSpace(m.input.Value())
	exact := false
	for _, v := range m.pickAll {
		if text == "" || strings.Contains(strings.ToLower(v), strings.ToLower(text)) {
			labels, values = append(labels, v), append(values, v)
		}
		exact = exact || v == text
	}
	if m.pickKind == aiRowModel && text != "" && !exact {
		labels, values = append(labels, `Use "`+text+`"`), append(values, text)
	}
	return labels, values
}

func (m aiPageModel) pickKey(msg tea.KeyPressMsg, k string) (aiPageModel, tea.Cmd) {
	_, values := m.pickRows()
	switch k {
	case "esc", "ctrl+c":
		m.mode, m.message = aiRows, ""
		m.input.Blur()
		return m, nil
	case "up", "down", "pgup", "pgdown":
		m.pickCursor = listCursor(k, m.pickCursor, len(values), max(1, m.height-10))
		return m, nil
	case "enter":
		if m.pickCursor >= len(values) {
			return m, nil
		}
		return m.choose(values[m.pickCursor]), nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.pickCursor = 0
	return m, cmd
}

func (m aiPageModel) choose(value string) aiPageModel {
	m.mode = aiRows
	m.input.Blur()
	if m.pickKind == aiRowModel {
		m.s.Model = value
		m.save()
		m.message = "Model set; run the connection test"
		return m
	}
	prev := m.s
	for _, p := range aimatch.Providers {
		if p.Name == value {
			m.s.Protocol, m.s.BaseURL = p.Protocol, p.BaseURL
		}
	}
	if value == aiCustomProvider {
		m.cursor = int(aiRowURL)
		if m.s.BaseURL == "" {
			m.s.Protocol = aimatch.ProtocolOpenAI
		}
		m.message = "Set the protocol and base URL of your endpoint"
	}
	if m.s.BaseURL != prev.BaseURL || m.s.Protocol != prev.Protocol {
		// Another endpoint has other models, and the old key belongs to the old one.
		m.s.Model, m.s.APIKey = "", ""
		if value != aiCustomProvider {
			m.message = "Provider set; add the API key and pick a model"
			m.cursor = int(aiRowKey)
		}
	}
	m.save()
	return m
}

func (m aiPageModel) discover() (aiPageModel, tea.Cmd) {
	c, err := newAILister(m.s)
	if err != nil {
		m.message = safeText(err.Error()) + ". Type a model name to use it."
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.busy = cancel, true
	m.gen++
	gen := m.gen
	m.spinner = newLoadingSpinner()
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		ids, err := c.Models(ctx)
		return aiModelsMsg{gen: gen, ids: ids, err: err}
	})
}

func (m aiPageModel) test() (aiPageModel, tea.Cmd) {
	c, err := newAIClient(m.s)
	if err != nil {
		m.message = "✗ " + safeText(err.Error())
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.busy = cancel, true
	m.gen++
	gen := m.gen
	m.spinner = newLoadingSpinner()
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		return aiTestMsg{gen: gen, err: c.Test(ctx)}
	})
}

func (m aiPageModel) status() string {
	switch {
	case m.s.Verified():
		return "✓ Verified: AI Match is available"
	case m.s.Configured():
		return "Not verified: run the connection test to turn AI Match on"
	}
	return "Not configured: AI Match is off"
}

func (m aiPageModel) view() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	title := "Settings / AI model"
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	if m.busy {
		note := "Testing the connection…"
		if m.mode == aiPick {
			note = "Listing models…"
		}
		return settingsCompactView(w, h, title, "", []string{"  " + m.spinner.View() + note}, "", []keys.Binding{uiKey("esc", "cancel")})
	}
	switch m.mode {
	case aiEdit:
		label, help := "Base URL", []string{"https, or http for localhost only. Anthropic-compatible endpoints omit /v1."}
		if m.editRow == aiRowKey {
			label = "API key"
			help = []string{"Saved to ai.json in your config directory, readable only by you.", "It is shown masked and never written to logs."}
			if m.s.APIKey != "" {
				help = append([]string{"A key is set (hidden). Paste a new one to replace it."}, help...)
			}
		}
		lines := []string{label, m.input.View(), ""}
		for _, l := range help {
			lines = append(lines, muted.Render(ansi.Truncate(l, max(1, w-2), "…")))
		}
		return settingsCompactView(w, h, title, "", lines, m.message, []keys.Binding{uiKey("esc", "cancel"), uiKey("enter", "save")})
	case aiPick:
		labels, _ := m.pickRows()
		name := map[aiRow]string{aiRowProvider: "Provider", aiRowModel: "Model"}[m.pickKind]
		lines := []string{name, m.input.View(), ""}
		visible := max(1, h-10)
		start := max(0, m.pickCursor-visible+1)
		for i := start; i < min(len(labels), start+visible); i++ {
			lines = append(lines, styledListRow(safeText(labels[i]), i == m.pickCursor, w, currentTheme))
		}
		if len(labels) == 0 {
			lines = append(lines, muted.Render("  No match"))
		}
		return settingsCompactView(w, h, title, "", lines, m.message, []keys.Binding{uiKey("esc", "back"), uiKey("enter", "choose"), uiKey("↑/↓", "select")})
	case aiConfirmRemove:
		return settingsCompactView(w, h, title, "", []string{"Remove the saved AI model settings, including the API key?"}, "", []keys.Binding{uiKey("y", "remove"), uiKey("any key", "keep")})
	}
	labelWidth := 16
	var lines []string
	for i := range aiRowCount {
		value := ""
		switch i {
		case aiRowProvider:
			value = m.provider()
		case aiRowProtocol:
			value = m.s.Protocol.Label()
			if m.s.Protocol == "" {
				value = "Not set"
			}
		case aiRowURL:
			value = m.s.BaseURL
			if value == "" {
				value = "Not set"
			}
		case aiRowKey:
			value = m.s.MaskedKey()
		case aiRowModel:
			value = m.s.Model
			if value == "" {
				value = "Not set"
			}
		case aiRowSearch:
			value = "Off"
			if m.s.WebSearch {
				value = "On"
			}
		}
		row := aiRowLabels[i] + strings.Repeat(" ", max(1, labelWidth-ansi.StringWidth(aiRowLabels[i])))
		lines = append(lines, styledListRow(row+ansi.Truncate(safeText(value), max(1, w-labelWidth-4), "…"), int(i) == m.cursor, w, currentTheme))
	}
	lines = append(lines, "", m.status())
	if h >= 17 {
		lines = append(lines, "",
			muted.Render("AI Match is off until a model passes the connection test, and runs only"),
			muted.Render("when you press a in the metadata lookup. It sends the title, authors,"),
			muted.Render("ISBN and file name (never a path) to this endpoint."))
	}
	return settingsCompactView(w, h, title, "", lines, m.message, []keys.Binding{uiKey("esc", "back"), uiKey("enter", "select"), uiKey("↑/↓", "move")})
}

// openAIPage shows the AI model page over the settings.
func (m *settingsModel) openAIPage() tea.Cmd {
	if m.page != "ai-page" {
		m.returnPage, m.returnCursor = m.page, m.cursor
	}
	page := newAIPage(m.ctx, m.width, m.height)
	m.aiPage = &page
	m.page = "ai-page"
	m.message = ""
	return nil
}

// updateAIPage forwards messages to the embedded page and returns to the
// settings when it is done.
func (m settingsModel) updateAIPage(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
	}
	next, cmd := m.aiPage.update(msg)
	if !next.done {
		m.aiPage = &next
		return m, cmd
	}
	next.stopWork()
	m.aiPage = nil
	m.page = m.returnPage
	if m.page == "" || m.page == "ai-page" {
		m.page = "home"
	}
	m.cursor = m.returnCursor
	_ = m.reload()
	return m, nil
}

func (m settingsModel) aiPageView() tea.View {
	return m.aiPage.view()
}
