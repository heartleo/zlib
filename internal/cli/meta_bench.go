package cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// The fields page is a workbench: every field the file can store, what the file
// holds now and what the chosen match says, side by side. Each field is decided
// on its own: keep the file's value, take the match's, type a new one or clear
// it. Opened from the prepare page without a search, it edits the file's own
// metadata with no match column.

// benchOrigin says where the value a field will be written with comes from.
type benchOrigin int

const (
	originFile    benchOrigin = iota // unchanged: nothing is written
	originMatch                      // the match's value
	originEdited                     // typed by the user
	originCleared                    // emptied by the user
)

func (o benchOrigin) label() string {
	switch o {
	case originMatch:
		return "from the match"
	case originEdited:
		return "edited"
	case originCleared:
		return "cleared"
	}
	return "unchanged"
}

// benchRow is one field of the workbench.
type benchRow struct {
	field metasource.Field
	file  string // what the file holds, "" when empty
	match string // what the match says, "" when it has nothing or there is no match
	plan  int    // index in m.plan of the match's proposal, -1 when it proposes nothing
}

// metaCoverImage is a cover loaded for a preview: its bytes (reused when it is
// written) and the decoded image.
type metaCoverImage struct {
	cover *ebookmeta.Cover
	img   image.Image
	err   error
}

type metaCoverFetched struct {
	url string
	got *metaCoverImage
}

// loadFileCover reads the cover embedded in the file, for the previews. A file
// without one, or with an image Go cannot decode, has none.
func loadFileCover(path string) *metaCoverImage {
	cov, err := ebookmeta.CoverFile(path)
	if err != nil || cov == nil || len(cov.Data) == 0 {
		return nil
	}
	img, err := decodePreview(cov.Data)
	if err != nil {
		return &metaCoverImage{cover: cov, err: err}
	}
	return &metaCoverImage{cover: cov, img: img}
}

// decodePreview decodes a cover for a preview. The bytes come from the file, a
// catalogue or a file the user named, so the size is read first and an image
// too large to hold in memory safely is refused, with the same limits as the
// book list's covers; it is then described by its label only.
func decodePreview(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4_000_000 {
		return nil, errors.New("the cover is too large to preview")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// coverLabel describes a cover in words: its format and size, for terminals
// that cannot draw it and for the field's row.
func coverLabel(c *metaCoverImage, mediaType string) string {
	if c == nil {
		if mediaType == "" {
			return ""
		}
		return mediaType
	}
	kind := ""
	if c.cover != nil {
		kind = strings.ToUpper(strings.TrimPrefix(ebookmeta.SniffImage(c.cover.Data), "image/"))
	}
	if kind == "" {
		kind = mediaType
	}
	if c.img != nil {
		b := c.img.Bounds()
		return kind + " " + strconv.Itoa(b.Dx()) + "×" + strconv.Itoa(b.Dy())
	}
	return kind
}

// benchFields are the fields the file's format can store, in the editor's
// order, then any other identifier the match proposes.
func (m metaPageModel) benchRows() []benchRow {
	var rows []benchRow
	seen := map[metasource.Field]bool{}
	for _, f := range metasource.EditableFields {
		if !ebookmeta.CanStore(m.format, string(f)) {
			continue
		}
		if f == metasource.FieldCover && m.format == ebookmeta.FormatMOBI && m.current.Cover == "" {
			continue // MOBI can only replace a cover it has
		}
		seen[f] = true
		rows = append(rows, m.benchRow(f))
	}
	for _, ch := range m.plan {
		if !seen[ch.Field] {
			seen[ch.Field] = true
			rows = append(rows, m.benchRow(ch.Field))
		}
	}
	// The Z-Library record a download came from, so the panels show it too.
	if f := metasource.Field("identifier:" + metasource.SchemeZlibID); !seen[f] && m.current.Identifier(metasource.SchemeZlibID) != "" {
		rows = append(rows, m.benchRow(f))
	}
	return rows
}

func (m metaPageModel) benchRow(f metasource.Field) benchRow {
	r := benchRow{field: f, file: metasource.FileValue(m.current, f), plan: -1}
	if !m.manual {
		r.match = metasource.MatchValue(m.cand, f)
	}
	for i, ch := range m.plan {
		if ch.Field == f {
			r.plan = i
		}
	}
	if f == metasource.FieldCover {
		r.file = coverLabel(m.fileCover, m.current.Cover)
		if r.match != "" {
			if got := m.coverCache[r.match]; got != nil && got.img != nil {
				r.match = coverLabel(got, "")
			} else if r.plan >= 0 {
				r.match = m.plan[r.plan].New
			}
		}
	}
	return r
}

// shownValue is v as a screen shows it: a description's markup as plain text.
// The value itself, which is what gets compared and written, is unchanged.
func shownValue(f metasource.Field, v string) string {
	if f == metasource.FieldDescription {
		v = metasource.PlainText(v)
	}
	return safeText(oneLine(v))
}

// pending is the value r will be written with and where it comes from.
func (m metaPageModel) pending(r benchRow) (string, benchOrigin) {
	if ch, ok := m.extra[r.field]; ok {
		if ch.New == "" {
			return "", originCleared
		}
		return ch.New, originEdited
	}
	if r.plan >= 0 && r.plan < len(m.selected) && m.selected[r.plan] {
		if m.plan[r.plan].Edited() {
			return m.plan[r.plan].New, originEdited
		}
		return r.matchOr(m.plan[r.plan].New), originMatch
	}
	return r.file, originFile
}

func (r benchRow) matchOr(s string) string {
	if r.field == metasource.FieldCover && r.match != "" {
		return r.match
	}
	return s
}

// userChanged reports whether the user changed anything since the workbench
// opened: a typed or cleared field, or a match value taken or dropped. What the
// match preselected on its own is not the user's change.
func (m metaPageModel) userChanged() bool {
	if len(m.benchChanges()) == 0 {
		return false // nothing would be written: nothing to lose
	}
	if len(m.extra) > 0 {
		return true
	}
	for i, s := range m.selected {
		if i < len(m.baseline) && s != m.baseline[i] {
			return true
		}
	}
	return false
}

// benchChanges counts the fields that would be written.
func (m metaPageModel) benchChanges() []benchRow {
	var out []benchRow
	for _, r := range m.benchRows() {
		if _, o := m.pending(r); o != originFile {
			out = append(out, r)
		}
	}
	return out
}

// openManual opens the workbench on the file's own metadata, with no lookup.
func (m metaPageModel) openManual() metaPageModel {
	m.prev = m.snapshot() // over the results, if this prepare page was opened from them
	m.manual = true
	m.cand = metasource.Candidate{}
	m.plan, m.selected, m.hidden = nil, nil, nil
	m.extra = map[metasource.Field]metasource.Change{}
	m.baseline = nil
	m.stage, m.focus, m.message, m.confirmDiscard = metaFields, 0, "", false
	return m
}

// benchKey handles a key on the workbench.
func (m metaPageModel) benchKey(k string) (tea.Model, tea.Cmd) {
	if m.confirmDiscard {
		switch k {
		case "y", "d":
			m.confirmDiscard = false
			return m.leaveBench(), nil
		case "n", "enter", "esc", "q", "ctrl+c": // Enter keeps editing: the safe default
			m.confirmDiscard = false
		}
		return m, nil
	}
	rows := m.benchRows()
	n := len(rows)
	if n == 0 {
		if k == "esc" || k == "ctrl+c" || k == "q" {
			return m.leaveBench(), nil
		}
		return m, nil
	}
	m.focus = min(max(0, m.focus), n-1)
	r := rows[m.focus]
	switch k {
	case "esc", "ctrl+c", "q":
		if m.userChanged() {
			m.confirmDiscard = true
			return m, nil
		}
		return m.leaveBench(), nil
	case "up", "k", "shift+tab", "down", "j", "tab", "home", "end", "pgup", "pgdown":
		m.focus = listCursor(k, m.focus, n, 5)
		m.message = ""
	case "right", "l", "space", " ":
		if r.plan < 0 {
			m.message = metaFieldLabel(r.field) + ": the match has nothing else to offer"
			if m.manual {
				m.message = "There is no match to take values from: press e to edit"
			}
			return m, nil
		}
		delete(m.extra, r.field)
		if k == "space" || k == " " {
			m.selected[r.plan] = !m.selected[r.plan]
		} else {
			m.selected[r.plan] = true
		}
		m.message = ""
	case "left", "h":
		delete(m.extra, r.field)
		if r.plan >= 0 {
			m.selected[r.plan] = false
		}
		m.message = ""
	case "enter", "e":
		return m.startBenchEdit(r)
	case "x", "delete":
		if scheme, ok := strings.CutPrefix(string(r.field), "identifier:"); ok && scheme == m.current.UniqueScheme {
			m.message = metaFieldLabel(r.field) + " is this EPUB's unique identifier, which it must keep: press e to give it another value"
			return m, nil
		}
		if !metasource.Clearable(r.field) || r.file == "" {
			if r.file == "" {
				m.message = metaFieldLabel(r.field) + " is already empty"
			} else {
				m.message = metaFieldLabel(r.field) + " cannot be cleared"
			}
			return m, nil
		}
		ch, err := metasource.Clear(r.field, r.file)
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		m.extra[r.field] = ch
		if r.plan >= 0 {
			m.selected[r.plan] = false
		}
		m.message = ""
	case "a":
		return m.toggleAll(), nil
	case "u":
		m.extra = map[metasource.Field]metasource.Change{}
		for i := range m.selected {
			m.selected[i] = false
		}
		m.message = "Every field is back to what the file holds"
	case "ctrl+s":
		return m.apply()
	case "n":
		return m.openRename()
	}
	return m, nil
}

// leaveBench goes back to the page that opened the workbench.
func (m metaPageModel) leaveBench() metaPageModel {
	m.message, m.extra = "", nil
	if m.manual {
		m.manual = false
		if m.prev != nil {
			return m.abandonSearch()
		}
		m.stage = metaPrepare
		return m
	}
	m.stage = metaCandidates
	return m
}

// toggleAll takes the match's value for every field it proposes, or for none
// when it is taken for all of them already.
func (m metaPageModel) toggleAll() metaPageModel {
	all := len(m.selected) > 0
	for _, s := range m.selected {
		all = all && s
	}
	for i := range m.selected {
		m.selected[i] = !all
		delete(m.extra, m.plan[i].Field)
	}
	m.message = ""
	return m
}

// startBenchEdit opens the editor on r: one line for most fields, several for
// the description, a path for the cover.
func (m metaPageModel) startBenchEdit(r benchRow) (tea.Model, tea.Cmd) {
	label := metaFieldLabel(r.field)
	value, _ := m.pending(r)
	var cmd tea.Cmd
	switch {
	case r.field == metasource.FieldCover:
		m.editor, cmd = newMetaEditor("Replace the cover", []string{"Image file"}, []string{""}, m.width)
		m.editor.help = "Path to a JPEG, PNG, GIF or WebP image. It is read when you press Enter."
	case r.field == metasource.FieldDescription:
		m.editor, cmd = newMetaTextEditor("Edit the description", value, m.width, max(4, m.height-12))
		m.editor.help = "Enter starts a new line. Ctrl+S keeps the text, Esc cancels."
	case !(metasource.Change{Field: r.field}).Editable():
		m.message = label + " cannot be edited here: take it from the match or leave it"
		return m, nil
	default:
		m.editor, cmd = newMetaEditor("Edit "+label, []string{label}, []string{value}, m.width)
		m.editor.help = metaEditHelp(r.field)
	}
	m.editing, m.editKind, m.editField, m.message = true, editValue, r.field, ""
	return m, cmd
}

// acceptBenchEdit keeps what the user typed for the field being edited, or
// leaves the editor open with the reason it cannot.
func (m metaPageModel) acceptBenchEdit() metaPageModel {
	var r benchRow
	for _, row := range m.benchRows() {
		if row.field == m.editField {
			r = row
		}
	}
	text := m.editor.values()[0]
	if r.field == metasource.FieldCover {
		p, err := expandTilde(strings.TrimSpace(text))
		if err != nil || p == "" {
			m.editor.err = "Enter the path of an image file"
			return m
		}
		data, err := readLimited(p, 64<<20)
		if err != nil {
			m.editor.err = "Cannot read the file: " + renameErrorText(err)
			return m
		}
		if ebookmeta.SniffImage(data) == "" {
			m.editor.err = "That is not a JPEG, PNG, GIF or WebP image"
			return m
		}
		cov := &ebookmeta.Cover{Data: data}
		got := &metaCoverImage{cover: cov}
		if img, err := decodePreview(data); err == nil {
			got.img = img
		}
		m.coverEdit = got
		m.extra[r.field] = metasource.ChangeFromCover(r.file, coverLabel(got, "")+" from "+filepath.Base(p), cov)
	} else {
		// Kept as the file has it, even in a form the editor would not accept
		// typed (a date with a time, say): that is no edit, and it also drops a
		// match value taken for the field.
		if strings.TrimSpace(text) == strings.TrimSpace(r.file) {
			return m.keepFileValue(r)
		}
		ch, err := (metasource.Change{Field: r.field, Old: r.file}).Edit(text)
		if err != nil {
			m.editor.err = err.Error()
			return m
		}
		if before, _ := (metasource.Change{Field: r.field, Old: r.file}).Edit(r.file); before.New != "" && ch.New == before.New {
			return m.keepFileValue(r)
		}
		m.extra[r.field] = ch
	}
	if r.plan >= 0 {
		m.selected[r.plan] = false
	}
	m.editing = false
	m.message = "Edited " + strings.ToLower(metaFieldLabel(r.field))
	return m
}

// keepFileValue closes the editor with r back at the file's value.
func (m metaPageModel) keepFileValue(r benchRow) metaPageModel {
	delete(m.extra, r.field)
	if r.plan >= 0 {
		m.selected[r.plan] = false
	}
	m.editing = false
	m.message = metaFieldLabel(r.field) + " is unchanged"
	return m
}

// apply writes every field that differs from the file, in the background. A
// cover taken from the match is the image the preview downloaded, or is
// downloaded now; if that fails nothing is written.
func (m metaPageModel) apply() (tea.Model, tea.Cmd) {
	var chosen []metasource.Change
	coverURL := ""
	var coverBytes *ebookmeta.Cover
	m.applied = m.applied[:0:0]
	for _, r := range m.benchRows() {
		_, origin := m.pending(r)
		switch {
		case origin == originFile:
			continue
		case m.extra != nil && origin != originMatch && hasExtra(m.extra, r.field):
			chosen = append(chosen, m.extra[r.field])
		default:
			ch := m.plan[r.plan]
			chosen = append(chosen, ch)
			if ch.Field == metasource.FieldCover {
				coverURL = ch.CoverURL()
				if got := m.coverCache[coverURL]; got != nil && got.cover != nil {
					coverBytes = got.cover
				}
			}
		}
		m.applied = append(m.applied, string(r.field))
	}
	changes := metasource.BuildChanges(chosen, func(metasource.Change) bool { return true })
	// A new title or authors would clear the sort form the file has (ebookmeta
	// drops a sort value that no longer matches). Keep it unless the user
	// decided the sort field too: the workbench promises a value changes only
	// when it is taken or typed.
	if changes != nil {
		if changes.Title != nil && changes.TitleSort == nil && m.current.TitleSort != "" {
			v := m.current.TitleSort
			changes.TitleSort = &v
		}
		if changes.Authors != nil && changes.AuthorSort == nil && m.current.AuthorSort != "" {
			v := m.current.AuthorSort
			changes.AuthorSort = &v
		}
	}
	if changes == nil && coverURL == "" {
		m.applied = nil
		m.message = "Nothing to save: every field is as the file has it"
		return m, nil
	}
	m.stage, m.message = metaWriting, ""
	write, fetch, path := m.write, m.cover, m.path
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		if coverURL != "" {
			cov := coverBytes
			if cov == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				var err error
				if cov, err = fetch(ctx, coverURL); err != nil {
					return metaWriteDone{err: metaCoverError{err}}
				}
			}
			if changes == nil {
				changes = &ebookmeta.Changes{}
			}
			changes.Cover = cov
		}
		return metaWriteDone{err: write(path, changes)}
	})
}

func hasExtra(extra map[metasource.Field]metasource.Change, f metasource.Field) bool {
	_, ok := extra[f]
	return ok
}

// coverState says what a cover place shows.
type coverState int

const (
	coverNone        coverState = iota // the book has no cover
	coverLoading                       // being downloaded
	coverUnavailable                   // the image could not be fetched or read
	coverLoaded                        // an image, drawn over a blank block
)

func (s coverState) label() string {
	switch s {
	case coverLoading:
		return "loading…"
	case coverUnavailable:
		return "cover unavailable"
	}
	return "no cover"
}

// leftCoverState is the state of the file's cover place: the cover the file
// will have, which is the user's or the match's once taken.
func (m metaPageModel) leftCoverState() coverState {
	switch {
	case m.benchLeftCover() != nil:
		return coverLoaded
	case m.fileCover != nil:
		return coverUnavailable
	}
	return coverNone
}

// matchCoverState is the state of the match's cover place.
func (m metaPageModel) matchCoverState() coverState {
	url := m.cand.CoverURL
	if url == "" {
		return coverNone
	}
	got := m.coverCache[url]
	switch {
	case got != nil && got.img != nil:
		return coverLoaded
	case got != nil && (got.err != nil || got.cover != nil): // fetched, but not readable
		return coverUnavailable
	case got == nil && m.cover == nil:
		return coverNone // nothing can fetch it
	}
	return coverLoading
}

// coverBlock is the cover place as benchCoverRows lines of benchCoverCols
// cells: blank where the image is drawn over it, else a bordered label.
func (m metaPageModel) coverBlock(state coverState) []string {
	var block string
	slot := lipgloss.NewStyle().Width(benchCoverCols).Height(benchCoverRows)
	if state == coverLoaded {
		block = slot.Render("")
	} else {
		block = slot.Foreground(currentTheme.Muted).Align(lipgloss.Center, lipgloss.Center).
			Border(lipgloss.RoundedBorder()).BorderForeground(currentTheme.Muted).Render(state.label())
	}
	lines := strings.Split(block, "\n")
	// A bordered slot is two cells larger than asked; keep the place's size.
	if len(lines) > benchCoverRows {
		lines = lines[:benchCoverRows]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, benchCoverCols, "") + strings.Repeat(" ", max(0, benchCoverCols-ansi.StringWidth(l)))
	}
	return lines
}

// coverNeededCmd asks for the cover of the match on show, when the page is
// where it is drawn and has not asked yet: after a resize, when the terminal
// has just proved it can draw images.
func (m metaPageModel) coverNeededCmd() tea.Cmd {
	switch m.stage {
	case metaCandidates:
		if m.compareFits() && m.cursor < len(m.res.Candidates) {
			return m.matchCoverCmd(m.res.Candidates[m.cursor].CoverURL)
		}
	case metaFields:
		if !m.manual {
			return m.matchCoverCmd(m.cand.CoverURL)
		}
	}
	return nil
}

// matchCoverCmd downloads the match's cover for the preview, once per address,
// when the terminal can draw it. The bytes are reused if the cover is written.
func (m metaPageModel) matchCoverCmd(url string) tea.Cmd {
	if url == "" || m.cover == nil || !coverFits(m.width, m.height) {
		return nil
	}
	if _, asked := m.coverCache[url]; asked {
		return nil
	}
	m.coverCache[url] = &metaCoverImage{} // asked; filled in when it arrives
	fetch := m.cover
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cov, err := fetch(ctx, url)
		got := &metaCoverImage{cover: cov, err: err}
		if err == nil && cov != nil {
			if img, derr := decodePreview(cov.Data); derr == nil {
				got.img = img
			}
		}
		return metaCoverFetched{url: url, got: got}
	}
}

// --- Layout ------------------------------------------------------------------

const (
	benchCoverCols = 16
	benchCoverRows = 9
	benchLabelW    = 12 // the least width of the label column
	benchLabelMax  = 16 // and the most, so a very long identifier name cannot take the panel
)

// benchWide reports whether the two panels fit: wide and tall enough for every
// row, the panel borders and a few lines of the focused field.
func (m metaPageModel) benchWide() bool {
	return m.width >= 100 && m.height-6 >= len(m.benchRows())+2+len(m.fieldsHeaderLines())
}

// benchCovers reports whether the panels draw covers: the terminal can show
// images and there is room for them.
func (m metaPageModel) benchCovers() bool {
	return m.benchWide() && coverFits(m.width, m.height)
}

func (m metaPageModel) fieldsHeaderLines() []string {
	width := max(20, m.width)
	// The file's name, with the name the title and authors above would give it.
	var file []string
	if m.height >= 16 {
		title, authors := m.pendingFileNameParts()
		file = []string{m.fileNameLine("  ", width, title, authors)}
	}
	if m.manual {
		return append([]string{ansi.Truncate("  "+colorFaint("Editing the file's own metadata. Nothing is looked up."), width, "…")}, file...)
	}
	lines := []string{ansi.Truncate("  "+colorFaint("Match: ")+safeText(m.cand.Title)+colorFaint(" · "+safeText(strings.Join(m.cand.Sources, "+"))+" · "+metaMatchLabel(m.cand)), width, "…")}
	lines = append(lines, file...)
	warn := lipgloss.NewStyle().Foreground(currentTheme.Warning)
	switch {
	case m.cand.Match == metasource.MatchISBN && m.cand.Score < 1:
		lines = append(lines, ansi.Truncate("  "+warn.Render("The ISBN matches but the title is different: check this is the right book."), width, "…"))
	case m.cand.Match != metasource.MatchISBN:
		lines = append(lines, ansi.Truncate("  "+warn.Render("Found by title only: check the edition before taking its ISBN, publisher, date or language."), width, "…"))
	}
	return lines
}

// fieldsBody is the workbench: two panels side by side when they fit, else a
// table, then the focused field in full, or the discard question.
func (m metaPageModel) fieldsBody() string {
	if m.confirmDiscard {
		return m.discardBody()
	}
	rows := m.benchRows()
	if len(rows) == 0 {
		return "  " + colorFaint("This file's format has no metadata fields zlib can edit.")
	}
	lines := m.fieldsHeaderLines()
	var table []string
	if m.benchWide() {
		table = m.benchPanels(rows)
	} else {
		table = m.benchTable(rows, len(lines))
	}
	lines = append(lines, table...)
	room := max(0, m.height-6) - len(lines) - 1
	if detail := m.benchDetail(rows[min(m.focus, len(rows)-1)], room); len(detail) > 0 {
		lines = append(lines, "")
		lines = append(lines, detail...)
	}
	return strings.Join(lines, "\n")
}

// benchPanels draws the file's panel and, after a lookup, the match's beside it.
// Rows line up across the two, so a field reads straight across.
func (m metaPageModel) benchPanels(rows []benchRow) []string {
	return m.benchPanelsWith(rows, m.benchCovers())
}

// benchPanelsWith is benchPanels with covers drawn or not, for the results page
// as well as the workbench.
func (m metaPageModel) benchPanelsWith(rows []benchRow, covers bool) []string {
	width := max(20, m.width)
	// Both panels reserve their cover place whenever covers are drawn, with a
	// placeholder in it until there is an image, so the match's cover is always
	// where the user looks for it and nothing shifts when it arrives.
	var left, right []string
	if covers {
		left = m.coverBlock(m.leftCoverState())
		if !m.manual {
			right = m.coverBlock(m.matchCoverState())
		}
	}
	if m.manual {
		return benchBox("This file", m.panelLines(rows, true, width-4, left), width, true)
	}
	half := (width - 1) / 2
	changes := len(m.benchChanges())
	leftTitle := "This file"
	if changes > 0 {
		leftTitle += " · " + strconv.Itoa(changes) + " to write"
	}
	rightTitle := "Match · " + strings.Join(m.cand.Sources, "+")
	a := benchBox(leftTitle, m.panelLines(rows, true, half-4, left), half, true)
	b := benchBox(rightTitle, m.panelLines(rows, false, width-half-1-4, right), width-half-1, false)
	out := make([]string, max(len(a), len(b)))
	for i := range out {
		la, lb := "", ""
		if i < len(a) {
			la = a[i]
		}
		if i < len(b) {
			lb = b[i]
		}
		out[i] = la + " " + lb
	}
	return out
}

// panelLines are the rows of one panel, inner width w: the file's side shows
// what will be written, the match's side what it says. With covers, the first
// rows leave room for the image on the left.
func (m metaPageModel) panelLines(rows []benchRow, file bool, w int, block []string) []string {
	labelW := benchLabelWidth(rows)
	n := len(rows)
	covers := len(block) > 0
	if covers {
		n = max(n, benchCoverRows)
	}
	out := make([]string, n)
	indent := 0
	if covers {
		indent = benchCoverCols + 2
	}
	for i := range n {
		cell := strings.Repeat(" ", indent)
		if covers && i < len(block) {
			cell = block[i] + "  "
		}
		if i >= len(rows) {
			out[i] = cell + strings.Repeat(" ", max(0, w-indent))
			continue
		}
		r := rows[i]
		text := m.panelCell(r, file, max(10, w-indent), labelW)
		if i == m.focus {
			text = lipgloss.NewStyle().Background(currentTheme.Surface).Bold(true).Render(ansi.Strip(text))
		}
		out[i] = cell + text
	}
	return out
}

// benchLabelWidth is the width of the label column for rows: the widest field
// label and a gap, never less than benchLabelW, so a long label such as
// "ID openlibrary" does not run into its value or lose its end. A label longer
// than the cap is cut with an ellipsis.
func benchLabelWidth(rows []benchRow) int {
	w := benchLabelW
	for _, r := range rows {
		w = max(w, ansi.StringWidth(metaFieldLabel(r.field))+1)
	}
	return min(w, benchLabelMax)
}

// panelCell is one field in a panel: a mark, the label and the value, w cells.
func (m metaPageModel) panelCell(r benchRow, file bool, w, labelW int) string {
	value, origin := m.pending(r)
	mark := "  "
	if file {
		switch origin {
		case originMatch:
			mark = colorGreen("← ")
		case originEdited:
			mark = colorCyan("✎ ")
		case originCleared:
			mark = colorRed("✗ ")
		}
	} else {
		value = r.match
		switch {
		case r.match == "":
		case origin == originMatch:
			mark = colorGreen("✓ ")
		case r.plan >= 0 || !sameValue(r.field, r.match, r.file):
			mark = lipgloss.NewStyle().Foreground(currentTheme.Warning).Render("≠ ")
		default:
			mark = colorFaint("= ")
		}
	}
	shown := shownValue(r.field, value)
	if shown == "" {
		shown = colorFaint("—")
	}
	label := colorFaint(padCell(metaFieldLabel(r.field), labelW))
	cell := mark + label + ansi.Truncate(shown, max(1, w-2-labelW), "…")
	return cell + strings.Repeat(" ", max(0, w-ansi.StringWidth(cell)))
}

// sameValue reports whether two values of f read the same once case, spacing
// and markup are set aside.
func sameValue(f metasource.Field, a, b string) bool {
	return strings.EqualFold(shownValue(f, a), shownValue(f, b))
}

// benchBox draws lines in a rounded border with a title in the top edge.
func benchBox(title string, lines []string, width int, accent bool) []string {
	border := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	titleStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted).Bold(true)
	if accent {
		titleStyle = lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	}
	inner := max(1, width-4)
	t := ansi.Truncate(safeText(title), max(1, width-6), "…")
	top := border.Render("╭─ ") + titleStyle.Render(t) + border.Render(" "+strings.Repeat("─", max(0, width-5-ansi.StringWidth(t)))+"╮")
	out := []string{top}
	for _, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		out = append(out, border.Render("│ ")+l+strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))+border.Render(" │"))
	}
	return append(out, border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
}

// benchTable is the narrow layout: one row per field with the value to write
// and the match's, scrolled to keep the focused row in sight.
func (m metaPageModel) benchTable(rows []benchRow, header int) []string {
	width := max(20, m.width)
	n := len(rows)
	room := max(1, m.height-6-header-1-4) // the column titles and a short detail
	shown := min(n, room)
	top := min(max(0, m.focus-shown/2), max(0, n-shown))
	labelW := benchLabelWidth(rows)
	colW := max(8, (width-4-labelW-4)/2)
	if m.manual {
		colW = max(8, width-4-labelW-2)
	}
	head := "    " + padCell("Field", labelW) + padCell("This file", colW)
	if !m.manual {
		head += "  Match"
	}
	out := []string{colorFaint(ansi.Truncate(head, width, "…"))}
	for i := top; i < top+shown; i++ {
		r := rows[i]
		value, origin := m.pending(r)
		mark := "  "
		switch origin {
		case originMatch:
			mark = "← "
		case originEdited:
			mark = "✎ "
		case originCleared:
			mark = "✗ "
		}
		v := shownValue(r.field, value)
		if v == "" {
			v = "—"
		}
		label := mark + padCell(metaFieldLabel(r.field), labelW) + padCell(v, colW)
		if !m.manual {
			mv := shownValue(r.field, r.match)
			if mv == "" {
				mv = "—"
			}
			label += "  " + mv
		}
		out = append(out, styledListRow(label, i == m.focus, width, currentTheme))
	}
	if shown < n {
		out = append(out, "  "+colorFaint(strconv.Itoa(m.focus+1)+" of "+strconv.Itoa(n)+" fields"))
	}
	return out
}

// benchDetail is the focused field in full: what the file holds, what will be
// written and what the match says, each wrapped, within room lines.
func (m metaPageModel) benchDetail(r benchRow, room int) []string {
	if room < 2 {
		return nil
	}
	width := max(20, m.width)
	value, origin := m.pending(r)
	out := []string{sectionHeader(metaFieldLabel(r.field), origin.label(), width)}
	var parts [][2]string
	parts = append(parts, [2]string{"In the file", r.file})
	if origin != originFile {
		parts = append(parts, [2]string{"Will write", value})
	}
	if !m.manual && r.match != "" && (origin != originMatch) {
		parts = append(parts, [2]string{"Match", r.match})
	}
	each := max(1, (room-1)/len(parts))
	for _, p := range parts {
		v := p[1]
		if v == "" {
			v = "(empty)"
		}
		wrapped := strings.Split(ansi.Wrap(shownValue(r.field, v), max(10, width-16), ""), "\n")
		if len(wrapped) > each {
			wrapped = append(wrapped[:each-1], ansi.Truncate(wrapped[each-1], max(1, width-18), "…")+" …")
		}
		for j, w := range wrapped {
			prefix := "  " + colorFaint(padCell(p[0], 12)) + "  "
			if j > 0 {
				prefix = strings.Repeat(" ", 16)
			}
			out = append(out, ansi.Truncate(prefix+w, width, "…"))
		}
	}
	return out[:min(len(out), room)]
}

func (m metaPageModel) discardBody() string {
	n := len(m.benchChanges())
	noun := "changes"
	if n == 1 {
		noun = "change"
	}
	lines := []string{"", "  " + colorBold("Discard "+strconv.Itoa(n)+" "+noun+"?"), "",
		"  " + colorFaint("Nothing has been written to the file yet."), ""}
	return strings.Join(append(lines, "  "+renderActionButton("Discard", true, true)+"  "+renderActionButton("Keep editing", false, false)), "\n")
}

// fieldsStatus says what will be written, or the last message.
func (m metaPageModel) fieldsStatus() string {
	if m.message != "" {
		return m.message
	}
	var parts []string
	changes := m.benchChanges()
	if len(changes) == 0 {
		parts = append(parts, "Nothing changed yet")
	} else {
		var names []string
		for _, r := range changes {
			names = append(names, metaFieldLabel(r.field))
		}
		parts = append(parts, strconv.Itoa(len(changes))+" to write: "+strings.Join(names, ", "))
	}
	if len(m.hidden) > 0 {
		parts = append(parts, strings.ToUpper(string(m.format))+" files cannot store: "+strings.Join(m.hidden, ", "))
	}
	return strings.Join(parts, " · ")
}

// fieldsKeys are the key hints of the workbench.
func (m metaPageModel) fieldsKeys() []keys.Binding {
	if m.confirmDiscard {
		return []keys.Binding{uiKey("y", "discard"), uiKey("n", "keep editing")}
	}
	if m.manual {
		return m.keysFor(withHelpKey(
			[][2]string{{"↑/↓", "field"}, {"e", "edit"}, {"x", "clear"}, {"←", "undo"}, {"ctrl+s", "save"}, {"n", "rename"}, {"?", "keys"}, {"esc", "back"}},
			[][2]string{{"↑/↓", ""}, {"e", "edit"}, {"x", "clear"}, {"ctrl+s", "save"}, {"n", "rename"}, {"esc", "back"}},
			[][2]string{{"e", ""}, {"x", ""}, {"ctrl+s", ""}, {"n", ""}, {"esc", ""}})...)
	}
	return m.keysFor(withHelpKey(
		[][2]string{{"↑/↓", "field"}, {"→", "use match"}, {"←", "keep file"}, {"e", "edit"}, {"x", "clear"}, {"a", "all"}, {"ctrl+s", "save"}, {"n", "rename"}, {"?", "keys"}, {"esc", "back"}},
		[][2]string{{"→", "use match"}, {"←", "keep file"}, {"e", "edit"}, {"a", "all"}, {"ctrl+s", "save"}, {"n", "rename"}, {"esc", "back"}},
		[][2]string{{"→", "match"}, {"←", "file"}, {"e", "edit"}, {"ctrl+s", "save"}, {"n", ""}, {"esc", "back"}},
		[][2]string{{"→", ""}, {"←", ""}, {"e", ""}, {"ctrl+s", ""}, {"n", ""}, {"esc", ""}})...)
}

// --- Results comparison ------------------------------------------------------

// compareModel is the page as the workbench would open on the highlighted
// match, with no row focused: the source of the results page's panels.
func (m metaPageModel) compareModel() metaPageModel {
	c := m
	c = c.openFields()
	c.focus = -1
	return c
}

// compareRows are the rows the results page compares: the fields either side
// has a value for. The workbench lists every field; a preview needs only these.
func (m metaPageModel) compareRows() []benchRow {
	var out []benchRow
	for _, r := range m.benchRows() {
		if r.file != "" || r.match != "" {
			out = append(out, r)
		}
	}
	return out
}

// compareNeed is how many lines the comparison takes under the list: a blank
// line, the effect line and the panels with their borders.
func (m metaPageModel) compareNeed() int {
	c := m.compareModel()
	rows := len(c.compareRows())
	if m.compareCovers() {
		rows = max(rows, benchCoverRows)
	}
	return 1 + 1 + rows + 2
}

// compareFits reports whether the results page shows the panels: a wide
// terminal with room for them and at least two rows of matches.
func (m metaPageModel) compareFits() bool {
	if m.width < 100 || len(m.res.Candidates) == 0 || m.stage != metaCandidates {
		return false
	}
	return max(0, m.height-6)-m.compareNeed()-2 >= min(2, len(m.res.Candidates))
}

// compareCovers reports whether the results panels draw covers.
func (m metaPageModel) compareCovers() bool { return coverFits(m.width, m.height) }

// compareTop is the screen row of the panels' top border on the results page.
func (m metaPageModel) compareTop() int {
	rows := m.listRows()
	shown := min(len(m.res.Candidates), m.top+rows) - m.top
	more := btoi(len(m.res.Candidates)-rows > 0)
	return 3 + 1 + shown + more + 1 + 1
}

// --- Covers --------------------------------------------------------------------

// sideCoverRect is where the pages other than the workbench show a cover: top
// right of the body, the book card's size.
func sideCoverRect(width int) image.Rectangle {
	return image.Rect(width-coverWidth-2, 3, width-2, 3+coverRows)
}

// sideCover reports whether a page shows a cover in the top right, and narrows
// its body to leave room for it.
func (m metaPageModel) sideCoverImage() image.Image {
	if !coverFits(m.width, m.height) {
		return nil
	}
	switch m.stage {
	case metaInfo:
		if m.fileCover != nil {
			return m.fileCover.img
		}
	case metaCandidates:
		if m.compareFits() {
			return nil // the panels show the covers
		}
		if m.cursor < len(m.res.Candidates) {
			if got := m.coverCache[m.res.Candidates[m.cursor].CoverURL]; got != nil {
				return got.img
			}
		}
	}
	return nil
}

// sideNarrow is m with its width narrowed by the side cover, when the page
// shows one, so the text it draws stays clear of the image.
func (m metaPageModel) sideNarrow() metaPageModel {
	if m.sideCoverImage() != nil {
		m.width = max(20, m.width-coverWidth-4)
	}
	return m
}

// coverSlots are the covers the page shows now, for the graphics layer. None
// while an editor, the help or a question covers the page.
func (m metaPageModel) coverSlots() []coverSource {
	if m.editing || m.helpOpen || m.confirmDiscard || !coverFits(m.width, m.height) {
		return nil
	}
	if m.stage == metaFields {
		if !m.benchCovers() {
			return nil
		}
		top := 3 + len(m.fieldsHeaderLines()) + 1
		var out []coverSource
		if img := m.benchLeftCover(); img != nil {
			out = append(out, coverSource{url: "file:" + m.path + "/" + m.benchCoverKey(), img: img, width: m.width, height: m.height,
				area: image.Rect(2, top, 2+benchCoverCols, top+benchCoverRows)})
		}
		if !m.manual {
			if img := m.benchRightCover(); img != nil {
				got := &metaCoverImage{img: img}
				x := (max(20, m.width)-1)/2 + 1 + 2
				out = append(out, coverSource{url: m.cand.CoverURL, img: got.img, width: m.width, height: m.height,
					area: image.Rect(x, top, x+benchCoverCols, top+benchCoverRows)})
			}
		}
		return out
	}
	if m.stage == metaPrepare {
		// The book card's cover: top left of the body, inside the card's border.
		if lay := m.prepareLayout(); lay.cover && m.fileCover != nil && m.fileCover.img != nil {
			return []coverSource{{url: "card:" + m.path, img: m.fileCover.img, width: m.width, height: m.height,
				area: image.Rect(2, 4, 2+coverWidth, 4+coverRows)}}
		}
		return nil
	}
	if m.stage == metaCandidates && m.compareFits() {
		top := m.compareTop() + 1
		var out []coverSource
		cm := m.compareModel()
		if img := cm.benchLeftCover(); img != nil { // as the panels reserve it
			out = append(out, coverSource{url: "file:" + m.path + "/" + cm.benchCoverKey(), img: img, width: m.width, height: m.height,
				area: image.Rect(2, top, 2+benchCoverCols, top+benchCoverRows)})
		}
		c := m.res.Candidates[m.cursor]
		if got := m.coverCache[c.CoverURL]; got != nil && got.img != nil {
			x := (max(20, m.width)-1)/2 + 1 + 2
			out = append(out, coverSource{url: c.CoverURL, img: got.img, width: m.width, height: m.height,
				area: image.Rect(x, top, x+benchCoverCols, top+benchCoverRows)})
		}
		return out
	}
	if img := m.sideCoverImage(); img != nil {
		return []coverSource{{url: "side:" + m.path + "/" + strconv.Itoa(int(m.stage)) + "/" + strconv.Itoa(m.cursor), img: img,
			width: m.width, height: m.height, area: sideCoverRect(m.width)}}
	}
	return nil
}

// benchRightCover is the match's cover, once downloaded.
func (m metaPageModel) benchRightCover() image.Image {
	if m.manual {
		return nil
	}
	if got := m.coverCache[m.cand.CoverURL]; got != nil {
		return got.img
	}
	return nil
}

// benchLeftCover is the cover the file will have: one the user chose, the
// match's when it is taken, else the file's own.
func (m metaPageModel) benchLeftCover() image.Image {
	if _, ok := m.extra[metasource.FieldCover]; ok && m.coverEdit != nil {
		return m.coverEdit.img
	}
	for _, r := range m.benchRows() {
		if r.field != metasource.FieldCover {
			continue
		}
		if _, origin := m.pending(r); origin == originMatch {
			if got := m.coverCache[m.cand.CoverURL]; got != nil && got.img != nil {
				return got.img // the match's cover, once it is here
			}
		}
	}
	if m.fileCover != nil {
		return m.fileCover.img // until then, the one the file has
	}
	return nil
}

func (m metaPageModel) benchCoverKey() string {
	if _, ok := m.extra[metasource.FieldCover]; ok {
		return "edited"
	}
	for i, ch := range m.plan {
		if ch.Field == metasource.FieldCover && m.selected[i] {
			if got := m.coverCache[m.cand.CoverURL]; got != nil && got.img != nil {
				return "match"
			}
		}
	}
	return "own"
}
