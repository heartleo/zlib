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
		fmt.Fprint(w, `{"success":1,"book":{"id":42,"hash":"abc123","title":"Detail Book","author":"Author","extension":"epub"}}`)
	}))
	defer server.Close()

	book, err := newLoggedInEAPITestClient(server.URL).FetchBook("42:abc123")
	if err != nil {
		t.Fatalf("FetchBook() error = %v", err)
	}
	if book.Name != "Detail Book" || book.DownloadURL != server.URL+"/eapi/book/42/abc123/file" {
		t.Errorf("FetchBook() = %+v", book)
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
