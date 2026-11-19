package metasource

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// grPageOpts changes the invented book a test page describes.
type grPageOpts struct {
	id           string
	idAsString   bool
	language     string
	isbn, isbn13 string
	series       any // a record key to a series, or an inline series
	position     string
	authors      [][2]string // name and role, the first is the primary contributor
	date         any
}

// grPage builds a book page in the shape of the live ones: an HTML shell around
// a script that holds the page's data as records that refer to each other. The
// book is invented.
func grPage(t *testing.T, o grPageOpts) string {
	t.Helper()
	if o.language == "" {
		o.language = "English"
	}
	if o.isbn13 == "" && o.isbn == "" {
		o.isbn13 = "9780441013593"
	}
	if o.authors == nil {
		o.authors = [][2]string{{"Ada Writer", "Author"}}
	}
	if o.date == nil {
		o.date = 1541055600000 // 2018-11-01 07:00 UTC, midnight in California
	}
	state := map[string]any{}
	edge := func(i int, role string) map[string]any {
		key := "Contributor:kca://author/" + string(rune('a'+i))
		state[key] = map[string]any{"__typename": "Contributor", "name": o.authors[i][0]}
		return map[string]any{"node": map[string]any{"__ref": key}, "role": role}
	}
	var legacy any = json.Number(o.id)
	if o.idAsString {
		legacy = o.id
	}
	book := map[string]any{
		"__typename":  "Book",
		"legacyId":    legacy,
		"title":       "A Specimen Book",
		"description": "<b>A specimen</b> of a book.<br /><br />Second paragraph &amp; more.",
		"imageUrl":    "https://m.media-amazon.com/images/S/compressed.photo.goodreads.com/books/1i/" + o.id + ".jpg",
		"details": map[string]any{
			"publisher": "Example Press", "isbn": o.isbn, "isbn13": o.isbn13, "publicationTime": o.date,
			"language": map[string]any{"name": o.language}, "numPages": 176, "asin": "B000000000", "format": "Kindle Edition",
		},
		"bookGenres":             []any{map[string]any{"genre": map[string]any{"name": "Fiction"}}, map[string]any{"genre": map[string]any{"name": "Classics"}}},
		"primaryContributorEdge": edge(0, o.authors[0][1]),
	}
	var secondary []any
	for i := 1; i < len(o.authors); i++ {
		secondary = append(secondary, edge(i, o.authors[i][1]))
	}
	book["secondaryContributorEdges"] = secondary
	switch s := o.series.(type) {
	case string: // a record key
		state[s] = map[string]any{"__typename": "Series", "title": "The Specimen Cycle"}
		book["bookSeries"] = []any{map[string]any{"userPosition": o.position, "series": map[string]any{"__ref": s}}}
	case map[string]any: // inline
		book["bookSeries"] = []any{map[string]any{"userPosition": o.position, "series": s}}
	default:
		book["bookSeries"] = []any{}
	}
	state["Book:kca://book/amzn1.gr.book.v3.example"] = book
	// Other books the page lists, which must not be taken for the one asked for.
	state["Book:kca://book/amzn1.gr.book.v3.other"] = map[string]any{"__typename": "Book", "legacyId": 999, "title": "Another Book"}
	state["Work:kca://work/example"] = map[string]any{"stats": map[string]any{"averageRating": 3.5, "ratingsCount": 100}}
	data, err := json.Marshal(map[string]any{"props": map[string]any{"pageProps": map[string]any{"apolloState": state}}})
	if err != nil {
		t.Fatal(err)
	}
	return `<!DOCTYPE html><html><head><title>x</title></head><body><script id="__NEXT_DATA__" type="application/json">` +
		string(data) + `</script></body></html>`
}

func TestGoodreadsReadsAWholeEditionFromItsPage(t *testing.T) {
	isbn := "9780441013593"
	page := grPage(t, grPageOpts{id: "52", series: "Series:kca://cycle", position: "2",
		authors: [][2]string{{"Ada Writer", "Author"}, {"Bo Coauthor", "Author"}, {"Cy Translator", "Translator"}}})
	src, reqs := goodreadsServerWith(t, grFixture{known: map[string]string{isbn: "52"}, pages: map[string]string{"52": page}})
	got, err := src.Identify(context.Background(), Query{ISBNs: []string{isbn}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := got[0]
	if c.Title != "A Specimen Book" || !c.EditionLevel || c.Publisher != "Example Press" || c.Published != "2018-11-01" ||
		!reflect.DeepEqual(c.Languages, []string{"en"}) || !reflect.DeepEqual(c.Tags, []string{"Fiction", "Classics"}) {
		t.Fatalf("candidate = %+v", c)
	}
	// Authors in the Author role, in order; the translator is not one.
	if want := []string{"Ada Writer", "Bo Coauthor"}; !reflect.DeepEqual(c.Authors, want) {
		t.Errorf("authors = %v, want %v", c.Authors, want)
	}
	if c.Description != "A specimen of a book.\n\nSecond paragraph & more." {
		t.Errorf("description = %q", c.Description)
	}
	if c.Series != "The Specimen Cycle" || c.SeriesIndex != 2 {
		t.Errorf("series = %q #%v", c.Series, c.SeriesIndex)
	}
	if c.CoverURL != "https://m.media-amazon.com/images/S/compressed.photo.goodreads.com/books/1i/52.jpg" {
		t.Errorf("cover = %q", c.CoverURL)
	}
	if c.ISBN() != isbn || goodreadsID(c) != "52" {
		t.Errorf("identifiers = %+v", c.Identifiers)
	}
	// The redirect and the page: with the page read, no suggestion is asked for.
	if len(*reqs) != 2 {
		t.Errorf("requests = %v, want the redirect and the page", *reqs)
	}
}

// A role can name several things at once, as "Author/Narrator" does on an
// audiobook; the narrator, the reader and the like are not authors.
func TestGoodreadsTakesAnAuthorWhoseRoleNamesMoreThanOneThing(t *testing.T) {
	for role, want := range map[string]bool{
		"Author": true, "author": true, "Author/Narrator": true, "Narrator/Author": true, "Author, Illustrator": true,
		"Reader": false, "Narrator": false, "Translator": false, "Illustrator": false, "Editor": false, "Foreword": false, "": false,
	} {
		if got := isAuthorRole(role); got != want {
			t.Errorf("isAuthorRole(%q) = %v, want %v", role, got, want)
		}
	}
	body := grPage(t, grPageOpts{id: "7", authors: [][2]string{{"Ada Writer", "Author/Narrator"}, {"Rae Reader", "Reader"}}})
	c, err := mapGoodreadsPage([]byte(body), "7")
	if err != nil || !reflect.DeepEqual(c.Authors, []string{"Ada Writer"}) {
		t.Errorf("authors = %v, %v", c.Authors, err)
	}
}

func TestGoodreadsKeepsTheHanNamesOfAChineseEdition(t *testing.T) {
	body := grPage(t, grPageOpts{id: "7", language: "Chinese", isbn13: "9787201141527",
		authors: [][2]string{{"Ada Writer", "Author"}, {"艾达·作家", "Author"}, {"顾译者", "Translator"}}})
	c, err := mapGoodreadsPage([]byte(body), "7")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Authors, []string{"艾达·作家"}) || !reflect.DeepEqual(c.Languages, []string{"zh"}) {
		t.Errorf("authors = %v, languages = %v", c.Authors, c.Languages)
	}
	// Another language keeps every name in the Author role.
	body = grPage(t, grPageOpts{id: "7", language: "French",
		authors: [][2]string{{"Ada Writer", "Author"}, {"Bo Coauthor", "Author"}}})
	if c, _ := mapGoodreadsPage([]byte(body), "7"); !reflect.DeepEqual(c.Authors, []string{"Ada Writer", "Bo Coauthor"}) {
		t.Errorf("authors = %v", c.Authors)
	}
}

// Only a Latin name followed by its Han form is one author. A co-author with no
// Han form is kept, wherever it stands, and an edition that Goodreads does not
// give a language (or gives a wrong one) keeps both forms: that lists a person
// twice, and loses no one.
func TestGoodreadsCollapsesOnlyTheNeighbouringLatinAndHanPair(t *testing.T) {
	authors := func(language string, names ...string) []string {
		t.Helper()
		var list [][2]string
		for _, n := range names {
			list = append(list, [2]string{n, "Author"})
		}
		c, err := mapGoodreadsPage([]byte(grPage(t, grPageOpts{id: "7", language: language, authors: list})), "7")
		if err != nil {
			t.Fatal(err)
		}
		return c.Authors
	}
	for name, tc := range map[string]struct {
		language string
		names    []string
		want     []string
	}{
		"the pair first, then a co-author": {"Chinese", []string{"Ada Writer", "艾达·作家", "Bo Coauthor"}, []string{"艾达·作家", "Bo Coauthor"}},
		"a co-author first, then the pair": {"Chinese", []string{"Bo Coauthor", "Ada Writer", "艾达·作家"}, []string{"Bo Coauthor", "艾达·作家"}},
		"two pairs":                        {"Chinese", []string{"Ada Writer", "艾达·作家", "Bo Coauthor", "波·合著者"}, []string{"艾达·作家", "波·合著者"}},
		"Han names alone":                  {"Chinese", []string{"艾达·作家", "波·合著者"}, []string{"艾达·作家", "波·合著者"}},
		"Latin names alone":                {"Chinese", []string{"Ada Writer", "Bo Coauthor"}, []string{"Ada Writer", "Bo Coauthor"}},
		"no language given":                {"", []string{"Ada Writer", "艾达·作家"}, []string{"Ada Writer", "艾达·作家"}},
		"another language":                 {"English", []string{"Ada Writer", "艾达·作家"}, []string{"Ada Writer", "艾达·作家"}},
	} {
		if got := authors(tc.language, tc.names...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: authors = %v, want %v", name, got, tc.want)
		}
	}
}

func TestGoodreadsPageVariations(t *testing.T) {
	read := func(o grPageOpts) *Candidate {
		t.Helper()
		c, err := mapGoodreadsPage([]byte(grPage(t, o)), o.id)
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		return c
	}
	// The ID as text, an ISBN-13 worked out from the ISBN-10 alone, a language
	// that is not known gives none, and an inline series.
	c := read(grPageOpts{id: "8", idAsString: true, isbn: "0441013597", language: "Klingon",
		series: map[string]any{"title": "Inline Cycle"}, position: "3"})
	if c.ISBN() != "9780441013593" || len(c.Languages) != 0 || c.Series != "Inline Cycle" || c.SeriesIndex != 3 {
		t.Errorf("candidate = %+v", c)
	}
	// A box set's range is no position; the series is still named.
	if c := read(grPageOpts{id: "9", series: "Series:kca://x", position: "1-3"}); c.Series == "" || c.SeriesIndex != 0 {
		t.Errorf("series = %q #%v", c.Series, c.SeriesIndex)
	}
	// A publication time that is not a date gives none, and a plain one a date.
	if c := read(grPageOpts{id: "10", date: 0}); c.Published != "" {
		t.Errorf("published = %q", c.Published)
	}
	if c := read(grPageOpts{id: "10", date: "1541055600000"}); c.Published != "2018-11-01" {
		t.Errorf("published = %q", c.Published)
	}
	// Only the book whose ID was asked for is read.
	if _, err := mapGoodreadsPage([]byte(grPage(t, grPageOpts{id: "11"})), "12"); err == nil {
		t.Error("a page of another book was read")
	}
}

func TestGoodreadsFallsBackToTheSuggestionWhenAPageIsABotCheck(t *testing.T) {
	isbn := "9780441013593"
	auto := map[string]string{isbn: "[" + grItem("52", "Dune", "Dune (Dune, #1)", "Frank Herbert", "x", false) + "]"}
	for name, fx := range map[string]grFixture{
		"HTTP 202":               {status: map[string]int{"52": http.StatusAccepted}},
		"403":                    {status: map[string]int{"52": http.StatusForbidden}},
		"a 200 with no data":     {pages: map[string]string{"52": `<html><script>window.awsWafCookieDomainList=[]</script><div id="challenge-container"></div></html>`}},
		"a page of another book": {pages: map[string]string{"52": grPage(t, grPageOpts{id: "999"})}},
	} {
		fx.known, fx.auto = map[string]string{isbn: "52"}, auto
		src, reqs := goodreadsServerWith(t, fx)
		got, err := src.Identify(context.Background(), Query{ISBNs: []string{isbn}})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: got %+v, %v", name, got, err)
		}
		if c := got[0]; c.Title != "Dune" || !c.EditionLevel || c.ISBN() != isbn || c.Publisher != "" {
			t.Errorf("%s: candidate = %+v", name, c)
		}
		if last := (*reqs)[len(*reqs)-1]; !strings.HasPrefix(last, "/book/auto_complete") {
			t.Errorf("%s: the suggestion was not asked for: %v", name, *reqs)
		}
	}
}

// titleSearch is a title search whose suggestions are five books, all of which
// have a page unless a test says otherwise.
func titleSearch(t *testing.T, status map[string]int) (*Goodreads, *[]string) {
	t.Helper()
	var items []string
	pages := map[string]string{}
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		items = append(items, grItem(id, "Dune", "Dune", "Frank Herbert", "x", true))
		pages[id] = grPage(t, grPageOpts{id: id})
	}
	return goodreadsServerWith(t, grFixture{auto: map[string]string{"Dune": "[" + strings.Join(items, ",") + "]"}, pages: pages, status: status})
}

func pageRequests(reqs []string) int {
	n := 0
	for _, r := range reqs {
		if strings.HasPrefix(r, "/book/show/") {
			n++
		}
	}
	return n
}

func TestGoodreadsReadsAtMostThreePagesOfASearch(t *testing.T) {
	old := goodreadsPagePause
	goodreadsPagePause = time.Millisecond
	defer func() { goodreadsPagePause = old }()

	src, reqs := titleSearch(t, nil)
	got, err := src.Identify(context.Background(), Query{Title: "Dune"})
	if err != nil || len(got) != 5 {
		t.Fatalf("got %d rows, %v", len(got), err)
	}
	// The number is written out on purpose: the limit is a promise to Goodreads, so
	// changing it should mean changing this test.
	if n := pageRequests(*reqs); n != 3 {
		t.Errorf("%d pages read, want 3", n)
	}
	for i, c := range got {
		if full := c.Publisher != ""; full != (i < 3) || c.EditionLevel != full {
			t.Errorf("row %d: publisher %q, edition level %v", i, c.Publisher, c.EditionLevel)
		}
	}
}

// The first page that is a bot check, or a rate limit, ends the reading: the
// pages already read are kept, the rest stay suggestions, and nothing more is
// asked for in that lookup.
func TestGoodreadsStopsReadingPagesAfterTheFirstBlock(t *testing.T) {
	old := goodreadsPagePause
	goodreadsPagePause = time.Millisecond
	defer func() { goodreadsPagePause = old }()

	for name, tc := range map[string]struct {
		status   map[string]int
		pages    int // requests made
		fullRows int
	}{
		"the first page":  {map[string]int{"1": http.StatusAccepted}, 1, 0},
		"the second page": {map[string]int{"2": http.StatusAccepted}, 2, 1},
		"a rate limit":    {map[string]int{"2": http.StatusTooManyRequests}, 2, 1},
	} {
		src, reqs := titleSearch(t, tc.status)
		got, err := src.Identify(context.Background(), Query{Title: "Dune"})
		if err != nil || len(got) != 5 {
			t.Fatalf("%s: %d rows, %v", name, len(got), err)
		}
		if n := pageRequests(*reqs); n != tc.pages {
			t.Errorf("%s: %d page requests, want %d", name, n, tc.pages)
		}
		full := 0
		for _, c := range got {
			if c.Publisher != "" {
				full++
			}
		}
		if full != tc.fullRows {
			t.Errorf("%s: %d rows from pages, want %d", name, full, tc.fullRows)
		}
	}
}

// A lookup that is cancelled while it waits between two pages returns the rows
// it has, and no error: the lookup drops every row of a source that fails.
func TestGoodreadsCancelledBetweenPagesKeepsItsRows(t *testing.T) {
	old := goodreadsPagePause
	goodreadsPagePause = time.Hour
	defer func() { goodreadsPagePause = old }()

	src, _ := titleSearch(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := src.Identify(ctx, Query{Title: "Dune"})
	if err != nil || len(got) != 5 {
		t.Fatalf("got %d rows, %v", len(got), err)
	}
	if got[0].Publisher == "" || got[1].Publisher != "" {
		t.Errorf("the page read before the wait is lost, or one after it appeared: %q, %q", got[0].Publisher, got[1].Publisher)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the wait between pages ignored the context")
	}
}

// Every request, to every endpoint, carries the source's User-Agent: the
// browser's by default, another when a Go option names one, and the default
// again for an option that names nothing usable.
func TestGoodreadsSendsOneUserAgentToEveryEndpoint(t *testing.T) {
	isbn := "9780441013593"
	run := func(wantUA string, opts ...GoodreadsOption) {
		t.Helper()
		// A redirect, a page (a 404, so the suggestion follows) and the suggestion
		// list: all three endpoints are asked, and the host fails the test for any
		// request whose User-Agent differs.
		src, reqs := goodreadsServerWith(t, grFixture{
			wantUA: wantUA, known: map[string]string{isbn: "52"},
			auto: map[string]string{isbn: "[" + grItem("52", "Dune", "Dune", "Frank Herbert", "x", false) + "]"},
		}, opts...)
		if got, err := src.Identify(context.Background(), Query{ISBNs: []string{isbn}}); err != nil || len(got) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if len(*reqs) != 3 {
			t.Errorf("requests = %v, want the redirect, the page and the suggestions", *reqs)
		}
	}
	const chrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/"
	if !strings.HasPrefix(goodreadsUserAgent, chrome) || strings.Contains(goodreadsUserAgent, "Headless") {
		t.Fatalf("the default is not a desktop Chrome string: %q", goodreadsUserAgent)
	}
	run(goodreadsUserAgent)
	run("custom-agent/1.0", WithGoodreadsUserAgent("custom-agent/1.0"))
	run("custom-agent/2.0", WithGoodreadsUserAgent("  custom-agent/2.0 "))
	run(goodreadsUserAgent, WithGoodreadsUserAgent(""))
	run(goodreadsUserAgent, WithGoodreadsUserAgent("   "))
	run(goodreadsUserAgent, WithGoodreadsUserAgent("two\nlines"))
}

// The page's own cover host is trusted for a cover, and no other Amazon host.
func TestGoodreadsPageCoverHostIsTrusted(t *testing.T) {
	const good = "https://m.media-amazon.com/images/S/compressed.photo.goodreads.com/books/1i/52.jpg"
	if got := goodreadsCoverURL(good); got != good || !TrustedCoverURL(good) {
		t.Errorf("the page's cover is not accepted: %q", got)
	}
	for _, bad := range []string{
		"http://m.media-amazon.com/images/x.jpg",
		"https://m.media-amazon.com:8443/images/x.jpg",
		"https://images-na.ssl-images-amazon.com/images/x.jpg",
		"https://m.media-amazon.com.evil.test/x.jpg",
		"https://m.media-amazon.com/images/nophoto/book/111x148.png",
	} {
		if got := goodreadsCoverURL(bad); got != "" {
			t.Errorf("goodreadsCoverURL(%q) = %q", bad, got)
		}
	}
}
