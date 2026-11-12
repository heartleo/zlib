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

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
)

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

	gen int
	run *metaRun

	// Outcome for the caller.
	applied   []string
	cancelled bool
}

// newMetaPageModel reads path and prepares the lookup. The lookup runs on
// the first Init, not here, so building the page costs nothing.
func newMetaPageModel(path string, lookup metaLookupFunc, write metaWriteFunc) (metaPageModel, error) {
	cur, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		return metaPageModel{}, metaError(path, err)
	}
	q := metasource.QueryFromMetadata(cur)
	if q.Empty() {
		return metaPageModel{}, fmt.Errorf("%s has no title, author or ISBN to search with; set one with zlib meta first", filepath.Base(path))
	}
	m := metaPageModel{
		path: path, format: format, current: cur, query: q, lookup: lookup, write: write,
		spinner: newLoadingSpinner(), width: 80, height: 24, run: newMetaRun(),
	}
	return m, nil
}

// newDefaultMetaLookup resolves the built-in sources, and the proxy they use,
// now. The lookup itself runs on another goroutine and must not read
// package state then.
func newDefaultMetaLookup() metaLookupFunc {
	sources := newMetaSources(metaTransport())
	return func(ctx context.Context, q metasource.Query) (metasource.Result, error) {
		return metasource.Lookup(ctx, sources, q, metasource.Options{})
	}
}

func (m metaPageModel) Init() tea.Cmd {
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
		return m, nil
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
		return m.lookedUp(msg), nil
	case metaWriteDone:
		return m.written(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m metaPageModel) lookedUp(msg metaLookupDone) metaPageModel {
	m.res, m.err = msg.res, msg.err
	if len(m.res.Candidates) == 0 {
		m.stage = metaFailed
		return m
	}
	m.stage, m.err, m.cursor, m.top = metaCandidates, nil, 0, 0
	return m
}

func (m metaPageModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.stage {
	case metaLooking:
		if k == "esc" || k == "ctrl+c" {
			m.stop()
			m.cancelled = true
			return m, tea.Quit
		}
	case metaFailed:
		switch k {
		case "esc", "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "r":
			m.gen++
			m.stage, m.err, m.res = metaLooking, nil, metasource.Result{}
			m.run = newMetaRun() // a fresh lookup on a fresh channel
			return m, tea.Batch(m.spinner.Tick, m.start())
		}
	case metaCandidates:
		switch k {
		case "esc", "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			return m.openFields(), nil
		default:
			m.cursor = listCursor(k, m.cursor, len(m.res.Candidates), 1)
			m.scrollTo(m.cursor)
		}
	case metaFields:
		return m.fieldsKey(k)
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
		if !ebookmeta.CanStore(m.format, string(ch.Field)) {
			m.hidden = append(m.hidden, string(ch.Field))
			continue
		}
		m.plan = append(m.plan, ch)
		m.selected = append(m.selected, ch.DefaultSelected())
	}
	m.stage, m.focus, m.message = metaFields, 0, ""
	if len(m.plan) == 0 {
		m.message = "Nothing to add: the file already has these values"
	}
	return m
}

func (m metaPageModel) fieldsKey(k string) (tea.Model, tea.Cmd) {
	n := len(m.plan)
	if n == 0 { // only Back is on offer
		if k == "esc" || k == "ctrl+c" || k == "enter" {
			m.stage, m.message = metaCandidates, ""
		}
		return m, nil
	}
	switch k {
	case "esc", "ctrl+c":
		m.stage, m.message = metaCandidates, ""
		return m, nil
	case "up", "shift+tab", "k":
		m.focus = (m.focus + n + 1) % (n + 2)
	case "down", "tab", "j":
		m.focus = (m.focus + 1) % (n + 2)
	case "space", " ":
		if m.focus < n {
			m.selected[m.focus] = !m.selected[m.focus]
			m.message = ""
		}
	case "ctrl+s":
		return m.apply()
	case "enter":
		switch {
		case m.focus < n:
			m.selected[m.focus] = !m.selected[m.focus]
			m.message = ""
		case m.focus == n:
			return m.apply()
		default:
			m.stage, m.message = metaCandidates, ""
		}
	}
	return m, nil
}

// apply writes the selected fields in the background.
func (m metaPageModel) apply() (tea.Model, tea.Cmd) {
	var chosen []metasource.Change
	for i, ch := range m.plan {
		if m.selected[i] {
			chosen = append(chosen, ch)
		}
	}
	changes := metasource.BuildChanges(chosen, func(metasource.Change) bool { return true })
	if changes == nil {
		m.message = "Select at least one field"
		return m, nil
	}
	m.applied = m.applied[:0:0]
	for _, ch := range chosen {
		m.applied = append(m.applied, string(ch.Field))
	}
	m.stage, m.message = metaWriting, ""
	write, path := m.write, m.path
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg { return metaWriteDone{err: write(path, changes)} })
}

func (m metaPageModel) written(msg metaWriteDone) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// The file is untouched; stay here so the user can try again.
		m.stage = metaFields
		m.message = "Could not write the file: " + safeText(msg.err.Error())
		m.applied = nil
		return m, nil
	}
	return m, tea.Quit
}

// listRows is how many candidate rows fit above the detail pane.
func (m metaPageModel) listRows() int {
	return max(1, min(len(m.res.Candidates), max(3, (m.height-8)/2)))
}

func (m metaPageModel) View() tea.View {
	subtitle := safeText(filepath.Base(m.path)) + " · " + strings.ToUpper(string(m.format))
	switch m.stage {
	case metaLooking:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.lookingBody(), "", uiKey("esc", "cancel"))
	case metaFailed:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.failedBody(), "", uiKey("r", "retry"), uiKey("esc", "back"))
	case metaCandidates:
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.candidatesBody(), m.sourceNote(),
			uiKey("↑/↓", "choose"), uiKey("enter", "review fields"), uiKey("esc", "back"))
	case metaFields:
		status := m.message
		if status == "" && len(m.hidden) > 0 {
			status = strings.ToUpper(string(m.format)) + " files cannot store: " + strings.Join(m.hidden, ", ")
		}
		return screenView(m.width, m.height, "Fix metadata", subtitle, m.fieldsBody(), status,
			uiKey("↑/↓", "move"), uiKey("space", "select"), uiKey("ctrl+s", "apply"), uiKey("esc", "back"))
	}
	return screenView(m.width, m.height, "Fix metadata", subtitle, "  "+m.spinner.View()+"Writing the file…", "")
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
	case errors.Is(m.err, metasource.ErrAllSourcesFailed):
		lines = append(lines, "  "+colorRed(symbolError)+" No catalogue could be reached", "")
	default:
		lines = append(lines, "  No catalogue knows this book", "",
			"    Editions without an ISBN, and books in other languages, are often missing.")
	}
	for _, e := range m.res.Errors {
		lines = append(lines, "    "+colorFaint(safeText(e.Error())))
	}
	if errors.Is(m.err, metasource.ErrAllSourcesFailed) {
		lines = append(lines, "", "    Check the connection or ZLIB_PROXY, then press r to retry.")
	}
	return strings.Join(lines, "\n")
}

// sourceNote tells which sources failed while others answered.
func (m metaPageModel) sourceNote() string {
	if len(m.res.Errors) == 0 {
		return ""
	}
	return safeText(m.res.ErrorSummary())
}

func (m metaPageModel) candidatesBody() string {
	width := max(20, m.width)
	rows := m.listRows()
	end := min(len(m.res.Candidates), m.top+rows)
	var lines []string
	for i := m.top; i < end; i++ {
		c := m.res.Candidates[i]
		label := metaScoreCell(c) + " " + safeText(c.Title)
		if len(c.Authors) > 0 {
			label += " — " + safeText(strings.Join(c.Authors, " & "))
		}
		if y := c.Published; y != "" {
			label += " (" + safeText(y[:min(4, len(y))]) + ")"
		}
		lines = append(lines, styledListRow(label, i == m.cursor, width, currentTheme))
	}
	c := m.res.Candidates[m.cursor]
	lines = append(lines, "", sectionHeader("Details", "", width))
	for _, l := range formatDetailRows(metaDetailRows(c), "  ", colorFaint) {
		lines = append(lines, ansi.Truncate(l, width, "…"))
	}
	return strings.Join(lines, "\n")
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
	add("Publisher", c.Publisher)
	add("Published", c.Published)
	add("Languages", strings.Join(c.Languages, ", "))
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

func (m metaPageModel) fieldsBody() string {
	width := max(20, m.width)
	lines := []string{"  " + colorFaint("From ") + safeText(m.cand.Title) + colorFaint(" · "+strings.Join(m.cand.Sources, ", ")), ""}
	if m.cand.Match == metasource.MatchISBN && m.cand.Score < 1 {
		warn := lipgloss.NewStyle().Foreground(currentTheme.Warning)
		lines = append(lines, "  "+warn.Render("The ISBN matches but the title is different: check this is the right book."), "")
	}
	for i, ch := range m.plan {
		box := "[ ]"
		if m.selected[i] {
			box = "[x]"
		}
		old := safeText(oneLine(ch.Old))
		if old == "" {
			old = "(empty)"
		}
		label := fmt.Sprintf("%s %-12s %s → %s", box, metaFieldLabel(ch.Field), old, safeText(oneLine(ch.New)))
		if !ch.Fill {
			label += "  (replaces)"
		}
		lines = append(lines, styledListRow(label, m.focus == i, width, currentTheme))
	}
	if len(m.plan) == 0 {
		lines = append(lines, "  "+colorFaint("Nothing to change."))
	}
	lines = append(lines, "")
	n := len(m.plan)
	// Button focus is one-based: Apply is plan index n, Back is n+1.
	buttons := actionButtonRows([]string{"Apply", "Back"}, max(0, m.focus-n+1), width)
	if n == 0 {
		buttons = actionButtonRows([]string{"", "Back"}, 2, width)
	}
	return strings.Join(append(lines, buttons...), "\n")
}

func metaFieldLabel(f metasource.Field) string {
	name := strings.TrimPrefix(string(f), "identifier:")
	if name != string(f) {
		return "ID " + name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// summary words what was applied, for the notice shown after the page ends.
func (m metaPageModel) summary() string {
	n := len(m.applied)
	noun := "fields"
	if n == 1 {
		noun = "field"
	}
	return "Updated " + strconv.Itoa(n) + " " + noun + " in " + filepath.Base(m.path)
}
