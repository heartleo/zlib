package cli

import tea "charm.land/bubbletea/v2"

type shellConfirm struct {
	width, height         int
	title, detail, action string
	// note is an optional line below detail.
	note               string
	confirmed, focused bool
}

func (m shellConfirm) Init() tea.Cmd { return nil }
func (m shellConfirm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c", "n":
			return m, tea.Quit
		case "left", "right", "tab", "shift+tab":
			m.focused = !m.focused
		case "enter":
			m.confirmed = m.focused
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m shellConfirm) View() tea.View {
	v := screenView(m.width, m.height, m.title, m.detail, "", "", uiKey("←/→", "select"), uiKey("enter", "confirm"), uiKey("esc", "back"))
	v.Content = confirmationDialog(v.Content, m.width, m.height, m.title, m.detail, "", m.note, m.action, m.focused, false)
	return v
}
