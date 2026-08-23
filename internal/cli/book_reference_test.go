package cli

import (
	"testing"

	"github.com/heartleo/zlib"
)

func TestBookCommandIDUsesEAPIIDAndHash(t *testing.T) {
	book := zlib.Book{ID: "115066162", Hash: "e9f13b", URL: "https://z-lib.gd/book/slug"}
	if got := bookCommandID(book); got != "115066162:e9f13b" {
		t.Fatalf("bookCommandID() = %q", got)
	}
}

func TestHistoryCommandIDUsesEAPIIDAndHash(t *testing.T) {
	item := zlib.DownloadHistoryItem{ID: "42", Hash: "abc123"}
	if got := historyCommandID(item); got != "42:abc123" {
		t.Fatalf("historyCommandID() = %q", got)
	}
}
