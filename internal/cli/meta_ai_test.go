package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// fakeAI answers with out and err, reports the given steps, and records what it
// was asked.
func fakeAI(out aimatch.Outcome, err error, asked *[]aimatch.Input, steps ...aimatch.Event) metaAIFunc {
	return func(_ context.Context, _ metaLookupFunc, in aimatch.Input, progress func(aimatch.Event)) (aimatch.Outcome, error) {
		if asked != nil {
			*asked = append(*asked, in)
		}
		for _, e := range steps {
			progress(e)
		}
		return out, err
	}
}

func rec(c metasource.Candidate, rel aimatch.Relation, conf float64, why string, evidence ...string) aimatch.Recommendation {
	return aimatch.Recommendation{Candidate: c, Relation: rel, Confidence: conf, Explanation: why, Evidence: evidence}
}

var processSteps = []aimatch.Event{
	{Phase: "plan", Message: "The model suggested 1 search(es)"},
	{Phase: "search", Message: `Searched "The Trial" by Franz Kafka (1937): 2 row(s)`},
	{Phase: "judge", Message: "Kept 1 row(s), left out 0 the model rejected or could not vouch for"},
}

// aiPageAfterLookup is the results page of a standard search, with AI Match
// offered through fn, on a terminal wide enough for the comparison panels.
func aiPageAfterLookup(t *testing.T, fn metaAIFunc, write metaWriteFunc) metaPageModel {
	t.Helper()
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), write, withMetaAI(fn))
	return sized(t, m, 120, 30)
}

// pressA presses a and feeds the answer of the match it starts.
func pressA(t *testing.T, m metaPageModel) metaPageModel {
	t.Helper()
	m, cmd := metaStep(t, m, key("a"))
	if m.stage != metaAI || cmd == nil {
		t.Fatalf("a did not start an AI match: stage=%v message=%q", m.stage, m.message)
	}
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaAIDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	return m
}

func goodOutcome() aimatch.Outcome {
	second := twoCandidates().Candidates[0]
	second.Title = "Before (2nd ed.)"
	second.Match, second.Score = metasource.MatchTitle, 0.71
	return aimatch.Outcome{Recommendations: []aimatch.Recommendation{
		rec(twoCandidates().Candidates[0], aimatch.SameEdition, 0.92, "ISBN and title agree", "https://example.com/dune"),
		rec(second, aimatch.SameWork, 0.6, "same book, newer printing"),
	}}
}

func TestAIMatchIsNotOfferedWithoutAModelHook(t *testing.T) {
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	m = sized(t, m, 120, 30)
	next, cmd := metaStep(t, m, key("a"))
	if next.stage != metaCandidates || cmd != nil {
		t.Fatalf("a changed the page: stage=%v", next.stage)
	}
	if strings.Contains(metaScreen(m), "AI") {
		t.Fatalf("the page mentions AI without being offered it:\n%s", metaScreen(m))
	}
}

func TestAIMatchIsOfferedOnResultsAndNoMatchPages(t *testing.T) {
	m := aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, nil, nil), nil)
	if !strings.Contains(metaScreen(m), "a AI match") {
		t.Fatalf("results footer lacks the AI key:\n%s", metaScreen(m))
	}
	none := metaPageAfterLookup(t, answerWith(metasource.Result{}, nil, nil), nil, withMetaAI(fakeAI(aimatch.Outcome{}, nil, nil)))
	if none.stage != metaFailed {
		t.Fatalf("stage = %v", none.stage)
	}
	none = sized(t, none, 120, 30)
	if !strings.Contains(metaScreen(none), "a AI match") {
		t.Fatalf("no-match footer lacks the AI key:\n%s", metaScreen(none))
	}
	if got := pressA(t, none); got.stage != metaAI || got.aiState != aiNoMatch {
		t.Fatalf("stage=%v state=%v", got.stage, got.aiState)
	}
}

func TestAIMatchSendsTheSearchAndFileNameOnly(t *testing.T) {
	var asked []aimatch.Input
	m := aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, nil, &asked), nil)
	pressA(t, m)
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	in := asked[0]
	if strings.ContainsAny(in.FileName, `/\`) || in.FileName != "book.epub" {
		t.Fatalf("file name = %q", in.FileName)
	}
	if len(in.Existing) != 2 || in.Query.Empty() {
		t.Fatalf("input = %+v", in)
	}
}

func TestAIResultsUseTheSameListAndComparisonAsAnySource(t *testing.T) {
	std := aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil), nil)
	standard := metaScreen(std)
	m := pressA(t, std)
	if m.stage != metaCandidates || !m.aiResults || len(m.res.Candidates) != 2 {
		t.Fatalf("stage=%v aiResults=%v rows=%d", m.stage, m.aiResults, len(m.res.Candidates))
	}
	screen := metaScreen(m)
	// The same columns and rows as the standard list, with the AI tag on the source.
	for _, want := range []string{"Match", "Title", "Authors", "Year", "Source", "ISBN  Before", " 71%  Before (2nd ed.)", "[AI+Open Library+Google Books]", "AI match results"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	// The match column is the catalogue's own score, not the model's confidence.
	if strings.Contains(screen, " 92%  ") || strings.Contains(screen, " 60%  ") {
		t.Errorf("the model's confidence is in the Match column:\n%s", screen)
	}
	// Side by side: the same panels the standard list shows, plus the verdict.
	if !m.compareFits() {
		t.Fatal("the comparison should fit at 120x30")
	}
	for _, marker := range comparePanelMarkers(t, standard) {
		if !strings.Contains(screen, marker) {
			t.Errorf("the comparison panels lack %q on the AI list:\n%s", marker, screen)
		}
	}
	if !strings.Contains(screen, "AI: same edition 92%") {
		t.Errorf("no verdict beside the comparison:\n%s", screen)
	}
}

// comparePanelMarkers are the words of the comparison panels of a standard
// results page: the AI list must draw the same ones.
func comparePanelMarkers(t *testing.T, standard string) []string {
	t.Helper()
	var out []string
	for _, w := range []string{"In the file", "Match", "Publisher", "Published"} {
		if strings.Contains(standard, w) {
			out = append(out, w)
		}
	}
	if len(out) < 2 {
		t.Fatalf("the standard page shows no comparison panels:\n%s", standard)
	}
	return out
}

func TestAIResultsKeepTheCataloguesOwnRowsAndOrder(t *testing.T) {
	out := goodOutcome()
	m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil), nil))
	for i, r := range out.Recommendations {
		got := m.res.Candidates[i]
		if !reflect.DeepEqual(got.Sources, append([]string{aiTag}, r.Candidate.Sources...)) {
			t.Errorf("row %d sources = %v", i, got.Sources)
		}
		got.Sources = r.Candidate.Sources
		if !reflect.DeepEqual(got, r.Candidate) {
			t.Errorf("row %d is not the catalogue's row:\n got %+v\nwant %+v", i, got, r.Candidate)
		}
	}
}

func TestAIEscReturnsToTheStandardResultsUntouched(t *testing.T) {
	std := aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil), nil)
	std, _ = metaStep(t, std, metaKey("down"))
	rows, cursor := std.res, std.cursor
	m := pressA(t, std)
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || m.aiResults || m.aiBase != nil || len(m.aiRecs) != 0 {
		t.Fatalf("stage=%v aiResults=%v", m.stage, m.aiResults)
	}
	if !reflect.DeepEqual(m.res, rows) || m.cursor != cursor {
		t.Fatalf("the standard results changed: cursor=%d want %d", m.cursor, cursor)
	}
	if strings.Contains(metaScreen(m), "AI match results") || strings.Contains(metaScreen(m), "[AI+") {
		t.Fatalf("AI rows are still on the page:\n%s", metaScreen(m))
	}
}

func TestAIEscFromTheNoMatchPageReturnsThere(t *testing.T) {
	none := metaPageAfterLookup(t, answerWith(metasource.Result{}, nil, nil), nil, withMetaAI(fakeAI(goodOutcome(), nil, nil)))
	m := pressA(t, sized(t, none, 120, 30))
	if m.stage != metaCandidates || !m.aiResults {
		t.Fatalf("stage=%v", m.stage)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaFailed || m.aiResults {
		t.Fatalf("stage=%v aiResults=%v", m.stage, m.aiResults)
	}
}

func TestAIRunAgainStartsFromTheStandardRowsNotTheTaggedOnes(t *testing.T) {
	var asked []aimatch.Input
	m := pressA(t, aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, &asked), nil))
	m = pressA(t, m) // a on the AI list
	if len(asked) != 2 || len(asked[1].Existing) != 2 {
		t.Fatalf("asked %d times, existing %d", len(asked), len(asked[1].Existing))
	}
	for _, c := range asked[1].Existing {
		if len(c.Sources) > 0 && c.Sources[0] == aiTag {
			t.Fatalf("the AI tag reached the model's input: %v", c.Sources)
		}
	}
	if m.aiBase == nil || len(m.aiBase.res.Candidates) != 2 {
		t.Fatal("the standard results were lost")
	}
}

func TestAIListSurvivesThePrepareRoundTripAndANewSearchLeavesAIMode(t *testing.T) {
	choices := []metaSourceChoice{{name: metasource.NameOpenLibrary, label: "Open Library", enabled: true, available: true}}
	factory := func([]string) metaLookupFunc { return answerWith(twoCandidates(), nil, nil) }
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil,
		withMetaAI(fakeAI(goodOutcome(), nil, nil)), withMetaSources(choices, factory))
	m = pressA(t, sized(t, m, 120, 30))
	m, _ = metaStep(t, m, key("s")) // the prepare page, over the AI list
	if m.stage != metaPrepare {
		t.Fatalf("stage = %v", m.stage)
	}
	back, _ := metaStep(t, m, key("esc"))
	if back.stage != metaCandidates || !back.aiResults || len(back.aiRecs) != 2 {
		t.Fatalf("Esc did not restore the AI list: stage=%v aiResults=%v", back.stage, back.aiResults)
	}
	back, _ = metaStep(t, back, key("esc"))
	if back.aiResults || len(back.res.Candidates) != 2 || back.res.Candidates[0].Sources[0] == aiTag {
		t.Fatalf("the second Esc did not reach the standard results: aiResults=%v", back.aiResults)
	}
	// A new standard search replaces the AI list.
	m = searchFromPrepare(t, m)
	if m.aiResults || m.aiBase != nil || len(m.aiRecs) != 0 {
		t.Fatalf("a new search kept AI mode: aiResults=%v", m.aiResults)
	}
}

func TestAIRecommendationOpensTheSameFieldsPageAndWritesNothingUntilConfirmed(t *testing.T) {
	var writes atomic.Int32
	write := func(string, *ebookmeta.Changes) error { writes.Add(1); return nil }
	m := pressA(t, aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil), write))
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields || m.cand.Title != "Before" {
		t.Fatalf("stage=%v cand=%q", m.stage, m.cand.Title)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || !m.aiResults {
		t.Fatalf("Esc on the fields page should return to the AI list: stage=%v aiResults=%v", m.stage, m.aiResults)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.aiResults || writes.Load() != 0 {
		t.Fatalf("aiResults=%v writes=%d", m.aiResults, writes.Load())
	}
}

func TestAIFailureStaysOnTheAIStageAndLeavesTheResultsAlone(t *testing.T) {
	m := pressA(t, aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, &aimatch.APIError{Status: 401, Message: "bad key"}, nil), nil))
	if m.aiState != aiFailed {
		t.Fatalf("state = %v", m.aiState)
	}
	screen := metaScreen(m)
	for _, want := range []string{"AI Match failed", "401", "Check the key with /config ai", "standard results are unchanged", "r retry"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || len(m.res.Candidates) != 2 || m.aiResults {
		t.Fatalf("stage=%v results=%d", m.stage, len(m.res.Candidates))
	}
}

func TestAINotReadyPointsToConfig(t *testing.T) {
	m := pressA(t, aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, errAINotReady, nil), nil))
	if m.aiState != aiNeedsSetup || !strings.Contains(metaScreen(m), "/config ai") {
		t.Fatalf("state=%v\n%s", m.aiState, metaScreen(m))
	}
}

func TestAINoMatchSaysSoAndChangesNothing(t *testing.T) {
	m := pressA(t, aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, nil, nil), nil))
	if m.aiState != aiNoMatch || !strings.Contains(metaScreen(m), "No reliable match") {
		t.Fatalf("state=%v\n%s", m.aiState, metaScreen(m))
	}
}

func TestAIEscCancelsARunningMatchAndALateAnswerIsDropped(t *testing.T) {
	stopped := make(chan struct{})
	fn := func(ctx context.Context, _ metaLookupFunc, _ aimatch.Input, _ func(aimatch.Event)) (aimatch.Outcome, error) {
		<-ctx.Done()
		close(stopped)
		return aimatch.Outcome{}, ctx.Err()
	}
	m := aiPageAfterLookup(t, fn, nil)
	m, cmd := metaStep(t, m, key("a"))
	if !m.aiRunning() || cmd == nil {
		t.Fatalf("stage=%v", m.stage)
	}
	if !strings.Contains(metaScreen(m), "Esc cancels") {
		t.Fatalf("no cancel hint:\n%s", metaScreen(m))
	}
	gen := m.aiGen
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || m.aiResults {
		t.Fatalf("Esc left stage %v", m.stage)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the model call was not cancelled")
	}
	m, _ = metaStep(t, m, metaAIDone{gen: gen, out: goodOutcome()})
	if m.stage != metaCandidates || m.aiResults {
		t.Fatalf("a late answer changed the page: stage=%v aiResults=%v", m.stage, m.aiResults)
	}
}

func TestAIMatchNeedsSomethingToSearchWith(t *testing.T) {
	m := aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, nil, nil), nil)
	m.query = metasource.Query{}
	next, cmd := metaStep(t, m, key("a"))
	if next.stage != metaCandidates || cmd != nil || !strings.Contains(next.message, "Nothing to match") {
		t.Fatalf("stage=%v message=%q", next.stage, next.message)
	}
}

func TestAIProcessViewIsHiddenByDefaultAndOpensWithP(t *testing.T) {
	m := pressA(t, aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil, processSteps...), nil))
	if m.aiLogOpen {
		t.Fatal("the process view must be hidden until asked for")
	}
	screen := metaScreen(m)
	for _, hidden := range []string{"Catalogue search", "Search terms", "The model suggested"} {
		if strings.Contains(screen, hidden) {
			t.Fatalf("the process leaks into the list (%q):\n%s", hidden, screen)
		}
	}
	if !strings.Contains(screen, "p process") {
		t.Fatalf("the footer does not offer the process view:\n%s", screen)
	}
	m, _ = metaStep(t, m, key("p"))
	if !m.aiLogOpen {
		t.Fatal("p did not open the process view")
	}
	screen = metaScreen(m)
	for _, want := range []string{"AI match process", "Search terms", "The model suggested 1 search(es)", "Catalogue search", `Searched "The Trial" by Franz Kafka (1937)`, "Comparison", "Kept 1 row(s)"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the process view lacks %q:\n%s", want, screen)
		}
	}
	// Esc closes the view only: the AI list stays.
	m, _ = metaStep(t, m, key("esc"))
	if m.aiLogOpen || m.stage != metaCandidates || !m.aiResults {
		t.Fatalf("Esc: open=%v stage=%v aiResults=%v", m.aiLogOpen, m.stage, m.aiResults)
	}
	m, _ = metaStep(t, m, key("p"))
	m, _ = metaStep(t, m, key("p"))
	if m.aiLogOpen {
		t.Fatal("p did not close the process view")
	}
}

func TestAIProcessViewIsNotOfferedOnAStandardList(t *testing.T) {
	m := aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil), nil)
	next, _ := metaStep(t, m, key("p"))
	if next.aiLogOpen {
		t.Fatal("p opened a process view on a standard list")
	}
	if strings.Contains(metaScreen(m), "p process") {
		t.Fatalf("the footer offers a process view on a standard list:\n%s", metaScreen(m))
	}
}

func TestAIProcessViewFollowsARunningMatch(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	fn := func(_ context.Context, _ metaLookupFunc, _ aimatch.Input, progress func(aimatch.Event)) (aimatch.Outcome, error) {
		progress(aimatch.Event{Phase: "plan", Message: "Asking the model for search terms (title, authors, year)…"})
		close(started)
		<-release
		progress(aimatch.Event{Phase: "search", Message: "Searched something: 1 row(s)"})
		return aimatch.Outcome{}, nil
	}
	m := aiPageAfterLookup(t, fn, nil)
	m, cmd := metaStep(t, m, key("a"))
	<-started
	if !strings.Contains(metaScreen(m), "Asking the model for search terms") {
		t.Fatalf("the running page does not show the latest step:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("p"))
	if !strings.Contains(metaScreen(m), "Search terms") || !strings.Contains(metaScreen(m), "working") {
		t.Fatalf("the open view lacks the running step:\n%s", metaScreen(m))
	}
	close(release)
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaAIDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	if !strings.Contains(metaScreen(m), "Searched something") || strings.Contains(metaScreen(m), "working") {
		t.Fatalf("the open view did not follow the match to its end:\n%s", metaScreen(m))
	}
}

func TestAIOutputCannotInjectTerminalEscapes(t *testing.T) {
	steps := []aimatch.Event{{Phase: "judge", Message: "x\x1b]0;pwned\x07y"}}
	out := aimatch.Outcome{Recommendations: []aimatch.Recommendation{
		rec(twoCandidates().Candidates[0], aimatch.SameEdition, 0.9, "x\x1b]0;pwned\x07y", "https://example.com/a\x1b[31m"),
	}}
	for _, width := range []int{120, 80} { // the comparison layout and the details one
		m := pressA(t, sized(t, aiPageAfterLookup(t, fakeAI(out, nil, nil, steps...), nil), width, 30))
		check := func(where, raw string) {
			if strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "\x07") {
				t.Fatalf("%d columns, %s: a terminal escape from the model reached the screen: %q", width, where, raw)
			}
		}
		check("list", m.View().Content)
		m, _ = metaStep(t, m, key("p"))
		check("process view", m.View().Content)
	}
}

func TestAIStageAndListFitCompactTerminals(t *testing.T) {
	out := goodOutcome()
	out.Recommendations[0].Explanation = strings.Repeat("a long reason ", 20)
	out.Recommendations[0].Evidence = []string{"https://example.com/" + strings.Repeat("p", 80)}
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 14}, {40, 10}} {
		m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil, processSteps...), nil))
		m = sized(t, m, size[0], size[1])
		views := map[string]metaPageModel{"list": m}
		opened, _ := metaStep(t, m, key("p"))
		views["process"] = opened
		for name, v := range views {
			screen := metaScreen(v)
			for line := range strings.SplitSeq(screen, "\n") {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%s %dx%d: line is %d wide: %q", name, size[0], size[1], w, line)
				}
			}
			if !strings.Contains(screen, "esc") {
				t.Errorf("%s %dx%d: the footer lost esc:\n%s", name, size[0], size[1], screen)
			}
		}
	}
}

func TestAIHelpListsTheKeysOfTheStageOnlyWhenTheyApply(t *testing.T) {
	m := aiPageAfterLookup(t, fakeAI(goodOutcome(), nil, nil), nil)
	m.helpStage = metaCandidates
	help := joinHelp(m.metaHelpRows())
	if !strings.Contains(help, "AI match") || strings.Contains(help, "process") {
		t.Fatalf("the standard list's help:\n%s", help)
	}
	ai := pressA(t, m)
	ai.helpStage = metaCandidates
	if !strings.Contains(joinHelp(ai.metaHelpRows()), "show the AI match process") {
		t.Fatal("the AI list's help lacks the process key")
	}
	plain := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil)
	plain.helpStage = metaCandidates
	if strings.Contains(joinHelp(plain.metaHelpRows()), "AI match") {
		t.Fatal("the help mentions AI on a page that does not offer it")
	}
}

func joinHelp(rows [][2]string) string {
	var sb strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s %s\n", r[0], r[1])
	}
	return sb.String()
}

// aiEndpoint answers the plan step with an empty plan and the judge step with
// the ranking, over the OpenAI-compatible chat route.
func aiEndpoint(t *testing.T, ranking string) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := `{}`
		if calls.Add(1) > 1 {
			answer = ranking
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDefaultAIHookUsesTheVerifiedModelAndShowsCatalogueRows(t *testing.T) {
	isolateConfigHome(t)
	srv := aiEndpoint(t, `{"ranking":[{"id":"c1","relation":"same_edition","confidence":0.9,"explanation":"matches"},{"id":"c77","relation":"same_edition","confidence":0.99,"explanation":"invented"}]}`)
	s := aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: strings.Repeat("k", 24), Model: "m"}
	s.MarkVerified()
	if err := config.SaveAI(s); err != nil {
		t.Fatal(err)
	}
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil, withMetaAI(newDefaultMetaAI()))
	m = pressA(t, sized(t, m, 120, 30))
	if !m.aiResults || len(m.res.Candidates) != 1 {
		t.Fatalf("aiResults=%v rows=%d err=%v", m.aiResults, len(m.res.Candidates), m.aiErr)
	}
	got, want := m.res.Candidates[0], twoCandidates().Candidates[0]
	got.Sources = want.Sources
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the listed row is not the catalogue's row: %+v", got)
	}
	if len(m.aiLog) == 0 {
		t.Fatal("the match recorded no process")
	}
}

func TestDefaultAIHookRefusesUnverifiedSettings(t *testing.T) {
	isolateConfigHome(t)
	s := aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: strings.Repeat("k", 24), Model: "m"}
	if err := config.SaveAI(s); err != nil { // never tested
		t.Fatal(err)
	}
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil, withMetaAI(newDefaultMetaAI()))
	m = pressA(t, m)
	if m.aiState != aiNeedsSetup {
		t.Fatalf("state = %v", m.aiState)
	}
	// The same after an edit: a verified model goes stale on any change.
	s.MarkVerified()
	s.Model = "other"
	_ = config.SaveAI(s)
	m = metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil, withMetaAI(newDefaultMetaAI()))
	if m = pressA(t, m); m.aiState != aiNeedsSetup {
		t.Fatalf("state after an edit = %v", m.aiState)
	}
}
