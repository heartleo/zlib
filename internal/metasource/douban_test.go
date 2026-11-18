package metasource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// doubanServer answers the three Douban endpoints from one host, like
// newDouban(srv.URL, srv.URL, nil) expects.
func doubanServer(t *testing.T, known map[string]string, search string, details map[string]string) (*Douban, *[]string) {
	t.Helper()
	var paths []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "zlib-metadata-lookup") {
			t.Errorf("User-Agent = %q", ua)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/isbn/"):
			isbn := strings.Trim(strings.TrimPrefix(r.URL.Path, "/isbn/"), "/")
			if id, ok := known[isbn]; ok {
				http.Redirect(w, r, "/subject/"+id+"/", http.StatusMovedPermanently)
				return
			}
			http.NotFound(w, r)
		case r.URL.Path == "/rexxar/api/v2/search/book":
			if r.Header.Get("Referer") == "" {
				t.Error("search sent without a Referer")
			}
			w.Write([]byte(search))
		case strings.HasPrefix(r.URL.Path, "/rexxar/api/v2/book/"):
			if r.Header.Get("Referer") == "" {
				t.Error("detail sent without a Referer")
			}
			id := strings.TrimPrefix(r.URL.Path, "/rexxar/api/v2/book/")
			if body, ok := details[id]; ok {
				w.Write([]byte(body))
				return
			}
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
	return newDouban(srv.URL, srv.URL, nil), &paths
}

// The detail response as the live service returns it: lists of strings, an
// empty tags list, and no isbn, language or publisher key.
const doubanThreeBody = `{"id":"2567698","title":"三体","author":["刘慈欣"],"press":["重庆出版社"],"pubdate":["2008-1"],
"pages":["302"],"tags":[],"intro":"文化大革命如火如荼进行的同时。\n军方探寻外星文明的绝秘计划取得了突破性进展。"}`

func TestDoubanByISBNResolvesRedirectWithoutFollowingIt(t *testing.T) {
	src, paths := doubanServer(t, map[string]string{"9787536692930": "2567698"}, "",
		map[string]string{"2567698": doubanThreeBody})
	got, err := src.Identify(context.Background(), Query{ISBNs: []string{"9787536692930"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := got[0]
	if c.Title != "三体" || c.Publisher != "重庆出版社" || c.Published != "2008-01" || !c.EditionLevel ||
		!reflect.DeepEqual(c.Authors, []string{"刘慈欣"}) || c.ISBN() != "9787536692930" ||
		!reflect.DeepEqual(c.Languages, []string{"zh"}) {
		t.Fatalf("candidate = %+v", c)
	}
	if len(c.Tags) != 0 {
		t.Errorf("tags = %v", c.Tags)
	}
	if !strings.Contains(c.Description, "\n军方") {
		t.Errorf("description = %q", c.Description)
	}
	var douban string
	for _, id := range c.Identifiers {
		if id.Scheme == "douban" {
			douban = id.Value
		}
	}
	if douban != "2567698" {
		t.Errorf("identifiers = %+v", c.Identifiers)
	}
	for _, p := range *paths {
		if strings.HasPrefix(p, "/subject/") {
			t.Errorf("the subject page %s was downloaded", p)
		}
	}
}

// The detail response names the cover; only an https URL on a Douban image host
// is kept, and the cover fetch sends the Referer those hosts require.
func TestDoubanCandidateCarriesTheCoverOnDoubansImageHost(t *testing.T) {
	const img = "https://img3.doubanio.com/view/subject/l/public/s2768378.jpg"
	body := strings.Replace(doubanThreeBody, `"id":"2567698",`, `"id":"2567698","cover_url":"`+img+`",`, 1)
	src, _ := doubanServer(t, map[string]string{"9787536692930": "2567698"}, "", map[string]string{"2567698": body})
	got, err := src.Identify(context.Background(), Query{ISBNs: []string{"9787536692930"}})
	if err != nil || len(got) != 1 || got[0].CoverURL != img {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, raw := range []string{"http://img3.doubanio.com/a.jpg", "https://evil.example/a.jpg", "https://doubanio.com.evil.example/a.jpg", ""} {
		if got := doubanCoverURL(raw); got != "" {
			t.Errorf("doubanCoverURL(%q) = %q", raw, got)
		}
	}
	u, _ := url.Parse(img)
	if !coverHosts.allows(u) || coverReferer(u) != doubanCoverReferer {
		t.Errorf("the Douban image host is not allowed with its Referer")
	}
	other, _ := url.Parse("https://covers.openlibrary.org/b/id/1-L.jpg")
	if coverReferer(other) != "" {
		t.Error("a Douban Referer would be sent to another host")
	}
}

func TestDoubanTitleSearchFetchesDetailsForTheBestTitles(t *testing.T) {
	search := `{"total":4,"items":[
{"target_type":"book","target":{"id":"1","title":"三体Ⅱ"}},
{"target_type":"book","target":{"id":"2567698","title":"三体"}},
{"target_type":"book","target":{"id":"6414884","title":"三体"}},
{"target_type":"movie","target":{"id":"9","title":"三体"}}]}`
	src, paths := doubanServer(t, nil, search, map[string]string{
		"2567698": doubanThreeBody,
		"6414884": `{"title":"三体","author":["刘慈欣"],"press":["重庆出版社"],"pubdate":["2016-1"]}`,
		"1":       `{"title":"三体Ⅱ","author":["刘慈欣"]}`,
	})
	got, err := src.Identify(context.Background(), Query{Title: "三体", Authors: []string{"刘慈欣"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	// The exact title comes first, in Douban's own order, then the sequel.
	if got[0].Published != "2008-01" || got[1].Published != "2016-01" || got[2].Title != "三体Ⅱ" {
		t.Errorf("order = %q %q %q", got[0].Published, got[1].Published, got[2].Title)
	}
	for _, p := range *paths {
		if p == "/rexxar/api/v2/book/9" {
			t.Error("a movie hit was read as a book")
		}
	}
}

func TestDoubanEditionsOfOneTitleStayApart(t *testing.T) {
	two := []Candidate{
		{Sources: []string{"Douban"}, EditionLevel: true, Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Published: "2008-01",
			Identifiers: appendIdentifier(nil, "douban", "2567698")},
		{Sources: []string{"Douban"}, EditionLevel: true, Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Published: "2016-01",
			Identifiers: appendIdentifier(nil, "douban", "6414884")},
	}
	got := rankAndMerge(Query{Title: "三体"}, two, defaultMinScore, 10)
	if len(got) != 2 {
		t.Fatalf("two Douban editions were merged into %d: %+v", len(got), got)
	}
	// A work-level result names no edition and may still join one.
	work := Candidate{Sources: []string{"Open Library"}, Title: "三体", Authors: []string{"刘慈欣"}, Published: "2008"}
	got = rankAndMerge(Query{Title: "三体"}, []Candidate{two[0], work}, defaultMinScore, 10)
	if len(got) != 1 || len(got[0].Sources) != 2 {
		t.Fatalf("work-level result not pooled: %+v", got)
	}
}

func TestDoubanFallsBackToTitleWhenISBNIsUnknown(t *testing.T) {
	search := `{"items":[{"target_type":"book","target":{"id":"7","title":"原则"}}]}`
	src, _ := doubanServer(t, nil, search, map[string]string{"7": `{"title":"原则","author":["[美] 瑞·达利欧"]}`})
	got, err := src.Identify(context.Background(), Query{Title: "原则", ISBNs: []string{"9787508684031"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0].Authors, []string{"瑞·达利欧"}) {
		t.Errorf("authors = %v", got[0].Authors)
	}
	if got[0].ISBN() != "" {
		t.Errorf("a title hit claimed the query ISBN: %q", got[0].ISBN())
	}
}

func TestDoubanRefusesOffHostRedirects(t *testing.T) {
	other := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("followed an off-host redirect") })
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/subject/1/", http.StatusFound)
	})
	src := newDouban(srv.URL, srv.URL, nil)
	if _, err := src.Identify(context.Background(), Query{ISBNs: []string{"9787536692930"}}); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ISBN hop err = %v", err)
	}
	if _, err := src.Identify(context.Background(), Query{Title: "三体"}); err == nil ||
		!strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("search err = %v", err)
	}
}

func TestDoubanThrottleAndBanAreSourceErrors(t *testing.T) {
	limited := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	if _, err := newDouban(limited.URL, limited.URL, nil).Identify(context.Background(), Query{Title: "三体"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("search err = %v", err)
	}
	if _, err := newDouban(limited.URL, limited.URL, nil).Identify(context.Background(), Query{ISBNs: []string{"9787536692930"}}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("isbn err = %v", err)
	}
	banned := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"banned":"x","items":[]}`)) })
	if _, err := newDouban(banned.URL, banned.URL, nil).Identify(context.Background(), Query{Title: "三体"}); err == nil ||
		!strings.Contains(err.Error(), "refused") {
		t.Fatalf("banned err = %v", err)
	}
}

func TestDoubanErrorsDoNotLeakTheQuery(t *testing.T) {
	srv := serve(t, func(http.ResponseWriter, *http.Request) {})
	url := srv.URL
	srv.Close()
	_, err := newDouban(url, url, nil).Identify(context.Background(), Query{Title: "A Very Private Title"})
	if err == nil || strings.Contains(err.Error(), "Private") || strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceNames(t *testing.T) {
	if got := ParseSourceNames(""); !reflect.DeepEqual(got, DefaultSourceNames) {
		t.Errorf("default = %v", got)
	}
	if got := ParseSourceNames(" Douban, google ,douban,"); !reflect.DeepEqual(got, []string{"douban", "google"}) {
		t.Errorf("parsed = %v", got)
	}
	var names []string
	for _, s := range DefaultSources(nil, "") {
		names = append(names, s.Name())
	}
	if !reflect.DeepEqual(names, []string{"Open Library"}) {
		t.Errorf("default sources = %v", names)
	}
	srcs := NewSources([]string{"douban", "doubann"}, SourceConfig{})
	if srcs[0].Name() != "Douban" {
		t.Errorf("first = %s", srcs[0].Name())
	}
	if _, err := srcs[1].Identify(context.Background(), Query{Title: "x"}); err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Errorf("typo err = %v", err)
	}
}
