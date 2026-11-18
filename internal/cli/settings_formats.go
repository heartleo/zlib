package cli

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m settingsModel) formatValue() string {
	var values []string
	for _, ext := range searchFormats {
		if slices.Contains(m.formats, ext) {
			values = append(values, strings.ToLower(ext.String()))
		}
	}
	return strings.Join(values, ",")
}

func (m settingsModel) updateFormats(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.back()
	case "ctrl+c":
		return m, tea.Quit
	case "enter", "ctrl+s":
		m.save(m.formatValue())
	case "ctrl+a":
		// Same as the search form: select everything, or clear when all are set.
		if len(m.formats) == len(searchFormats) {
			m.formats = nil
		} else {
			m.formats = slices.Clone(searchFormats)
		}
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		m.formatCursor = listCursor(k, m.formatCursor, len(searchFormats), max(1, m.height-10))
	case "space":
		ext := searchFormats[m.formatCursor]
		m.formats = slices.Clone(m.formats)
		if i := slices.Index(m.formats, ext); i >= 0 {
			m.formats = slices.Delete(m.formats, i, i+1)
		} else {
			m.formats = append(m.formats, ext)
		}
	}
	return m, nil
}

func (m settingsModel) formatsView(w, h int) tea.View {
	summary := "Any format"
	if len(m.formats) > 0 {
		summary = fmt.Sprintf("%d selected", len(m.formats))
	}
	items := make([]checkItem, len(searchFormats))
	for i, ext := range searchFormats {
		items[i] = checkItem{label: ext.String(), checked: slices.Contains(m.formats, ext)}
	}
	return m.checklistView(w, h, checklist{
		short: "Search formats", title: "Default search formats", summary: summary,
		override: m.value(searchExtensionsEnv).Override, items: items, cursor: m.formatCursor, fallbackPage: "search",
	})
}

// checkItem is one row of a checklist page.
type checkItem struct {
	label   string
	checked bool
}

// checklist is what a settings checklist page shows: the default search formats
// and the lookup sources share one look. summary says what is selected, and a
// message the page holds (a refusal) replaces it.
type checklist struct {
	short, title, summary string
	override              string // the setting that wins over what is saved here, or ""
	items                 []checkItem
	cursor                int
	fallbackPage          string // the list page shown dimmed behind the panel when the opener is unknown
	radio                 bool   // one row is chosen: (•) marks it, and Enter chooses
	wide                  bool   // rows with notes: the panel may be wider
}

// checklistView draws a checklist as a panel over the dimmed list page, or full
// screen when the terminal is small. The keys are those of updateFormats:
// move, Space, Ctrl+A, Enter, Esc.
func (m settingsModel) checklistView(w, h int, c checklist) tea.View {
	if h < 7 || w < 16 {
		return fullScreenView(w, h, c.short+"\nEnlarge terminal\nEsc cancel")
	}
	overlay := w >= 36 && h >= 14
	inner := w
	if overlay {
		inner = min(map[bool]int{false: 54, true: 66}[c.wide], w-6)
	}
	surface := lipgloss.NewStyle()
	if overlay {
		surface = surface.Background(currentTheme.Surface)
	}
	visible := max(1, min(len(c.items), h-5))
	if overlay {
		visible = max(1, min(len(c.items), h-10))
	}
	start := min(max(0, c.cursor-visible+1), len(c.items)-visible)
	paint := func(text string, ink color.Color) string {
		return surface.Foreground(ink).Width(inner).Render(ansi.Truncate(text, inner, "…"))
	}
	summary := c.summary
	if visible < len(c.items) {
		summary += fmt.Sprintf(" · %d–%d/%d", start+1, start+visible, len(c.items))
	}
	summaryColor := currentTheme.Muted
	if m.message != "" {
		summary = m.message
		summaryColor = currentTheme.Error
	} else if c.override != "" {
		summary = "Using " + c.override + " · " + summary
	}
	rows := []string{paint(c.title, currentTheme.Accent), paint(summary, summaryColor)}
	if overlay {
		rows = append(rows, paint("", currentTheme.Muted))
	}
	for i := start; i < start+visible; i++ {
		mark := "[ ] "
		switch {
		case c.radio && c.items[i].checked:
			mark = "(•) "
		case c.radio:
			mark = "( ) "
		case c.items[i].checked:
			mark = "[✓] "
		}
		row := mark + c.items[i].label
		style := surface.Foreground(currentTheme.Title).Width(inner)
		if i == c.cursor {
			style = style.Foreground(currentTheme.Surface).Background(currentTheme.Accent).Bold(true)
		}
		rows = append(rows, style.Render(ansi.Truncate(row, inner, "…")))
	}
	if overlay {
		rows = append(rows, paint("", currentTheme.Muted))
	}
	toggle, finish := "↑↓ move · Space toggle · Ctrl+A all/none", "Enter save · Esc cancel"
	switch {
	case inner < 26:
		toggle, finish = "Space · Ctrl+A", "↵ save · Esc cancel"
	case inner < 40:
		toggle = "Space toggle · Ctrl+A all"
	}
	if c.radio {
		toggle, finish = "↑↓ move · Enter choose", "Esc cancel"
		if inner < 26 {
			toggle = "↑↓ · ↵ choose"
		}
	}
	rows = append(rows, paint(toggle, currentTheme.Muted), paint(finish, currentTheme.Muted))
	if !overlay {
		return fullScreenView(w, h, strings.Join(rows, "\n"))
	}
	panel := surface.Width(inner).Padding(1, 1).Border(lipgloss.RoundedBorder()).
		BorderForeground(currentTheme.Accent).Render(strings.Join(rows, "\n"))
	background := m
	background.page, background.cursor = m.returnPage, m.returnCursor
	if !isSettingsListPage(background.page) {
		background.page = c.fallbackPage
	}
	base := lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(ansi.Strip(background.View().Content))
	x, y := max(0, (w-lipgloss.Width(panel))/2), max(0, (h-lipgloss.Height(panel))/2)
	return fullScreenView(w, h, lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(panel).X(x).Y(y)).Render())
}
