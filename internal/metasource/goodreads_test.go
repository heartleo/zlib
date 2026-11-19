package metasource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// grItem builds one autocomplete hit in the shape the live endpoint returns:
// the ID as a string, the counts as numbers, the rating as a string and the
// author as an object.
func grItem(id, bareTitle, title, author, blurb string, truncated bool) string {
	return `{"bookId":"` + id + `","workId":"9","bookUrl":"/book/show/` + id + `-x","title":"` + title +
		`","bookTitleBare":"` + bareTitle + `","numPages":412,"avgRating":"4.29","ratingsCount":1729978,` +
		`"imageUrl":"https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1i/` + id + `._SY75_.jpg",` +
		`"author":{"id":58,"name":"` + author + `","isGoodreadsAuthor":false},` +
		`"description":{"html":"` + blurb + `","truncated":` + map[bool]string{true: "true", false: "false"}[truncated] + `}}`
}

// grFixture is what a fake Goodreads host serves.
type grFixture struct {
	known  map[string]string // an ISBN to the book ID its redirect names
	auto   map[string]string // an autocomplete query to the JSON body
	pages  map[string]string // a book ID to the body of its page; a missing one is a 404
	status map[string]int    // a book ID to a status its page is answered with instead
	wantUA string            // the User-Agent every request must carry; the default when empty
}

// goodreadsServerWith answers the three Goodreads endpoints from one host, and
// fails the test for a request that carries cookies or another User-Agent than
// the one expected.
func goodreadsServerWith(t *testing.T, fx grFixture, opts ...GoodreadsOption) (*Goodreads, *[]string) {
	t.Helper()
	want := fx.wantUA
	if want == "" {
		want = goodreadsUserAgent
	}
	var reqs []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.URL.RequestURI())
		if ua := r.Header.Get("User-Agent"); ua != want {
			t.Errorf("%s: User-Agent = %q, want %q", r.URL.Path, ua, want)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/book/isbn/"):
			if id, ok := fx.known[strings.TrimPrefix(r.URL.Path, "/book/isbn/")]; ok {
				http.Redirect(w, r, "/book/show/"+id+"-some-title", http.StatusMovedPermanently)
				return
			}
			http.NotFound(w, r)
		case r.URL.Path == "/book/auto_complete":
			if r.URL.Query().Get("format") != "json" {
				t.Errorf("format = %q", r.URL.Query().Get("format"))
			}
			if body, ok := fx.auto[r.URL.Query().Get("q")]; ok {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(body))
				return
			}
			w.Write([]byte("[]"))
		case strings.HasPrefix(r.URL.Path, "/book/show/"):
			id := strings.TrimPrefix(r.URL.Path, "/book/show/")
			id, _, _ = strings.Cut(id, "-")
			if st, ok := fx.status[id]; ok {
				w.WriteHeader(st)
				w.Write([]byte(`<html><script>window.awsWafCookieDomainList=[]</script><div id="challenge-container"></div></html>`))
				return
			}
			if body, ok := fx.pages[id]; ok {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(body))
				return
			}
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
	return newGoodreads(srv.URL, nil, opts...), &reqs
}

// goodreadsServer is goodreadsServerWith for a host that has no book pages: a
// page is a 404, so a lookup has the suggestions only.
func goodreadsServer(t *testing.T, known map[string]string, auto map[string]string) (*Goodreads, *[]string) {
	t.Helper()
	return goodreadsServerWith(t, grFixture{known: known, auto: auto})
}

func TestGoodreadsByISBNResolvesRedirectAndPicksThatBook(t *testing.T) {
	isbn := "9780441013593"
	src, reqs := goodreadsServer(t, map[string]string{isbn: "260994194"}, map[string]string{
		// Autocomplete for the ISBN also lists another book; only the ID the
		// redirect named is the book.
		isbn: "[" + grItem("111", "Other", "Other", "Someone", "x", false) + "," +
			grItem("260994194", "Dune", "Dune (Dune, #1)", "Frank Herbert", "A <b>desert</b> planet.<br>Spice.", false) + "]",
	})
	got, err := src.Identify(context.Background(), Query{ISBNs: []string{isbn}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := got[0]
	if c.Title != "Dune" || !reflect.DeepEqual(c.Authors, []string{"Frank Herbert"}) || !c.EditionLevel || c.ISBN() != isbn {
		t.Fatalf("candidate = %+v", c)
	}
	if c.Description != "A desert planet.\nSpice." {
		t.Errorf("description = %q", c.Description)
	}
	if c.CoverURL != "https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1i/260994194.jpg" {
		t.Errorf("cover = %q (the thumbnail size token should be gone)", c.CoverURL)
	}
	var gid string
	for _, id := range c.Identifiers {
		if id.Scheme == "goodreads" {
			gid = id.Value
		}
	}
	if gid != "260994194" {
		t.Errorf("identifiers = %+v", c.Identifiers)
	}
	// The page is asked for first, and with none to read the suggestion stands in.
	var paths []string
	for _, r := range *reqs {
		paths = append(paths, strings.SplitN(r, "?", 2)[0])
	}
	if want := []string{"/book/isbn/" + isbn, "/book/show/260994194", "/book/auto_complete"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("requests = %v, want %v", paths, want)
	}
}

func TestGoodreadsDropsATruncatedBlurb(t *testing.T) {
	src, _ := goodreadsServer(t, nil, map[string]string{
		"Dune": "[" + grItem("1", "Dune", "Dune", "Frank Herbert", "It was the best of…", true) + "]",
	})
	got, err := src.Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].Description != "" {
		t.Errorf("a blurb cut short was kept: %q", got[0].Description)
	}
	if got[0].EditionLevel {
		t.Error("a title hit describes a work, not an edition")
	}
}

func TestGoodreadsTitleSearchMergesTheTwoQueriesAndFiltersByTitle(t *testing.T) {
	dune := grItem("44767458", "Dune", "Dune (Dune, #1)", "Frank Herbert", "x", false)
	src, reqs := goodreadsServer(t, nil, map[string]string{
		"Dune": "[" + dune + "," + grItem("10480882", "Chapterhouse: Dune by Frank Herbert Summary & Study Guide", "Chapterhouse", "Bookcaps", "x", false) + "]",
		// The title-and-author query repeats Dune, and adds a book whose
		// title is nothing like the one asked for.
		"Dune Frank Herbert": "[" + dune + "," + grItem("5", "Messiah", "Messiah", "Frank Herbert", "x", false) + "]",
	})
	got, err := src.Identify(context.Background(), Query{Title: "Dune", Authors: []string{"Frank Herbert"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Dune" {
		t.Fatalf("got %+v, want only Dune once", got)
	}
	searches := 0
	for _, r := range *reqs {
		if strings.HasPrefix(r, "/book/auto_complete") {
			searches++
		}
	}
	if searches != 2 {
		t.Errorf("requests = %v, want one search per query", *reqs)
	}
}

func TestGoodreadsReadsEitherShapeOfAnItem(t *testing.T) {
	// The ID and counts as numbers, the author and description as plain text,
	// and a title with only the series suffix: a changed shape must not lose
	// the result.
	body := `[{"bookId":260994194,"title":"Dune (Dune, #1)","author":"Frank Herbert","numPages":"687","ratingsCount":"1,729,978","description":"Plain blurb."}]`
	src, _ := goodreadsServer(t, nil, map[string]string{"Dune": body})
	got, err := src.Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].Title != "Dune" || !reflect.DeepEqual(got[0].Authors, []string{"Frank Herbert"}) || got[0].Description != "Plain blurb." {
		t.Fatalf("candidate = %+v", got[0])
	}
}

func TestGoodreadsSkipsItemsWithoutAnIDOrATitle(t *testing.T) {
	body := `[{"bookId":"abc","bookUrl":"/nowhere","bookTitleBare":"No id"},{"bookId":"7","bookTitleBare":""},{"bookId":"8","bookTitleBare":"Dune"}]`
	src, _ := goodreadsServer(t, nil, map[string]string{"Dune": body})
	got, err := src.Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 1 || got[0].Title != "Dune" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestGoodreadsUnknownISBNFallsBackToTheTitle(t *testing.T) {
	src, _ := goodreadsServer(t, nil, map[string]string{
		"Dune": "[" + grItem("1", "Dune", "Dune", "Frank Herbert", "x", false) + "]",
	})
	got, err := src.Identify(context.Background(), Query{ISBNs: []string{"9780000000002"}, Title: "Dune"})
	if err != nil || len(got) != 1 || got[0].EditionLevel {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err := src.Identify(context.Background(), Query{ISBNs: []string{"9780000000002"}}); err != nil || got != nil {
		t.Fatalf("an unknown ISBN alone found %+v, %v", got, err)
	}
}

func TestGoodreadsRefusesARedirectOffTheHost(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.test/book/show/1", http.StatusFound)
	})
	src := newGoodreads(srv.URL, nil)
	if _, err := src.Identify(context.Background(), Query{ISBNs: []string{"9780441013593"}}); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestGoodreadsReportsABotChallengeAndRateLimit(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{{http.StatusAccepted, errGoodreadsChallenge}, {http.StatusTooManyRequests, ErrRateLimited}} {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) })
		src := newGoodreads(srv.URL, nil)
		if _, err := src.Identify(context.Background(), Query{Title: "Dune"}); !errors.Is(err, tc.want) {
			t.Errorf("title, status %d: err = %v", tc.status, err)
		}
		if _, err := src.Identify(context.Background(), Query{ISBNs: []string{"9780441013593"}}); !errors.Is(err, tc.want) {
			t.Errorf("isbn, status %d: err = %v", tc.status, err)
		}
	}
}

func TestGoodreadsRetriesAShedLoadStatusOnce(t *testing.T) {
	old := goodreadsRetryDelay
	goodreadsRetryDelay = time.Millisecond
	defer func() { goodreadsRetryDelay = old }()

	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/book/show/") { // no page: the suggestion stands
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("[" + grItem("1", "Dune", "Dune", "Frank Herbert", "x", false) + "]"))
	})
	got, err := newGoodreads(srv.URL, nil).Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 1 || calls.Load() != 2 {
		t.Fatalf("got %+v, %v after %d calls", got, err, calls.Load())
	}

	// A second failure is reported, not retried again; 500 is never retried.
	calls.Store(0)
	always := serve(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusBadGateway) })
	if _, err := newGoodreads(always.URL, nil).Identify(context.Background(), Query{Title: "Dune"}); err == nil || calls.Load() != 2 {
		t.Fatalf("err = %v after %d calls, want an error after 2", err, calls.Load())
	}
	calls.Store(0)
	fault := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := newGoodreads(fault.URL, nil).Identify(context.Background(), Query{Title: "Dune"}); err == nil || calls.Load() != 1 {
		t.Fatalf("err = %v after %d calls, want an error after 1", err, calls.Load())
	}
}

func TestGoodreadsRetryWaitEndsWithTheContext(t *testing.T) {
	old := goodreadsRetryDelay
	goodreadsRetryDelay = time.Hour
	defer func() { goodreadsRetryDelay = old }()
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := newGoodreads(srv.URL, nil).Identify(ctx, Query{Title: "Dune"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the retry wait ignored the context")
	}
}

func TestGoodreadsCoverURLOnlyAcceptsTheImageHost(t *testing.T) {
	const good = "https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1i/7.jpg"
	for raw, want := range map[string]string{
		"https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1i/7._SY75_.jpg": good,
		"https://i.gr-assets.com/images/S/compressed.photo.goodreads.com/books/1i/7._SX50_.jpg": good,
		good:                                      good,
		"http://i.gr-assets.com/books/1i/7.jpg":   "", // not https
		"https://evil.test/books/1i/7.jpg":        "",
		"https://i.gr-assets.com.evil.test/7.jpg": "",
		"https://i.gr-assets.com/images/nophoto/book/111x148._SX50_.png": "", // the no-cover placeholder
		"": "",
	} {
		if got := goodreadsCoverURL(raw); got != want {
			t.Errorf("goodreadsCoverURL(%q) = %q, want %q", raw, got, want)
		}
	}
	if !TrustedCoverURL(good) {
		t.Error("the Goodreads image host is not a trusted cover host")
	}
	if u, _ := url.Parse("https://i.gr-assets.com:8443/x.jpg"); coverHosts.allows(u) {
		t.Error("another port on the image host was allowed")
	}
}

func TestGoodreadsIsKnownButNeverADefault(t *testing.T) {
	got, err := NormalizeSourceList("goodreads, openlibrary")
	if err != nil || got != "openlibrary,goodreads" {
		t.Fatalf("NormalizeSourceList = %q, %v", got, err)
	}
	for _, names := range [][]string{DefaultSourceNamesFor(false, false), DefaultSourceNamesFor(false, true), DefaultSourceNamesFor(true, true)} {
		for _, n := range names {
			if n == NameGoodreads {
				t.Errorf("Goodreads is a default source: %v", names)
			}
		}
	}
	srcs := NewSources([]string{NameGoodreads}, SourceConfig{MaxResults: 7})
	if len(srcs) != 1 || srcs[0].Name() != "Goodreads" || srcs[0].(*Goodreads).limit != 7 {
		t.Fatalf("NewSources = %+v", srcs)
	}
	if !WritableIdentifier("goodreads") {
		t.Error("a goodreads identifier cannot be written")
	}
}
