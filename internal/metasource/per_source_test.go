package metasource

import (
	"context"
	"reflect"
	"testing"
)

// principles is one book as a source describes it, with the ISBN every source
// agrees on and only what that source knows.
func principles(publisher string) Candidate {
	c := cand("", "Principles", []string{"Ray Dalio"}, "9781501124020")
	c.Publisher = publisher
	return c
}

// The same book from three sources is three rows, one per source, each with
// only what its own source said: nothing is joined, so no row borrows a
// publisher, a series or a cover from another.
func TestLookupKeepsEachSourcesAnswerInItsOwnRow(t *testing.T) {
	douban := principles("Simon & Schuster")
	douban.Published = "2018"
	goodreads := principles("")
	goodreads.Series, goodreads.SeriesIndex = "Dalio", 1
	goodreads.CoverURL = "https://i.gr-assets.com/x.jpg"
	google := principles("")
	google.Description = "A guide to life and work."

	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Douban", cands: []Candidate{douban}},
		fakeSource{name: "Goodreads", cands: []Candidate{goodreads}},
		fakeSource{name: "Google Books", cands: []Candidate{google}},
	}, Query{Title: "Principles", Authors: []string{"Ray Dalio"}, ISBNs: []string{"9781501124020"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("%d rows, want one per source: %+v", len(res.Candidates), res.Candidates)
	}
	var sources []string
	for _, c := range res.Candidates {
		if len(c.Sources) != 1 {
			t.Errorf("a row names %v, want one source", c.Sources)
		}
		sources = append(sources, c.Sources[0])
		if c.Match != MatchISBN || c.Score != 1 {
			t.Errorf("%s: match %s score %v", c.Sources[0], c.Match, c.Score)
		}
	}
	// Equal rows keep the order of the sources.
	if want := []string{"Douban", "Goodreads", "Google Books"}; !reflect.DeepEqual(sources, want) {
		t.Errorf("order = %v, want %v", sources, want)
	}
	d, g, gb := res.Candidates[0], res.Candidates[1], res.Candidates[2]
	if d.Publisher != "Simon & Schuster" || d.Published != "2018" || d.Series != "" || d.CoverURL != "" || d.Description != "" {
		t.Errorf("the Douban row took something from another source: %+v", d)
	}
	if g.Publisher != "" || g.Series != "Dalio" || g.CoverURL == "" || g.Published != "" {
		t.Errorf("the Goodreads row is not its own: %+v", g)
	}
	if gb.Description == "" || gb.Publisher != "" || gb.Series != "" {
		t.Errorf("the Google Books row is not its own: %+v", gb)
	}
}

// One source can give several rows: its own editions stay separate rows, the
// limit is for each source, and a source with more answers does not push
// another's out of the list.
func TestLookupGivesEachSourceSeveralRowsUpToTheLimit(t *testing.T) {
	var many []Candidate
	for _, isbn := range []string{"9780441013593", "9780441172719", "9780593099322", "9780340960196"} {
		many = append(many, cand("", "Dune", []string{"Frank Herbert"}, isbn))
	}
	one := cand("", "Dune", []string{"Frank Herbert"}, "9780441013593")
	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Open Library", cands: many},
		fakeSource{name: "Goodreads", cands: []Candidate{one}},
	}, Query{Title: "Dune", Authors: []string{"Frank Herbert"}}, Options{MaxResults: 2})
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, c := range res.Candidates {
		count[c.Sources[0]]++
	}
	if count["Open Library"] != 2 || count["Goodreads"] != 1 {
		t.Errorf("rows per source = %v, want 2 and 1", count)
	}
}

// A source that lists the same book twice is still one row for it.
func TestLookupJoinsTheSameBookOnlyWithinOneSource(t *testing.T) {
	a := principles("Simon & Schuster")
	b := principles("")
	b.Description = "A guide to life and work."
	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Open Library", cands: []Candidate{a, b}},
		fakeSource{name: "Google Books", cands: []Candidate{principles("Other")}},
	}, Query{Title: "Principles", ISBNs: []string{"9781501124020"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("%d rows, want 2: %+v", len(res.Candidates), res.Candidates)
	}
	ol := res.Candidates[0]
	if ol.Sources[0] != "Open Library" || ol.Publisher != "Simon & Schuster" || ol.Description == "" {
		t.Errorf("the source's own duplicates were not joined: %+v", ol)
	}
	if res.Candidates[1].Publisher != "Other" {
		t.Errorf("the other source's row changed: %+v", res.Candidates[1])
	}
}

// An ISBN match from any source comes before a title match, and a title match
// that fits better comes before one that fits worse, whatever their sources.
func TestLookupRanksRowsOfAllSourcesTogether(t *testing.T) {
	exact := cand("", "Dune", []string{"Frank Herbert"}, "9780441013593")
	near := Candidate{Title: "Dune Messiah", Authors: []string{"Frank Herbert"}}
	res, err := Lookup(context.Background(), []Source{
		fakeSource{name: "Goodreads", cands: []Candidate{near}},
		fakeSource{name: "Douban", cands: []Candidate{exact}},
	}, Query{Title: "Dune", Authors: []string{"Frank Herbert"}, ISBNs: []string{"9780441013593"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) < 1 || res.Candidates[0].Sources[0] != "Douban" || res.Candidates[0].Match != MatchISBN {
		t.Errorf("the ISBN match is not first: %+v", res.Candidates)
	}
}
