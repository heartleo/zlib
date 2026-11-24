package zlib

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginEAPISavesRemixCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/user/login" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s, want POST /eapi/user/login", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if r.Form.Get("email") != "reader@example.com" || r.Form.Get("password") != "secret" {
			t.Errorf("login form = %v", r.Form)
		}
		fmt.Fprint(w, `{"success":1,"user":{"id":123,"email":"reader@example.com","name":"Reader","remix_user_key":"key-from-legacy-field","remix_userkey":"key123"}}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	if err := c.LoginEAPI("reader@example.com", "secret"); err != nil {
		t.Fatalf("LoginEAPI() error = %v", err)
	}
	if c.Mode() != ClientModeEAPI {
		t.Errorf("Mode() = %q, want %q", c.Mode(), ClientModeEAPI)
	}
	if c.Cookies()["remix_userid"] != "123" || c.Cookies()["remix_userkey"] != "key123" {
		t.Errorf("Cookies() = %v, want EAPI remix credentials", c.Cookies())
	}
}

func TestSearchEAPIIdentifiesClientSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != eapiSearchPath || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, eapiSearchPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("source") != "android" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"success":0,"error":"Access denied"}`)
			return
		}
		fmt.Fprint(w, `{"success":1,"books":[],"pagination":{"current":1,"total_pages":0}}`)
	}))
	defer server.Close()
	c := NewClient(WithDomain(server.URL))
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "FAKE-KEY"})
	result, err := c.SearchContext(t.Context(), "fixture nonexistent title", 1, 3, nil)
	if err != nil {
		t.Fatalf("SearchContext() error = %v, want a successful empty result", err)
	}
	if len(result.Books) != 0 || result.Page != 1 || result.TotalPages != 0 {
		t.Errorf("SearchContext() = %+v, want empty books on page 1", result)
	}
}

// EAPI answers with application/json, so a book whose title or a query that
// mentions the vendor must not be mistaken for a DiamWall block page.
func TestSearchEAPIAllowsDiamWallInJSONPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if _, ok := r.Form["order"]; ok {
			t.Error("default search sent an order value")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":1,"books":[{"id":"1","hash":"h","title":"Defeating DiamWall"}],"pagination":{"current":1,"total_pages":1}}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	got, err := c.searchEAPI("diamwall", 1, 10, nil)
	if err != nil {
		t.Fatalf("searchEAPI() error = %v, want the results", err)
	}
	if len(got.Books) != 1 || got.Books[0].Name != "Defeating DiamWall" {
		t.Errorf("searchEAPI() books = %+v, want the DiamWall title", got.Books)
	}
}

func TestSearchEAPISendsAllFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		for key, want := range map[string]string{"message": "books", "page": "2", "limit": "50", "yearFrom": "2001", "yearTo": "2020", "e": "1", "order": "bestmatch"} {
			if got := r.Form.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		if got := r.Form["languages[]"]; len(got) != 2 || got[0] != "chinese" || got[1] != "english" {
			t.Errorf("languages = %v", got)
		}
		if got := r.Form["extensions[]"]; len(got) != 2 || got[0] != "epub" || got[1] != "pdf" {
			t.Errorf("extensions = %v", got)
		}
		fmt.Fprint(w, `{"success":1,"books":[],"pagination":{"current":2,"total_pages":3}}`)
	}))
	defer server.Close()
	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})
	_, err := c.Search("books", 2, 50, &SearchOptions{FromYear: 2001, ToYear: 2020, Exact: true, Order: OrderBestMatch, Languages: []Language{LangChinese, LangEnglish}, Extensions: []Extension{ExtEPUB, ExtPDF}})
	if err != nil {
		t.Fatal(err)
	}
}

// A non-2xx EAPI response still carries {"success":0,"error":"..."}. The
// server's message must survive instead of being flattened into a bare status.
func TestEAPIPreservesServerErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"success":0,"error":"Daily download limit reached"}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	_, err := c.searchEAPI("go", 1, 10, nil)
	if err == nil {
		t.Fatal("searchEAPI() error = nil, want the server message")
	}
	if !strings.Contains(err.Error(), "Daily download limit reached") {
		t.Errorf("searchEAPI() error = %v, want it to carry the server message", err)
	}
}

// A non-2xx response that is NOT an EAPI envelope still fails on status.
func TestEAPIReportsStatusWhenBodyIsNotAnEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "gateway exploded")
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	_, err := c.searchEAPI("go", 1, 10, nil)
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("searchEAPI() error = %v, want HTTPStatusError 502", err)
	}
}

// A zero Page freezes the interactive pager, so fall back to the page asked for.
func TestSearchEAPIFallsBackToRequestedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":1,"books":[],"pagination":{"total_pages":5}}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "k"})

	got, err := c.searchEAPI("go", 3, 10, nil)
	if err != nil {
		t.Fatalf("searchEAPI() error = %v", err)
	}
	if got.Page != 3 {
		t.Errorf("searchEAPI() Page = %d, want 3", got.Page)
	}
}

func TestLoginEAPIReportsValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":0,"error":"Incorrect email or password"}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	err := c.LoginEAPI("reader@example.com", "wrong")
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("LoginEAPI() error = %v, want ErrLoginFailed", err)
	}
}

func TestSearchEAPIMapsJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/book/search" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s, want POST /eapi/book/search", r.Method, r.URL.Path)
		}
		if r.Header.Get("remix-userid") != "123" || r.Header.Get("remix-userkey") != "key123" {
			t.Errorf("missing EAPI auth headers: %v", r.Header)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if r.Form.Get("message") != "golang" || r.Form.Get("page") != "2" || r.Form.Get("limit") != "5" {
			t.Errorf("search form = %v", r.Form)
		}
		response := map[string]any{
			"success": 1,
			"books": []map[string]any{{
				"id": 115066162, "title": "Everyday Golang", "author": "Alex Ellis",
				"year": 2024, "language": "English", "extension": "pdf",
				"filesizeString": "2.51 MB", "href": "/book/4LYvvJ6V92/title.html",
				"hash": "e9f13b", "dl": "/dl/r9wJJD236l", "cover": "https://covers.example/book.jpg",
			}},
			"pagination": map[string]any{"current": 2, "total_pages": 9},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "123", "remix_userkey": "key123"})
	result, err := c.Search("golang", 2, 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Page != 2 || result.TotalPages != 9 || len(result.Books) != 1 {
		t.Fatalf("Search() = %+v", result)
	}
	book := result.Books[0]
	if book.Hash != "e9f13b" || book.Name != "Everyday Golang" || book.URL != server.URL+"/book/4LYvvJ6V92/title.html" {
		t.Errorf("mapped book = %+v", book)
	}
	if book.DownloadURL != server.URL+"/eapi/book/115066162/e9f13b/file" || book.Size != "2.51 MB" {
		t.Errorf("mapped download fields = %+v", book)
	}
}

func TestLoginEAPIRejectionDoesNotCallHTMLFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/user/login" {
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":0,"error":"Authorization failed"}`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	err := c.Login("reader@example.com", "secret")
	if !errors.Is(err, ErrLoginFailed) || !strings.Contains(err.Error(), "Authorization failed") {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestEAPIRequestReportsRejectedSessionAsExpired(t *testing.T) {
	for _, message := range []string{"Invalid credentials", "Please login"} {
		t.Run(message, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, `{"success":0,"error":%q}`, message)
			}))
			defer server.Close()
			client := NewClient(WithDomain(server.URL))
			client.SetCookies(map[string]string{"remix_userid": "123", "remix_userkey": "stale"})

			calls := map[string]func() error{
				"profile":   func() error { _, err := client.GetProfile(); return err },
				"history":   func() error { _, err := client.DownloadHistory(1); return err },
				"favorites": func() error { _, err := client.SavedBooks(1); return err },
				"search":    func() error { _, err := client.Search("dune", 1, 10, nil); return err },
			}
			for name, call := range calls {
				if err := call(); !errors.Is(err, ErrSessionExpired) {
					t.Errorf("%s error = %v, want ErrSessionExpired", name, err)
				}
			}
		})
	}
}

func TestEAPIRequestKeepsOtherRejectionsAndLoginFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if r.URL.Path == eapiLoginPath {
			fmt.Fprint(w, `{"success":0,"error":"Invalid credentials"}`)
			return
		}
		fmt.Fprint(w, `{"success":0,"error":"Daily limit reached"}`)
	}))
	defer server.Close()
	client := NewClient(WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "123", "remix_userkey": "key"})

	if _, err := client.SavedBooks(1); errors.Is(err, ErrSessionExpired) || err == nil || !strings.Contains(err.Error(), "Daily limit reached") {
		t.Fatalf("SavedBooks error = %v, want the server message", err)
	}
	// A wrong password must read as a failed login, even with a stale session
	// still loaded in the client.
	if err := client.Login("reader@example.com", "wrong"); !errors.Is(err, ErrLoginFailed) || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Login error = %v, want ErrLoginFailed only", err)
	}
}
