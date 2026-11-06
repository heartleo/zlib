package cli

import (
	"strings"

	"charm.land/bubbles/v2/help"
	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func newLoadingSpinner() spinner.Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	return s
}

// screenView keeps the title and controls in place as the body changes.
func screenView(width, height int, title, subtitle, body, status string, bindings ...keys.Binding) tea.View {
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	width, height = max(1, width), max(1, height)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	h := help.New()
	h.SetWidth(width)
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	h.Styles.ShortDesc = muted
	footer := h.ShortHelpView(bindings)
	lines := []string{lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render(title), muted.Render(subtitle), ""}
	available := max(0, height-6)
	bodyLines := strings.Split(body, "\n")
	lines = append(lines, bodyLines[:min(len(bodyLines), available)]...)
	for len(lines) < height-3 {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(status), "", footer)
	if height < 7 {
		lines = []string{title, "Enlarge terminal", footer}
		lines = lines[:min(height, len(lines))]
	}
	return fullScreenView(width, height, strings.Join(lines, "\n"))
}

// tightScreenView is screenView without its spacer rows, for a body that needs
// nearly the whole terminal, such as the login QR code. Title, subtitle,
// status and key hints take four rows and the body gets the rest; one spare
// row beyond the body becomes a blank after the subtitle. The caller styles
// status.
func tightScreenView(width, height int, title, subtitle string, body []string, status string, bindings ...keys.Binding) tea.View {
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	width, height = max(1, width), max(1, height)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	h := help.New()
	h.SetWidth(width)
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	h.Styles.ShortDesc = muted
	footer := h.ShortHelpView(bindings)
	if height < 5 {
		return fullScreenView(width, height, strings.Join([]string{title, "Enlarge terminal", footer}[:min(height, 3)], "\n"))
	}
	avail := height - 4
	body = body[:min(len(body), avail)]
	lines := []string{lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render(title), muted.Render(subtitle)}
	if avail-len(body) >= 1 {
		lines = append(lines, "")
	}
	lines = append(lines, body...)
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, status, footer)
	return fullScreenView(width, height, strings.Join(lines, "\n"))
}

// fullScreenView gives every page the same terminal-sized canvas. Keep each
// line's own styling; repainting styled text with an outer background can
// leave gaps when an inner ANSI reset clears that background.
func fullScreenView(width, height int, content string) tea.View {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(len(lines), height)]
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = ansi.Truncate(line, width, "…")
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	return themedView(strings.Join(lines, "\n"))
}

// themedView is the full-screen view every program renders. It sets the
// terminal's default background and foreground to the theme's own, which
// Bubble Tea restores on exit. Without it every screen but the theme picker
// drew the palette on the terminal's background, so a light theme on a dark
// terminal (or the reverse) left titles near 1:1 contrast.
func themedView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	if currentTheme.Background != nil {
		v.BackgroundColor = currentTheme.Background
		v.ForegroundColor = currentTheme.Title
	}
	return v
}

func uiKey(key, label string) keys.Binding {
	return keys.NewBinding(keys.WithKeys(key), keys.WithHelp(key, label))
}

// actionButtonRows renders borderless actions and wraps them on narrow screens.
// Focus is one-based so zero can represent a text field before the actions.
func renderActionButton(label string, focused, danger bool) string {
	style := lipgloss.NewStyle().Foreground(currentTheme.Title).Background(currentTheme.Surface).Padding(0, 2)
	if focused {
		background := currentTheme.Accent
		if danger {
			background = currentTheme.Error
		}
		style = style.Foreground(currentTheme.Surface).Background(background).Bold(true)
	}
	return style.Render(label)
}

func actionButtonRows(actions []string, focused, width int) []string {
	return actionButtonRowsOn(actions, focused, width, lipgloss.NewStyle())
}

// actionButtonRowsOn is actionButtonRows for a panel with its own
// background: the gap between buttons is drawn in surface, or the terminal's
// default background shows through as a dark block after each button.
func actionButtonRowsOn(actions []string, focused, width int, surface lipgloss.Style) []string {
	gap := surface.Render("  ")
	var rows []string
	row := ""
	for i, label := range actions {
		if label == "" {
			continue
		}
		button := renderActionButton(label, focused == i+1, label == "Reset")
		button = ansi.Truncate(button, max(1, width), "…")
		if row != "" && ansi.StringWidth(row)+2+ansi.StringWidth(button) > width {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += gap
		}
		row += button
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}

func selectionRow(label string, selected bool, width int) string {
	return styledListRow(bookCell(label), selected, width, currentTheme)
}

// styledListRow preserves measured column spacing; callers sanitize cell text.
func styledListRow(label string, selected bool, width int, palette Theme) string {
	style := lipgloss.NewStyle().Foreground(palette.Title)
	prefix := "  "
	if selected {
		prefix = "> "
		style = style.Background(palette.Surface).Bold(true)
	}
	return style.Width(max(1, width)).Render(ansi.Truncate(prefix+label, max(1, width), "…"))
}

func listCursor(k string, cursor, count, pageSize int) int {
	switch k {
	case "up", "k", "shift+tab":
		cursor--
	case "down", "j", "tab":
		cursor++
	case "home":
		cursor = 0
	case "end":
		cursor = count - 1
	case "pgup":
		cursor -= pageSize
	case "pgdown":
		cursor += pageSize
	}
	return min(max(0, cursor), max(0, count-1))
}
