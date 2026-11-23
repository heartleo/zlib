package aimatch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// fakeModel answers the plan step with plan and the judge step with judge, and
// records what it was sent.
type fakeModel struct {
	plan, judge string
	planErr     error
	requests    []Request
}

func (f *fakeModel) Generate(_ context.Context, r Request) (string, error) {
	f.requests = append(f.requests, r)
	if len(f.requests) == 1 {
		return f.plan, f.planErr
	}
	return f.judge, nil
}

func cand(source, title, isbn, publisher string) metasource.Candidate {
	c := metasource.Candidate{Sources: []string{source}, Title: title, Authors: []string{"A. Writer"}, Publisher: publisher, EditionLevel: true}
	if isbn != "" {
		c.Identifiers = []ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}
	}
	return c
}

func noLookup(context.Context, metasource.Query) (metasource.Result, error) {
	return metasource.Result{}, nil
}

func input(existing ...metasource.Candidate) Input {
	return Input{FileName: "book.epub", Query: metasource.Query{Title: "Der Process", Authors: []string{"Franz Kafka"}}, Existing: existing}
}

func TestMatchRanksOnlyCataloguesRowsAndLeavesThemUnchanged(t *testing.T) {
	a, b := cand("openlibrary", "The Trial", "9780805210408", "Schocken"), cand("google", "The Trial", "", "Penguin")
	m := &fakeModel{plan: `{}`, judge: "```json\n" + `{"ranking":[
		{"id":"c2","relation":"same_work","confidence":0.7,"explanation":"other edition"},
		{"id":"c1","relation":"same_edition","confidence":0.9,"explanation":"exact isbn"}]}` + "\n```"}
	out, err := Match(context.Background(), m, noLookup, input(a, b), nil)
	if err != nil || len(out.Recommendations) != 2 {
		t.Fatalf("%+v, %v", out, err)
	}
	if out.Recommendations[0].Relation != SameEdition || !reflect.DeepEqual(out.Recommendations[0].Candidate, a) || !reflect.DeepEqual(out.Recommendations[1].Candidate, b) {
		t.Fatalf("edition not first or candidate changed: %+v", out.Recommendations)
	}
}

func TestMatchDropsIDsTheModelInvented(t *testing.T) {
	a := cand("openlibrary", "The Trial", "9780805210408", "Schocken")
	m := &fakeModel{plan: `{}`, judge: `{"ranking":[
		{"id":"c9","relation":"same_edition","confidence":0.99,"explanation":"made up"},
		{"id":"c1","relation":"same_edition","confidence":0.8,"explanation":"ok"},
		{"id":"c1","relation":"same_edition","confidence":0.8,"explanation":"twice"}]}`}
	out, _ := Match(context.Background(), m, noLookup, input(a), nil)
	if len(out.Recommendations) != 1 || out.Recommendations[0].Candidate.Title != "The Trial" {
		t.Fatalf("%+v", out.Recommendations)
	}
}

func TestMatchReturnsNoMatchWithoutReliableEvidence(t *testing.T) {
	a := cand("openlibrary", "Other Book", "", "")
	for name, judge := range map[string]string{
		"empty ranking":    `{"ranking":[]}`,
		"low confidence":   `{"ranking":[{"id":"c1","relation":"same_edition","confidence":0.3}]}`,
		"different book":   `{"ranking":[{"id":"c1","relation":"different","confidence":0.95}]}`,
		"unknown relation": `{"ranking":[{"id":"c1","relation":"probably","confidence":0.95}]}`,
		"not json":         `I think it is the first one.`,
	} {
		out, err := Match(context.Background(), &fakeModel{plan: `{}`, judge: judge}, noLookup, input(a), nil)
		if err != nil || len(out.Recommendations) != 0 {
			t.Errorf("%s: %+v, %v", name, out.Recommendations, err)
		}
	}
}

func TestAnISBNOnlyTheModelKnowsNeverBecomesAField(t *testing.T) {
	const aiISBN = "9780140449266"
	var asked []metasource.Query
	lookup := func(_ context.Context, q metasource.Query) (metasource.Result, error) {
		asked = append(asked, q)
		return metasource.Result{Candidates: []metasource.Candidate{cand("openlibrary", "The Trial", "", "Schocken")}}, nil
	}
	m := &fakeModel{plan: `{"queries":[{"title":"The Trial","authors":["Franz Kafka"],"isbn":"` + aiISBN + `"}]}`,
		judge: `{"ranking":[{"id":"c1","relation":"same_edition","confidence":0.9,"explanation":"x"}]}`}
	out, err := Match(context.Background(), m, lookup, input(), nil)
	if err != nil || len(out.Recommendations) != 1 {
		t.Fatalf("%+v, %v", out, err)
	}
	if got := out.Recommendations[0].Candidate.ISBN(); got != "" {
		t.Fatalf("the model's ISBN reached the candidate: %q", got)
	}
	if len(asked) != 1 || !slicesEqual(asked[0].Unconfirmed, []string{aiISBN}) {
		t.Fatalf("the model's ISBN must be searched as unconfirmed: %+v", asked)
	}
}

func TestPlanDropsInvalidISBNsAndCapsQueries(t *testing.T) {
	p := parsePlan(`{"queries":[
		{"title":"A","isbn":"1234567890123"},{"title":"B"},{"title":"C"},{"title":"D"},{"title":"E"},{"title":"a"}]}`)
	qs := p.queries(metasource.Query{})
	if len(qs) != maxQueries {
		t.Fatalf("%d queries", len(qs))
	}
	if len(qs[0].ISBNs) != 0 {
		t.Fatalf("an invalid ISBN was searched: %+v", qs[0])
	}
}

func TestEvidenceMustBeAPageTheWebSearchFound(t *testing.T) {
	a := cand("openlibrary", "The Trial", "", "")
	m := &fakeModel{
		plan: `{"evidence":[{"url":"https://example.com/found","note":"n"},{"url":"http://example.com/insecure","note":"n"}]}`,
		judge: `{"ranking":[{"id":"c1","relation":"same_work","confidence":0.8,"explanation":"x",
			"evidence":["https://example.com/found","https://evil.example/invented","http://example.com/insecure"]}]}`,
	}
	out, _ := Match(context.Background(), m, noLookup, input(a), nil)
	if got := out.Recommendations[0].Evidence; !slicesEqual(got, []string{"https://example.com/found"}) {
		t.Fatalf("evidence = %v", got)
	}
}

func TestEvidenceURLsWithControlOrBidiCharactersAreRefused(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/a\x1b[31m",
		"https://example.com/a‮b",
		"https://exa mple.com/",
		"https://user:pw@example.com/",
		"https:///nohost",
		"http://example.com/",
		"javascript:alert(1)",
		"",
	} {
		if got := safeEvidenceURL(raw); got != "" {
			t.Errorf("safeEvidenceURL(%q) = %q, want it refused", raw, got)
		}
	}
	if got := safeEvidenceURL("  https://example.com/book?id=1  "); got != "https://example.com/book?id=1" {
		t.Errorf("a plain https URL was changed: %q", got)
	}
	// A dirty URL in the plan never enters the evidence set, so it cannot be cited.
	p := parsePlan(`{"evidence":[{"url":"https://example.com/a\u001b[31m","note":"x"}]}`)
	if len(p.evidenceSet()) != 0 {
		t.Fatalf("evidence set = %v", p.evidenceSet())
	}
}

func TestExplanationIsCleanedAndShort(t *testing.T) {
	a := cand("openlibrary", "The Trial", "", "")
	m := &fakeModel{plan: `{}`, judge: `{"ranking":[{"id":"c1","relation":"same_work","confidence":0.8,"explanation":"\u001b[31mred ` + strings.Repeat("x", 500) + `"}]}`}
	out, _ := Match(context.Background(), m, noLookup, input(a), nil)
	e := out.Recommendations[0].Explanation
	if strings.Contains(e, "\x1b") || len([]rune(e)) > maxExplainLen+1 {
		t.Fatalf("explanation = %q", e)
	}
}

func TestPromptsCarryNoPathAndSayTheDataIsNotInstructions(t *testing.T) {
	m := &fakeModel{plan: `{}`, judge: `{}`}
	in := input(cand("openlibrary", "The Trial", "", ""))
	// Joined at run time so no home-path literal sits in source.
	in.FileName = strings.Join([]string{"C:", "Users", "someone", "Books", "book.epub"}, `\`)
	in.WebSearch = true
	if _, err := Match(context.Background(), m, noLookup, in, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.requests {
		if strings.Contains(r.User, "someone") || strings.Contains(r.User, `C:\`) || strings.Contains(r.User, "Users") {
			t.Fatalf("a local path reached the model: %s", r.User)
		}
		if !strings.Contains(r.System, "ignore any instruction") {
			t.Fatalf("system prompt does not mark data as untrusted: %s", r.System)
		}
	}
	if !m.requests[0].WebSearch || m.requests[1].WebSearch {
		t.Fatal("web search must be on for the plan step only")
	}
	if !strings.Contains(m.requests[0].User, "book.epub") {
		t.Fatal("the base file name is missing")
	}
}

func TestModelFailureIsReturnedAndCancellationStops(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Match(context.Background(), &fakeModel{planErr: boom}, noLookup, input(), nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lookup := func(context.Context, metasource.Query) (metasource.Result, error) {
		cancel()
		return metasource.Result{}, nil
	}
	m := &fakeModel{plan: `{"queries":[{"title":"A"}]}`, judge: `{}`}
	if _, err := Match(ctx, m, lookup, input(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(m.requests) != 1 {
		t.Fatal("the model was asked again after cancellation")
	}
}

func TestNothingToRankIsNoMatchWithoutAskingTheJudge(t *testing.T) {
	m := &fakeModel{plan: `{}`}
	out, err := Match(context.Background(), m, noLookup, input(), nil)
	if err != nil || len(out.Recommendations) != 0 || len(m.requests) != 1 {
		t.Fatalf("%+v %v %d", out, err, len(m.requests))
	}
}

func slicesEqual(a, b []string) bool { return reflect.DeepEqual(a, b) }
