package metasource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const olISBNBody = `{"ISBN:9780140328721":{"title":"Matilda","subtitle":"a tale","key":"/books/OL7353617M","publish_date":"March 14, 2002",
"authors":[{"name":"Roald Dahl","url":"x"}],"publishers":[{"name":"Puffin"}],
"subjects":[{"name":"Accessible book"},{"name":"Fiction"},{"name":"Children"}],
"identifiers":{"isbn_13":["9780140328721"],"openlibrary":["OL7353617M"]},
"cover":{"large":"https://covers.openlibrary.org/b/id/1-L.jpg"}}}`

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenLibraryByISBN(t *testing.T) {
	var gotQuery string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "zlib-metadata-lookup") {
			t.Errorf("User-Agent = %q", ua)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		w.Write([]byte(olISBNBody))
	})
	got, err := newOpenLibrary(srv.URL, nil).Identify(context.Background(), Query{ISBNs: []string{"9780140328721"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(gotQuery, "ISBN%3A9780140328721") {
		t.Errorf("query = %q", gotQuery)
	}
	c := got[0]
	if c.Title != "Matilda: a tale" || c.Published != "2002-03-14" || c.Publisher != "Puffin" || !c.EditionLevel ||
		!reflect.DeepEqual(c.Authors, []string{"Roald Dahl"}) || c.ISBN() != "9780140328721" ||
		c.CoverURL != "https://covers.openlibrary.org/b/id/1-L.jpg" {
		t.Fatalf("candidate = %+v", c)
	}
}

func TestOpenLibraryTitleSearchIsWorkLevel(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" || r.URL.Query().Get("title") != "原则" || r.URL.Query().Get("author") != "瑞·达利欧" {
			t.Errorf("request = %s", r.URL)
		}
		w.Write([]byte(`{"docs":[{"key":"/works/OL1W","title":"原则","author_name":["瑞·达利欧"],"first_publish_year":2018,
"subject":["Business"],"language":["chi","eng"],"cover_i":7,"publisher":["Many","Publishers"]}]}`))
	})
	got, err := newOpenLibrary(srv.URL, nil).Identify(context.Background(), Query{Title: "原则", Authors: []string{"[美] 瑞·达利欧 著"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := got[0]
	if c.EditionLevel || c.ISBN() != "" || c.Publisher != "" || len(c.Languages) != 0 || c.Published != "2018" {
		t.Fatalf("work-level result claims edition data: %+v", c)
	}
	if c.CoverURL != "https://covers.openlibrary.org/b/id/7-L.jpg?default=false" {
		t.Errorf("cover = %q", c.CoverURL)
	}
}

func TestSourceRefusesOffHostRedirectAndHugeBodies(t *testing.T) {
	other := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("followed an off-host redirect") })
	redirect := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	})
	src := newOpenLibrary(redirect.URL, nil)
	if _, err := src.Identify(context.Background(), Query{ISBNs: []string{"9780140328721"}}); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
	huge := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"x":"` + strings.Repeat("a", maxBodyBytes) + `"}`))
	})
	if _, err := newOpenLibrary(huge.URL, nil).Identify(context.Background(), Query{ISBNs: []string{"9780140328721"}}); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceErrorsDoNotLeakTheQuery(t *testing.T) {
	srv := serve(t, func(http.ResponseWriter, *http.Request) {})
	url := srv.URL
	srv.Close()
	_, err := newOpenLibrary(url, nil).Identify(context.Background(), Query{Title: "A Very Private Title"})
	if err == nil || strings.Contains(err.Error(), "Private") || strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
}

func TestGoogleBooks(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "isbn:9780140328721" {
			t.Errorf("q = %q", q)
		}
		w.Write([]byte(`{"items":[{"id":"abc","volumeInfo":{"title":"Matilda","authors":["Roald Dahl"],"publisher":"Puffin Books",
"publishedDate":"2007-03","description":"<p>A girl &amp; her <b>books</b>.</p><p>Second.</p>","categories":["Juvenile Fiction / Humorous Stories"],
"language":"en","industryIdentifiers":[{"type":"ISBN_10","identifier":"0140328726"},{"type":"ISBN_13","identifier":"9780140328721"}],
"imageLinks":{"thumbnail":"http://books.google.com/books/content?id=abc"}}}]}`))
	})
	got, err := newGoogleBooks(srv.URL, nil, "").Identify(context.Background(), Query{ISBNs: []string{"9780140328721"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := got[0]
	if c.Description != "A girl & her books.\n\nSecond." || c.Published != "2007-03" || c.ISBN() != "9780140328721" ||
		!reflect.DeepEqual(c.Tags, []string{"Juvenile Fiction", "Humorous Stories"}) ||
		c.CoverURL != "https://books.google.com/books/content?id=abc" || !reflect.DeepEqual(c.Languages, []string{"en"}) {
		t.Fatalf("candidate = %+v", c)
	}
}

func TestGoogleBooksRateLimitIsASourceError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":429}}`))
	})
	_, err := newGoogleBooks(srv.URL, nil, "").Identify(context.Background(), Query{Title: "Dune"})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

func TestGoogleBooksQuotesTitleAndAuthor(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		// The leading article is dropped: "War of Art" finds "The War of Art".
		want := `intitle:"War of Art" inauthor:"Steven Pressfield"`
		if q := r.URL.Query().Get("q"); q != want {
			t.Errorf("q = %q, want %q", q, want)
		}
		w.Write([]byte(`{}`))
	})
	if _, err := newGoogleBooks(srv.URL, nil, "").Identify(context.Background(),
		Query{Title: "The War of Art: Winning the Inner Creative Battle", Authors: []string{"Pressfield, Steven"}}); err != nil {
		t.Fatal(err)
	}
}
