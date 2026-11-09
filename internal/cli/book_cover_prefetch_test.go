package cli

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestBookCoverLoadingKeepsMetadataPosition(t *testing.T) {
	book := zlib.Book{ID: "1", Name: "Stable title", Authors: []string{"Stable author"}, Cover: "/cover.png"}
	cover := renderBookCover(image.NewRGBA(image.Rect(0, 0, 120, 180)))
	for _, width := range []int{100, 120} {
		before := strings.Split(ansi.Strip(selectedBookCardWithCover(book, width, 24, "")), "\n")
		after := strings.Split(ansi.Strip(selectedBookCardWithCover(book, width, 24, cover)), "\n")
		for _, text := range []string{"Stable title", "Stable author"} {
			position := func(lines []string) (int, int) {
				for y, line := range lines {
					if x := strings.Index(line, text); x >= 0 {
						return ansi.StringWidth(line[:x]), y
					}
				}
				return -1, -1
			}
			x, y := position(before)
			xx, yy := position(after)
			if x < 0 || x != xx || y != yy {
				t.Fatalf("%s moved at width %d: (%d,%d) -> (%d,%d)", text, width, x, y, xx, yy)
			}
		}
	}
}

func TestCoverPrefetchWindowPriorityAndDedup(t *testing.T) {
	books := make([]zlib.Book, 10)
	for i := range books {
		books[i] = zlib.Book{ID: fmt.Sprint(i), Cover: fmt.Sprintf("/cover-%d.png", i)}
	}
	m := searchSelectModel{client: zlib.NewClient(), books: books, cursor: 4, width: 120, height: 24}
	if m.queueSelectedDetail() == nil {
		t.Fatal("no predownload scheduled")
	}
	if len(m.details.coverPending) != 8 || !m.details.coverPending[books[4].Cover] || !m.details.coverPending[books[5].Cover] {
		t.Fatal("selected cover was not first in bounded prefetch")
	}
	if m.queueSelectedCover() != nil {
		t.Fatal("duplicate in-flight requests")
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	for range 5 {
		var finished []string
		for url := range m.details.coverPending {
			finished = append(finished, url)
		}
		for _, url := range finished {
			m.details.storeCover(bookCoverResultMsg{url: url, view: "fixture", image: img})
		}
		m.queueSelectedCover()
		if len(m.details.coverPending) > 8 {
			t.Fatal("prefetch exceeded concurrency limit")
		}
	}
	if len(m.details.covers) != 9 {
		t.Fatalf("wanted selected and four neighbors per side, got %d", len(m.details.covers))
	}
	for i := 0; i <= 8; i++ {
		if m.details.coverImages[books[i].Cover] == nil {
			t.Fatal("neighbor not prefetched")
		}
	}
	m.cursor = 5
	if m.details.coverImages[m.selectedBook().Cover] == nil {
		t.Fatal("next selected book still waits for network")
	}
	m.width = 80
	if m.queueSelectedCover() != nil {
		t.Fatal("narrow terminal started prefetch")
	}
}

func TestCoverCacheEvictsOldestWithoutDroppingSelected(t *testing.T) {
	d := bookDetailState{coverWanted: "selected"}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	d.storeCover(bookCoverResultMsg{url: "selected", view: "fixture", image: img})
	for i := range 12 {
		d.storeCover(bookCoverResultMsg{url: fmt.Sprint(i), view: "fixture", image: img})
	}
	if d.coverImages["selected"] == nil || len(d.covers) != 12 || d.coverImages["0"] != nil {
		t.Fatal("cache cleared hot covers instead of evicting oldest")
	}
	d.touchCover("1")
	d.storeCover(bookCoverResultMsg{url: "new", view: "fixture", image: img})
	if d.coverImages["1"] == nil || d.coverImages["2"] != nil {
		t.Fatal("recently used cover not retained")
	}
}

func TestCoverPrefetchStopsWhenLargeNeighborsAreEvicted(t *testing.T) {
	books := make([]zlib.Book, 5)
	for i := range books {
		books[i] = zlib.Book{ID: fmt.Sprint(i), Cover: fmt.Sprint(i)}
	}
	m := searchSelectModel{client: zlib.NewClient(), books: books, cursor: 2, width: 120, height: 24}
	img := image.NewRGBA(image.Rect(0, 0, 1000, 3000))
	m.queueSelectedCover()
	completed := 0
	for range 8 {
		var pending []string
		for url := range m.details.coverPending {
			pending = append(pending, url)
		}
		if len(pending) == 0 {
			break
		}
		for _, url := range pending {
			m.details.storeCover(bookCoverResultMsg{url: url, view: "fixture", image: img})
			completed++
		}
		m.queueSelectedCover()
	}
	if len(m.details.coverPending) != 0 || completed != 5 {
		t.Fatalf("prefetch cycled under pixel budget: %d downloads", completed)
	}
	if m.details.coverImages[books[2].Cover] == nil {
		t.Fatal("large neighbor evicted selected cover")
	}
	// Selecting an evicted neighbor must still issue its foreground request.
	for i, book := range books {
		if m.details.coverImages[book.Cover] == nil {
			m.cursor = i
			break
		}
	}
	if m.queueSelectedCover() == nil || !m.details.coverPending[m.selectedBook().Cover] {
		t.Fatal("prefetch marker blocked foreground request")
	}
}
