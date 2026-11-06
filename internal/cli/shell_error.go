package cli

import (
	"errors"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/zlib"
)

// shellErrorModel keeps a failed operation and its next action on screen.
// The shell owns the retry so it can start a fresh, generation-scoped request.
type shellErrorModel struct {
	width, height int
	title         string
	subtitle      string
	detail        string
	retry         bool
	selectedRetry bool
}

func (m shellErrorModel) Init() tea.Cmd { return nil }

func (m shellErrorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "r":
			if m.retry {
				m.selectedRetry = true
				return m, tea.Quit
			}
		case "esc", "q", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m shellErrorModel) View() tea.View {
	message := lipgloss.NewStyle().Foreground(currentTheme.Error).Render(m.title)
	body := "  " + message + "\n\n  " + bookCell(m.detail)
	if m.retry {
		return screenView(m.width, m.height, "Download", m.subtitle, body, "", uiKey("r", "retry"), uiKey("esc", "back"))
	}
	return screenView(m.width, m.height, "Send to Kindle", m.subtitle, body+"\n\n  Check your Kindle inbox before sending again.", "", uiKey("esc", "back"))
}

func downloadFailureDetail(err error) string {
	if err == nil {
		return "Download failed."
	}
	// Download errors may include signed URLs. Keep the operation useful without
	// printing those links in the terminal or in a screenshot.
	words := strings.Fields(err.Error())
	for i, word := range words {
		if strings.Contains(word, "://") {
			words[i] = "[URL hidden]"
		}
	}
	return strings.Join(words, " ")
}

// shellErrorText words an error for the shell, where the remedy for a missing
// or rejected session is /login rather than the CLI's `zlib login`.
func shellErrorText(err error) string {
	switch {
	case errors.Is(err, errNotLoggedIn), errors.Is(err, zlib.ErrNotLoggedIn):
		return "Not logged in. Use /login to sign in."
	case errors.Is(err, zlib.ErrSessionExpired):
		return "Session expired. Use /login to sign in again."
	}
	if hint := wereadErrorHint(err, wereadLoginShell); hint != "" {
		return err.Error() + ". " + hint
	}
	return err.Error()
}

// safeText drops control characters other than newline and tab, so an error
// message carrying server text cannot reach the terminal as an escape
// sequence. The library already cleans EAPI responses; this guards the
// outputs that print error text.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}
