package zlib

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchBookEAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/book/42/abc123" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"success":1,"book":{"id":42,"hash":"abc123","title":"Detail Book","author":"Author","extension":"epub","categories":"Self-Help, Relationships & Lifestyle - Relationships","year":2015,"edition":6,"publisher":"人民邮电出版社","language":"Chinese","pages":540,"isbn10":"7115390576","isbn13":"9787115390578"}}`)
	}))
	defer server.Close()

	book, err := newLoggedInEAPITestClient(server.URL).FetchBook("42:abc123")
	if err != nil {
		t.Fatalf("FetchBook() error = %v", err)
	}
	if book.Name != "Detail Book" || book.DownloadURL != server.URL+"/eapi/book/42/abc123/file" {
		t.Errorf("FetchBook() = %+v", book)
	}
	if book.Categories != "Self-Help, Relationships & Lifestyle - Relationships" || book.Year != "2015" || book.Edition != "6" || book.Publisher != "人民邮电出版社" || book.Language != "Chinese" || book.Pages != "540" || book.ISBN10 != "7115390576" || book.ISBN13 != "9787115390578" {
		t.Errorf("FetchBook() lost publication details: %+v", book)
	}
}

func TestEAPIBookDerivesISBNPair(t *testing.T) {
	client := NewClient(WithDomain("https://example.com"))
	for _, tc := range []struct {
		identifier string
		isbn10     string
		isbn13     string
	}{
		{"9787115390578", "7115390576", "9787115390578"},
		{"978-7-115-39057-8", "7115390576", "9787115390578"},
		{"7115390576", "7115390576", "9787115390578"},
		{"9791234567896", "", "9791234567896"},
		// Z-Library lists several identifiers in one comma-separated string: an
		// ISBN-13 with its ISBN-10 (the usual form in search results), either
		// order, and sometimes an Amazon ASIN, which is not an ISBN.
		{"9787229042066,7229042062", "7229042062", "9787229042066"},
		{"7229042062,9787229042066", "7229042062", "9787229042066"},
		{"978-7-229-04206-6, 7-229-04206-2", "7229042062", "9787229042066"},
		{"9787229042066;7229042062", "7229042062", "9787229042066"},
		{"B07S7CCQGL", "", ""},
		{"B07S7CCQGL,9787229042066", "7229042062", "9787229042066"},
		{"9787229042066,9787229042067", "7229042062", "9787229042066"}, // the second has a bad check digit
		// An identifier can list the ISBNs of several editions. The pair stays
		// consistent: the ISBN-10 is the one for the chosen ISBN-13, never one
		// that belongs to another edition in the list.
		{"9787229042066,7536692935", "7229042062", "9787229042066"},
		{"9787520150446,9780120167067,0120167069,7520150445", "7520150445", "9787520150446"},
		{"9788666362526,9798666362525,8666362529", "8666362529", "9788666362526"},
		{"9781593272203,9781593272913,1593272200,159327291X", "1593272200", "9781593272203"},
		{"0120167069,9787520150446", "7520150445", "9787520150446"}, // an ISBN-10 alone is not matched to a later ISBN-13
	} {
		book := client.mapEAPIBook(eapiBook{Identifier: tc.identifier})
		if book.ISBN10 != tc.isbn10 || book.ISBN13 != tc.isbn13 {
			t.Errorf("identifier %q: ISBN10=%q ISBN13=%q", tc.identifier, book.ISBN10, book.ISBN13)
		}
	}
}

func TestDownloadResolvesEAPIFileLink(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eapi/book/42/abc123/file":
			if r.Header.Get("remix-userkey") != "key123" {
				t.Error("file-link request missing EAPI credentials")
			}
			fmt.Fprintf(w, `{"success":1,"file":{"downloadLink":%q}}`, server.URL+"/cdn/book.epub")
		case "/cdn/book.epub":
			w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
			fmt.Fprint(w, "epub-data")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	result, err := newLoggedInEAPITestClient(server.URL).Download(
		server.URL+"/eapi/book/42/abc123/file",
		dir,
		nil,
	)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "book.epub"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "epub-data" || result.Size != int64(len(data)) {
		t.Errorf("download result = %+v, data = %q", result, data)
	}
}
