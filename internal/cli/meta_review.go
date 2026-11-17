package cli

import (
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// The page follows three steps: prepare the search, choose a match, review the
// changes. The subtitle says which one the user is on, so the flow is clear
// without reading every page.

// metaStepInfo is the step a stage belongs to, 1 to 3, and its name. Pages
// outside the flow (rename, file details) have step 0.
func (m metaPageModel) metaStepInfo() (int, string) {
	stage := m.stage
	if stage == metaInfo || stage == metaRename {
		if stage == metaRename {
			return 0, "Rename the file"
		}
		return 0, "File details"
	}
	switch stage {
	case metaPrepare:
		return 1, "Prepare the search"
	case metaLooking:
		return 1, "Searching"
	case metaCandidates:
		return 2, "Choose a match"
	case metaFailed:
		return 2, "No match"
	default:
		return 3, "Review changes"
	}
}

// metaSubtitle is the file, its format and the step, in the longest form that
// fits the width.
func (m metaPageModel) metaSubtitle() string {
	file := safeText(filepath.Base(m.path)) + " · " + strings.ToUpper(string(m.format))
	n, name := m.metaStepInfo()
	width := max(20, m.width)
	var forms []string
	if n > 0 {
		forms = []string{"Step " + strconv.Itoa(n) + " of 3: " + name, strconv.Itoa(n) + "/3 " + name, strconv.Itoa(n) + "/3"}
	} else {
		forms = []string{name}
	}
	for _, f := range forms {
		if s := file + "  ·  " + f; ansi.StringWidth(s) <= width {
			return s
		}
	}
	return file
}

// --- Fields page -----------------------------------------------------------

// fieldsLayout says which parts of the fields page fit and how many plan rows it
// shows from which one. It depends only on the size, the plan and the focus,
// so scrolling follows the cursor.
//
// The lines are handed out in order of what matters: the buttons and the
// focused row always, then the match line, the warning, the column titles, the
// other rows (with a line counting those out of sight), the spacers and last
// the preview of the focused row.
type fieldsLayout struct {
	top, rows                   int
	from, warn, columns, spaced bool
	more                        bool // a line counts the rows out of sight
	previewRows                 int
}

func (m metaPageModel) fieldsHeaderLines() []string {
	width := max(20, m.width)
	lines := []string{ansi.Truncate("  "+colorFaint("From ")+safeText(m.cand.Title)+colorFaint(" · "+safeText(strings.Join(m.cand.Sources, ", "))), width, "…")}
	warn := lipgloss.NewStyle().Foreground(currentTheme.Warning)
	switch {
	case m.cand.Match == metasource.MatchISBN && m.cand.Score < 1:
		lines = append(lines, ansi.Truncate("  "+warn.Render("The ISBN matches but the title is different: check this is the right book."), width, "…"))
	case m.cand.Match != metasource.MatchISBN:
		lines = append(lines, ansi.Truncate("  "+warn.Render("Found by title only: check the edition before keeping its ISBN, publisher, date or language."), width, "…"))
	}
	return lines
}

func (m metaPageModel) fieldsLayoutFor(header, buttons int) fieldsLayout {
	n := len(m.plan)
	budget := max(0, m.height-6) - buttons - 1 // what screenView leaves, less the buttons and the focused row
	take := func(k int) bool {
		if budget >= k {
			budget -= k
			return true
		}
		return false
	}
	l := fieldsLayout{rows: 1}
	l.from = take(1)
	// Saying that rows are out of sight matters more than the warning and the
	// column titles; it is given back below when every row fits.
	l.more = n > 1 && take(1)
	l.warn = header > 1 && l.from && take(1)
	l.columns = take(1)
	// Keep room for a preview when there is plenty, so a long value can be read.
	reserve := 0
	if budget-4 >= min(n-1, 3)+2 {
		reserve = 4
		budget -= reserve
	}
	if l.more && n-1 <= budget+1 { // every row fits in what the count would take
		l.more, budget = false, budget+1
	}
	if n-1 <= budget {
		l.rows, budget = n, budget-(n-1)
	} else {
		l.rows, budget = 1+budget, 0
	}
	l.spaced = take(2)
	budget += reserve
	if budget >= 3 {
		l.previewRows = budget - 1 // one blank line before it
	}
	focus := min(m.focus, max(0, n-1))
	l.top = min(max(0, focus-l.rows/2), max(0, n-l.rows))
	return l
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fieldsBody is the review page: the match, a table of the proposed changes
// with what the file holds now beside each, the focused row in full, and the
// Apply and Back buttons.
func (m metaPageModel) fieldsBody() string {
	width := max(20, m.width)
	n := len(m.plan)
	// Button focus is one-based: Apply is plan index n, Back is n+1.
	buttons := actionButtonRows([]string{"Apply", "Back"}, max(0, m.focus-n+1), width)
	if n == 0 {
		buttons = actionButtonRows([]string{"", "Back"}, 2, width)
	}
	header := m.fieldsHeaderLines()
	if n == 0 {
		lines := append(append([]string{}, header...), "", "  "+colorFaint("Nothing to change."), "")
		return strings.Join(append(lines, buttons...), "\n")
	}
	l := m.fieldsLayoutFor(len(header), len(buttons))
	var lines []string
	if l.from {
		lines = append(lines, header[0])
	}
	if l.warn {
		lines = append(lines, header[1])
	}
	if l.spaced && l.from {
		lines = append(lines, "")
	}
	fieldW, oldW, newW := m.fieldColumns(width)
	if l.columns {
		lines = append(lines, colorFaint(ansi.Truncate("      "+padCell("Field", fieldW)+"  "+padCell("In the file", oldW)+"  "+"New", width, "…")))
	}
	for i := l.top; i < min(n, l.top+l.rows); i++ {
		lines = append(lines, m.fieldRow(i, fieldW, oldW, newW, width))
	}
	if l.more {
		above, below := l.top, n-l.top-l.rows
		var parts []string
		if above > 0 {
			parts = append(parts, "↑ "+strconv.Itoa(above)+" more")
		}
		if below > 0 {
			parts = append(parts, "↓ "+strconv.Itoa(below)+" more")
		}
		lines = append(lines, "  "+colorFaint(strings.Join(parts, "  ")))
	}
	if preview := m.fieldPreview(l.previewRows, width); len(preview) > 0 {
		lines = append(lines, "")
		lines = append(lines, preview...)
	}
	if l.spaced {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, buttons...), "\n")
}

// fieldColumns splits the width between the field name, the value in the file
// and the new value, after the longest notes, so a long note such as
// "(replaces, may be another book)" does not squeeze the new value away. The
// new value gets the larger share: it is what would be written.
func (m metaPageModel) fieldColumns(width int) (fieldW, oldW, newW int) {
	fieldW, noteW := 5, 0
	for _, ch := range m.plan {
		fieldW = max(fieldW, ansi.StringWidth(metaFieldLabel(ch.Field)))
		noteW = max(noteW, ansi.StringWidth(fieldNote(ch)))
	}
	fieldW = min(fieldW, 14)
	// "> [x] " is six cells, and two spaces separate each column.
	rest := max(10, width-6-fieldW-4-min(noteW, width/3))
	oldW = max(5, rest*2/5)
	newW = max(5, rest-oldW)
	return fieldW, oldW, newW
}

// fieldNote is the notes after a row's new value, such as "  (replaces)".
func fieldNote(ch metasource.Change) string {
	var notes []string
	if !ch.Fill {
		notes = append(notes, "replaces")
	}
	if ch.Note != "" {
		notes = append(notes, ch.Note)
	}
	if len(notes) == 0 {
		return ""
	}
	return "  (" + strings.Join(notes, ", ") + ")"
}

// fieldRow is one proposed change: the checkbox, the field, the value in the
// file and the new one, then the notes. A field the change adds is coloured
// apart from one it replaces, and the notes say the same in words.
func (m metaPageModel) fieldRow(i, fieldW, oldW, newW, width int) string {
	ch := m.plan[i]
	box := "[ ]"
	if m.selected[i] {
		box = "[x]"
	}
	old := safeText(oneLine(ch.Old))
	if old == "" {
		old = "(empty)"
	}
	note := fieldNote(ch)
	value := safeText(oneLine(ch.New))
	// The new value gives way to the notes, which say what selecting it does.
	room := newW
	if extra := 6 + fieldW + 2 + oldW + 2 + newW + ansi.StringWidth(note); extra > width {
		room = max(5, newW-(extra-width))
	}
	name := padCell(metaFieldLabel(ch.Field), fieldW)
	if m.focus != i { // a selected row is drawn in one style; colour the rest
		if ch.Fill {
			name = colorGreen(name)
		} else {
			name = lipgloss.NewStyle().Foreground(currentTheme.Warning).Render(name)
		}
	}
	label := box + " " + name + "  " + padCell(old, oldW) + "  " + ansi.Truncate(value, room, "…") + note
	return styledListRow(label, m.focus == i, width, currentTheme)
}

// padCell cuts s to w cells and pads it to exactly w, counting wide
// characters as two.
func padCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// fieldPreview shows the focused row in full: what the file holds and what
// would be written, each wrapped, within rows lines.
func (m metaPageModel) fieldPreview(rows, width int) []string {
	if rows < 3 || len(m.plan) == 0 {
		return nil
	}
	ch := m.plan[min(m.focus, len(m.plan)-1)]
	state := "will be written"
	if i := min(m.focus, len(m.plan)-1); !m.selected[i] {
		state = "not selected"
	}
	out := []string{sectionHeader(metaFieldLabel(ch.Field), state, width)}
	each := max(1, (rows-1)/2)
	add := func(label, value string) {
		if value == "" {
			value = "(empty)"
		}
		wrapped := strings.Split(ansi.Wrap(safeText(oneLine(value)), max(10, width-16), ""), "\n")
		if len(wrapped) > each {
			wrapped = append(wrapped[:each-1], ansi.Truncate(wrapped[each-1], max(1, width-17), "…")+" …")
		}
		for j, w := range wrapped {
			prefix := "  " + colorFaint(padCell(label, 12)) + "  "
			if j > 0 {
				prefix = strings.Repeat(" ", 16)
			}
			out = append(out, ansi.Truncate(prefix+w, width, "…"))
		}
	}
	add("In the file", ch.Old)
	add("New", ch.New)
	return out[:min(len(out), rows)]
}

// fieldsStatus says how many changes are selected and how many replace a
// value, then which fields the format cannot store, or the last message.
func (m metaPageModel) fieldsStatus() string {
	var parts []string
	if m.message != "" {
		parts = append(parts, m.message)
	} else if n := len(m.plan); n > 0 {
		chosen, replacing := 0, 0
		for i, ch := range m.plan {
			if m.selected[i] {
				chosen++
				if !ch.Fill {
					replacing++
				}
			}
		}
		s := strconv.Itoa(chosen) + " of " + strconv.Itoa(n) + " selected"
		if replacing > 0 {
			s += ", " + strconv.Itoa(replacing) + " replacing a value"
		}
		parts = append(parts, s)
	}
	if len(m.hidden) > 0 {
		parts = append(parts, strings.ToUpper(string(m.format))+" files cannot store: "+strings.Join(m.hidden, ", "))
	}
	return strings.Join(parts, " · ")
}

// toggleAll selects every row, or none when every row is selected already.
func (m metaPageModel) toggleAll() metaPageModel {
	all := true
	for _, s := range m.selected {
		all = all && s
	}
	for i := range m.selected {
		m.selected[i] = !all
	}
	m.message = "" // the status line counts the selection
	return m
}

// --- Results page ------------------------------------------------------------

// candidateColumns sizes the title and author columns to what the visible rows
// need, within a share of the width each.
func (m metaPageModel) candidateColumns(width int) (titleW, authorW int) {
	for _, c := range m.res.Candidates {
		titleW = max(titleW, ansi.StringWidth(safeText(c.Title)))
		authorW = max(authorW, ansi.StringWidth(safeText(strings.Join(c.Authors, " & "))))
	}
	rest := max(20, width-2-5-1-4-6) // cursor, marker, year and the gaps
	titleW = min(max(5, titleW), max(10, rest*45/100))
	if authorW > 0 { // a column is never narrower than its title
		authorW = max(len("Authors"), min(authorW, max(8, rest*28/100)))
	}
	return titleW, authorW
}

func (m metaPageModel) candidatesBody() string {
	width := max(20, m.width)
	rows := m.listRows()
	end := min(len(m.res.Candidates), m.top+rows)
	titleW, authorW := m.candidateColumns(width)
	head := "  " + padCell("Match", 5) + " " + padCell("Title", titleW)
	if authorW > 0 {
		head += "  " + padCell("Authors", authorW)
	}
	head += "  Year  Sources"
	lines := []string{colorFaint(ansi.Truncate(head, width, "…"))}
	for i := m.top; i < end; i++ {
		c := m.res.Candidates[i]
		label := metaScoreCell(c) + " " + padCell(safeText(c.Title), titleW)
		if authorW > 0 {
			label += "  " + padCell(safeText(strings.Join(c.Authors, " & ")), authorW)
		}
		year := "    "
		if y := safeText(c.Published); y != "" {
			year = padCell(ansi.Truncate(y, 4, ""), 4)
		}
		label += "  " + year
		// Where the match came from, so an entry from one catalogue is not
		// taken for a merge of several.
		if len(c.Sources) > 0 {
			label += "  [" + safeText(strings.Join(c.Sources, "+")) + "]"
		}
		lines = append(lines, styledListRow(label, i == m.cursor, width, currentTheme))
	}
	if more := len(m.res.Candidates) - rows; more > 0 {
		lines = append(lines, "  "+colorFaint(strconv.Itoa(m.cursor+1)+" of "+strconv.Itoa(len(m.res.Candidates))))
	}
	c := m.res.Candidates[m.cursor]
	lines = append(lines, "", sectionHeader("Details", "", width))
	for _, l := range formatDetailRows(append(m.candidateEffect(c), metaDetailRows(c)...), "  ", colorFaint) {
		lines = append(lines, ansi.Truncate(l, width, "…"))
	}
	return strings.Join(lines, "\n")
}

// candidateEffect says what choosing c would do to the file, before the fields
// are opened: the empty fields it fills and the values it differs on, which
// start unselected. It is what metasource.Plan proposes, less what the format
// cannot store.
func (m metaPageModel) candidateEffect(c metasource.Candidate) [][2]string {
	var adds, offers, differs, unstorable []string
	for _, ch := range metasource.Plan(m.current, c) {
		if ch.Field == metasource.FieldCover && m.cover == nil {
			continue
		}
		if !m.canOffer(ch) {
			unstorable = append(unstorable, metaFieldLabel(ch.Field))
			continue
		}
		switch {
		case ch.DefaultSelected():
			adds = append(adds, metaFieldLabel(ch.Field))
		case ch.Fill:
			offers = append(offers, metaFieldLabel(ch.Field))
		default:
			differs = append(differs, metaFieldLabel(ch.Field))
		}
	}
	var rows [][2]string
	if len(adds)+len(offers)+len(differs) == 0 {
		if len(unstorable) == 0 {
			return [][2]string{{"Would change", "nothing: the file already has these values"}}
		}
		rows = append(rows, [2]string{"Would change", "nothing this file can store"})
	}
	if len(adds) > 0 {
		rows = append(rows, [2]string{"Would add", strings.Join(adds, ", ")})
	}
	if len(offers) > 0 {
		rows = append(rows, [2]string{"Could add", strings.Join(offers, ", ") + " (check first: not selected)"})
	}
	if len(differs) > 0 {
		rows = append(rows, [2]string{"Differs on", strings.Join(differs, ", ") + " (kept unless you select them)"})
	}
	if len(unstorable) > 0 {
		rows = append(rows, [2]string{"Cannot store", strings.Join(unstorable, ", ") + " (" + strings.ToUpper(string(m.format)) + " has no place for them)"})
	}
	return rows
}

// withHelpKey adds a bare ? before the last hint (esc) of every tier that does
// not name it, so the key that lists every other key is always on screen.
func withHelpKey(tiers ...[][2]string) [][][2]string {
	out := make([][][2]string, len(tiers))
	for i, t := range tiers {
		has := false
		for _, kd := range t {
			has = has || kd[0] == "?"
		}
		if has || len(t) == 0 {
			out[i] = t
			continue
		}
		withQ := append([][2]string{}, t[:len(t)-1]...)
		out[i] = append(withQ, [2]string{"?", ""}, t[len(t)-1])
	}
	return out
}

// --- Key help ------------------------------------------------------------------

// metaHelpRows lists every key of the stage the help was opened on, with what
// it does. The footer shows the ones that fit; this shows all of them.
func (m metaPageModel) metaHelpRows() [][2]string {
	common := [][2]string{{"?", "show or hide this help"}, {"esc", "go back, or leave from the first page"}, {"ctrl+c", "cancel"}}
	// Keys that do nothing on this page are left out, as in the footer.
	skip := map[string]bool{}
	if len(m.draft) == 0 && m.helpStage == metaPrepare {
		skip["↑/↓"], skip["space"], skip["ctrl+d"] = true, true, true
	}
	if m.saveSources == nil {
		skip["ctrl+d"] = true
	}
	if m.extraction.AltTitle == "" {
		skip["t"] = true
	}
	if len(m.sources) == 0 || m.lookupFor == nil {
		skip["s"] = true
	}
	var rows [][2]string
	switch m.helpStage {
	case metaPrepare:
		rows = [][2]string{{"↑/↓", "move between sources"}, {"space", "turn the highlighted source on or off"},
			{"e", "edit the title, authors and ISBN to search for"}, {"t", "use the other title"},
			{"ctrl+d", "save the sources that are on as the default"}, {"i", "show everything in the file"},
			{"n", "rename the file"}, {"enter", "search"}}
	case metaCandidates:
		rows = [][2]string{{"↑/↓", "choose a match"}, {"pgup/pgdown", "move a page"}, {"enter", "review its fields"},
			{"e", "edit the search"}, {"s", "change the sources"}, {"n", "rename the file"}}
	case metaFailed:
		rows = [][2]string{{"e", "edit the search"}, {"s", "change the sources"}, {"r", "search again"}, {"n", "rename the file"}}
	case metaFields:
		rows = [][2]string{{"↑/↓", "move between fields and the buttons"}, {"space", "select or deselect the field"},
			{"a", "select all, or none when all are selected"}, {"e", "edit the new value before it is written"},
			{"enter", "toggle the field, or press the focused button"}, {"ctrl+s", "write the selected fields"}, {"n", "rename the file"}}
	case metaRename:
		rows = [][2]string{{"↑/↓", "choose a name"}, {"enter", "rename to it"}, {"e", "edit the name first"}}
	case metaInfo:
		rows = [][2]string{{"↑/↓", "scroll"}, {"pgup/pgdown", "scroll a page"}}
	}
	var kept [][2]string
	for _, r := range rows {
		if !skip[r[0]] {
			kept = append(kept, r)
		}
	}
	return append(kept, common...)
}

func (m metaPageModel) helpBody() string {
	width := max(20, m.width)
	var lines []string
	for _, l := range formatDetailRows(m.metaHelpRows(), "  ", func(s string) string {
		return lipgloss.NewStyle().Foreground(currentTheme.Accent).Render(s)
	}) {
		lines = append(lines, ansi.Truncate(l, width, "…"))
	}
	return strings.Join(lines, "\n")
}

// openHelp shows every key of the page in a viewport, which scrolls when the
// terminal is too short for the list; ?, esc, q or enter close it.
func (m metaPageModel) openHelp() metaPageModel {
	m.helpOpen, m.helpStage = true, m.stage
	m.helpView = viewport.New(viewport.WithWidth(max(1, m.width)), viewport.WithHeight(max(1, m.height-7)))
	m.helpView.SetContent(m.helpBody())
	return m
}

// helpKey closes the help, or scrolls it.
func (m metaPageModel) helpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc", "q", "enter", "ctrl+c":
		m.helpOpen = false
		return m, nil
	}
	var cmd tea.Cmd
	m.helpView, cmd = m.helpView.Update(msg)
	return m, cmd
}

// helpScreen is the help page: the list, with its position when it scrolls.
func (m metaPageModel) helpScreen(subtitle string) tea.View {
	body := sectionHeader("Keys", "", max(20, m.width)) + "\n" + m.helpView.View()
	if m.helpView.TotalLineCount() > m.helpView.Height() {
		return screenView(m.width, m.height, "Fix metadata", subtitle, body, strconv.Itoa(int(m.helpView.ScrollPercent()*100+0.5))+"%",
			uiKey("↑/↓", "scroll"), uiKey("esc", "close"))
	}
	return screenView(m.width, m.height, "Fix metadata", subtitle, body, "", uiKey("esc", "close"), uiKey("?", "close"))
}

// --- File details --------------------------------------------------------------

// openInfo shows everything in the file in a viewport, so a long description
// scrolls instead of being cut.
func (m metaPageModel) openInfo() metaPageModel {
	m.stage = metaInfo
	m.infoView = viewport.New(viewport.WithWidth(max(1, m.width)), viewport.WithHeight(max(1, m.height-7)))
	m.infoView.SetContent(m.infoContent())
	return m
}

func (m metaPageModel) resizeInfo() metaPageModel {
	if m.helpOpen {
		m.helpView.SetWidth(max(1, m.width))
		m.helpView.SetHeight(max(1, m.height-7))
		m.helpView.SetContent(m.helpBody())
	}
	if m.stage != metaInfo {
		return m
	}
	m.infoView.SetWidth(max(1, m.width))
	m.infoView.SetHeight(max(1, m.height-7))
	m.infoView.SetContent(m.infoContent())
	return m
}

func (m metaPageModel) infoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "q", "i", "enter":
		m.stage = metaPrepare
		return m, nil
	}
	var cmd tea.Cmd
	m.infoView, cmd = m.infoView.Update(msg)
	return m, cmd
}

func (m metaPageModel) infoScrollable() bool {
	return m.infoView.TotalLineCount() > m.infoView.Height()
}

func (m metaPageModel) infoStatus() string {
	if !m.infoScrollable() {
		return ""
	}
	return strconv.Itoa(int(m.infoView.ScrollPercent()*100+0.5)) + "%"
}
