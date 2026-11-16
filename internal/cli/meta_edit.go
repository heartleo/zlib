package cli

import (
	"strings"

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
)

// search starts a fresh lookup for m.query on a fresh channel, dropping the
// answer of any lookup the page already left.
func (m metaPageModel) search() (tea.Model, tea.Cmd) {
	m.stop()
	m.gen++
	m.stage, m.err, m.res = metaLooking, nil, metasource.Result{}
	m.run = newMetaRun()
	return m, tea.Batch(m.spinner.Tick, m.start())
}

// startQueryEdit opens the editor on the search: title, authors and ISBNs, as
// they are searched now. Nothing is written to the file by changing them.
func (m metaPageModel) startQueryEdit() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.editor, cmd = newMetaEditor("Edit the search", []string{"Title", "Authors", "ISBN"},
		[]string{m.query.Title, strings.Join(m.query.Authors, " & "), strings.Join(m.query.ISBNs, ", ")}, m.width)
	m.editor.help = "Authors: separate with &. ISBNs: separate with commas. The file is not changed."
	m.editing, m.editKind, m.message = true, editQuery, ""
	return m, cmd
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
	m.query, m.editing = q, false
	return m.search()
}

// startValueEdit opens the editor on the focused row, filled with the value
// the catalogue proposed (or the one typed before). A row that cannot be edited
// says so; on a button the key does nothing.
func (m metaPageModel) startValueEdit() (tea.Model, tea.Cmd) {
	if m.focus >= len(m.plan) {
		return m, nil
	}
	ch := m.plan[m.focus]
	label := metaFieldLabel(ch.Field)
	if !ch.Editable() {
		m.message = label + " cannot be edited here"
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = newMetaEditor("Edit "+label, []string{label}, []string{ch.New}, m.width)
	m.editor.help = metaEditHelp(ch.Field)
	m.editing, m.editKind, m.editRow, m.message = true, editValue, m.focus, ""
	return m, cmd
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
		if m.editKind == editQuery {
			return m.acceptQueryEdit()
		}
		return m.acceptValueEdit(), nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.update(msg)
	return m, cmd
}

// acceptValueEdit replaces the row with the edited one, or keeps the editor
// open with the reason the text was refused.
func (m metaPageModel) acceptValueEdit() metaPageModel {
	ch, err := m.plan[m.editRow].Edit(m.editor.values()[0])
	if err != nil {
		m.editor.err = err.Error()
		return m
	}
	m.plan[m.editRow] = ch
	m.selected[m.editRow] = true
	m.editing = false
	m.message = "Edited " + strings.ToLower(metaFieldLabel(ch.Field))
	return m
}

// metaEditor is the text entry the metadata page opens in place: one input to
// edit a proposed value, or several to edit what is searched for. While it is
// open it owns the keyboard, so q, j, k and space type into the field instead
// of acting on the page, and Esc closes only the editor.
type metaEditor struct {
	title  string
	labels []string
	inputs []textinput.Model
	focus  int
	err    string // why the last attempt to accept was refused
	help   string // one line under the inputs
}

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
	for i := range e.inputs {
		e.inputs[i].SetWidth(max(10, width-6))
	}
}

// values are the texts typed so far, one per label.
func (e metaEditor) values() []string {
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
