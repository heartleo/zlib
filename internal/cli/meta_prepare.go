package cli

import (
	"path/filepath"
	"slices"
	"strings"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// backToPrepare is Esc on the results, or on the page that says nothing was
// found: the step before them, the prepare page, with the terms and the sources
// as they were searched. Enter there searches again. Unlike s, it keeps nothing
// to return to, so Esc on the prepare page leaves, as it does the first time the
// page opens. A page built with no sources to choose from has no prepare page to
// go back to, and Esc leaves.
func (m metaPageModel) backToPrepare() (tea.Model, tea.Cmd) {
	if len(m.sources) == 0 || m.lookupFor == nil {
		m.cancelled = true
		return m, tea.Quit
	}
	m.draft = append([]metaSourceChoice(nil), m.sources...)
	m.stage, m.sourceCursor, m.message, m.prev = metaPrepare, 0, "", nil
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
	case "ctrl+d":
		return m.rememberSources()
	case "n":
		return m.openRename()
	case "i":
		return m.openInfo(), nil
	case "m":
		return m.openManual(), nil
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

// prepareBody is the prepare page, top to bottom: the book as the file has it,
// in a detail card like the search results' (the cover beside the metadata),
// then what will be searched and where each part came from, then the sources to
// ask. On a short terminal the card and the terms give up rows before the
// sources do.
func (m metaPageModel) prepareBody() string {
	width := max(20, m.width)
	lay := m.prepareLayout()
	var lines []string
	if lay.rows > 0 {
		lines = m.prepareCard(width, lay)
	}
	if m.showsFileLine() {
		lines = append(lines, m.fileNameLine("  ", width, m.query.Title, m.query.Authors))
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, m.prepareTerms(width, lay.compactTerms)...)
	if src := m.prepareSources(width); len(src) > 0 {
		lines = append(lines, "")
		lines = append(lines, src...)
	}
	return strings.Join(lines, "\n")
}

// showsFileLine reports whether the page has room for the line with the file's
// name, which short terminals give up first.
func (m metaPageModel) showsFileLine() bool { return m.height >= 16 }

// prepareCardLayout says how the page is divided: how many text rows the card
// has, whether it shows a cover and whether the terms are in their short form.
type prepareCardLayout struct {
	rows         int
	cover        bool
	compactTerms bool
}

// prepareLayout divides the body. The cover (and so the card's full height)
// needs a terminal that draws images and rows for it; without them the card is
// a few text lines.
func (m metaPageModel) prepareLayout() prepareCardLayout {
	width := max(20, m.width)
	budget := max(0, m.height-6)
	sources := len(m.prepareSources(width))
	blanks := 1 // between the card and the terms
	if sources > 0 {
		blanks++
	}
	if m.showsFileLine() {
		blanks++ // the file's name, under the card
	}
	full := len(m.prepareTerms(width, false)) + sources + blanks
	short := len(m.prepareTerms(width, true)) + sources + blanks
	canCover := coverFits(m.width, m.height)
	if canCover && budget-full >= coverRows+2 {
		return prepareCardLayout{rows: coverRows, cover: true}
	}
	if canCover && budget-short >= coverRows+2 {
		return prepareCardLayout{rows: coverRows, cover: true, compactTerms: true}
	}
	if rows := budget - full - 2; rows >= 3 {
		return prepareCardLayout{rows: min(rows, 7)}
	}
	// Too short for a card of three lines: a title line, or no card at all, so the
	// terms and the sources are never what is cut.
	if rows := budget - short - 2; rows >= 1 {
		return prepareCardLayout{rows: min(rows, 7), compactTerms: true}
	}
	return prepareCardLayout{compactTerms: true}
}

// prepareCard is the book detail card: a rounded accent border, the cover
// slot on the left when there is one and the book's metadata beside it.
func (m metaPageModel) prepareCard(width int, lay prepareCardLayout) []string {
	inner := max(1, width-6)
	if lay.cover {
		inner -= coverWidth + 2
	}
	body := strings.Join(m.prepareCardLines(max(1, inner), lay.rows), "\n")
	if lay.cover {
		slot := lipgloss.NewStyle().Width(coverWidth).Height(coverRows)
		var cover string
		if m.fileCover != nil && m.fileCover.img != nil {
			cover = slot.Render(renderBookCover(m.fileCover.img)) // blank: the image is drawn over it
		} else {
			label := "no cover"
			if m.fileCover != nil {
				label = "cover unavailable"
			}
			cover = slot.Foreground(currentTheme.Muted).Align(lipgloss.Center, lipgloss.Center).
				Border(lipgloss.RoundedBorder()).BorderForeground(currentTheme.Muted).Render(label)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, cover, "  ", lipgloss.NewStyle().Width(max(1, inner)).Render(body))
	}
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(currentTheme.Accent).
		Padding(0, 1).Width(width - 2).Render(body)
	return strings.Split(card, "\n")
}

// prepareCardLines are the card's text, cut to at most rows lines, with no blank
// lines: the title and the authors as in the search results' card, then the
// details a reader looks for as labelled rows whose values start in one column,
// then the start of the description. Only fields the file has are shown, so a
// sparse book gives a short card. Values are what the file holds (a placeholder
// title is shown as it is, so the user sees what there is to fix); the page
// below says what will be searched.
func (m metaPageModel) prepareCardLines(inner, rows int) []string {
	accent := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	primary := lipgloss.NewStyle().Foreground(currentTheme.Title)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	c := m.current
	// Every value comes from the file, so it is made safe to print here.
	clean := func(s string) string { return safeText(oneLine(s)) }
	title := clean(c.Title)
	if title == "" {
		title = clean(strings.TrimSuffix(filepath.Base(m.path), bookExt(m.path)))
	}
	var lines []string
	wrapped := strings.Split(ansi.Wrap(title, inner, ""), "\n")
	for i, l := range wrapped[:min(len(wrapped), 2)] {
		if i == 1 && len(wrapped) > 2 {
			l = ansi.Truncate(l, max(1, inner-1), "") + "…"
		}
		lines = append(lines, accent.Render(l))
		if rows < 6 { // a short card has one title line
			break
		}
	}
	if authors := clean(strings.Join(c.Authors, " & ")); authors != "" {
		lines = append(lines, primary.Render(ansi.Truncate(authors, inner, "…")))
	}

	// The labelled rows, in reading order.
	ids := ""
	if isbn := c.Identifier("isbn"); isbn != "" {
		ids = isbn
	}
	var details [][2]string
	add := func(label, value string) {
		if value = clean(value); value != "" {
			details = append(details, [2]string{label, value})
		}
	}
	add("Publisher", c.Publisher)
	add("Published", c.Published)
	add("Language", strings.Join(c.Languages, ", "))
	add("Series", seriesLine(c))
	add("Tags", strings.Join(c.Tags, ", "))
	add("ISBN", ids)
	add("Z-Library ID", c.Identifier(metasource.SchemeZlibID))
	add("Format", strings.ToUpper(string(m.format)))
	desc := clean(metasource.PlainText(c.Description))

	room := max(0, rows-len(lines))
	descRows := 0
	if desc != "" && room >= 2 {
		descRows = 1
	}
	detailRoom := room - descRows
	labelled := len(details) <= detailRoom
	if labelled {
		labelW := 0
		for _, d := range details {
			labelW = max(labelW, ansi.StringWidth(d[0]))
		}
		labelW = min(max(labelW, ansi.StringWidth("About")), 12)
		valueW := max(1, inner-labelW-2)
		for _, d := range details {
			lines = append(lines, muted.Render(padCell(d[0], labelW))+"  "+ansi.Truncate(d[1], valueW, "…"))
		}
		if desc != "" && rows-len(lines) >= 1 {
			wrappedDesc := strings.Split(ansi.Wrap(desc, valueW, ""), "\n")
			n := min(len(wrappedDesc), min(3, rows-len(lines)))
			for i := range n {
				l := wrappedDesc[i]
				if i == n-1 && len(wrappedDesc) > n {
					l = ansi.Truncate(l, max(1, valueW-1), "") + "…"
				}
				label := ""
				if i == 0 {
					label = "About"
				}
				lines = append(lines, muted.Render(padCell(label, labelW))+"  "+l)
			}
		}
	} else if detailRoom > 0 || descRows == 0 {
		// Too many fields for the rows: pack them as values separated by dots,
		// most useful first, instead of dropping whole fields.
		var items []string
		for _, d := range details {
			v := d[1]
			if d[0] == "ISBN" || d[0] == "Z-Library ID" {
				v = d[0] + " " + v
			}
			items = append(items, v)
		}
		for len(items) > 0 && detailRoom > 0 {
			line := items[0]
			items = items[1:]
			for len(items) > 0 && ansi.StringWidth(line+" · "+items[0]) <= inner {
				line += " · " + items[0]
				items = items[1:]
			}
			lines = append(lines, muted.Render(ansi.Truncate(line, inner, "…")))
			detailRoom--
		}
		if desc != "" && rows-len(lines) >= 1 {
			lines = append(lines, muted.Render(ansi.Truncate(desc, inner, "…")))
		}
	}
	if desc == "" && len(metadataRows(m.format, c)) <= 1 { // only the format row
		lines = append(lines, muted.Render("The file has no metadata of its own."))
	}
	return lines[:min(len(lines), rows)]
}

// seriesLine is the series and its position, or "".
func seriesLine(c *ebookmeta.Metadata) string { return metasource.FileValue(c, metasource.FieldSeries) }

// prepareTerms are the search terms and where each part came from, or in short
// form two lines: the terms on one, nothing else.
func (m metaPageModel) prepareTerms(width int, compact bool) []string {
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
	lines := []string{sectionHeader("Search for", "m edit file · i details", width)}
	if q.Empty() {
		return append(lines, "    "+colorFaint(ansi.Truncate("Nothing to search yet. Press e to enter a title, author or ISBN.", max(1, width-4), "…")))
	}
	title, note := q.Title, prepareFrom(x.TitleFrom)
	if title == "" {
		title, note = "(none)", ""
	} else if q.TitleGuessed {
		note += ", a guess"
	}
	authors := strings.Join(q.Authors, " & ")
	if authors == "" {
		authors = "(none)"
	}
	if compact {
		short := safeText(oneLine(title)) + " · " + safeText(oneLine(authors))
		if len(q.ISBNs) > 0 {
			short += " · ISBN " + safeText(q.ISBNs[0])
		}
		return append(lines, "    "+ansi.Truncate(short, max(1, width-4), "…"))
	}
	lines = append(lines, row("Title", title, note))
	if x.AltTitle != "" {
		lines = append(lines, row("Other", x.AltTitle, "press t to use it"))
	}
	lines = append(lines, row("Authors", authors, prepareFrom(x.AuthorsFrom)))
	if len(q.ISBNs) == 0 {
		return lines // a row that only says "none" is a line the card can use
	}
	return append(lines, row("ISBN", isbnLine(q, x.ISBNFrom), ""))
}

// prepareSources are the source rows to turn on or off, and what the choice
// sends where.
func (m metaPageModel) prepareSources(width int) []string {
	if len(m.draft) == 0 {
		return nil
	}
	lines := []string{sectionHeader("Sources", "", width)}
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
	return lines
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

// fileRows are the file's name, as it is now (after a rename), and the metadata
// it holds, as the command prints it, each value on one line. With description,
// the first line of the description is the last row.
func (m metaPageModel) fileRows(description bool) [][2]string {
	rows := [][2]string{{"Name", safeText(strings.TrimSuffix(filepath.Base(m.path), bookExt(m.path)))}}
	for _, r := range metadataRows(m.format, m.current) {
		rows = append(rows, [2]string{r[0], oneLine(r[1])})
	}
	if first, _, _ := strings.Cut(metasource.PlainText(m.current.Description), "\n"); description && strings.TrimSpace(first) != "" {
		rows = append(rows, [2]string{"Description", oneLine(safeText(first))})
	}
	return rows
}

// infoContent is the file details: every value the file holds and the whole
// description, wrapped. The page scrolls it in a viewport.
func (m metaPageModel) infoContent() string {
	width := max(20, m.width)
	var lines []string
	rows := m.fileRows(false)
	for _, l := range formatDetailRows(rows, "    ", colorFaint) {
		lines = append(lines, ansi.Truncate(l, width, "…"))
	}
	if d := strings.TrimSpace(safeText(metasource.PlainText(m.current.Description))); d != "" {
		lines = append(lines, "", "  "+colorBold("Description"))
		var wrapped []string
		for para := range strings.SplitSeq(d, "\n") {
			if para = strings.TrimSpace(para); para != "" {
				wrapped = append(wrapped, strings.Split(ansi.Wrap(para, max(10, width-6), ""), "\n")...)
			}
		}
		for _, l := range wrapped {
			lines = append(lines, "    "+l)
		}
	}
	return strings.Join(lines, "\n")
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
	type words struct{ move, toggle, edit, title, save, rename, info, search, esc, help string }
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
			out = append(out, [2]string{"ctrl+d", w.save})
		}
		out = append(out, [2]string{"n", w.rename})
		if more {
			out = append(out, [2]string{"i", w.info})
		}
		if w.help != "" {
			out = append(out, [2]string{"?", w.help})
		}
		return append(out, [2]string{"enter", w.search}, [2]string{"esc", w.esc})
	}
	return m.keysFor(withHelpKey(
		tier(true, true, words{"move", "toggle", "edit search", "other title", "save sources", "rename", "file info", "search", "back", "keys"}),
		tier(false, true, words{"", "toggle", "edit", "title", "save", "rename", "info", "search", "back", "keys"}),
		tier(false, false, words{"", "toggle", "edit", "", "", "rename", "", "search", "back", ""}),
		tier(false, false, words{"", "", "", "", "", "rename", "", "", "", ""}),
		tier(false, false, words{}),
		[][2]string{{"enter", ""}, {"esc", ""}})...)
}
