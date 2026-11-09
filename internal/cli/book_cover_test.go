package cli

import (
	"image"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestCoverPreviewShortcutRemoved(t *testing.T) {
	m := pixelCoverFixture()
	for _, details := range []bool{false, true} {
		m.detailOpen = details
		before := m.View().Content
		next, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
		after := next.(searchSelectModel)
		if after.detailOpen != details || after.View().Content != before || strings.Contains(before, "p cover") {
			t.Fatal("p still changes book page")
		}
	}
	h := historySelectModel{width: 120, height: 24, state: historyStateResults, items: []zlib.DownloadHistoryItem{{ID: "1"}}, details: m.details, detailOpen: true}
	before := h.View().Content
	next, _ := h.Update(tea.KeyPressMsg{Code: 'p'})
	if next.(historySelectModel).View().Content != before {
		t.Fatal("p still changes history details")
	}
}

func TestCoverCacheBounded(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 2000))
	d := bookDetailState{}
	for i := range 13 {
		d.storeCover(bookCoverResultMsg{url: strings.Repeat("x", i), view: "fixture", image: img})
	}
	pixels := 0
	for _, img := range d.coverImages {
		pixels += img.Bounds().Dx() * img.Bounds().Dy()
	}
	if pixels > 8_000_000 || len(d.coverImages) > 12 || len(d.coverImages) != len(d.covers) {
		t.Fatal("preview images escaped cache eviction")
	}
}

func TestBookDetailsOpensAndReturnsToSelection(t *testing.T) {
	m := searchSelectModel{width: 120, height: 24, state: searchStateResults, cursor: 1, listOffset: 1, books: []zlib.Book{{ID: "1"}, {ID: "2", Name: "Selected book"}}}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'i'})
	m = next.(searchSelectModel)
	if !m.detailOpen || !strings.Contains(m.feedView(), "Book details") {
		t.Fatal("details did not open")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(searchSelectModel)
	if m.detailOpen || m.cursor != 1 || m.listOffset != 1 || m.quitting {
		t.Fatal("return lost list state")
	}
	h := historySelectModel{width: 120, height: 24, state: historyStateResults, items: []zlib.DownloadHistoryItem{{ID: "1"}}}
	next, _ = h.Update(tea.KeyPressMsg{Code: 'i'})
	h = next.(historySelectModel)
	if !h.detailOpen {
		t.Fatal("history details did not open")
	}
	next, _ = h.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	h = next.(historySelectModel)
	if h.detailOpen || h.quitting {
		t.Fatal("history details did not return")
	}
}

func TestBookCoverResponsiveCard(t *testing.T) {
	img := image.NewRGBA(image.Rect(5, 7, 105, 157))
	for y := 7; y < 157; y++ {
		for x := 5; x < 105; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	// The rendered cover is only the blank block the pixel image is drawn over.
	cover := renderBookCover(img)
	rows := strings.Split(cover, "\n")
	if len(rows) != coverRows || ansi.StringWidth(rows[0]) != coverWidth || strings.TrimSpace(cover) != "" {
		t.Fatalf("cover slot = %d rows, %q", len(rows), rows[0])
	}
	if renderBookCover(nil) != "" {
		t.Fatal("a missing image must not look loaded")
	}
	book := zlib.Book{ID: "1", Name: "A book", Authors: []string{"A writer"}}
	for _, size := range [][2]int{{100, 24}, {120, 24}, {80, 24}, {100, 19}} {
		card := selectedBookCardWithCover(book, size[0], size[1], "SLOT")
		if strings.Contains(card, "SLOT") != (size[0] >= 100 && size[1] >= 20) {
			t.Fatalf("cover visibility at %v", size)
		}
		if len(strings.Split(card, "\n")) != selectedBookCardRows(size[0], size[1]) {
			t.Fatalf("card height changed at %v", size)
		}
		for line := range strings.SplitSeq(card, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("card overflow at %v", size)
			}
		}
	}
}

func TestBookCoverLateResponseKeepsSelectedCover(t *testing.T) {
	m := searchSelectModel{width: 120, height: 24, books: []zlib.Book{{ID: "2", Cover: "second"}}}
	m.details.covers = map[string]string{"second": "current"}
	m.details.coverPending = map[string]bool{"first": true}
	m.details.storeCover(bookCoverResultMsg{url: "first", view: "old"})
	if m.details.covers[m.selectedBook().Cover] != "current" {
		t.Fatal("late cover replaced selection")
	}
}

func TestCoverLoadingShowsOnlyASpinner(t *testing.T) {
	var d bookDetailState
	for frame := range 3 {
		d.coverFrame = frame
		view := ansi.Strip(d.coverView("1:hash", "https://example.com/c.jpg"))
		lines := strings.Split(view, "\n")
		if len(lines) != coverRows {
			t.Fatalf("loading slot has %d rows, want %d", len(lines), coverRows)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != coverWidth {
				t.Fatalf("loading slot line is %d cells wide, want %d", ansi.StringWidth(line), coverWidth)
			}
		}
		text := strings.Join(strings.Fields(view), "")
		if len([]rune(text)) != 1 || text[0] < 0xe2 {
			t.Fatalf("loading slot shows %q, want a single spinner glyph", text)
		}
	}
	d.coverFrame = 0
	if got := strings.Join(strings.Fields(ansi.Strip(d.coverView("1:hash", "https://example.com/c.jpg"))), ""); got != "⣾" {
		t.Fatalf("first frame is %q, want ⣾", got)
	}
}

func TestCoverFailureKeepsItsBorderedLabel(t *testing.T) {
	d := bookDetailState{covers: map[string]string{"https://example.com/c.jpg": ""}}
	if view := ansi.Strip(d.coverView("1:hash", "https://example.com/c.jpg")); !strings.Contains(view, "unavailable") || !strings.Contains(view, "╭") {
		t.Fatalf("failed cover lost its label or border:\n%s", view)
	}
}
