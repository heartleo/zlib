package cli

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/metasource"
)

// openSourcesPage shows the lookup sources as a checklist, like the default
// search formats: the saved list is ticked, and none ticked means the sources
// follow the book's language. value is the saved list, empty when there is none.
func (m *settingsModel) openSourcesPage(value string) {
	m.page = "sources"
	m.sourceCursor = 0
	m.sourceChoices = metaSourceChoices(metasource.ExplicitSourceNames(value))
	m.input.Blur()
}

// sourcesValue is the list the page would save, in the canonical order; empty
// when nothing is ticked.
func (m settingsModel) sourcesValue() string {
	return strings.Join(enabledMetaSources(m.sourceChoices), ",")
}

func (m settingsModel) updateSources(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.back()
	case "ctrl+c":
		return m, tea.Quit
	case "enter", "ctrl+s":
		m.save(m.sourcesValue())
	case "ctrl+a":
		// Tick every source that can run, or clear when they all are.
		all := true
		for _, c := range m.sourceChoices {
			all = all && (c.enabled || !c.available)
		}
		m.sourceChoices = append([]metaSourceChoice(nil), m.sourceChoices...)
		for i := range m.sourceChoices {
			if m.sourceChoices[i].available {
				m.sourceChoices[i].enabled = !all
			}
		}
		m.message = ""
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		m.sourceCursor = listCursor(k, m.sourceCursor, len(m.sourceChoices), max(1, m.height-10))
		m.message = ""
	case "space":
		m.sourceChoices = append([]metaSourceChoice(nil), m.sourceChoices...)
		c := &m.sourceChoices[m.sourceCursor]
		if !c.available && !c.enabled { // it can be turned off, but not on without its key
			m.message = c.label + " " + c.note
			return m, nil
		}
		c.enabled = !c.enabled
		m.message = ""
	}
	return m, nil
}

func (m settingsModel) sourcesView(w, h int) tea.View {
	summary := "None ticked: by book language"
	if names := enabledMetaSources(m.sourceChoices); len(names) > 0 {
		summary = strings.Join(enabledLabels(m.sourceChoices), ", ")
	}
	items := make([]checkItem, len(m.sourceChoices))
	for i, c := range m.sourceChoices {
		label := c.label
		if c.note != "" {
			label += "  " + c.note
		}
		items[i] = checkItem{label: label, checked: c.enabled}
	}
	return m.checklistView(w, h, checklist{
		short: "Lookup sources", title: "Lookup sources", summary: summary,
		override: m.value(metaSourcesEnv).Override, items: items, cursor: m.sourceCursor, fallbackPage: "metadata",
	})
}
