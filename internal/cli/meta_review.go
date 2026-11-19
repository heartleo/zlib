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
	if stage == metaFields && m.manual {
		return 0, "Edit metadata"
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

// padCell cuts s to w cells and pads it to exactly w, counting wide
// characters as two.
func padCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
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
	if m.compareFits() {
		// The file beside the highlighted match, field for field, before
		// choosing it: what it would add, then both panels.
		var effect []string
		for _, r := range m.candidateEffect(c) {
			effect = append(effect, r[0]+": "+r[1])
		}
		lines = append(lines, "", "  "+colorFaint(ansi.Truncate(strings.Join(effect, " · "), max(1, width-4), "…")))
		cm := m.compareModel()
		return strings.Join(append(lines, cm.benchPanelsWith(cm.compareRows(), m.compareCovers())...), "\n")
	}
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
			{"ctrl+d", "save the sources that are on as the default"}, {"i", "show everything in the file"}, {"m", "edit the file's metadata without a search"},
			{"n / f2", "rename the file"}, {"enter", "search"}}
	case metaCandidates:
		rows = [][2]string{{"↑/↓", "choose a match"}, {"pgup/pgdown", "move a page"}, {"enter", "review its fields"},
			{"e", "edit the search"}, {"s", "change the sources"}, {"n / f2", "rename the file"}}
	case metaFailed:
		rows = [][2]string{{"e", "edit the search"}, {"s", "change the sources"}, {"r", "search again"}, {"n / f2", "rename the file"}}
	case metaFields:
		rows = [][2]string{{"↑/↓", "move between fields"}, {"→ / l", "use the match's value for this field"},
			{"space", "switch between the match's value and the file's"}, {"← / h", "keep the file's value (undo this field)"},
			{"e / enter", "type a value; the description opens a text area"}, {"x", "clear this field"},
			{"a", "use the match for every field it proposes, or for none"}, {"u", "undo every change"},
			{"ctrl+s", "write the changed fields"}, {"n / f2", "rename the file"}}
		if m.manual {
			rows = [][2]string{{"↑/↓", "move between fields"}, {"e / enter", "type a value; the description opens a text area"},
				{"x", "clear this field"}, {"← / h", "undo this field"}, {"u", "undo every change"},
				{"ctrl+s", "write the changed fields"}, {"n / f2", "rename the file"}}
		}
	case metaRename:
		rows = [][2]string{{"↑/↓", "choose where the name comes from"}, {"← / →", "change the format: Title - Author, Author - Title, Title (Author)"},
			{"1 2 3", "pick a format directly"}, {"enter", "rename to the new name"}, {"e", "edit the name first"},
			{"ctrl+d", "save the format as the default"}}
		if m.saveLayout == nil {
			skip["ctrl+d"] = true
		}
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
	m.infoView = viewport.New(viewport.WithWidth(max(1, m.sideNarrow().width)), viewport.WithHeight(max(1, m.height-7)))
	m.infoView.SetContent(m.sideNarrow().infoContent())
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
	m.infoView.SetWidth(max(1, m.sideNarrow().width))
	m.infoView.SetHeight(max(1, m.height-7))
	m.infoView.SetContent(m.sideNarrow().infoContent())
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
