package cli

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/metasource"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// A setting whose values are a short list gets a choice page: the common values
// as rows, one marked, and where any other value is sensible a last row that
// opens the text box (the custom row). The box alone told nobody what it
// accepted, and everything it did not know was refused, so a value outside the
// list could not be entered and the list could not be seen.

// settingChoice is one row of a choice page.
type settingChoice struct {
	label string
	value string // what Enter saves; "" removes the setting, which restores its default
	reads string // what the setting reads as once this row is saved, when that is not value
	note  string // an example or a hint, after the label
}

// choiceSpec describes the choice page of one setting.
type choiceSpec struct {
	title   string
	choices []settingChoice
	// custom is the label of the row that opens the text box, or "" when the
	// setting takes only the listed values. placeholder, help and preview are the
	// box's: an example, what it accepts, and what the typed value would do.
	custom      string
	placeholder string
	help        []string
	preview     func(value string) string
	// matchSaved finds the marked row by the saved value, not the effective one,
	// for a setting whose default shows as a value (SMTP security).
	matchSaved bool
}

// choiceSpecFor returns the choice page of key, if it has one.
func choiceSpecFor(key string) (choiceSpec, bool) {
	switch key {
	case nameLayoutEnv:
		return choiceSpec{
			title: "File name format",
			choices: []settingChoice{
				{label: "Title - Author", reads: "title-author", note: "Dune - Frank Herbert.epub  (default)"},
				{label: "Author - Title", value: "author-title", note: "Frank Herbert - Dune.epub"},
				{label: "Title (Author)", value: "title-paren", note: "Dune (Frank Herbert).epub"},
			},
			custom:      "Custom pattern…",
			placeholder: "{title} [{authors}]",
			help: []string{"Write {title} and {authors}; any other text is kept.",
				`Not allowed in that text: / \ : " < > | ? *`, "With no author known, the name is the title."},
			preview: func(v string) string {
				l, err := metasource.ParseNameLayout(v)
				if err != nil {
					return err.Error()
				}
				return "Dune by Frank Herbert → " + metasource.FileNameWith(l, "Dune", []string{"Frank Herbert"}, ".epub")
			},
		}, true
	case metaResultsEnv:
		return choiceSpec{
			title: "Lookup results",
			choices: []settingChoice{
				{label: "5", value: "5", note: "matches"},
				{label: "10", reads: "10", note: "matches  (default)"},
				{label: "20", value: "20", note: "matches"},
				{label: "30", value: "30", note: "matches"},
				{label: "50", value: "50", note: "matches (the most)"},
			},
			custom: "Other number…", placeholder: "25",
			help: []string{"A whole number from 1 to 50."},
		}, true
	case zlib.EnvRetries:
		return choiceSpec{
			title: "Download retries",
			choices: []settingChoice{
				{label: "0", value: "0", note: "one request, no retry"},
				{label: "1", value: "1"},
				{label: "3", reads: "3", note: "(default)"},
				{label: "5", value: "5"},
				{label: "10", value: "10"},
			},
			custom: "Other number…", placeholder: "4",
			help: []string{"How many times a failed download request is retried: 0 or more."},
		}, true
	case kindlePortKey:
		return choiceSpec{
			title: "SMTP port",
			choices: []settingChoice{
				{label: "587", reads: "587", note: "STARTTLS: Gmail, Outlook  (default)"},
				{label: "465", value: "465", note: "SSL/TLS"},
				{label: "25", value: "25", note: "plain, often blocked"},
			},
			custom: "Other port…", placeholder: "2525",
			help: []string{"A port from 1 to 65535."},
		}, true
	case kindleSecurityKey:
		return choiceSpec{
			title: "SMTP security",
			choices: []settingChoice{
				{label: "Follow the port", note: "587 STARTTLS, 465 SSL/TLS  (default)"},
				{label: "STARTTLS", value: "starttls", note: "upgrade after connecting"},
				{label: "SSL/TLS", value: "tls", note: "encrypted from the start"},
			},
			matchSaved: true,
		}, true
	}
	return choiceSpec{}, false
}

// currentChoice is the row that holds value now, or -1. value is compared as the
// setting reads it, so "title-paren" and the pattern that spells it are one row.
func (c choiceSpec) currentChoice(value string) int {
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		if l, err := metasource.ParseNameLayout(s); err == nil && s != "" {
			return string(l)
		}
		if n, err := strconv.Atoi(s); err == nil {
			return strconv.Itoa(n)
		}
		return strings.ToLower(s)
	}
	for i, ch := range c.choices {
		reads := ch.reads
		if reads == "" {
			reads = ch.value
		}
		if norm(reads) == norm(value) && (reads != "" || strings.TrimSpace(value) == "") {
			return i
		}
	}
	return -1
}

// openChoicePage shows the choice page of key, on the row that holds its value.
// The page the settings were on stays the one Esc and a save return to.
func (m *settingsModel) openChoicePage(spec choiceSpec, v settingValue) {
	m.choice = spec
	m.page = "choice"
	cur := v.Value
	if spec.matchSaved {
		cur = v.Saved
	}
	m.choiceCursor = spec.currentChoice(cur)
	m.choiceCustom = m.choiceCursor < 0 && spec.custom != ""
	if m.choiceCustom {
		m.choiceCursor = len(spec.choices) // the custom row, holding a value of its own
		m.choiceValue = cur
	}
	m.choiceCursor = max(0, m.choiceCursor)
	m.input.Blur()
}

// choiceRows is the number of rows on the page: the choices and the custom row.
func (m settingsModel) choiceRows() int {
	n := len(m.choice.choices)
	if m.choice.custom != "" {
		n++
	}
	return n
}

func (m settingsModel) updateChoice(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.back()
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown", "tab", "shift+tab":
		m.choiceCursor = listCursor(k, m.choiceCursor, m.choiceRows(), max(1, m.height-10))
		m.message = ""
	case "enter", "space", "ctrl+s":
		if m.choiceCursor < len(m.choice.choices) {
			m.save(m.choice.choices[m.choiceCursor].value)
			return m, nil
		}
		return m, m.editCustom()
	}
	return m, nil
}

// editText opens the text box of key even when the setting has a choice page.
func (m *settingsModel) editText(key string) tea.Cmd {
	m.choiceEditing = true
	defer func() { m.choiceEditing = false }()
	return m.edit(key)
}

// editCustom opens the text box of the custom row, over the choice page: Esc
// there comes back to the choices, and a save goes on to the settings list.
func (m *settingsModel) editCustom() tea.Cmd {
	listPage, listCursor := m.returnPage, m.returnCursor
	cmd := m.editText(m.key)
	m.returnPage, m.returnCursor = listPage, listCursor
	m.fromChoice = true
	value := ""
	if m.choiceCustom {
		value = m.choiceValue // a value of its own is offered for editing
	}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Placeholder = m.choice.placeholder
	m.original = value
	return cmd
}

func (m settingsModel) choiceView(w, h int) tea.View {
	spec := m.choice
	items := make([]checkItem, 0, m.choiceRows())
	current := spec.currentChoice(m.currentChoiceValue())
	for i, ch := range spec.choices {
		label := ch.label
		if ch.note != "" {
			label += "  " + ch.note
		}
		items = append(items, checkItem{label: label, checked: i == current})
	}
	summary := "Choose a value"
	if spec.custom != "" {
		label := spec.custom
		if m.choiceCustom && current < 0 {
			label = safeText(m.choiceValue) + "  (your own value)"
		}
		items = append(items, checkItem{label: label, checked: current < 0 && m.choiceCustom})
		summary = "Choose a value, or enter your own"
	}
	return m.checklistView(w, h, checklist{
		short: spec.title, title: spec.title, summary: summary, override: m.value(m.key).Override,
		items: items, cursor: m.choiceCursor, fallbackPage: pageOfSetting(m.key), radio: true, wide: true,
	})
}

// currentChoiceValue is the value the marked row is found by.
func (m settingsModel) currentChoiceValue() string {
	v := m.value(m.key)
	if m.choice.matchSaved {
		return v.Saved
	}
	return v.Value
}

// pageOfSetting is the settings list page that holds key, "home" when none does.
func pageOfSetting(key string) string {
	for _, section := range settingsSections {
		for _, k := range section.keys {
			if k == key {
				return section.page
			}
		}
	}
	return "home"
}
