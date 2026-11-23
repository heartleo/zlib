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

// fakeAI answers with out and err, and records what it was asked.
func fakeAI(out aimatch.Outcome, err error, asked *[]aimatch.Input) metaAIFunc {
	return func(_ context.Context, _ metaLookupFunc, in aimatch.Input, progress func(string)) (aimatch.Outcome, error) {
		if asked != nil {
			*asked = append(*asked, in)
		}
		progress("Comparing editions…")
		return out, err
	}
}

func rec(c metasource.Candidate, rel aimatch.Relation, conf float64, why string, evidence ...string) aimatch.Recommendation {
	return aimatch.Recommendation{Candidate: c, Relation: rel, Confidence: conf, Explanation: why, Evidence: evidence}
}

// aiPageAfterLookup is the results page of a standard search, with AI Match
// offered through fn.
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

func TestAIMatchShowsRecommendationsWithSourcesConfidenceAndWhy(t *testing.T) {
	out := aimatch.Outcome{Recommendations: []aimatch.Recommendation{
		rec(dune(), aimatch.SameEdition, 0.92, "ISBN and title agree", "https://example.com/dune"),
		rec(twoCandidates().Candidates[1], aimatch.SameWork, 0.6, "same book, newer printing"),
	}}
	m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil), nil))
	if m.aiState != aiResults {
		t.Fatalf("state = %v", m.aiState)
	}
	screen := metaScreen(m)
	for _, want := range []string{"AI recommendations", "1 of 2", "92%", "edition", "Before", "[", "same edition", "92% confident", "ISBN and title agree", "https://example.com/dune", "AI match"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	m, _ = metaStep(t, m, metaKey("down"))
	screen = metaScreen(m)
	if !strings.Contains(screen, "2 of 2") || !strings.Contains(screen, "another edition") {
		t.Errorf("the second row does not warn that it is another edition:\n%s", screen)
	}
}

func TestAIRecommendationOpensTheSameFieldsPageAndWritesNothingUntilConfirmed(t *testing.T) {
	var writes atomic.Int32
	write := func(string, *ebookmeta.Changes) error { writes.Add(1); return nil }
	out := aimatch.Outcome{Recommendations: []aimatch.Recommendation{rec(dune(), aimatch.SameEdition, 0.9, "ok")}}
	m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil), write))
	listCursor, listRes := m.cursor, m.res

	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields || m.cand.Title != "Before" {
		t.Fatalf("stage=%v cand=%q", m.stage, m.cand.Title)
	}
	if writes.Load() != 0 {
		t.Fatal("choosing a recommendation wrote the file")
	}
	// Esc returns to the recommendations, and Esc again to the untouched list.
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaAI || m.aiState != aiResults {
		t.Fatalf("stage=%v state=%v", m.stage, m.aiState)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates || m.cursor != listCursor || len(m.res.Candidates) != len(listRes.Candidates) {
		t.Fatalf("the standard results changed: stage=%v cursor=%d", m.stage, m.cursor)
	}
	if writes.Load() != 0 {
		t.Fatal("leaving wrote the file")
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
	if m.stage != metaCandidates || len(m.res.Candidates) != 2 {
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
	fn := func(ctx context.Context, _ metaLookupFunc, _ aimatch.Input, _ func(string)) (aimatch.Outcome, error) {
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
	if m.stage != metaCandidates {
		t.Fatalf("Esc left stage %v", m.stage)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the model call was not cancelled")
	}
	late := aimatch.Outcome{Recommendations: []aimatch.Recommendation{rec(dune(), aimatch.SameEdition, 0.9, "late")}}
	m, _ = metaStep(t, m, metaAIDone{gen: gen, out: late})
	if m.stage != metaCandidates {
		t.Fatalf("a late answer moved the page to stage %v", m.stage)
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

func TestAIOutputCannotInjectTerminalEscapes(t *testing.T) {
	out := aimatch.Outcome{Recommendations: []aimatch.Recommendation{
		rec(dune(), aimatch.SameEdition, 0.9, "x\x1b]0;pwned\x07y", "https://example.com/a"),
	}}
	m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil), nil))
	raw := m.View().Content
	if strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "\x07") {
		t.Fatalf("a terminal escape from the model reached the screen: %q", raw)
	}
}

func TestAIStageFitsCompactTerminals(t *testing.T) {
	out := aimatch.Outcome{Recommendations: []aimatch.Recommendation{
		rec(dune(), aimatch.SameEdition, 0.92, strings.Repeat("a long reason ", 20), "https://example.com/"+strings.Repeat("p", 80)),
		rec(twoCandidates().Candidates[1], aimatch.SameWork, 0.6, "short"),
	}}
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 14}, {40, 10}} {
		m := pressA(t, aiPageAfterLookup(t, fakeAI(out, nil, nil), nil))
		m = sized(t, m, size[0], size[1])
		screen := metaScreen(m)
		for line := range strings.SplitSeq(screen, "\n") {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, line)
			}
		}
		if !strings.Contains(screen, "esc") {
			t.Errorf("%dx%d: the footer lost esc:\n%s", size[0], size[1], screen)
		}
	}
}

func TestAIHelpListsTheKeysOfTheStageOnlyWhenOffered(t *testing.T) {
	m := aiPageAfterLookup(t, fakeAI(aimatch.Outcome{}, nil, nil), nil)
	m.helpStage = metaCandidates
	if !strings.Contains(joinHelp(m.metaHelpRows()), "AI match") {
		t.Fatal("the results help lacks the AI key")
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

func TestDefaultAIHookUsesTheVerifiedModelAndRanksCatalogueRows(t *testing.T) {
	isolateConfigHome(t)
	srv := aiEndpoint(t, `{"ranking":[{"id":"c1","relation":"same_edition","confidence":0.9,"explanation":"matches"},{"id":"c77","relation":"same_edition","confidence":0.99,"explanation":"invented"}]}`)
	s := aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: strings.Repeat("k", 24), Model: "m"}
	s.MarkVerified()
	if err := config.SaveAI(s); err != nil {
		t.Fatal(err)
	}
	m := metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil, withMetaAI(newDefaultMetaAI()))
	m = pressA(t, sized(t, m, 120, 30))
	if m.aiState != aiResults || len(m.aiOut.Recommendations) != 1 {
		t.Fatalf("state=%v recs=%d err=%v", m.aiState, len(m.aiOut.Recommendations), m.aiErr)
	}
	got := m.aiOut.Recommendations[0].Candidate
	if !reflect.DeepEqual(got, twoCandidates().Candidates[0]) {
		t.Fatalf("the recommended row is not the catalogue's row: %+v", got)
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
	// The same after the key is edited: a verified model goes stale on any change.
	s.MarkVerified()
	s.Model = "other"
	_ = config.SaveAI(s)
	m = metaPageAfterLookup(t, answerWith(twoCandidates(), nil, nil), nil, withMetaAI(newDefaultMetaAI()))
	if m = pressA(t, m); m.aiState != aiNeedsSetup {
		t.Fatalf("state after an edit = %v", m.aiState)
	}
}
