package cli

import (
	"context"
	"fmt"
	"image"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestDownloadTransferDetail(t *testing.T) {
	m := downloadModel{state: stateDownloading, written: 5 << 20, total: 20 << 20, started: time.Now().Add(-5 * time.Second)}
	got := m.transferDetail()
	for _, want := range []string{"5.0 MiB / 20.0 MiB", "/s", "left"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transferDetail() = %q, missing %q", got, want)
		}
	}
	if got := m.percentLabel(); got != " 25%" {
		t.Fatalf("percentLabel() = %q", got)
	}
	m.total = 0
	if got := m.percentLabel(); got != "" {
		t.Fatalf("percentLabel() without total = %q", got)
	}
	if got := m.transferDetail(); strings.Contains(got, "left") {
		t.Fatalf("unknown total must not show an ETA: %q", got)
	}
}

func TestDownloadConnectingState(t *testing.T) {
	m := newDownloadModel("1:abc", t.TempDir(), nil)
	m.state = stateDownloading
	if got := m.viewContent(); !strings.Contains(got, "Connecting") {
		t.Fatalf("viewContent() = %q, want connecting state", got)
	}
}

func TestCoverViewPlaceholders(t *testing.T) {
	var d bookDetailState
	if got := ansi.Strip(d.coverView("", "")); !strings.Contains(got, "no cover") {
		t.Fatalf("coverView(\"\") = %q, want %q", got, "no cover")
	}
	if got := ansi.Strip(d.coverView("", "https://example.com/a.jpg")); strings.Contains(got, "loading") || !strings.Contains(got, "⣾") {
		t.Fatalf("loading cover = %q, want only the spinner", got)
	}
	d.covers = map[string]string{"https://example.com/b.jpg": ""}
	if got := ansi.Strip(d.coverView("", "https://example.com/b.jpg")); !strings.Contains(got, "unavailable") {
		t.Fatalf("failed cover placeholder = %q", got)
	}
	d.covers["https://example.com/c.jpg"] = "pixels"
	if got := d.coverView("", "https://example.com/c.jpg"); got != "pixels" {
		t.Fatalf("loaded cover = %q", got)
	}
}

func TestCoverAnimationClock(t *testing.T) {
	c := zlib.NewClient()
	var d bookDetailState
	// Loading slot: the clock starts once, and advancing keeps it running.
	if d.startCoverTick(c, 120, 30, "1:a", "https://example.com/a.jpg") == nil || d.coverTicking != true {
		t.Fatal("loading cover must start the animation clock")
	}
	if d.startCoverTick(c, 120, 30, "1:a", "https://example.com/a.jpg") != nil {
		t.Fatal("a second clock must not start while one is running")
	}
	if d.advanceCoverTick(c, 120, 30, "1:a", "https://example.com/a.jpg") == nil || d.coverFrame != 1 {
		t.Fatal("clock must continue while the cover is loading")
	}
	// Loaded: the clock stops by itself.
	d.covers = map[string]string{"https://example.com/a.jpg": "pixels"}
	if d.advanceCoverTick(c, 120, 30, "1:a", "https://example.com/a.jpg") != nil || d.coverTicking {
		t.Fatal("clock must stop once nothing animates")
	}
}

// Without a Kitty or Sixel protocol covers are hidden entirely: no column, no
// placeholder, no animation clock and no network requests.
func TestCoversHiddenWithoutProtocol(t *testing.T) {
	coverGraphicsAvailable.Store(false)
	t.Cleanup(func() { coverGraphicsAvailable.Store(true) })

	book := zlib.Book{ID: "1", Name: "Stable title", Authors: []string{"Stable author"}, Cover: "https://example.com/a.jpg"}
	if coverFits(120, 30) {
		t.Fatal("coverFits must be false without a protocol")
	}
	card := ansi.Strip(selectedBookCardWithCover(book, 120, 30, "SHOULD-NOT-APPEAR"))
	if strings.Contains(card, "SHOULD-NOT-APPEAR") || strings.Contains(card, "loading") || strings.Contains(card, "no cover") {
		t.Fatalf("hidden covers must leave no slot:\n%s", card)
	}
	textOnly := ansi.Strip(selectedBookCardWithCover(book, 99, 30, ""))
	if strings.Count(card, "\n") != strings.Count(textOnly, "\n") {
		t.Fatal("card height should match the text-only card")
	}

	fetched := false
	m := searchSelectModel{client: zlib.NewClient(), books: []zlib.Book{book}, width: 120, height: 30, state: searchStateResults}
	m.details.coverFetcher = func(context.Context, string) (image.Image, error) { fetched = true; return nil, nil }
	if cmd := m.queueSelectedCover(); cmd != nil {
		t.Fatal("no cover work may be queued without a protocol")
	}
	if fetched || len(m.details.coverPending) != 0 || m.details.coverTicking {
		t.Fatal("cover request or clock started without a protocol")
	}
}

// The outer model decides whether covers exist: hidden until a protocol and
// pixel metrics are confirmed, hidden again if the terminal stops reporting one.
func TestCoverAvailabilityFollowsTerminalReplies(t *testing.T) {
	coverGraphicsAvailable.Store(false)
	t.Cleanup(func() { coverGraphicsAvailable.Store(true) })

	book := zlib.Book{ID: "1", Hash: "h", Cover: "https://example.com/a.jpg"}
	inner := searchSelectModel{client: zlib.NewClient(), books: []zlib.Book{book}, state: searchStateResults}
	inner.details.coverFetcher = func(context.Context, string) (image.Image, error) { return nil, nil }
	m := &coverGraphicsModel{inner: inner, output: &coverOutput{target: io.Discard}}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page := func() searchSelectModel { return m.inner.(searchSelectModel) }
	if coverGraphicsAvailable.Load() || len(page().details.coverPending) != 0 {
		t.Fatal("covers must stay hidden until a protocol is confirmed")
	}

	m.Update(uv.PrimaryDeviceAttributesEvent{62, 4})
	m.Update(uv.CellSizeEvent{Width: 8, Height: 16})
	if !coverGraphicsAvailable.Load() || len(page().details.coverPending) != 1 {
		t.Fatalf("covers should start once Sixel is confirmed (available=%v pending=%d)",
			coverGraphicsAvailable.Load(), len(page().details.coverPending))
	}

	m.Update(uv.PrimaryDeviceAttributesEvent{62})
	if coverGraphicsAvailable.Load() {
		t.Fatal("covers must hide again when the protocol disappears")
	}
}

func TestSelectedCoverIsNotStarvedByPrefetch(t *testing.T) {
	d := bookDetailState{coverPending: map[string]bool{}}
	for i := range coverConcurrency {
		d.coverPending[fmt.Sprintf("https://example.com/%d.jpg", i)] = true
	}
	c := zlib.NewClient()
	book := zlib.Book{Cover: "https://example.com/selected.jpg"}
	if d.queueCoverAt(c, book, 120, 30, false) != nil {
		t.Fatal("neighbour prefetch must respect the concurrency cap")
	}
	if d.queueCoverAt(c, book, 120, 30, true) == nil {
		t.Fatal("selected cover must bypass the cap")
	}
}
