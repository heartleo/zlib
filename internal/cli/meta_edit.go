package cli

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// metaEditKind says what the open editor is editing.
type metaEditKind int

const (
	editValue metaEditKind = iota // a proposed value on the fields page
	editQuery                     // what is searched for
	editName                      // the file's new name
)

// search starts a fresh lookup for m.query on a fresh channel, dropping the
// answer of any lookup the page already left.
func (m metaPageModel) search() (tea.Model, tea.Cmd) {
	if m.query.Empty() {
		// Nothing to send: ask what to search for instead.
		return m.startQueryEdit()
	}
	m.stop()
	m.remember()
	m.gen++
	m.stage, m.err, m.res = metaLooking, nil, metasource.Result{}
	m.run = newMetaRun()
	return m, tea.Batch(m.spinner.Tick, m.start())
}

// metaSnapshot is the page as it was before a repeat search began: the stage
// and results, and the query, lookup and sources that produced them.
type metaSnapshot struct {
	stage       metaStage
	res         metasource.Result
	err         error
	query       metasource.Query
	extraction  metasource.Extraction
	cursor, top int
	lookup      metaLookupFunc
	sources     []metaSourceChoice
	draft       []metaSourceChoice
	// outer is the snapshot this one was taken over: a prepare page opened from
	// the results holds the results here, so Esc steps back one page at a time.
	outer *metaSnapshot
}

// remember keeps the page as it is so Esc can come back to it: the results, or
// the prepare page a search started from. Callers do it before they change the
// query or the sources; a snapshot already taken is kept. The prepare page
// itself is never remembered by an edit, only by the search that leaves it.
func (m *metaPageModel) remember() {
	if m.prev != nil || (m.stage != metaCandidates && m.stage != metaFailed && m.stage != metaPrepare) {
		return
	}
	m.prev = m.snapshot()
}

// snapshot is the page as it is now, over whatever snapshot is already kept.
func (m *metaPageModel) snapshot() *metaSnapshot {
	return &metaSnapshot{stage: m.stage, res: m.res, err: m.err, query: m.query, extraction: m.extraction, cursor: m.cursor, top: m.top,
		lookup: m.lookup, sources: m.sources, draft: append([]metaSourceChoice(nil), m.draft...), outer: m.prev}
}

// abandonSearch stops a search the user gave up on, or leaves the prepare page
// opened from the results, and puts back the page it replaced. A late answer to
// the search is dropped: the generation moves on.
func (m metaPageModel) abandonSearch() metaPageModel {
	p := m.prev
	m.stop()
	m.gen++
	m.stage, m.res, m.err, m.query, m.extraction, m.cursor, m.top, m.lookup, m.sources, m.draft =
		p.stage, p.res, p.err, p.query, p.extraction, p.cursor, p.top, p.lookup, p.sources, p.draft
	m.prev, m.run, m.message = p.outer, newMetaRun(), ""
	return m
}

// adoptQuery makes q the search and updates where each part came from: a
// title or authors the user changed are theirs, and an ISBN they added is typed.
func (m *metaPageModel) adoptQuery(q metasource.Query) {
	old, x := m.query, m.extraction
	if q.Title != old.Title {
		x.TitleFrom = "edited"
	}
	if !slices.Equal(q.Authors, old.Authors) {
		x.AuthorsFrom = "edited"
	}
	from := make([]string, len(q.ISBNs))
	for i, isbn := range q.ISBNs {
		if j := slices.Index(old.ISBNs, isbn); j >= 0 && j < len(x.ISBNFrom) {
			from[i] = x.ISBNFrom[j]
		} else {
			from[i] = "typed"
		}
	}
	x.ISBNFrom, x.Query = from, q
	m.query, m.extraction = q, x
}

// startQueryEdit opens the editor on the search: title, authors and ISBNs, as
// they are searched now. Nothing is written to the file by changing them.
func (m metaPageModel) startQueryEdit() (tea.Model, tea.Cmd) {
	cmd := m.beginQueryEdit()
	return m, cmd
}

// beginQueryEdit opens the search editor on m and returns its first command.
func (m *metaPageModel) beginQueryEdit() tea.Cmd {
	var cmd tea.Cmd
	m.editor, cmd = newMetaEditor("Edit the search", []string{"Title", "Authors", "ISBN"},
		[]string{m.query.Title, strings.Join(m.query.Authors, " & "), strings.Join(m.query.ISBNs, ", ")}, m.width)
	m.editor.help = "Authors: separate with &. ISBNs: separate with commas. The file is not changed."
	m.editing, m.editKind, m.message = true, editQuery, ""
	return cmd
}

// acceptQueryEdit searches again with the edited terms, or keeps the editor
// open with the reason they cannot be searched.
func (m metaPageModel) acceptQueryEdit() (tea.Model, tea.Cmd) {
	v := m.editor.values()
	q, err := metasource.ParseQueryEdit(m.query, v[0], v[1], v[2])
	if err != nil {
		m.editor.err = err.Error()
		return m, nil
	}
	m.editing = false
	if m.stage == metaPrepare {
		// On the prepare page the terms are kept; Enter there starts the search.
		m.adoptQuery(q)
		return m, nil
	}
	m.remember() // before the query changes
	m.adoptQuery(q)
	return m.search()
}

// metaEditHelp says how the value of a field is written.
func metaEditHelp(f metasource.Field) string {
	switch f {
	case metasource.FieldAuthors:
		return "Separate authors with &"
	case metasource.FieldLanguages:
		return "Language codes separated by commas, such as en, zh"
	case metasource.FieldTags:
		return "Separate tags with commas"
	case metasource.FieldPublished:
		return "A year, 2008-01 or 2008-01-05"
	case "identifier:isbn":
		return "One ISBN-10 or ISBN-13"
	}
	return ""
}

// editKey handles a key while an editor is open. Enter accepts, Esc and
// Ctrl+C close only the editor, and everything else goes to the input.
func (m metaPageModel) editKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.editing = false
		return m, nil
	case "enter", "ctrl+s":
		// In the description's text area Enter starts a new line; Ctrl+S keeps it.
		if m.editor.multiline() && msg.String() == "enter" {
			break
		}
		switch m.editKind {
		case editQuery:
			return m.acceptQueryEdit()
		case editName:
			return m.acceptNameEdit()
		}
		return m.acceptBenchEdit(), nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.update(msg)
	return m, cmd
}

// metaEditor is the text entry the metadata page opens in place: one input to
// edit a proposed value, or several to edit what is searched for. While it is
// open it owns the keyboard, so q, j, k and space type into the field instead
// of acting on the page, and Esc closes only the editor.
type metaEditor struct {
	title  string
	labels []string
	inputs []textinput.Model
	// area replaces the inputs for a text of several lines, the description.
	area  *textarea.Model
	focus int
	err   string // why the last attempt to accept was refused
	help  string // one line under the inputs
}

// newMetaTextEditor opens a text area for a value of several lines.
func newMetaTextEditor(title, value string, width, height int) (metaEditor, tea.Cmd) {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = "│ "
	a.CharLimit = 0
	a.MaxHeight = 0
	a.SetWidth(max(10, width-6))
	a.SetHeight(max(3, height))
	a.SetValue(value)
	e := metaEditor{title: title, labels: []string{title}, area: &a}
	return e, a.Focus()
}

func (e metaEditor) multiline() bool { return e.area != nil }

// newMetaEditor opens an editor with one input per label, filled with values.
// The first input has the cursor.
func newMetaEditor(title string, labels, values []string, width int) (metaEditor, tea.Cmd) {
	e := metaEditor{title: title, labels: labels}
	for i := range labels {
		in := textinput.New()
		in.Prompt = "> "
		in.KeyMap.Paste.SetKeys("ctrl+v", "ctrl+shift+v", "shift+insert")
		in.SetWidth(max(10, width-6))
		if i < len(values) {
			in.SetValue(values[i])
		}
		in.CursorEnd()
		e.inputs = append(e.inputs, in)
	}
	return e, e.refocus()
}

// refocus gives the cursor to the focused input and takes it from the rest.
func (e *metaEditor) refocus() tea.Cmd {
	var cmd tea.Cmd
	for i := range e.inputs {
		if i == e.focus {
			cmd = e.inputs[i].Focus()
		} else {
			e.inputs[i].Blur()
		}
	}
	return cmd
}

func (e *metaEditor) resize(width int) {
	if e.area != nil {
		e.area.SetWidth(max(10, width-6))
	}
	for i := range e.inputs {
		e.inputs[i].SetWidth(max(10, width-6))
	}
}

// values are the texts typed so far, one per label.
func (e metaEditor) values() []string {
	if e.area != nil {
		return []string{e.area.Value()}
	}
	out := make([]string, len(e.inputs))
	for i, in := range e.inputs {
		out[i] = in.Value()
	}
	return out
}

// update moves between inputs with Tab and Shift+Tab and gives everything else
// to the focused input: typing, editing keys, paste and the cursor blink. A
// change clears the last refusal, since it no longer describes the text.
func (e metaEditor) update(msg tea.Msg) (metaEditor, tea.Cmd) {
	if e.area != nil {
		before := e.area.Value()
		a, cmd := e.area.Update(msg)
		e.area = &a
		if a.Value() != before {
			e.err = ""
		}
		return e, cmd
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && len(e.inputs) > 1 {
		switch k.String() {
		case "tab", "down":
			e.focus = (e.focus + 1) % len(e.inputs)
			return e, e.refocus()
		case "shift+tab", "up":
			e.focus = (e.focus + len(e.inputs) - 1) % len(e.inputs)
			return e, e.refocus()
		}
	}
	before := e.inputs[e.focus].Value()
	var cmd tea.Cmd
	e.inputs[e.focus], cmd = e.inputs[e.focus].Update(msg)
	if e.inputs[e.focus].Value() != before {
		e.err = ""
	}
	return e, cmd
}

// view draws the title, the inputs with their labels, the refusal if any and
// the help line, each cut to the width.
func (e metaEditor) view(width int) string {
	lines := []string{"  " + colorBold(safeText(e.title))}
	if e.area != nil {
		for _, l := range strings.Split(e.area.View(), "\n") {
			lines = append(lines, "  "+ansi.Truncate(l, max(1, width-2), "…"))
		}
	}
	for i, in := range e.inputs {
		if len(e.labels) > 1 {
			label := e.labels[i]
			if i == e.focus {
				label = colorBold(label)
			} else {
				label = colorFaint(label)
			}
			lines = append(lines, "  "+label)
		}
		lines = append(lines, "  "+ansi.Truncate(in.View(), max(1, width-2), "…"))
	}
	if e.err != "" {
		lines = append(lines, "  "+colorRed(symbolError)+" "+ansi.Truncate(safeText(e.err), max(1, width-6), "…"))
	}
	if e.help != "" {
		lines = append(lines, "  "+colorFaint(ansi.Truncate(e.help, max(1, width-4), "…")))
	}
	return strings.Join(lines, "\n")
}
