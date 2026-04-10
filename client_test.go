package zlib

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginRPCPath && r.Method == "POST" {
			http.SetCookie(w, &http.Cookie{Name: "remix_userid", Value: "123"})
			http.SetCookie(w, &http.Cookie{Name: "remix_userkey", Value: "abc"})
			resp := map[string]interface{}{
				"response": map[string]interface{}{},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loginDomain = buildLoginURL(server.URL)

	err := c.Login("test@example.com", "testpassword")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if c.cookies["remix_userid"] != "123" {
		t.Errorf("expected userid cookie '123', got %q", c.cookies["remix_userid"])
	}
	if c.cookies["remix_userkey"] != "abc" {
		t.Errorf("expected userkey cookie 'abc', got %q", c.cookies["remix_userkey"])
	}
}

func TestFetchBook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>
		<z-cover id="123" title="Test Book"><img class="image" src="/c.jpg"></z-cover>
		<i class="authors"><a href="/a">Author A</a></i>
		<div class="bookDetailsBox">
		  <div class="bookProperty property_year"><div class="property_value">2020</div></div>
		  <div class="bookProperty property__file"><div class="property_value">PDF, 10 MB</div></div>
		</div>
		<a class="btn btn-default addDownloadedBook" href="/dl/abc">PDF</a>
		</html>`)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	book, err := c.FetchBook("123")
	if err != nil {
		t.Fatalf("FetchBook() error = %v", err)
	}
	if book.Name != "Test Book" {
		t.Errorf("Name = %q", book.Name)
	}
	if book.DownloadURL != server.URL+"/dl/abc" {
		t.Errorf("DownloadURL = %q", book.DownloadURL)
	}
}

func TestLogin_ValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"response": map[string]interface{}{
				"validationError": "Invalid credentials",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loginDomain = buildLoginURL(server.URL)

	err := c.Login("bad@example.com", "wrong")
	if err == nil {
		t.Fatal("Login() expected error, got nil")
	}
}

func TestWithDomain(t *testing.T) {
	c := NewClient(WithDomain("https://example.com"))

	if c.Domain() != "https://example.com" {
		t.Fatalf("Domain() = %q, want %q", c.Domain(), "https://example.com")
	}
	if c.loginDomain != buildLoginURL("https://example.com") {
		t.Fatalf("loginDomain = %q, want %q", c.loginDomain, buildLoginURL("https://example.com"))
	}
}
