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
	if h < 7 || w < 16 {
		return fullScreenView(w, h, "Search formats\nEnlarge terminal\nEsc cancel")
	}
	overlay := w >= 36 && h >= 14
	inner := w
	if overlay {
		inner = min(54, w-6)
	}
	surface := lipgloss.NewStyle()
	if overlay {
		surface = surface.Background(currentTheme.Surface)
	}
	visible := max(1, min(len(searchFormats), h-5))
	if overlay {
		visible = max(1, min(len(searchFormats), h-10))
	}
	start := min(max(0, m.formatCursor-visible+1), len(searchFormats)-visible)
	paint := func(text string, ink color.Color) string {
		return surface.Foreground(ink).Width(inner).Render(ansi.Truncate(text, inner, "…"))
	}
	summary := "Any format"
	if len(m.formats) > 0 {
		summary = fmt.Sprintf("%d selected", len(m.formats))
	}
	if visible < len(searchFormats) {
		summary += fmt.Sprintf(" · %d–%d/%d", start+1, start+visible, len(searchFormats))
	}
	summaryColor := currentTheme.Muted
	if m.message != "" {
		summary = m.message
		summaryColor = currentTheme.Error
	} else if override := m.value(searchExtensionsEnv).Override; override != "" {
		summary = "Using " + override + " · " + summary
	}
	rows := []string{paint("Default search formats", currentTheme.Accent), paint(summary, summaryColor)}
	if overlay {
		rows = append(rows, paint("", currentTheme.Muted))
	}
	for i := start; i < start+visible; i++ {
		mark := "[ ] "
		if slices.Contains(m.formats, searchFormats[i]) {
			mark = "[✓] "
		}
		row := mark + searchFormats[i].String()
		style := surface.Foreground(currentTheme.Title).Width(inner)
		if i == m.formatCursor {
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
	rows = append(rows, paint(toggle, currentTheme.Muted), paint(finish, currentTheme.Muted))
	if !overlay {
		return fullScreenView(w, h, strings.Join(rows, "\n"))
	}
	panel := surface.Width(inner).Padding(1, 1).Border(lipgloss.RoundedBorder()).
		BorderForeground(currentTheme.Accent).Render(strings.Join(rows, "\n"))
	background := m
	background.page, background.cursor = m.returnPage, m.returnCursor
	if !isSettingsListPage(background.page) {
		background.page = "search"
	}
	base := lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(ansi.Strip(background.View().Content))
	x, y := max(0, (w-lipgloss.Width(panel))/2), max(0, (h-lipgloss.Height(panel))/2)
	return fullScreenView(w, h, lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(panel).X(x).Y(y)).Render())
}
