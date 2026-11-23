package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
)

// AI Match is one extra stage of the metadata page (metaAI), opened by hand
// with `a` from the results or the no-match page. It never replaces or changes
// the standard lookup: Esc returns to those pages untouched, a failure stays on
// this stage, and a file is written only through the same fields page, after
// the user picks a row and confirms.

// errAINotReady means no AI model has passed the connection test.
var errAINotReady = errors.New("no AI model has passed the connection test")

type (
	// metaAIFunc runs the AI match; lookup searches the catalogues with the
	// sources the page uses now.
	metaAIFunc func(ctx context.Context, lookup metaLookupFunc, in aimatch.Input, progress func(string)) (aimatch.Outcome, error)

	metaAIDone struct {
		gen int
		out aimatch.Outcome
		err error
	}
)

// metaAIState is what the AI stage shows.
type metaAIState int

const (
	aiRunning metaAIState = iota
	aiNeedsSetup
	aiFailed
	aiNoMatch
	aiResults
)

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
	return func(ctx context.Context, lookup metaLookupFunc, in aimatch.Input, progress func(string)) (aimatch.Outcome, error) {
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

// startAI begins an AI match for the search as it stands. A late answer to an
// earlier match is dropped: the generation moves on.
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
	m.aiFrom = m.stage
	m.aiGen++
	m.stage, m.aiState, m.aiErr, m.aiOut, m.aiCursor, m.message = metaAI, aiRunning, nil, aimatch.Outcome{}, 0, ""
	run := newMetaRun()
	m.aiRun = run
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	fn, lookup, gen := m.ai, m.lookup, m.aiGen
	in := aimatch.Input{
		FileName: filepath.Base(m.path), Query: m.query, Existing: slices.Clone(m.res.Candidates),
	}
	if m.current != nil {
		in.Languages = m.current.Languages
	}
	go func() {
		out, err := fn(ctx, lookup, in, func(s string) { run.progress.Store(&s) })
		run.ch <- metaAIDone{gen: gen, out: out, err: err}
	}()
	return m, tea.Batch(m.spinner.Tick, waitForMsg(run.ch))
}

// aiDone takes the answer of the current match.
func (m metaPageModel) aiDone(msg metaAIDone) metaPageModel {
	m.aiOut, m.aiErr = msg.out, msg.err
	switch {
	case errors.Is(msg.err, errAINotReady):
		m.aiState = aiNeedsSetup
	case msg.err != nil:
		m.aiState = aiFailed
	case len(msg.out.Recommendations) == 0:
		m.aiState = aiNoMatch
	default:
		m.aiState, m.aiCursor = aiResults, 0
	}
	return m
}

// leaveAI returns to the page AI Match was opened from, untouched.
func (m metaPageModel) leaveAI() metaPageModel {
	if m.aiRun != nil {
		m.aiRun.stop()
	}
	m.aiGen++ // an answer still on its way is dropped
	m.stage, m.message, m.aiToFields = m.aiFrom, "", false
	return m
}

func (m metaPageModel) aiKey(k string) (tea.Model, tea.Cmd) {
	if m.aiState == aiRunning {
		if k == "esc" || k == "ctrl+c" {
			return m.leaveAI(), nil
		}
		return m, nil
	}
	switch k {
	case "esc":
		return m.leaveAI(), nil
	case "ctrl+c", "q":
		m.cancelled = true
		return m, tea.Quit
	case "r":
		if m.aiState == aiFailed || m.aiState == aiNoMatch {
			return m.startAI()
		}
	case "enter":
		if m.aiState == aiResults {
			rec := m.aiOut.Recommendations[m.aiCursor]
			m = m.openFieldsFor(rec.Candidate)
			m.aiToFields = true
			return m, m.matchCoverCmd(m.cand.CoverURL)
		}
	default:
		if m.aiState == aiResults {
			m.aiCursor = listCursor(k, m.aiCursor, len(m.aiOut.Recommendations), m.aiListRows())
		}
	}
	return m, nil
}

func (m metaPageModel) aiListRows() int {
	return max(1, min(len(m.aiOut.Recommendations), max(3, (m.height-12)/2)))
}

func (m metaPageModel) aiKeys() []keys.Binding {
	switch m.aiState {
	case aiRunning:
		return []keys.Binding{uiKey("esc", "cancel")}
	case aiResults:
		return m.keysFor(
			[][2]string{{"↑/↓", "choose"}, {"enter", "review fields"}, {"?", "keys"}, {"esc", "back"}},
			[][2]string{{"↑/↓", "choose"}, {"enter", "fields"}, {"esc", "back"}},
			[][2]string{{"enter", ""}, {"esc", ""}})
	case aiFailed, aiNoMatch:
		return m.keysFor([][2]string{{"r", "retry"}, {"?", "keys"}, {"esc", "back"}}, [][2]string{{"r", "retry"}, {"esc", "back"}}, [][2]string{{"esc", ""}})
	}
	return m.keysFor([][2]string{{"?", "keys"}, {"esc", "back"}}, [][2]string{{"esc", ""}})
}

// aiStatus is the line under the body: the catalogue failures behind the
// answer, the same way the results page reports them.
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
		if p := m.aiRun.progress.Load(); p != nil {
			step = safeText(*p)
		}
		lines := []string{"  " + m.spinner.View() + step, ""}
		rows := [][2]string{{"Title", m.query.Title}, {"Authors", strings.Join(m.query.Authors, " & ")}, {"ISBN", strings.Join(m.query.ISBNs, ", ")}}
		var shown [][2]string
		for _, r := range rows {
			if r[1] != "" {
				shown = append(shown, [2]string{r[0], safeText(r[1])})
			}
		}
		lines = append(lines, formatDetailRows(shown, "    ", colorFaint)...)
		lines = append(lines, "", "    "+colorFaint("Esc cancels. The file and the results are not changed."))
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
	case aiNoMatch:
		return strings.Join([]string{"  No reliable match", "",
			"    The model found no catalogue record it could vouch for.",
			"    Nothing is changed. Press e on the previous page to change the search."}, "\n")
	}
	return m.aiResultsBody()
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

// aiRelationLabel is how a recommendation relates to the file.
func aiRelationLabel(r aimatch.Recommendation) string {
	if r.Relation == aimatch.SameEdition {
		return "same edition"
	}
	return "same work, other edition"
}

func (m metaPageModel) aiResultsBody() string {
	recs := m.aiOut.Recommendations
	width := max(20, m.width)
	rows := m.aiListRows()
	top := max(0, m.aiCursor-rows+1)
	var lines []string
	for i := top; i < min(len(recs), top+rows); i++ {
		r := recs[i]
		tag := "edition"
		if r.Relation == aimatch.SameWork {
			tag = "work   "
		}
		who := strings.Join(r.Candidate.Authors, " & ")
		label := fmt.Sprintf("%3d%%  %s  %s", int(r.Confidence*100+0.5), tag, safeText(r.Candidate.Title))
		if who != "" {
			label += " — " + safeText(who)
		}
		label += "  [" + safeText(strings.Join(r.Candidate.Sources, ", ")) + "]"
		lines = append(lines, styledListRow(label, i == m.aiCursor, width, currentTheme))
	}
	lines = append(lines, "")
	r := recs[m.aiCursor]
	detail := [][2]string{{"AI says", fmt.Sprintf("%s, %d%% confident", aiRelationLabel(r), int(r.Confidence*100+0.5))}}
	if r.Explanation != "" {
		detail = append(detail, [2]string{"Why", safeText(r.Explanation)})
	}
	for i, u := range r.Evidence {
		label := ""
		if i == 0 {
			label = "Evidence"
		}
		detail = append(detail, [2]string{label, safeText(u)})
	}
	detail = append(detail, metaDetailRows(r.Candidate)...)
	if r.Relation == aimatch.SameWork {
		detail = append(detail, [2]string{"Note", "another edition: publisher, date and ISBN may not fit your file"})
	}
	for _, l := range formatDetailRows(detail, "  ", colorFaint) {
		lines = append(lines, ansi.Truncate(l, width, "…"))
	}
	head := sectionHeader("AI recommendations", fmt.Sprintf("%d of %d", m.aiCursor+1, len(recs)), width)
	return head + "\n" + strings.Join(lines, "\n")
}

// aiHelpRows lists the keys of the AI stage.
func aiHelpRows(state metaAIState) [][2]string {
	switch state {
	case aiResults:
		return [][2]string{{"↑/↓", "choose a recommendation"}, {"pgup/pgdown", "move a page"}, {"enter", "review its fields (nothing is written yet)"}}
	case aiFailed, aiNoMatch:
		return [][2]string{{"r", "ask the model again"}}
	}
	return nil
}
