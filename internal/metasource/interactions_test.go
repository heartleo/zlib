package metasource

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// An ISBN a catalogue does not know must not stop the title search: before
// the download recorded ISBNs, a Chinese book had no ISBN query and found
// candidates by title; with one it must still do so.
func TestGoogleBooksFallsBackToTheTitleWhenTheISBNIsUnknown(t *testing.T) {
	var queries []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		queries = append(queries, q)
		if strings.HasPrefix(q, "isbn:") {
			w.Write([]byte(`{"totalItems":0}`))
			return
		}
		w.Write([]byte(`{"items":[{"id":"g1","volumeInfo":{"title":"三体","authors":["刘慈欣"],"publishedDate":"2008"}}]}`))
	})
	got, err := newGoogleBooks(srv.URL, nil, "").Identify(context.Background(),
		Query{Title: "三体", Authors: []string{"刘慈欣"}, ISBNs: []string{"9787536692930"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(queries) != 2 || !strings.HasPrefix(queries[0], "isbn:") || !strings.HasPrefix(queries[1], "intitle:") {
		t.Errorf("queries = %q", queries)
	}
}

func TestLookupStillFindsTitleCandidatesForAnUnknownISBN(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Query().Get("q"), "isbn:") {
			w.Write([]byte(`{}`))
			return
		}
		w.Write([]byte(`{"items":[{"id":"g1","volumeInfo":{"title":"三体","authors":["刘慈欣"]}}]}`))
	})
	res, err := Lookup(context.Background(), []Source{newGoogleBooks(srv.URL, nil, "")},
		Query{Title: "三体", ISBNs: []string{"9787536692930"}}, Options{})
	if err != nil || len(res.Candidates) != 1 || res.Candidates[0].Match != MatchTitle {
		t.Fatalf("got %+v, %v", res, err)
	}
}

// An ISBN that only a download's record or a copyright page supplied is a
// hint: it confirms a match only when the title agrees closely, not at the
// matcher's looser 0.5.
func TestHintISBNNeedsAClosePartnerTitle(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "三体"}
	cur.SetIdentifier(SchemeZlibISBN, "9787536692930")
	q := QueryFromMetadata(cur)
	sequel := Candidate{Title: "三体Ⅱ", Authors: []string{"刘慈欣"}, EditionLevel: true, Sources: []string{"Douban"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9787536692930"}}}
	got := rankAndMerge(q, []Candidate{sequel}, defaultMinScore, 10)
	if len(got) != 1 || got[0].Score >= 1 {
		t.Fatalf("a sequel confirmed the hint: %+v", got)
	}
	for _, ch := range Plan(cur, got[0]) {
		if ch.DefaultSelected() {
			t.Errorf("%s preselected for an unconfirmed hint", ch.Field)
		}
	}

	// The same near-miss from the file's own ISBN keeps the matcher's rule.
	own := &ebookmeta.Metadata{Title: "三体"}
	own.SetIdentifier("isbn", "9787536692930")
	got = rankAndMerge(QueryFromMetadata(own), []Candidate{sequel}, defaultMinScore, 10)
	if len(got) != 1 || got[0].Score != 1 {
		t.Fatalf("file ISBN = %+v", got)
	}
}

// Two edition-level results that share no edition identifier cannot be shown
// to be one edition, so one's ISBN must not make the other an exact match.
func TestEditionWithoutISBNIsNotPooledIntoAnISBNMatch(t *testing.T) {
	douban2016 := Candidate{Sources: []string{"Douban"}, EditionLevel: true, Title: "三体", Authors: []string{"刘慈欣"},
		Publisher: "重庆出版社", Published: "2016-01", Identifiers: appendIdentifier(nil, "douban", "6414884")}
	google2008 := Candidate{Sources: []string{"Google Books"}, EditionLevel: true, Title: "三体", Authors: []string{"刘慈欣"},
		Publisher: "重庆出版社", Published: "2008-01", Identifiers: appendIdentifier(nil, "isbn", "9787536692930")}
	got := rankAndMerge(Query{Title: "三体", ISBNs: []string{"9787536692930"}}, []Candidate{douban2016, google2008}, defaultMinScore, 10)
	if len(got) != 2 {
		t.Fatalf("pooled into %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Published == "2016-01" && c.Match == MatchISBN {
			t.Errorf("the 2016 edition inherited the 2008 edition's ISBN match: %+v", c)
		}
	}

	// A work-level result says nothing about an edition and may still join.
	work := Candidate{Sources: []string{"Open Library"}, Title: "三体", Authors: []string{"刘慈欣"}, Published: "2008"}
	got = rankAndMerge(Query{Title: "三体"}, []Candidate{google2008, work}, defaultMinScore, 10)
	if len(got) != 1 || len(got[0].Sources) != 2 {
		t.Fatalf("work-level result not pooled: %+v", got)
	}
}
