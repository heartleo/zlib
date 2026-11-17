package cli

import (
	"path/filepath"
	"slices"
	"strings"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// scannedISBNs reads ISBNs printed in the book's own text, for a file that
// carries no valid ISBN of its own. They are hints, like any ISBN not read from
// the file's metadata.
func scannedISBNs(path string, cur *ebookmeta.Metadata) []string {
	if metasource.NormalizeISBN(cur.Identifier("isbn")) != "" {
		return nil
	}
	snippets, err := ebookmeta.ISBNSnippets(path)
	if err != nil {
		return nil
	}
	return metasource.ISBNsFromSnippets(snippets)
}

// openPrepare goes back to the prepare page from the results, to change the
// sources or the terms. Esc there returns to the results.
func (m metaPageModel) openPrepare() (tea.Model, tea.Cmd) {
	if len(m.sources) == 0 || m.lookupFor == nil {
		return m, nil
	}
	m.remember()
	m.draft = append([]metaSourceChoice(nil), m.sources...)
	m.stage, m.sourceCursor, m.message = metaPrepare, 0, ""
	return m, nil
}

// prepareKey handles a key on the prepare page. It is not a text input, so q
// leaves like Esc.
func (m metaPageModel) prepareKey(k string) (tea.Model, tea.Cmd) {
	n := len(m.draft)
	switch k {
	case "esc", "ctrl+c", "q":
		if m.prev != nil { // opened from the results: back to them
			return m.abandonSearch(), nil
		}
		m.cancelled = true
		return m, tea.Quit
	case "up", "shift+tab", "k":
		if n > 0 {
			m.sourceCursor = (m.sourceCursor + n - 1) % n
		}
	case "down", "tab", "j":
		if n > 0 {
			m.sourceCursor = (m.sourceCursor + 1) % n
		}
	case "space", " ":
		if n == 0 {
			return m, nil
		}
		c := &m.draft[m.sourceCursor]
		if !c.available && !c.enabled { // it can be turned off, but not on without its key
			m.message = c.label + " " + c.note
			return m, nil
		}
		c.enabled = !c.enabled
		m.message = ""
	case "e":
		return m.startQueryEdit()
	case "t":
		if m.extraction.AltTitle == "" {
			m.message = "There is no other title to use"
			return m, nil
		}
		m.extraction.Query = m.query
		m.extraction = m.extraction.UseOtherTitle()
		m.query = m.extraction.Query
		m.message = ""
	case "r":
		return m.rememberSources()
	case "n":
		return m.openRename()
	case "enter", "ctrl+s":
		return m.startSearch()
	}
	return m, nil
}

// rememberSources saves the sources that are on as the default, when the user
// asks for it. Nothing is saved otherwise.
func (m metaPageModel) rememberSources() (tea.Model, tea.Cmd) {
	names := enabledMetaSources(m.draft)
	switch {
	case m.saveSources == nil || len(m.draft) == 0:
		m.message = "The sources cannot be saved here"
	case len(names) == 0:
		m.message = "Enable at least one source to save"
	default:
		overriddenBy, err := m.saveSources(names)
		switch {
		case err != nil:
			m.message = "Could not save the sources: " + safeText(err.Error())
		case overriddenBy != "":
			m.message = "Saved, but " + safeText(overriddenBy) + " takes precedence"
		default:
			m.message = "Saved as the default: " + strings.Join(enabledLabels(m.draft), ", ")
		}
	}
	return m, nil
}

func enabledLabels(choices []metaSourceChoice) []string {
	var labels []string
	for _, c := range choices {
		if c.enabled {
			labels = append(labels, c.label)
		}
	}
	return labels
}

// startSearch commits the sources shown and searches for the terms. With no
// terms it asks for some instead, and with no source on it says so.
func (m metaPageModel) startSearch() (tea.Model, tea.Cmd) {
	if m.query.Empty() {
		return m.startQueryEdit()
	}
	if len(m.draft) > 0 && m.lookupFor != nil {
		names := enabledMetaSources(m.draft)
		if len(names) == 0 {
			m.message = "Enable at least one source"
			return m, nil
		}
		m.prev = m.snapshot() // before the sources and the lookup change; over the results, if it was opened from them
		m.sources = append([]metaSourceChoice(nil), m.draft...)
		m.lookup = m.lookupFor(names)
	}
	m.message = ""
	return m.search()
}

// prepareFrom words where a part of the query came from.
func prepareFrom(source string) string {
	switch source {
	case "file name":
		return "from the file name"
	case "embedded":
		return "from the file"
	case "download record":
		return "from the download"
	case "edited", "typed":
		return "typed by you"
	case "recorded by a download":
		return "recorded by the download"
	case "copyright page":
		return "on the copyright page"
	}
	return source
}

// prepareBody is the prepare page: what will be searched and where each part
// came from, the sources to ask, and what the file name and the file said. The
// terms and the sources come first, so a short terminal still shows them.
func (m metaPageModel) prepareBody() string {
	width := max(20, m.width)
	x, q := m.extraction, m.query
	// row is one labelled value with a note after it. A long value is cut so
	// that the note, which says where it came from or what to press, stays.
	row := func(label, value, note string) string {
		prefix := "    " + label + strings.Repeat(" ", max(1, 9-ansi.StringWidth(label)))
		value = safeText(oneLine(value))
		if note != "" {
			room := width - ansi.StringWidth(prefix) - ansi.StringWidth(note) - 2
			if room >= 8 {
				value = ansi.Truncate(value, room, "…")
			}
			return ansi.Truncate(prefix+value+"  "+colorFaint(note), width, "…")
		}
		return ansi.Truncate(prefix+value, width, "…")
	}
	lines := []string{"  " + colorBold("Search for")}
	if q.Empty() {
		lines = append(lines, "    "+colorFaint("Nothing to search with yet: press e to enter a title, an author or an ISBN."))
	} else {
		title, note := q.Title, prepareFrom(x.TitleFrom)
		if title == "" {
			title, note = "(none)", ""
		} else if q.TitleGuessed {
			note += ", a guess"
		}
		lines = append(lines, row("Title", title, note))
		if x.AltTitle != "" {
			lines = append(lines, row("Other", x.AltTitle, "press t to use it"))
		}
		authors := strings.Join(q.Authors, " & ")
		if authors == "" {
			authors = "(none)"
		}
		lines = append(lines, row("Authors", authors, prepareFrom(x.AuthorsFrom)))
		lines = append(lines, row("ISBN", isbnLine(q, x.ISBNFrom), ""))
	}
	if len(m.draft) > 0 {
		lines = append(lines, "", "  "+colorBold("Sources"))
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
		if m.wereadOn() {
			lines = append(lines, "  "+colorFaint(ansi.Truncate("WeRead sends each title to Tencent under your account.", max(1, width-4), "…")))
		}
		if note := m.titleNote(); note != "" {
			lines = append(lines, "  "+colorFaint(ansi.Truncate(note, max(1, width-4), "…")))
		}
	}
	lines = append(lines, "", "  "+colorBold("In the file"))
	lines = append(lines, row("Name", strings.TrimSuffix(filepath.Base(m.path), filepath.Ext(m.path)), "")) // the file as it is now, after a rename
	lines = append(lines, row("Embedded", embeddedSummary(x.Embedded), ""))
	return strings.Join(lines, "\n")
}

// isbnLine lists the query's ISBNs, each with where it came from, and says
// when it is only a hint.
func isbnLine(q metasource.Query, from []string) string {
	if len(q.ISBNs) == 0 {
		return "(none)"
	}
	var parts []string
	for i, isbn := range q.ISBNs {
		src := ""
		if i < len(from) {
			src = from[i]
		}
		part := isbn + " (" + strings.TrimPrefix(prepareFrom(src), "from ")
		if slices.Contains(q.Unconfirmed, isbn) {
			part += ", a hint"
		}
		parts = append(parts, part+")")
	}
	return strings.Join(parts, ", ")
}

// embeddedSummary is what the file itself says, in one line, or that it says
// nothing useful.
func embeddedSummary(e metasource.Embedded) string {
	var parts []string
	for _, p := range []string{e.Title, strings.Join(e.Authors, " & "), e.Publisher, e.Year, e.Language} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	for _, isbn := range e.ISBNs {
		parts = append(parts, "ISBN "+isbn)
	}
	if len(parts) == 0 {
		return "nothing useful"
	}
	return strings.Join(parts, " · ")
}

// prepareKeys are the key hints of the prepare page, in the longest wording
// that fits. Keys that do nothing here are left out: moving and toggling need
// sources, t needs another title and r needs a way to save. As the width
// shrinks the hints lose their labels and then the keys that matter least
// (moving, t, r), but every tier keeps n and esc.
func (m metaPageModel) prepareKeys() []keys.Binding {
	hasSources := len(m.draft) > 0
	hasAlt := m.extraction.AltTitle != ""
	canSave := m.saveSources != nil && hasSources
	type words struct{ move, toggle, edit, title, save, rename, search, esc string }
	// tier builds one wording. arrows, more (t and r) say which of the keys that
	// matter least it still shows.
	tier := func(arrows, more bool, w words) [][2]string {
		var out [][2]string
		if hasSources {
			if arrows {
				out = append(out, [2]string{"↑/↓", w.move})
			}
			out = append(out, [2]string{"space", w.toggle})
		}
		out = append(out, [2]string{"e", w.edit})
		if hasAlt && more {
			out = append(out, [2]string{"t", w.title})
		}
		if canSave && more {
			out = append(out, [2]string{"r", w.save})
		}
		return append(out, [2]string{"n", w.rename}, [2]string{"enter", w.search}, [2]string{"esc", w.esc})
	}
	return m.keysFor(
		tier(true, true, words{"move", "toggle", "edit search", "other title", "save sources", "rename", "search", "back"}),
		tier(false, true, words{"", "toggle", "edit", "title", "save", "rename", "search", "back"}),
		tier(false, false, words{"", "toggle", "edit", "", "", "rename", "search", "back"}),
		tier(false, false, words{"", "", "", "", "", "rename", "", ""}),
		tier(false, false, words{}),
		[][2]string{{"enter", ""}, {"esc", ""}})
}
