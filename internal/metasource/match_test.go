package metasource

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestNormalizeISBN(t *testing.T) {
	for in, want := range map[string]string{
		"9780140328721":         "9780140328721",
		"978-0-14-032872-1":     "9780140328721",
		"0140328726":            "9780140328721",
		"0-14-032872-6":         "9780140328721",
		"ISBN 0-14-032872-6":    "9780140328721",
		"isbn:9780140328721":    "9780140328721",
		"080442957X":            "9780804429573",
		"9780140328722":         "", // bad check digit
		"0140328727":            "",
		"1111111111111":         "",
		"12345":                 "",
		"":                      "",
		"978014032872\x1b[31m1": "",
	} {
		if got := NormalizeISBN(in); got != want {
			t.Errorf("NormalizeISBN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripHTMLDatesAndLanguages(t *testing.T) {
	if got := stripHTML(`<div>Frank&#8217;s <b>classic</b><br>SOON &amp; more</div><p>Next</p>`); got != "Frank’s classic\nSOON & more\n\nNext" {
		t.Errorf("stripHTML = %q", got)
	}
	for in, want := range map[string]string{
		"2003-08-26": "2003-08-26", "2003-08": "2003-08", "2003": "2003", "March 14, 2002": "2002-03-14",
		"Mar 2002": "2002-03", "14 March 2002": "2002-03-14", "2002-03-14T21:00:00+00:00": "2002-03-14",
		"c1999": "1999", "n.d.": "", "": "", "2003-13-45": "2003",
	} {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"eng": "en", "/languages/chi": "zh", "en-US": "en-us", "zh": "zh", "xx1": "", "": ""} {
		if got := normalizeLanguage(in); got != want {
			t.Errorf("normalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSimilarityHandlesCJKAndSubtitles(t *testing.T) {
	if s := titleSimilarity("The War of Art", "The War of Art: Winning the Inner Creative Battle"); s < 0.9 {
		t.Errorf("subtitle penalized: %v", s)
	}
	if s := titleSimilarity("人性的弱点增订全译版", "人性的弱点"); s < 0.5 {
		t.Errorf("CJK title similarity = %v", s)
	}
	if s := titleSimilarity("原则", "原则"); s != 1 {
		t.Errorf("two-character CJK title = %v", s)
	}
	if s := titleSimilarity("基督山伯爵", "红与黑"); s > 0.2 {
		t.Errorf("unrelated CJK titles = %v", s)
	}
	if s := titleSimilarity("Dune", "Pride and Prejudice"); s > 0.3 {
		t.Errorf("unrelated titles = %v", s)
	}
	if s := authorSimilarity([]string{"[美] 罗兰・米勒 著"}, []string{"罗兰·米勒"}); s < 0.9 {
		t.Errorf("tagged CJK author = %v", s)
	}
	if s := authorSimilarity([]string{"Herbert, Frank"}, []string{"Frank Herbert"}); s != 1 {
		t.Errorf("reordered name = %v", s)
	}
	if s := authorSimilarity([]string{"Frank Herbert"}, []string{"Brian Herbert"}); s > 0.8 {
		t.Errorf("different first names = %v", s)
	}
	if s := authorSimilarity(nil, []string{"Anyone"}); s != 1 {
		t.Errorf("no wanted author = %v", s)
	}
}

func cand(source, title string, authors []string, isbn string) Candidate {
	c := Candidate{Sources: []string{source}, Title: title, Authors: authors, EditionLevel: isbn != ""}
	if isbn != "" {
		c.Identifiers = []ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}
	}
	return c
}

func TestMergePoolsByISBNAndAppliesFieldRules(t *testing.T) {
	a := cand("A", "Dune", []string{"Frank Herbert"}, "9780441013593")
	a.Publisher, a.Published, a.Description = "Ace", "2005-08-02", "Short."
	a.Tags = []string{"Fiction", "Accessible book", "Science Fiction"}
	a.coverRank, a.CoverURL = 1, "https://thumb"
	b := cand("B", "Dune: Book One", []string{"Frank Herbert"}, "9780441013593")
	b.Publisher, b.Published, b.Description = "Ace", "2005", "A much longer description of the book."
	b.Tags = []string{"science fiction", "Classics"}
	b.CoverURL = "https://large"
	got := rankAndMerge(Query{Title: "Dune", ISBNs: []string{"9780441013593"}}, []Candidate{a, b}, 0.45, 10)
	if len(got) != 1 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	c := got[0]
	if c.Title != "Dune" || c.Publisher != "Ace" || c.Published != "2005-08-02" || c.Description != b.Description ||
		c.CoverURL != "https://large" || !reflect.DeepEqual(c.Sources, []string{"A", "B"}) ||
		!reflect.DeepEqual(c.Tags, []string{"Science Fiction", "Fiction", "Classics"}) {
		t.Fatalf("merged = %+v", c)
	}
	if c.Match != MatchISBN || c.Score != 1 {
		t.Errorf("match = %s %v", c.Match, c.Score)
	}
}

func TestDifferentISBNsStayDifferentEditions(t *testing.T) {
	a := cand("A", "Dune", []string{"Frank Herbert"}, "9780441013593")
	b := cand("B", "Dune", []string{"Frank Herbert"}, "9780340960196")
	work := cand("C", "Dune", []string{"Frank Herbert"}, "")
	work.Published = "1965"
	got := rankAndMerge(Query{Title: "Dune", Authors: []string{"Frank Herbert"}}, []Candidate{a, b, work}, 0.45, 10)
	isbns := map[string]bool{}
	for _, c := range got {
		isbns[c.ISBN()] = true
	}
	if len(got) != 2 || !isbns["9780441013593"] || !isbns["9780340960196"] {
		t.Fatalf("editions merged: %+v", got)
	}
}

func TestRankingPrefersISBNThenScoreAndDropsNoise(t *testing.T) {
	q := Query{Title: "The War of Art", Authors: []string{"Steven Pressfield"}, ISBNs: []string{"9781590710036"}}
	exact := cand("A", "The War of Art", []string{"Steven Pressfield"}, "9781590710036")
	near := cand("A", "The War of Art: Winning the Inner Creative Battle", []string{"Steven Pressfield"}, "9780446691437")
	wrongAuthor := cand("B", "The War of Art", []string{"Somebody Else"}, "9780000000002")
	unrelated := cand("B", "Cooking for Two", []string{"Steven Pressfield"}, "9780306406157")
	got := rankAndMerge(q, []Candidate{unrelated, wrongAuthor, near, exact}, 0.45, 10)
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Title)
	}
	want := []string{"The War of Art", "The War of Art: Winning the Inner Creative Battle", "The War of Art"}
	if !reflect.DeepEqual(titles, want) || got[0].Match != MatchISBN || got[1].Score <= got[2].Score {
		t.Fatalf("titles = %v scores = %v %v", titles, got[1].Score, got[2].Score)
	}
}

func TestCJKTitleMatchesWithoutSpaces(t *testing.T) {
	q := Query{Title: "人性的弱点增订全译版", Authors: []string{"卡耐基"}}
	hit := cand("A", "人性的弱点", []string{"戴尔·卡耐基"}, "")
	miss := cand("A", "红与黑", []string{"司汤达"}, "")
	got := rankAndMerge(q, []Candidate{miss, hit}, 0.45, 10)
	if len(got) != 1 || got[0].Title != "人性的弱点" {
		t.Fatalf("got %+v", got)
	}
}

type fakeSource struct {
	name  string
	cands []Candidate
	err   error
	delay time.Duration
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Identify(ctx context.Context, q Query) ([]Candidate, error) {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.cands, f.err
}

func TestLookupReportsPartialFailure(t *testing.T) {
	q := Query{Title: "Dune", ISBNs: []string{"9780441013593"}}
	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Down", err: ErrRateLimited},
		fakeSource{name: "Up", cands: []Candidate{cand("", "Dune", []string{"Frank Herbert"}, "9780441013593")}},
	}, q, Options{})
	if err != nil || len(res.Candidates) != 1 || len(res.Errors) != 1 || res.Errors[0].Source != "Down" ||
		!errors.Is(res.Errors[0], ErrRateLimited) || res.Candidates[0].Sources[0] != "Up" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestLookupAllFailedAndNothingFound(t *testing.T) {
	q := Query{Title: "Dune"}
	_, err := Lookup(context.Background(), []Source{fakeSource{name: "A", err: ErrRateLimited}, fakeSource{name: "B", err: errors.New("boom")}}, q, Options{})
	if !errors.Is(err, ErrAllSourcesFailed) {
		t.Fatalf("err = %v", err)
	}
	res, err := Lookup(context.Background(), []Source{fakeSource{name: "A"}}, q, Options{})
	if err != nil || len(res.Candidates) != 0 {
		t.Fatalf("nothing found should not be an error: %+v, %v", res, err)
	}
	if _, err := Lookup(context.Background(), nil, Query{}, Options{}); !errors.Is(err, ErrNoQuery) {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupStopsWaitingAfterGrace(t *testing.T) {
	q := Query{Title: "Dune"}
	start := time.Now()
	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Fast", cands: []Candidate{cand("", "Dune", nil, "")}},
		fakeSource{name: "Slow", delay: 5 * time.Second},
	}, q, Options{Grace: 50 * time.Millisecond})
	if err != nil || len(res.Candidates) != 1 || time.Since(start) > 2*time.Second {
		t.Fatalf("res = %+v, err = %v, took %v", res, err, time.Since(start))
	}
	if len(res.Errors) != 1 || res.Errors[0].Source != "Slow" || res.Errors[0].Err.Error() != "did not answer in time" {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestQueryFromMetadataUsesValidISBNOnly(t *testing.T) {
	m := &ebookmeta.Metadata{Title: "T", Authors: []string{"A"}, Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "0-14-032872-6"}}}
	if q := QueryFromMetadata(m); !reflect.DeepEqual(q.ISBNs, []string{"9780140328721"}) {
		t.Fatalf("q = %+v", q)
	}
	m.Identifiers[0].Value = "not an isbn"
	if q := QueryFromMetadata(m); len(q.ISBNs) != 0 {
		t.Fatalf("q = %+v", q)
	}
}

func FuzzNormalizers(f *testing.F) {
	for _, s := range []string{"978-0-14-032872-1", "<p>x &amp; y</p>", "March 14, 2002", "[美] 罗兰・米勒 著", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		NormalizeISBN(s)
		stripHTML(s)
		normalizeDate(s)
		normalizeLanguage(s)
		personKey(s)
		titleSimilarity(s, "The War of Art")
		authorSimilarity([]string{s}, []string{"Frank Herbert"})
	})
}

func TestISBNMatchWithADifferentTitleIsFlagged(t *testing.T) {
	q := Query{Title: "Matilda", Authors: []string{"Roald Dahl"}, ISBNs: []string{"9780140328721", "9780441013593"}}
	other := cand("A", "Fantastic Mr. Fox", []string{"Roald Dahl"}, "9780140328721")
	agree := cand("A", "Matilda", []string{"Roald Dahl"}, "9780441013593")
	got := rankAndMerge(q, []Candidate{other, agree}, 0.45, 10)
	if len(got) != 2 || got[0].Title != "Matilda" || got[0].Score != 1 || got[1].Match != MatchISBN || got[1].Score != 0.5 {
		t.Fatalf("got %+v", got)
	}
}

func TestTagNoiseIsDropped(t *testing.T) {
	a := cand("A", "Matilda", nil, "")
	a.Tags = []string{"Fiction", "Spanish language", "Translations from English", "Romans, nouvelles, etc. pour la jeunesse", "Humorous stories", "Protected DAISY"}
	if got := mergeTags([]Candidate{a}); !reflect.DeepEqual(got, []string{"Fiction", "Humorous stories"}) {
		t.Fatalf("tags = %v", got)
	}
}
