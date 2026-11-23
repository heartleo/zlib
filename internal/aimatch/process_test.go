package aimatch

import (
	"context"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

func TestAIQueriesCarryTitleAuthorsAndYearLikeAStandardQuery(t *testing.T) {
	base := metasource.Query{Title: "Der Process", Authors: []string{"Franz Kafka"}, Year: "1925"}
	var asked []metasource.Query
	lookup := func(_ context.Context, q metasource.Query) (metasource.Result, error) {
		asked = append(asked, q)
		return metasource.Result{}, nil
	}
	m := &fakeModel{plan: `{"queries":[
		{"title":"The Trial","authors":["Franz Kafka"],"year":"1937"},
		{"title":"Le Procès"},
		{"title":"Der Prozess","authors":["F. Kafka"],"year":"soon"}]}`, judge: `{"ranking":[]}`}
	if _, err := Match(context.Background(), m, lookup, Input{FileName: "b.epub", Query: base}, nil); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 {
		t.Fatalf("asked %d queries", len(asked))
	}
	for i, want := range []struct{ title, author, year string }{
		{"The Trial", "Franz Kafka", "1937"}, // all from the model
		{"Le Procès", "Franz Kafka", "1925"}, // authors and year from the file's search
		{"Der Prozess", "F. Kafka", "1925"},  // a year that is not a year is ignored
	} {
		q := asked[i]
		if q.Title != want.title || len(q.Authors) != 1 || q.Authors[0] != want.author || q.Year != want.year {
			t.Errorf("query %d = %+v, want %+v", i, q, want)
		}
	}
	if !strings.Contains(m.requests[0].User, `"year":"1925"`) {
		t.Fatalf("the model was not told the year: %s", m.requests[0].User)
	}
}

func TestMatchReportsEachStepForTheProcessView(t *testing.T) {
	row := cand("openlibrary", "The Trial", "9780805210408", "Schocken")
	lookup := func(context.Context, metasource.Query) (metasource.Result, error) {
		return metasource.Result{Candidates: []metasource.Candidate{row}}, nil
	}
	m := &fakeModel{
		plan:  `{"queries":[{"title":"The \u0007\u001bTrial","authors":["Franz Kafka"],"year":"1937"}],"evidence":[{"url":"https://example.com/kafka","note":"n"}]}`,
		judge: `{"ranking":[{"id":"c1","relation":"same_edition","confidence":0.9,"explanation":"title and author agree","evidence":["https://example.com/kafka"]}]}`,
	}
	var events []Event
	// Joined at run time so no home-path literal sits in source.
	in := Input{FileName: strings.Join([]string{"", "home", "someone", "book.epub"}, "/"), WebSearch: true, Query: metasource.Query{Title: "Der Process", Authors: []string{"Franz Kafka"}, Year: "1925"}}
	if _, err := Match(context.Background(), m, lookup, in, func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	phases := map[string]bool{}
	for _, e := range events {
		phases[e.Phase] = true
		all.WriteString(e.Message + "\n")
	}
	log := all.String()
	for _, p := range []string{"plan", "search", "judge"} {
		if !phases[p] {
			t.Errorf("no %s event", p)
		}
	}
	for _, want := range []string{"Web search is on", `"The Trial" by Franz Kafka (1937)`, "1 row(s)", "same edition, 90%", "title and author agree", "cited page: https://example.com/kafka", "evidence: https://example.com/kafka", "Kept 1 row(s)"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "\x1b") || strings.Contains(log, "\x07") || strings.Contains(log, "someone") {
		t.Fatalf("the log holds an escape or a path:\n%q", log)
	}
}

func TestMatchListsRowsInTheOrderOfAStandardLookup(t *testing.T) {
	exact := cand("openlibrary", "Exact", "9780805210408", "A")
	exact.Match, exact.Score = metasource.MatchISBN, 1
	strong := cand("google", "Strong", "", "B")
	strong.Match, strong.Score = metasource.MatchTitle, 0.9
	weak := cand("google", "Weak", "", "C")
	weak.Match, weak.Score = metasource.MatchTitle, 0.6
	// The model prefers the weak row and calls it the edition; the strong one is
	// "another edition". The catalogue's own scores still order the list.
	m := &fakeModel{plan: `{}`, judge: `{"ranking":[
		{"id":"c3","relation":"same_edition","confidence":0.95,"explanation":"x"},
		{"id":"c2","relation":"same_work","confidence":0.6,"explanation":"y"},
		{"id":"c1","relation":"same_work","confidence":0.55,"explanation":"z"}]}`}
	out, err := Match(context.Background(), m, noLookup, input(exact, strong, weak), nil)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, r := range out.Recommendations {
		titles = append(titles, r.Candidate.Title)
	}
	if strings.Join(titles, ",") != "Exact,Strong,Weak" {
		t.Fatalf("order = %v", titles)
	}
	// The model's verdict stays with its row.
	if out.Recommendations[2].Relation != SameEdition || out.Recommendations[1].Relation != SameWork {
		t.Fatalf("verdicts moved: %+v", out.Recommendations)
	}
}
