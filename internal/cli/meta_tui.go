package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// metaStage is where the metadata page is in its flow.
type metaStage int

const (
	metaLooking    metaStage = iota // asking the catalogues
	metaFailed                      // nothing to offer; r retries
	metaCandidates                  // choosing which match to use
	metaFields                      // choosing which fields to write
	metaWriting                     // rewriting the file
	metaPrepare                     // what is known about the book, and which sources to ask, before any search
	metaRename                      // choosing a new name for the file
	metaInfo                        // everything the file's own metadata says
)

// metaCoverError marks a failed cover download, which happens before
// anything is written.
type metaCoverError struct{ err error }

func (e metaCoverError) Error() string { return "cover download failed: " + e.err.Error() }
func (e metaCoverError) Unwrap() error { return e.err }

// metaRun is the state of one lookup. Copies of the page share it, so the
// lookup starts once however many times Init runs, and any copy can cancel
// it.
type metaRun struct {
	ch      chan tea.Msg
	started atomic.Bool
	mu      sync.Mutex
	cancel  context.CancelFunc
}

func newMetaRun() *metaRun { return &metaRun{ch: make(chan tea.Msg, 1)} }

func (r *metaRun) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

type (
	metaLookupFunc func(ctx context.Context, q metasource.Query) (metasource.Result, error)
	metaWriteFunc  func(path string, c *ebookmeta.Changes) error
	metaCoverFunc  func(ctx context.Context, url string) (*ebookmeta.Cover, error)

	metaLookupDone struct {
		gen int
		res metasource.Result
		err error
	}
	metaWriteDone struct{ err error }
)

// metaPageModel looks a local book up online, lets the user pick a match,
// shows what would change field by field and writes the chosen fields. It is
// one page: the shell and the standalone `zlib meta --fetch` both open it.
//
// Only empty fields are selected by default; replacing a value the file
// already has needs an explicit choice. Failures stay on the page that owns
// the request.
type metaPageModel struct {
	width, height int
	path          string
	format        ebookmeta.Format
	current       *ebookmeta.Metadata
	query         metasource.Query
	lookup        metaLookupFunc
	write         metaWriteFunc
	cover         metaCoverFunc // nil: covers are not offered

	stage    metaStage
	spinner  spinner.Model
	res      metasource.Result
	err      error
	cursor   int // candidate row
	top      int // first visible candidate row
	cand     metasource.Candidate
	plan     []metasource.Change
	hidden   []string // fields the format cannot store
	selected []bool
	focus    int // plan index, len(plan) = Apply, len(plan)+1 = Back
	message  string

	// editor is the text entry open over the fields (editing a proposed
	// value). While editing is set it owns the keyboard.
	editor    metaEditor
	editing   bool
	editKind  metaEditKind
	editField metasource.Field // the field being edited, when editKind is editValue

	// extraction is what the file name and the file said about the book, and
	// hint the download's record, if any. query is what will be searched; it
	// starts as extraction.Query and changes when the user edits it.
	extraction metasource.Extraction
	hint       metasource.Hint

	// sources and lookupFor give the page its choice of sources: what is
	// searched now, and how to build a lookup for another choice. Both are
	// empty when the page was built with a fixed lookup. draft is the copy the
	// user toggles on the prepare page, committed only by a search.
	// saveSources writes the enabled sources as the default, when the user asks.
	sources      []metaSourceChoice
	lookupFor    metaSourceLookup
	draft        []metaSourceChoice
	sourceCursor int
	saveSources  func(names []string) (overriddenBy string, err error)

	// blink is the editor's first cursor command, for a page that opens on it.
	blink tea.Cmd

	// prev is the page before a repeat search, which Esc during that search
	// returns to. It is nil on the first search and once an answer arrives.
	prev *metaSnapshot

	gen int
	run *metaRun

	// renameOpts are the names on offer on the rename page, renameCursor the
	// highlighted one and renameBack the stage Esc returns to. renamedFrom is
	// the path the file had before the first rename, empty if it was never
	// renamed.
	// helpOpen shows every key of helpStage over the page. infoView scrolls the
	// file details.
	helpOpen  bool
	helpStage metaStage
	infoView  viewport.Model
	helpView  viewport.Model

	renameOpts   []renameOption
	renameCursor int
	renameBack   metaStage
	renamedFrom  string
	// nameLayout is the order names are built in on the rename page: the saved
	// default, until the user picks another there. saveLayout writes it as the
	// default, when the user asks.
	nameLayout   metasource.NameLayout
	customLayout metasource.NameLayout // the configured custom pattern, offered next to the presets
	saveLayout   func(metasource.NameLayout) (overriddenBy string, err error)

	// The fields workbench. extra holds the values the user typed or cleared,
	// which win over the match's; manual is set when the workbench edits the
	// file with no lookup. fileCover is the file's own cover, coverCache the
	// match covers downloaded for the previews (shared by copies of the page),
	// coverEdit an image the user chose. confirmDiscard asks before leaving
	// with changes not written.
	extra          map[metasource.Field]metasource.Change
	manual         bool
	fileCover      *metaCoverImage
	coverCache     map[string]*metaCoverImage
	coverEdit      *metaCoverImage
	confirmDiscard bool
	baseline       []bool // m.selected as the workbench opened it

	// Outcome for the caller.
	applied   []string
	cancelled bool
}

// metaHint adds what is known about the book from outside the file, such as
// the Z-Library record of a download. Hints only fill gaps: they never
// replace a title or authors read from the file name or the file, and their
// ISBNs are searched as well, as unconfirmed ones.
type metaHint struct {
	Title   string
	Authors []string
	ISBNs   []string // any form; invalid ones are ignored
}

// metaOption adjusts a new metadata page.
type metaOption func(*metaPageModel)

func withMetaHint(h metaHint) metaOption {
	return func(m *metaPageModel) {
		m.hint = metasource.Hint{Title: h.Title, Authors: h.Authors, ISBNs: h.ISBNs}
	}
}

// withMetaSourceSaver lets the prepare page save the sources that are on as the
// default. save returns the name of a setting that overrides what it saved, or
// "" when the saved list takes effect.
func withMetaSourceSaver(save func(names []string) (overriddenBy string, err error)) metaOption {
	return func(m *metaPageModel) { m.saveSources = save }
}

func withMetaCover(fetch metaCoverFunc) metaOption {
	return func(m *metaPageModel) { m.cover = fetch }
}

// newMetaPageModel reads path and prepares the page. Nothing is searched here
// or on Init: the page opens on what the file name and the file say about the
// book, with the sources to ask, and the search starts when the user presses
// Enter. A file with nothing to search with opens on the search editor.
func newMetaPageModel(path string, lookup metaLookupFunc, write metaWriteFunc, opts ...metaOption) (metaPageModel, error) {
	cur, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return metaPageModel{}, metaError(path, err)
	}
	m := metaPageModel{
		path: path, format: format, current: cur, lookup: lookup, write: write,
		spinner: newLoadingSpinner(), width: 80, height: 24, run: newMetaRun(), stage: metaPrepare,
		fileCover: loadFileCover(path), coverCache: map[string]*metaCoverImage{},
	}
	for _, opt := range opts {
		opt(&m)
	}
	m.nameLayout = metaNameLayout()
	if !m.nameLayout.IsPreset() {
		m.customLayout = m.nameLayout
	}
	m.hint.NameLayout = m.nameLayout
	m.extraction = metasource.Extract(filepath.Base(path), cur, scannedISBNs(path, cur), m.hint)
	m.query = m.extraction.Query
	m.draft = append([]metaSourceChoice(nil), m.sources...)
	if m.query.Empty() {
		m.blink = m.beginQueryEdit()
	}
	return m, nil
}

// newDefaultMetaCover resolves the proxy now, like newMetaLookupFor.
func newDefaultMetaCover() metaCoverFunc {
	rt := metaTransport()
	return func(ctx context.Context, url string) (*ebookmeta.Cover, error) {
		return metasource.FetchCover(ctx, rt, url)
	}
}

func (m metaPageModel) Init() tea.Cmd {
	if m.stage == metaPrepare {
		return m.blink // nothing is searched until the user presses Enter
	}
	return tea.Batch(m.spinner.Tick, m.start())
}

// start launches the lookup once and returns the command that waits for its
// answer.
func (m metaPageModel) start() tea.Cmd {
	run, lookup, q, gen := m.run, m.lookup, m.query, m.gen
	if run.started.CompareAndSwap(false, true) {
		ctx, cancel := context.WithCancel(context.Background())
		run.mu.Lock()
		run.cancel = cancel
		run.mu.Unlock()
		go func() {
			res, err := lookup(ctx, q)
			run.ch <- metaLookupDone{gen: gen, res: res, err: err}
		}()
	}
	return waitForMsg(run.ch)
}

// stop abandons a lookup in flight.
func (m metaPageModel) stop() { m.run.stop() }

func (m metaPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.editing {
			m.editor.resize(m.width)
			if m.editor.area != nil {
				m.editor.area.SetHeight(max(3, m.height-12))
			}
		}
		m = m.resizeInfo()
		return m, m.coverNeededCmd()
	case spinner.TickMsg:
		if m.stage != metaLooking && m.stage != metaWriting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case metaLookupDone:
		if msg.gen != m.gen || m.stage != metaLooking {
			return m, nil // an answer to a lookup the user already left
		}
		m = m.lookedUp(msg)
		return m, m.lookedUpCmd()
	case metaWriteDone:
		return m.written(msg)
	case metaCoverFetched:
		m.coverCache[msg.url] = msg.got
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	default:
		// A paste and the input's cursor blink belong to the editor.
		if m.editing {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m metaPageModel) lookedUp(msg metaLookupDone) metaPageModel {
	m.prev = nil // the answer is the new state; there is nothing to go back to
	m.res, m.err = msg.res, msg.err
	if len(m.res.Candidates) == 0 {
		m.stage = metaFailed
		return m
	}
	m.stage, m.err, m.cursor, m.top = metaCandidates, nil, 0, 0
	return m
}

// lookedUpCmd fetches the first match's cover for the preview.
func (m metaPageModel) lookedUpCmd() tea.Cmd {
	if m.stage != metaCandidates || len(m.res.Candidates) == 0 {
		return nil
	}
	return m.matchCoverCmd(m.res.Candidates[m.cursor].CoverURL)
}

func (m metaPageModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// An open editor takes every key first, so q, j, k, e and space type into
	// it instead of acting on the page.
	if m.helpOpen {
		return m.helpKey(msg)
	}
	if m.editing {
		return m.editKey(msg)
	}
	k := msg.String()
	if k == "f2" && m.stage != metaRename { // F2 renames a file in every file manager
		k = "n"
	}
	if k == "?" && m.stage != metaLooking && m.stage != metaWriting {
		return m.openHelp(), nil
	}
	switch m.stage {
	case metaPrepare:
		return m.prepareKey(k)
	case metaLooking:
		if k == "esc" || k == "ctrl+c" {
			if m.prev != nil { // back to the page the search started from
				return m.abandonSearch(), nil
			}
			m.stop()
			m.cancelled = true
			return m, tea.Quit
		}
	case metaFailed:
		switch k {
		case "esc":
			return m.backToPrepare()
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "e":
			return m.startQueryEdit()
		case "s":
			return m.openPrepare()
		case "n":
			return m.openRename()
		case "r":
			return m.search()
		}
	case metaCandidates:
		switch k {
		case "esc":
			return m.backToPrepare()
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "e":
			return m.startQueryEdit()
		case "s":
			return m.openPrepare()
		case "n":
			return m.openRename()
		case "enter":
			m = m.openFields()
			return m, m.matchCoverCmd(m.cand.CoverURL)
		default:
			m.cursor = listCursor(k, m.cursor, len(m.res.Candidates), m.listRows())
			m.scrollTo(m.cursor)
			return m, m.matchCoverCmd(m.res.Candidates[m.cursor].CoverURL)
		}
	case metaFields:
		return m.benchKey(k)
	case metaRename:
		return m.renameKey(k)
	case metaInfo:
		return m.infoKey(msg)
	}
	return m, nil
}

func (m *metaPageModel) scrollTo(row int) {
	rows := m.listRows()
	if row < m.top {
		m.top = row
	}
	if row >= m.top+rows {
		m.top = row - rows + 1
	}
}

// openFields plans the changes the chosen candidate would make.
func (m metaPageModel) openFields() metaPageModel {
	m.cand = m.res.Candidates[m.cursor]
	m.plan, m.hidden, m.selected = nil, nil, nil
	for _, ch := range metasource.Plan(m.current, m.cand) {
		if ch.Field == metasource.FieldCover && m.cover == nil {
			continue
		}
		if !m.canOffer(ch) {
			m.hidden = append(m.hidden, string(ch.Field))
			continue
		}
		m.plan = append(m.plan, ch)
		m.selected = append(m.selected, ch.DefaultSelected())
	}
	m.stage, m.focus, m.message = metaFields, 0, ""
	m.manual, m.confirmDiscard = false, false
	m.extra = map[metasource.Field]metasource.Change{}
	m.baseline = append([]bool(nil), m.selected...)
	if len(m.plan) == 0 {
		m.message = "Nothing to add: the file already has these values"
		if len(m.hidden) > 0 {
			m.message = "Nothing to add that this file can store"
		}
	}
	return m
}

// canOffer reports whether the file's format can take the change. MOBI can
// only replace a cover it already has.
func (m metaPageModel) canOffer(ch metasource.Change) bool {
	return canOfferChange(m.format, m.current, ch)
}

func (m metaPageModel) written(msg metaWriteDone) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// The file is untouched; stay here so the user can try again.
		m.stage = metaFields
		m.applied = nil
		var cover metaCoverError
		if errors.As(msg.err, &cover) {
			m.message = "Cover download failed, nothing was written: " + safeText(cover.err.Error())
		} else {
			m.message = "Could not write the file: " + safeText(msg.err.Error())
		}
		return m, nil
	}
	return m, tea.Quit
}

// listRows is how many candidate rows fit above the detail pane, or above the
// comparison panels when they fit.
func (m metaPageModel) listRows() int {
	n := len(m.res.Candidates)
	if m.compareFits() {
		return max(1, min(n, max(0, m.height-6)-m.compareNeed()-2))
	}
	return max(1, min(n, max(3, (m.height-8)/2)))
}

func (m metaPageModel) View() tea.View {
	subtitle := m.metaSubtitle()
	if m.helpOpen {
		return m.helpScreen(subtitle)
	}
	if m.editing && m.editKind == editQuery {
		// From the prepare page Enter keeps the terms; from the results it searches.
		accept := "search"
		if m.stage == metaPrepare {
			accept = "use"
		}
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.editor.view(max(20, m.width)), "",
			m.keysFor(
				[][2]string{{"enter", accept}, {"tab", "next field"}, {"esc", "cancel"}},
				[][2]string{{"enter", accept}, {"tab", ""}, {"esc", "cancel"}},
				[][2]string{{"enter", ""}, {"tab", ""}, {"esc", ""}})...)
	}
	if m.editing && m.editKind == editName {
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.nameEditView(), "",
			m.keysFor(
				[][2]string{{"enter", "rename"}, {"esc", "cancel"}},
				[][2]string{{"enter", ""}, {"esc", ""}})...)
	}
	switch m.stage {
	case metaInfo:
		body := "  " + colorBold("File details") + "\n" + m.infoView.View()
		if m.infoScrollable() {
			return screenView(m.width, m.height, "Fix metadata", subtitle, body, m.infoStatus(),
				m.keysFor(withHelpKey([][2]string{{"↑/↓", "scroll"}, {"?", "keys"}, {"esc", "back"}}, [][2]string{{"↑/↓", ""}, {"esc", "back"}})...)...)
		}
		return screenView(m.width, m.height, "Fix metadata", subtitle, body, "", uiKey("?", "keys"), uiKey("esc", "back"))
	case metaRename:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.renameBody(), m.message, m.renameKeys()...)
	case metaPrepare:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.sideNarrow().prepareBody(), m.message, m.prepareKeys()...)
	case metaLooking:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.lookingBody(), "", uiKey("esc", "cancel"))
	case metaFailed:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.failedBody(), "",
			m.keysFor(withHelpKey(
				[][2]string{{"e", "edit search"}, {"s", "sources"}, {"n", "rename"}, {"r", "retry"}, {"?", "keys"}, {"esc", "back"}},
				[][2]string{{"e", "edit search"}, {"s", "sources"}, {"n", "rename"}, {"r", "retry"}, {"esc", "back"}},
				[][2]string{{"e", "search"}, {"s", "sources"}, {"n", "rename"}, {"r", "retry"}, {"esc", "back"}},
				[][2]string{{"e", ""}, {"s", ""}, {"n", "rename"}, {"r", ""}, {"esc", ""}},
				[][2]string{{"e", ""}, {"s", ""}, {"n", ""}, {"r", ""}, {"esc", ""}})...)...)
	case metaCandidates:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.sideNarrow().candidatesBody(), m.sourceNote(),
			m.keysFor(withHelpKey(
				[][2]string{{"↑/↓", "choose"}, {"enter", "review fields"}, {"e", "edit search"}, {"s", "sources"}, {"n", "rename"}, {"?", "keys"}, {"esc", "back"}},
				[][2]string{{"↑/↓", "choose"}, {"enter", "review fields"}, {"e", "edit search"}, {"s", "sources"}, {"n", "rename"}, {"esc", "back"}},
				[][2]string{{"↑/↓", "choose"}, {"enter", "fields"}, {"e", "edit search"}, {"s", "sources"}, {"n", "rename"}, {"esc", "back"}},
				[][2]string{{"↑/↓", "choose"}, {"enter", "fields"}, {"e", "search"}, {"s", "sources"}, {"n", "rename"}, {"esc", "back"}},
				[][2]string{{"enter", "fields"}, {"e", "search"}, {"s", "sources"}, {"n", "rename"}, {"esc", "back"}},
				[][2]string{{"enter", ""}, {"e", "search"}, {"s", "sources"}, {"n", "rename"}, {"esc", "back"}},
				[][2]string{{"↑/↓", ""}, {"enter", ""}, {"e", ""}, {"s", ""}, {"n", ""}, {"esc", ""}},
				[][2]string{{"enter", ""}, {"e", ""}, {"s", ""}, {"n", ""}, {"esc", ""}})...)...)
	case metaFields:
		if m.editing {
			// The editor leads the page, so a long list of fields can never push
			// it off the screen; the field's value in the file follows it.
			tiers := [][][2]string{{{"enter", "keep"}, {"esc", "cancel"}}, {{"enter", ""}, {"esc", ""}}}
			if m.editor.multiline() {
				tiers = [][][2]string{{{"ctrl+s", "keep"}, {"enter", "new line"}, {"esc", "cancel"}},
					{{"ctrl+s", "keep"}, {"esc", "cancel"}}, {{"ctrl+s", ""}, {"esc", ""}}}
			}
			return screenView(m.width, m.height, "Fix metadata", subtitle, m.editor.view(max(20, m.width))+"\n\n"+m.editingContext(), "",
				m.keysFor(tiers...)...)
		}
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.fieldsBody(), m.fieldsStatus(), m.fieldsKeys()...)
	}
	note := "Writing the file…"
	for i, ch := range m.plan {
		if ch.Field == metasource.FieldCover && m.selected[i] {
			note = "Downloading the cover and writing the file…"
		}
	}
	return screenView(m.width, m.height, "Fix metadata", subtitle, "  "+m.spinner.View()+note, "")
}

// editingContext says what the row being edited holds in the file now, so the
// user sees what the edit replaces without the list of fields.
func (m metaPageModel) editingContext() string {
	old := shownValue(m.editField, metasource.FileValue(m.current, m.editField))
	if m.editField == metasource.FieldCover {
		old = coverLabel(m.fileCover, m.current.Cover)
	}
	if old == "" {
		old = "(empty)"
	}
	return "  " + colorFaint(ansi.Truncate("In the file now: "+old, max(1, max(20, m.width)-4), "…"))
}

// keysFor picks the key hints for the page's width: the first wording in tiers
// whose hints all fit on one line, the last if none does. The sources key is
// left out when the page has no source panel. A hint with an empty label is
// the key alone.
func (m metaPageModel) keysFor(tiers ...[][2]string) []keys.Binding {
	width := m.width
	if width == 0 {
		width = 80
	}
	build := func(tier [][2]string) ([]keys.Binding, int) {
		var out []keys.Binding
		cells := 0
		for _, kd := range tier {
			if kd[0] == "s" && len(m.sources) == 0 {
				continue
			}
			if len(out) > 0 {
				cells += 3 // the separator between hints
			}
			// help draws a key, a space and its label, even when the label is empty.
			cells += ansi.StringWidth(kd[0]) + 1 + ansi.StringWidth(kd[1])
			out = append(out, uiKey(kd[0], kd[1]))
		}
		return out, cells
	}
	var last []keys.Binding
	for _, tier := range tiers {
		bindings, cells := build(tier)
		if cells <= width {
			return bindings
		}
		last = bindings
	}
	return last
}

func (m metaPageModel) lookingBody() string {
	var lines []string
	lines = append(lines, "  "+m.spinner.View()+"Asking online catalogues about this book…", "")
	rows := [][2]string{{"Title", m.query.Title}, {"Authors", strings.Join(m.query.Authors, " & ")}, {"ISBN", strings.Join(m.query.ISBNs, ", ")}}
	var shown [][2]string
	for _, r := range rows {
		if r[1] != "" {
			shown = append(shown, [2]string{r[0], safeText(r[1])})
		}
	}
	return strings.Join(append(lines, formatDetailRows(shown, "    ", colorFaint)...), "\n")
}

func (m metaPageModel) failedBody() string {
	var lines []string
	switch {
	case errors.Is(m.err, metasource.ErrNoQuery):
		lines = append(lines, "  Nothing to search with yet", "",
			"    This file has no title, author or ISBN.",
			"    Press e to enter one and search.")
	case errors.Is(m.err, metasource.ErrAllSourcesFailed):
		lines = append(lines, "  "+colorRed(symbolError)+" No catalogue could be reached", "")
	default:
		lines = append(lines, "  No catalogue knows this book", "",
			"    Editions without an ISBN, and books in other languages, are often missing.",
			"    Press e to change the title, authors or ISBN and search again.")
	}
	for _, e := range m.res.Errors {
		lines = append(lines, "    "+colorFaint(metaSourceError(e)))
	}
	if note := m.titleNote(); note != "" {
		lines = append(lines, "    "+colorFaint(note))
	}
	if errors.Is(m.err, metasource.ErrAllSourcesFailed) {
		lines = append(lines, "", "    Check the connection or ZLIB_PROXY, then press r to retry.")
	}
	return strings.Join(lines, "\n")
}

// sourceNote tells which sources failed while others answered.
func (m metaPageModel) sourceNote() string {
	var parts []string
	for _, e := range m.res.Errors {
		parts = append(parts, metaSourceError(e))
	}
	if note := m.titleNote(); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, "; ")
}

// metaScoreCell is the fixed-width match marker at the start of a row.
func metaScoreCell(c metasource.Candidate) string {
	switch {
	case c.Match == metasource.MatchISBN && c.Score < 1:
		return "ISBN?"
	case c.Match == metasource.MatchISBN:
		return "ISBN "
	}
	return fmt.Sprintf("%3d%% ", int(c.Score*100+0.5))
}

func metaDetailRows(c metasource.Candidate) [][2]string {
	var rows [][2]string
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, safeText(value)})
		}
	}
	add("Match", metaMatchLabel(c))
	add("Sources", strings.Join(c.Sources, ", "))
	// An edition says which ISBN it has, or that it has none: a match without
	// one is a guess about the edition. A work-level result names no edition.
	if c.EditionLevel {
		if isbn := c.ISBN(); isbn != "" {
			add("ISBN", isbn)
		} else {
			add("ISBN", "no ISBN listed")
		}
	}
	add("Publisher", c.Publisher)
	add("Published", c.Published)
	add("Languages", strings.Join(c.Languages, ", "))
	add("Series", c.SeriesLabel())
	add("Tags", strings.Join(c.Tags, ", "))
	var ids []string
	for _, id := range c.Identifiers {
		ids = append(ids, id.Scheme+":"+id.Value)
	}
	add("Identifiers", strings.Join(ids, ", "))
	if first, _, _ := strings.Cut(c.Description, "\n"); first != "" {
		add("Description", first)
	}
	return rows
}

func metaFieldLabel(f metasource.Field) string {
	name := strings.TrimPrefix(string(f), "identifier:")
	if name != string(f) {
		return "ID " + name
	}
	name = strings.ReplaceAll(name, "_", " ") // title_sort reads "Title sort"
	return strings.ToUpper(name[:1]) + name[1:]
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// summary words what was applied and whether the file was renamed, for the
// notice shown after the page ends.
func (m metaPageModel) summary() string {
	n := len(m.applied)
	if n == 0 {
		if m.renamedFrom == "" {
			return ""
		}
		return "Renamed to " + filepath.Base(m.path)
	}
	noun := "fields"
	if n == 1 {
		noun = "field"
	}
	if m.renamedFrom != "" {
		return "Updated " + strconv.Itoa(n) + " " + noun + " and renamed to " + filepath.Base(m.path)
	}
	return "Updated " + strconv.Itoa(n) + " " + noun + " in " + filepath.Base(m.path)
}
