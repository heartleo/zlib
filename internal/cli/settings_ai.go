package cli

import (
	"context"
	"strconv"
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

// The AI model page is a guided setup followed by a short summary. The first
// time (nothing configured) it walks through the steps by itself: provider,
// then, for a custom endpoint, protocol and base URL, then the API key, then
// the model, and it ends with the connection test, so a working setup needs
// only choices and a pasted key. Afterwards the page is six rows, and changing
// the key, the model or web search tests the connection again by itself.
// Every edit is saved at once; the saved verification goes stale on any change
// (Settings.Verified), so AI Match is on only for settings that passed.

// aiField is what a step or a list edits.
type aiField int

const (
	fieldProvider aiField = iota
	fieldProtocol
	fieldURL
	fieldKey
	fieldModel
)

// aiAction is a row of the summary page.
type aiAction int

const (
	actProvider aiAction = iota
	actModel
	actKey
	actSearch
	actTest
	actRemove
	actCount
)

var aiActionLabels = [actCount]string{"Provider", "Model", "API key", "Web search", "Test connection", "Remove settings"}

const aiCustomProvider = "Custom endpoint"

type aiMode int

const (
	aiRows aiMode = iota
	aiEdit
	aiPick
	aiConfirmRemove
)

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

type aiPageModel struct {
	ctx    context.Context
	s      aimatch.Settings
	mode   aiMode
	cursor int
	input  textinput.Model
	// field is what the open editor or list is for. wizard is set while the
	// guided setup runs, and custom when its provider is a custom endpoint.
	field         aiField
	wizard        bool
	custom        bool
	resetKey      bool
	pickAll       []string
	pickCursor    int
	spinner       spinner.Model
	busy          bool
	busyNote      string
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
	if !m.s.Configured() && err == nil {
		m.wizard = true
		m = m.openProviderPick() // nothing to summarise yet: start the setup
	}
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
		m.pickCursor = m.currentIndex()
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
		m.message = "✓ The connection works. AI Match is available in the metadata lookup."
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
			m.wizard = false
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
		m.cursor = listCursor(k, m.cursor, int(actCount), 1)
	case "enter", "space":
		return m.activate()
	}
	return m, nil
}

func (m aiPageModel) activate() (aiPageModel, tea.Cmd) {
	m.message = ""
	switch aiAction(m.cursor) {
	case actProvider:
		m.wizard = true
		return m.openProviderPick(), nil
	case actModel:
		return m.toModel()
	case actKey:
		return m.openEdit(fieldKey, "", true)
	case actSearch:
		m.s.WebSearch = !m.s.WebSearch
		m.save()
		if m.s.Configured() {
			return m.test() // the test covers web search when it is on
		}
	case actTest:
		return m.test()
	case actRemove:
		m.mode = aiConfirmRemove
	}
	return m, nil
}

// --- Steps ------------------------------------------------------------------

// steps lists the fields the setup visits for the provider chosen.
func (m aiPageModel) steps() []aiField {
	steps := []aiField{fieldProvider}
	if m.custom {
		steps = append(steps, fieldProtocol, fieldURL)
	}
	if !m.s.KeyOptional() || m.s.BaseURL == "" {
		steps = append(steps, fieldKey)
	}
	return append(steps, fieldModel)
}

// stepLabel is "Step 2 of 3" while the setup runs, else "".
func (m aiPageModel) stepLabel() string {
	if !m.wizard {
		return ""
	}
	steps := m.steps()
	for i, f := range steps {
		if f == m.field {
			return "Step " + strconv.Itoa(i+1) + " of " + strconv.Itoa(len(steps))
		}
	}
	return ""
}

// advance moves the setup on after field from was set; outside the setup it
// finishes the edit instead, testing the connection when there is a model.
func (m aiPageModel) advance(from aiField) (aiPageModel, tea.Cmd) {
	m.message = ""
	if !m.wizard {
		if m.s.Configured() {
			return m.test()
		}
		return m, nil
	}
	switch from {
	case fieldProvider:
		if m.custom {
			return m.openProtocolPick(), nil
		}
		return m.toKey()
	case fieldProtocol:
		return m.openEdit(fieldURL, m.s.BaseURL, false)
	case fieldURL:
		return m.toKey()
	case fieldKey:
		return m.toModel()
	}
	m.wizard = false // the model was the last step
	return m.test()
}

// toKey asks for the key, unless a local endpoint needs none or one is set.
func (m aiPageModel) toKey() (aiPageModel, tea.Cmd) {
	if m.s.KeyOptional() || m.s.APIKey != "" {
		return m.toModel()
	}
	return m.openEdit(fieldKey, "", true)
}

// toModel lists the endpoint's models, or says why it cannot; a name can be
// typed either way.
func (m aiPageModel) toModel() (aiPageModel, tea.Cmd) {
	m.field, m.pickAll = fieldModel, nil
	m = m.openPick()
	if err := m.s.ValidateEndpoint(); err != nil {
		m.message = safeText(err.Error()) + ". Type a model name to use it."
		return m, nil
	}
	return m.discover()
}

func (m aiPageModel) openProviderPick() aiPageModel {
	m.field = fieldProvider
	m.pickAll = nil
	for _, p := range aimatch.Providers {
		m.pickAll = append(m.pickAll, p.Name)
	}
	m.pickAll = append(m.pickAll, aiCustomProvider)
	m = m.openPick()
	return m
}

func (m aiPageModel) openProtocolPick() aiPageModel {
	m.field = fieldProtocol
	m.pickAll = nil
	for _, p := range aimatch.Protocols {
		m.pickAll = append(m.pickAll, p.Label())
	}
	return m.openPick()
}

// --- Editor -----------------------------------------------------------------

func (m aiPageModel) openEdit(field aiField, value string, secret bool) (aiPageModel, tea.Cmd) {
	m.mode, m.field, m.resetKey = aiEdit, field, false
	m.input.EchoMode = textinput.EchoNormal
	if secret {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.SetValue(value)
	m.input.Placeholder = ""
	if field == fieldURL && value == "" {
		m.input.Placeholder = "https://api.example.com/v1"
	}
	return m, m.input.Focus()
}

// leaveEdit closes the editor, or pauses the setup when it was running.
func (m aiPageModel) leaveEdit() aiPageModel {
	m.mode = aiRows
	m.input.Blur()
	m.message = ""
	if m.wizard {
		m.wizard = false
		m.message = "Setup paused; what you set so far is saved. Choose a row to continue."
	}
	return m
}

func (m aiPageModel) editKey(msg tea.KeyPressMsg, k string) (aiPageModel, tea.Cmd) {
	switch k {
	case "esc", "ctrl+c":
		return m.leaveEdit(), nil
	case "ctrl+r":
		if m.field == fieldKey {
			m.input.SetValue("")
			m.resetKey, m.message = true, "Key removal selected; Enter saves"
		}
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		switch m.field {
		case fieldURL:
			v = strings.TrimRight(v, "/")
			if err := aimatch.ValidateBaseURL(v); err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.s.BaseURL = v
		case fieldKey:
			if v == "" && !m.resetKey {
				if m.s.APIKey == "" && m.wizard && m.s.KeyOptional() {
					break // a local endpoint: Enter skips the key
				}
				m.message = "Paste a key to save, or press Ctrl+R to remove the saved one"
				return m, nil
			}
			m.s.APIKey = v
		}
		m.save()
		m.mode = aiRows
		m.input.Blur()
		return m.advance(m.field)
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.resetKey = false
	}
	return m, cmd
}

// --- Lists ------------------------------------------------------------------

func (m aiPageModel) openPick() aiPageModel {
	m.mode, m.pickCursor, m.message = aiPick, 0, ""
	m.input.EchoMode = textinput.EchoNormal
	m.input.SetValue("")
	m.input.Placeholder = "type to filter"
	if m.field == fieldModel {
		m.input.Placeholder = "type to filter, or a model name"
	}
	m.input.Focus()
	m.pickCursor = m.currentIndex()
	return m
}

// current is the value of the open list that the settings hold now.
func (m aiPageModel) current() string {
	switch m.field {
	case fieldModel:
		return m.s.Model
	case fieldProvider:
		return m.provider()
	case fieldProtocol:
		return m.s.Protocol.Label()
	}
	return ""
}

// currentIndex is where the cursor starts: on the current choice.
func (m aiPageModel) currentIndex() int {
	for i, v := range m.pickAll {
		if v == m.current() && v != "" {
			return i
		}
	}
	return 0
}

// pickRows are the rows the filter leaves, the current one marked, then a row
// that uses the typed text as it is, so a model the endpoint does not list can
// still be chosen.
func (m aiPageModel) pickRows() (labels, values []string) {
	text := strings.TrimSpace(m.input.Value())
	exact := false
	for _, v := range m.pickAll {
		if text == "" || strings.Contains(strings.ToLower(v), strings.ToLower(text)) {
			label := v
			if v == m.current() {
				label += "  ✓ current"
			}
			labels, values = append(labels, label), append(values, v)
		}
		exact = exact || v == text
	}
	if m.field == fieldModel && text != "" && !exact {
		labels, values = append(labels, `Use "`+text+`"`), append(values, text)
	}
	return labels, values
}

func (m aiPageModel) pickKey(msg tea.KeyPressMsg, k string) (aiPageModel, tea.Cmd) {
	_, values := m.pickRows()
	switch k {
	case "esc", "ctrl+c":
		m.mode = aiRows
		m.input.Blur()
		m.wizard = false
		m.message = ""
		return m, nil
	case "up", "down", "pgup", "pgdown":
		m.pickCursor = listCursor(k, m.pickCursor, len(values), max(1, m.height-10))
		return m, nil
	case "enter":
		if m.pickCursor >= len(values) {
			return m, nil
		}
		return m.choose(values[m.pickCursor])
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.pickCursor = 0
	return m, cmd
}

func (m aiPageModel) choose(value string) (aiPageModel, tea.Cmd) {
	m.mode = aiRows
	m.input.Blur()
	switch m.field {
	case fieldModel:
		m.s.Model = value
		m.save()
	case fieldProtocol:
		for _, p := range aimatch.Protocols {
			if p.Label() == value {
				m.s.Protocol = p
			}
		}
		m.s.Model = ""
		m.save()
	case fieldProvider:
		prev := m.s
		m.custom = value == aiCustomProvider
		for _, p := range aimatch.Providers {
			if p.Name == value {
				m.s.Protocol, m.s.BaseURL = p.Protocol, p.BaseURL
			}
		}
		if m.custom && m.s.Protocol == "" {
			m.s.Protocol = aimatch.ProtocolOpenAI
		}
		if m.s.BaseURL != prev.BaseURL || m.s.Protocol != prev.Protocol {
			// Another endpoint has other models, and the old key belongs to the old one.
			m.s.Model, m.s.APIKey = "", ""
		}
		m.save()
	}
	return m.advance(m.field)
}

func (m aiPageModel) discover() (aiPageModel, tea.Cmd) {
	c, err := newAILister(m.s)
	if err != nil {
		m.message = safeText(err.Error()) + ". Type a model name to use it."
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.busy, m.busyNote = cancel, true, "Listing models…"
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
	m.cancel, m.busy, m.busyNote = cancel, true, "Testing the connection…"
	m.mode = aiRows
	m.gen++
	gen := m.gen
	m.spinner = newLoadingSpinner()
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		return aiTestMsg{gen: gen, err: c.Test(ctx)}
	})
}

// --- Views ------------------------------------------------------------------

func (m aiPageModel) status() string {
	switch {
	case m.s.Verified():
		return "✓ Verified: AI Match is available"
	case m.s.Configured():
		return "Not verified: run the test to turn AI Match on"
	}
	return "Not configured: AI Match is off"
}

// stepTitle is the title of a setup step: where it is and what it asks.
func (m aiPageModel) stepTitle(what string) string {
	if label := m.stepLabel(); label != "" {
		return "Settings / AI model · " + label + " · " + what
	}
	return "Settings / AI model · " + what
}

func (m aiPageModel) view() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	title := "Settings / AI model"
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	if m.busy {
		return settingsCompactView(w, h, title, "", []string{"  " + m.spinner.View() + m.busyNote}, "", []keys.Binding{uiKey("esc", "cancel")})
	}
	switch m.mode {
	case aiEdit:
		what, help := "Base URL", []string{"https, or http for localhost only. Anthropic-compatible endpoints omit /v1."}
		if m.field == fieldKey {
			what = "API key"
			help = []string{"Saved to ai.json in your config directory, readable only by you.", "It is shown masked and never written to logs."}
			if m.s.APIKey != "" {
				help = append([]string{"A key is set (hidden). Paste a new one to replace it."}, help...)
			}
			if m.s.KeyOptional() {
				help = append(help, "A local endpoint needs no key: Enter on an empty box skips it.")
			}
		}
		lines := []string{m.input.View(), ""}
		for _, l := range help {
			lines = append(lines, muted.Render(ansi.Truncate(l, max(1, w-2), "…")))
		}
		controls := []keys.Binding{uiKey("enter", "next"), uiKey("esc", "stop")}
		if !m.wizard {
			controls = []keys.Binding{uiKey("enter", "save"), uiKey("esc", "cancel")}
		}
		return settingsCompactView(w, h, m.stepTitle(what), "", lines, m.message, controls)
	case aiPick:
		labels, _ := m.pickRows()
		name := map[aiField]string{fieldProvider: "Provider", fieldProtocol: "Protocol", fieldModel: "Model"}[m.field]
		lines := []string{m.input.View(), ""}
		visible := max(1, h-10)
		start := max(0, m.pickCursor-visible+1)
		for i := start; i < min(len(labels), start+visible); i++ {
			lines = append(lines, styledListRow(safeText(labels[i]), i == m.pickCursor, w, currentTheme))
		}
		if len(labels) == 0 {
			lines = append(lines, muted.Render("  No match"))
		}
		controls := []keys.Binding{uiKey("enter", "choose"), uiKey("↑/↓", "select"), uiKey("esc", "back")}
		return settingsCompactView(w, h, m.stepTitle(name), "", lines, m.message, controls)
	case aiConfirmRemove:
		return settingsCompactView(w, h, title, "", []string{"Remove the saved AI model settings, including the API key?"}, "", []keys.Binding{uiKey("y", "remove"), uiKey("any key", "keep")})
	}
	labelWidth := 16
	var lines []string
	for i := range actCount {
		value := ""
		switch i {
		case actProvider:
			value = m.provider()
			if value == aiCustomProvider {
				value += " · " + m.s.BaseURL
			}
		case actModel:
			value = m.s.Model
			if value == "" {
				value = "Not set"
			}
		case actKey:
			value = m.s.MaskedKey()
		case actSearch:
			value = "Off"
			if m.s.WebSearch {
				value = "On: the model may search the web"
			}
		case actTest:
			value = map[bool]string{true: "✓ verified", false: "not verified"}[m.s.Verified()]
		}
		row := aiActionLabels[i] + strings.Repeat(" ", max(1, labelWidth-ansi.StringWidth(aiActionLabels[i])))
		lines = append(lines, styledListRow(row+ansi.Truncate(safeText(value), max(1, w-labelWidth-4), "…"), int(i) == m.cursor, w, currentTheme))
	}
	lines = append(lines, "", m.status())
	if h >= 17 {
		lines = append(lines, "",
			muted.Render("Changing the key, model or web search tests the connection again."),
			muted.Render("AI Match runs only when you press a in the metadata lookup. It sends"),
			muted.Render("the title, authors, ISBN and file name (never a path) to this endpoint."))
	}
	return settingsCompactView(w, h, title, "", lines, m.message, []keys.Binding{uiKey("enter", "change"), uiKey("↑/↓", "move"), uiKey("esc", "back")})
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
