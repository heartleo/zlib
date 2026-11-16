package cli

import (
	"context"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// metaSourceChoice is one catalogue on the source panel.
type metaSourceChoice struct {
	name      string // the name in ZLIB_METADATA_SOURCES
	label     string
	enabled   bool
	available bool   // false: it cannot run now, and note says why
	note      string // shown beside the row
}

// metaSourceLookup rebuilds the lookup for the named sources.
type metaSourceLookup func(names []string) metaLookupFunc

// withMetaSources gives the page a source panel. choices says what is searched
// now and factory builds the lookup for another selection. Without it the page
// searches what its lookup was built with and has no panel.
func withMetaSources(choices []metaSourceChoice, factory metaSourceLookup) metaOption {
	return func(m *metaPageModel) {
		m.sources, m.lookupFor = choices, factory
	}
}

// newMetaSourcesFor builds the named sources; tests replace it to stay offline.
var newMetaSourcesFor = func(names []string) []metasource.Source {
	return metasource.NewSources(names, metaSourceConfig(metaTransport()))
}

// newMetaLookupFor is newDefaultMetaLookup for a chosen list of sources. The
// sources and the proxy they use are resolved now, as there: the lookup runs
// on another goroutine and must not read package state then.
func newMetaLookupFor(names []string) metaLookupFunc {
	sources := newMetaSourcesFor(names)
	return func(ctx context.Context, q metasource.Query) (metasource.Result, error) {
		return metasource.Lookup(ctx, sources, q, metasource.Options{})
	}
}

// defaultMetaSourceChoices lists the built-in sources with what the
// environment enables. It says whether a key is set and never shows one.
func defaultMetaSourceChoices() []metaSourceChoice {
	listed := metasource.ParseSourceNames(os.Getenv(metaSourcesEnv))
	on := func(name string) bool { return slices.Contains(listed, name) }
	googleNote := "shared quota"
	if strings.TrimSpace(os.Getenv(googleBooksKeyEnv)) != "" {
		googleNote = "key set"
	}
	wereadKey := strings.TrimSpace(os.Getenv(wereadAPIKeyEnv))
	wereadOK := metasource.ValidWeReadKey(wereadKey)
	wereadNote := "key set"
	switch {
	case wereadKey == "":
		wereadNote = "needs " + wereadAPIKeyEnv + " (not set)"
	case !wereadOK:
		wereadNote = wereadAPIKeyEnv + " is not a valid key"
	}
	return []metaSourceChoice{
		{name: metasource.NameOpenLibrary, label: "Open Library", enabled: on(metasource.NameOpenLibrary), available: true},
		{name: metasource.NameGoogle, label: "Google Books", enabled: on(metasource.NameGoogle), available: true, note: googleNote},
		{name: metasource.NameDouban, label: "Douban", enabled: on(metasource.NameDouban), available: true, note: "sends the title to Douban"},
		// Listed means the first search asked it, key or not: without a key it
		// answered with an error. The panel shows what was searched.
		{name: metasource.NameWeRead, label: "WeRead", enabled: on(metasource.NameWeRead), available: wereadOK, note: wereadNote},
	}
}

// enabledNames are the names of the sources that are on, in panel order.
func enabledMetaSources(choices []metaSourceChoice) []string {
	var names []string
	for _, c := range choices {
		if c.enabled {
			names = append(names, c.name)
		}
	}
	return names
}

// wereadOn reports whether the WeRead source is among the enabled ones.
func (m metaPageModel) wereadOn() bool {
	for _, c := range m.sources {
		if c.name == metasource.NameWeRead && c.enabled {
			return true
		}
	}
	return false
}

// titleNote warns that WeRead cannot search without a title: it has no lookup
// by ISBN, so a title-less search would quietly return nothing from it.
func (m metaPageModel) titleNote() string {
	if m.wereadOn() && len(strings.TrimSpace(m.query.Title)) == 0 {
		return "WeRead needs a title: it cannot search by ISBN"
	}
	return ""
}

// openPanel shows the source panel over a copy of the choices, so Esc leaves
// them as they were.
func (m metaPageModel) openPanel() (tea.Model, tea.Cmd) {
	if len(m.sources) == 0 || m.lookupFor == nil {
		return m, nil
	}
	m.draft = append([]metaSourceChoice(nil), m.sources...)
	m.panel, m.sourceCursor, m.message = true, 0, ""
	return m, nil
}

// panelKey handles a key on the source panel. The panel is not a text input,
// so q closes it like Esc.
func (m metaPageModel) panelKey(k string) (tea.Model, tea.Cmd) {
	n := len(m.draft)
	switch k {
	case "esc", "ctrl+c", "q":
		m.panel, m.message = false, ""
	case "up", "shift+tab", "k":
		m.sourceCursor = (m.sourceCursor + n - 1) % n
	case "down", "tab", "j":
		m.sourceCursor = (m.sourceCursor + 1) % n
	case "space", " ":
		c := &m.draft[m.sourceCursor]
		if !c.available && !c.enabled { // it can be turned off, but not on without its key
			m.message = c.label + " " + c.note
			return m, nil
		}
		c.enabled = !c.enabled
		m.message = ""
	case "enter", "ctrl+s":
		names := enabledMetaSources(m.draft)
		if len(names) == 0 {
			m.message = "Enable at least one source"
			return m, nil
		}
		m.remember() // before the sources and the lookup change
		m.sources, m.panel, m.message = m.draft, false, ""
		m.lookup = m.lookupFor(names)
		return m.search()
	}
	return m, nil
}

// panelBody draws one row per source, then the privacy note.
func (m metaPageModel) panelBody() string {
	width := max(20, m.width)
	lines := []string{"  " + colorBold("Sources for this search"), ""}
	for i, c := range m.draft {
		box := "[ ]"
		if c.enabled {
			box = "[x]"
		}
		label := box + " " + c.label
		if c.note != "" {
			label += "  " + c.note
		}
		lines = append(lines, styledListRow(label, i == m.sourceCursor, width, currentTheme))
	}
	lines = append(lines, "",
		"  "+colorFaint(ansi.Truncate("This search only: nothing is saved to .env.", max(1, width-4), "…")),
		"  "+colorFaint(ansi.Truncate("WeRead sends each title to Tencent under your account.", max(1, width-4), "…")))
	return strings.Join(lines, "\n")
}
