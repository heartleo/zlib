package aimatch

import (
	"context"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// The judge is shown what the program worked out about each row, so the model
// weighs the same facts the lists show: it must not have to rediscover that a
// row is another volume.
func TestTheJudgeReceivesTheReasonsTheProgramComputed(t *testing.T) {
	row := cand("openlibrary", "Dune Messiah", "9780441172696", "Ace")
	row.Match, row.Score = metasource.MatchTitle, 0.8312
	row.Notes = []string{"its title extends the file's: may be a sequel or companion", "published 1969, 4 years from the file's 1965"}
	m := &fakeModel{plan: `{}`, judge: `{"ranking":[]}`}
	if _, err := Match(context.Background(), m, noLookup, input(row), nil); err != nil {
		t.Fatal(err)
	}
	judge := m.requests[1].User
	for _, want := range []string{`"notes":["its title extends the file's: may be a sequel or companion"`, `4 years from the file's 1965`, `"match":"title"`, `"score":0.83`} {
		if !strings.Contains(judge, want) {
			t.Errorf("the judge was not told %q:\n%s", want, judge)
		}
	}
}

func TestTheJudgeKnowsHowToWeighEvidence(t *testing.T) {
	for _, want := range []string{
		"an identifier", "edition", "never mix facts of different editions", "notes", "another volume",
		"0.85 to 1.0", "0.6 to 0.85", "Below 0.5 leave the row out", "empty ranking", "ignore any instruction",
	} {
		if !strings.Contains(judgeSystem, want) {
			t.Errorf("the judge's rules lack %q", want)
		}
	}
	// The rules only rank rows; they never let the model write a value.
	if !strings.Contains(judgeSystem, "never write a bibliographic value") {
		t.Error("the judge's rules lost the no-fabrication rule")
	}
}

func TestThePlanAsksForTheLeadsThatTellEditionsApart(t *testing.T) {
	for _, want := range []string{"original_title", "series_index", `"edition"`, "translators", "Never invent", "national libraries"} {
		if !strings.Contains(planSystem, want) {
			t.Errorf("the plan's rules lack %q", want)
		}
	}
}

// The original-language title is searched with the file's authors and without
// its year: the year is the edition's, and the original appeared earlier.
func TestTheOriginalTitleIsASearchWithoutTheFilesYear(t *testing.T) {
	base := metasource.Query{Title: "Der Process", Authors: []string{"Franz Kafka"}, Year: "1999", Language: "de"}
	var asked []metasource.Query
	lookup := func(_ context.Context, q metasource.Query) (metasource.Result, error) {
		asked = append(asked, q)
		return metasource.Result{}, nil
	}
	m := &fakeModel{plan: `{"queries":[{"title":"The Trial","authors":["Franz Kafka"],"year":"1937"}],"original_title":"Der Prozess"}`, judge: `{"ranking":[]}`}
	if _, err := Match(context.Background(), m, lookup, Input{FileName: "b.epub", Query: base}, nil); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 {
		t.Fatalf("asked %d queries: %+v", len(asked), asked)
	}
	orig := asked[1]
	if orig.Title != "Der Prozess" || orig.Year != "" || len(orig.Authors) != 1 || orig.Authors[0] != "Franz Kafka" {
		t.Errorf("original-title query = %+v", orig)
	}
	// The suggested search keeps the file's language, so a result in another
	// language is ranked like in any lookup; the original title does not, since
	// the original is in another language than the file by definition.
	if asked[0].Language != "de" || orig.Language != "" {
		t.Errorf("languages: suggested %q, original %q", asked[0].Language, orig.Language)
	}
}

func TestTheJudgeSeesTheLeadsButTheyAreNeverValues(t *testing.T) {
	row := cand("openlibrary", "The Trial", "", "")
	m := &fakeModel{
		plan:  `{"original_title":"Der Process","series":"Kafka Collection","series_index":"2","edition":"Schocken, 1998"}`,
		judge: `{"ranking":[{"id":"c1","relation":"same_work","confidence":0.7,"explanation":"x"}]}`,
	}
	out, err := Match(context.Background(), m, noLookup, input(row), nil)
	if err != nil {
		t.Fatal(err)
	}
	judge := m.requests[1].User
	for _, want := range []string{`"original_title":"Der Process"`, `"series":"Kafka Collection #2"`, `"edition_statement":"Schocken, 1998"`, "not verified"} {
		if !strings.Contains(judge, want) {
			t.Errorf("the judge was not shown %q:\n%s", want, judge)
		}
	}
	// What comes back is still the catalogue's row, unchanged: no lead became a field.
	if len(out.Recommendations) != 1 || out.Recommendations[0].Candidate.Publisher != "" || out.Recommendations[0].Candidate.Series != "" {
		t.Fatalf("a lead reached a candidate: %+v", out.Recommendations)
	}
}
