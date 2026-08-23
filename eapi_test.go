package zlib

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
