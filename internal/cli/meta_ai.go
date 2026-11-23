package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
	"github.com/heartleo/zlib/internal/metasource"
)

// AI Match is opened by hand with `a` from the results or the no-match page.
// It searches like a source does, with the file's title, authors and year (the
// model adds other-language titles and aliases), and its answer is shown as one
// more list of results: the same rows, columns and side-by-side comparison as
// any source's, tagged `AI`, each with the model's verdict beside it. Esc on
// that list returns to the standard results untouched. How the match went is a
// separate process view, hidden until `p` opens it. A failure stays on the AI
// page, and a file is written only through the fields page, after the user
// picks a row and confirms.

// errAINotReady means no AI model has passed the connection test.
var errAINotReady = errors.New("no AI model has passed the connection test")

// aiTag is the source name AI rows carry in front of the catalogue's own.
const aiTag = "AI"

type (
	// metaAIFunc runs the AI match; lookup searches the catalogues with the
	// sources the page uses now.
	metaAIFunc func(ctx context.Context, lookup metaLookupFunc, in aimatch.Input, progress func(aimatch.Event)) (aimatch.Outcome, error)

	metaAIDone struct {
		gen int
		out aimatch.Outcome
		err error
	}

	// metaAIBase is the standard results page AI Match was opened from.
	metaAIBase struct {
		stage       metaStage
		res         metasource.Result
		err         error
		cursor, top int
	}
)

// metaAIState is what the AI stage shows while it has no results to list.
type metaAIState int

const (
	aiRunning metaAIState = iota
	aiNeedsSetup
	aiFailed
	aiNoMatch
)

// addLog records a step of the match; events returns a copy of them all.
func (r *metaRun) addLog(e aimatch.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, e)
}

func (r *metaRun) events() []aimatch.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

// withMetaAI offers AI Match on the page. Without it `a` does nothing and no
// hint is shown.
func withMetaAI(fn metaAIFunc) metaOption {
	return func(m *metaPageModel) { m.ai = fn }
}

// newDefaultMetaAI reads the AI settings and proxy now: the match runs on
// another goroutine and must not read package state then. Settings that have
// not passed the connection test, or that changed since, give errAINotReady.
func newDefaultMetaAI() metaAIFunc {
	s, err := loadAISettings()
	return func(ctx context.Context, lookup metaLookupFunc, in aimatch.Input, progress func(aimatch.Event)) (aimatch.Outcome, error) {
		if err != nil || !s.Verified() {
			return aimatch.Outcome{}, errAINotReady
		}
		c, err := newAIClient(s)
		if err != nil {
			return aimatch.Outcome{}, err
		}
		in.WebSearch = s.WebSearch
		return aimatch.Match(ctx, c, aimatch.LookupFunc(lookup), in, progress)
	}
}

func (m metaPageModel) aiRunning() bool { return m.stage == metaAI && m.aiState == aiRunning }

// startAI begins an AI match for the search as it stands. It starts from the
// standard results, even when an earlier AI list is on screen, so the AI tag
// never reaches the model. A late answer to an earlier match is dropped: the
// generation moves on.
func (m metaPageModel) startAI() (tea.Model, tea.Cmd) {
	if m.ai == nil {
		return m, nil
	}
	if m.query.Empty() {
		m.message = "Nothing to match: press e to enter a title, author or ISBN"
		return m, nil
	}
	if m.aiRun != nil {
		m.aiRun.stop()
	}
	if !m.aiResults {
		m.aiBase = &metaAIBase{stage: m.stage, res: m.res, err: m.err, cursor: m.cursor, top: m.top}
	}
	m.aiFrom = m.stage
	m.aiGen++
	m.stage, m.aiState, m.aiErr, m.aiOut, m.message = metaAI, aiRunning, nil, aimatch.Outcome{}, ""
	run := newMetaRun()
	m.aiRun = run
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	fn, lookup, gen := m.ai, m.lookup, m.aiGen
	in := aimatch.Input{
		FileName: filepath.Base(m.path), Query: m.query, Existing: slices.Clone(m.aiBase.res.Candidates),
	}
	if m.current != nil {
		in.Languages = m.current.Languages
	}
	go func() {
		out, err := fn(ctx, lookup, in, run.addLog)
		run.ch <- metaAIDone{gen: gen, out: out, err: err}
	}()
	return m, tea.Batch(m.spinner.Tick, waitForMsg(run.ch))
}

// aiDone takes the answer of the current match: rows replace the list, or the
// AI stage says why there are none.
func (m metaPageModel) aiDone(msg metaAIDone) metaPageModel {
	m.aiOut, m.aiErr = msg.out, msg.err
	m.aiLog = m.aiRun.events()
	switch {
	case errors.Is(msg.err, errAINotReady):
		m.aiState = aiNeedsSetup
	case msg.err != nil:
		m.aiState = aiFailed
	case len(msg.out.Recommendations) == 0:
		m.aiState = aiNoMatch
	default:
		m = m.showAIResults(msg.out)
	}
	if m.aiLogOpen {
		m = m.refreshAILog()
	}
	return m
}

// showAIResults makes the model's rows the list of the results page. A row is
// the catalogue's own, with `AI` in front of its source; its score and match
// kind are the catalogue's, from the search that found it, so the Match column
// means the same on every row. The model's verdict is kept beside each row.
func (m metaPageModel) showAIResults(out aimatch.Outcome) metaPageModel {
	cands := make([]metasource.Candidate, len(out.Recommendations))
	for i, r := range out.Recommendations {
		c := r.Candidate
		c.Sources = append([]string{aiTag}, c.Sources...)
		cands[i] = c
	}
	m.res = metasource.Result{Candidates: cands, Errors: out.Errors}
	m.aiRecs, m.aiResults, m.err = out.Recommendations, true, nil
	m.stage, m.cursor, m.top, m.message = metaCandidates, 0, 0, ""
	return m
}

// leaveAI returns to the page AI Match was opened from, untouched.
func (m metaPageModel) leaveAI() metaPageModel {
	if m.aiRun != nil {
		m.aiRun.stop()
	}
	m.aiGen++ // an answer still on its way is dropped
	m.stage, m.message = m.aiFrom, ""
	if !m.aiResults {
		m.aiBase = nil
	}
	return m
}

// leaveAIResults returns from the AI list to the standard results, or the
// no-match page, as they were.
func (m metaPageModel) leaveAIResults() (tea.Model, tea.Cmd) {
	b := m.aiBase
	if b == nil {
		m.aiResults, m.aiRecs = false, nil
		return m, nil
	}
	m.res, m.err, m.cursor, m.top, m.stage = b.res, b.err, b.cursor, b.top, b.stage
	m.aiResults, m.aiRecs, m.aiBase, m.message = false, nil, nil, ""
	return m, m.lookedUpCmd()
}

func (m metaPageModel) aiKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "p":
		if m.aiState != aiNeedsSetup {
			return m.openAILog(), nil
		}
	case "esc":
		return m.leaveAI(), nil
	case "ctrl+c":
		if m.aiState == aiRunning {
			return m.leaveAI(), nil
		}
		m.cancelled = true
		return m, tea.Quit
	case "q":
		if m.aiState != aiRunning {
			m.cancelled = true
			return m, tea.Quit
		}
	case "r":
		if m.aiState == aiFailed || m.aiState == aiNoMatch {
			return m.startAI()
		}
	}
	return m, nil
}

func (m metaPageModel) aiKeys() []keys.Binding {
	switch m.aiState {
	case aiRunning:
		return []keys.Binding{uiKey("p", "process"), uiKey("esc", "cancel")}
	case aiFailed, aiNoMatch:
		return m.keysFor([][2]string{{"r", "retry"}, {"p", "process"}, {"?", "keys"}, {"esc", "back"}}, [][2]string{{"r", "retry"}, {"p", ""}, {"esc", "back"}}, [][2]string{{"esc", ""}})
	}
	return m.keysFor([][2]string{{"?", "keys"}, {"esc", "back"}}, [][2]string{{"esc", ""}})
}

// aiStatus is the line under the body: the catalogue failures behind the answer.
func (m metaPageModel) aiStatus() string {
	var parts []string
	for _, e := range m.aiOut.Errors {
		parts = append(parts, metaSourceError(e))
	}
	return strings.Join(parts, "; ")
}

func (m metaPageModel) aiBody() string {
	switch m.aiState {
	case aiRunning:
		step := "Asking the model…"
		if ev := m.aiRun.events(); len(ev) > 0 {
			step = strings.TrimSpace(safeText(ev[len(ev)-1].Message))
		}
		lines := []string{"  " + m.spinner.View() + step, ""}
		rows := [][2]string{{"Title", m.query.Title}, {"Authors", strings.Join(m.query.Authors, " & ")}, {"Year", m.query.Year}, {"ISBN", strings.Join(m.query.ISBNs, ", ")}}
		var shown [][2]string
		for _, r := range rows {
			if r[1] != "" {
				shown = append(shown, [2]string{r[0], safeText(r[1])})
			}
		}
		lines = append(lines, formatDetailRows(shown, "    ", colorFaint)...)
		lines = append(lines, "", "    "+colorFaint("p shows the process, Esc cancels. The file and the results are not changed."))
		return strings.Join(lines, "\n")
	case aiNeedsSetup:
		return strings.Join([]string{"  AI Match needs a working AI model", "",
			"    No model has passed the connection test yet.",
			"    Set one up with /config ai (or `zlib config ai`), run its",
			"    connection test, then press a again."}, "\n")
	case aiFailed:
		return strings.Join([]string{"  " + colorRed(symbolError) + " AI Match failed", "",
			"    " + aiErrorText(m.aiErr), "",
			"    The standard results are unchanged. Press r to retry."}, "\n")
	}
	return strings.Join([]string{"  No reliable match", "",
		"    The model found no catalogue record it could vouch for.",
		"    Nothing is changed. Press p to see what it looked at."}, "\n")
}

// aiErrorText words a model failure for the screen, with a hint on the setting.
func aiErrorText(err error) string {
	text := safeText(err.Error())
	var api *aimatch.APIError
	if errors.As(err, &api) && (api.Status == 401 || api.Status == 403) {
		text += ". Check the key with /config ai"
	}
	return text
}

// aiRec is the model's verdict on the highlighted row, if the list is an AI one.
func (m metaPageModel) aiRec() (aimatch.Recommendation, bool) {
	if !m.aiResults || m.cursor < 0 || m.cursor >= len(m.aiRecs) {
		return aimatch.Recommendation{}, false
	}
	return m.aiRecs[m.cursor], true
}

func aiPercent(r aimatch.Recommendation) string {
	return strconv.Itoa(int(r.Confidence*100+0.5)) + "%"
}

// aiVerdictPrefix leads the line above the comparison panels on an AI list: the
// model's verdict in a few words, before what the row would change.
func (m metaPageModel) aiVerdictPrefix() string {
	r, ok := m.aiRec()
	if !ok {
		return ""
	}
	label := "same edition"
	if r.Relation == aimatch.SameWork {
		label = "other edition"
	}
	return "AI: " + label + " " + aiPercent(r) + " · "
}

// aiDetailRows are the rows the model adds to the details of an AI list's
// highlighted row: its verdict, its reason and the pages it cites.
func (m metaPageModel) aiDetailRows() [][2]string {
	r, ok := m.aiRec()
	if !ok {
		return nil
	}
	rows := [][2]string{{"AI says", r.Relation.Label() + ", " + aiPercent(r) + " confident"}}
	if r.Explanation != "" {
		rows = append(rows, [2]string{"Why", safeText(r.Explanation)})
	}
	for i, u := range r.Evidence {
		label := ""
		if i == 0 {
			label = "Evidence"
		}
		rows = append(rows, [2]string{label, safeText(u)})
	}
	if r.Relation == aimatch.SameWork {
		rows = append(rows, [2]string{"Note", "another edition: publisher, date and ISBN may not fit your file"})
	}
	return rows
}

// aiHelpRows lists the keys of the AI stage.
func aiHelpRows(state metaAIState) [][2]string {
	switch state {
	case aiRunning:
		return [][2]string{{"p", "show or hide the process"}, {"esc", "cancel"}}
	case aiFailed, aiNoMatch:
		return [][2]string{{"r", "ask the model again"}, {"p", "show or hide the process"}}
	}
	return nil
}

// --- Process view -----------------------------------------------------------

// The process view is the steps of the match in a scrolling viewport: the
// search terms the model suggested (and the web pages it cited), each catalogue
// search and its rows, and the comparison with the verdict on every row kept.
// It is hidden by default and opens with `p` on the AI page and on an AI list.

func (m metaPageModel) aiLogSize() (w, h int) { return max(1, m.width), max(1, m.height-7) }

func (m metaPageModel) openAILog() metaPageModel {
	w, h := m.aiLogSize()
	m.aiLogOpen = true
	m.aiLogView = viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
	m.aiLogView.SoftWrap = true
	return m.refreshAILog()
}

func (m metaPageModel) resizeAILog() metaPageModel {
	if !m.aiLogOpen {
		return m
	}
	w, h := m.aiLogSize()
	m.aiLogView.SetWidth(w)
	m.aiLogView.SetHeight(h)
	return m.refreshAILog()
}

// refreshAILog redraws the log, and follows a running match to its newest step.
func (m metaPageModel) refreshAILog() metaPageModel {
	m.aiLogView.SetContent(m.aiLogText())
	if m.aiRunning() {
		m.aiLogView.GotoBottom()
	}
	return m
}

func (m metaPageModel) aiLogText() string {
	events := m.aiLog
	if m.aiRunning() {
		events = m.aiRun.events()
	}
	width := max(20, m.width)
	titles := map[string]string{"plan": "Search terms", "search": "Catalogue search", "judge": "Comparison"}
	var lines []string
	phase := ""
	for _, e := range events {
		if e.Phase != phase {
			phase = e.Phase
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, sectionHeader(titles[phase], "", width))
		}
		lines = append(lines, "  "+safeText(e.Message))
	}
	switch {
	case m.aiRunning():
		lines = append(lines, "", "  "+m.spinner.View()+"working…")
	case m.aiErr != nil && m.stage == metaAI:
		lines = append(lines, "", "  "+colorRed(symbolError)+" "+aiErrorText(m.aiErr))
	case len(events) == 0:
		lines = append(lines, "  Nothing was recorded.")
	}
	return strings.Join(lines, "\n")
}

func (m metaPageModel) aiLogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p", "esc", "q", "enter", "ctrl+c":
		m.aiLogOpen = false
		return m, nil
	}
	var cmd tea.Cmd
	m.aiLogView, cmd = m.aiLogView.Update(msg)
	return m, cmd
}

func (m metaPageModel) aiLogScreen(subtitle string) tea.View {
	body := sectionHeader("AI match process", "", max(20, m.width)) + "\n" + m.aiLogView.View()
	status := ""
	if m.aiLogView.TotalLineCount() > m.aiLogView.Height() {
		status = strconv.Itoa(int(m.aiLogView.ScrollPercent()*100+0.5)) + "%"
	}
	return screenView(m.width, m.height, "Fix metadata", subtitle, ansi.Truncate(body, 1<<20, ""), status,
		uiKey("↑/↓", "scroll"), uiKey("p", "close"), uiKey("esc", "close"))
}
