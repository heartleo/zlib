package metasource

import (
	"context"
	"net/http"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestParseMaxResults(t *testing.T) {
	for in, want := range map[string]int{"": 10, " 7 ": 7, "1": 1, "50": 50} {
		if got, err := ParseMaxResults(in); err != nil || got != want {
			t.Errorf("ParseMaxResults(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-3", "51", "ten", "5.5", "1e1"} {
		if _, err := ParseMaxResults(bad); err == nil {
			t.Errorf("ParseMaxResults(%q) was accepted", bad)
		}
	}
}

// The sources are asked for enough hits to fill the limit, within their API's
// ceiling, and Lookup returns no more than the limit.
func TestSourcesFetchEnoughForTheLimit(t *testing.T) {
	var google, openlib string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/books/v1/volumes":
			google = r.URL.Query().Get("maxResults")
			w.Write([]byte(`{}`))
		default:
			openlib = r.URL.Query().Get("limit")
			w.Write([]byte(`{"docs":[]}`))
		}
	})
	for _, tc := range []struct {
		limit        int
		google, olib string
	}{{0, "20", "20"}, {10, "20", "20"}, {3, "6", "6"}, {50, "40", "100"}} {
		g, o := newGoogleBooks(srv.URL, nil, ""), newOpenLibrary(srv.URL, nil)
		g.limit, o.limit = tc.limit, tc.limit
		if _, err := g.Identify(context.Background(), Query{Title: "Dune"}); err != nil {
			t.Fatal(err)
		}
		if _, err := o.Identify(context.Background(), Query{Title: "Dune"}); err != nil {
			t.Fatal(err)
		}
		if google != tc.google || openlib != tc.olib {
			t.Errorf("limit %d: google asked for %s, open library for %s; want %s and %s", tc.limit, google, openlib, tc.google, tc.olib)
		}
	}
	if detailCount(0) != 10 || detailCount(3) != 3 || detailCount(50) != maxDetailRequests {
		t.Errorf("detail counts: %d %d %d", detailCount(0), detailCount(3), detailCount(50))
	}
}

func TestLookupReturnsNoMoreThanTheLimit(t *testing.T) {
	var cands []Candidate
	// Five editions of one title: different ISBNs keep them apart.
	for _, isbn := range []string{"9780000000001", "9780000000002", "9780000000003", "9780000000004", "9780000000005"} {
		cands = append(cands, Candidate{Title: "Dune", Authors: []string{"Frank Herbert"}, EditionLevel: true,
			Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: isbn}}})
	}
	src := fakeSource{name: "Fake", cands: cands}
	for limit, want := range map[int]int{0: 5, 2: 2, 50: 5} {
		res, err := Lookup(context.Background(), []Source{src}, Query{Title: "Dune", Authors: []string{"Frank Herbert"}}, Options{MaxResults: limit})
		if err != nil || len(res.Candidates) != want {
			t.Errorf("limit %d: %d candidates, %v; want %d", limit, len(res.Candidates), err, want)
		}
	}
}
