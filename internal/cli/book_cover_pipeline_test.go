package cli

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// pumpCmds drives a model like the bubbletea runtime: commands run on their own
// goroutines, their messages return through Update, and the loop ends once no
// command is outstanding. It fails the test if the model never settles.
func pumpCmds(t *testing.T, m tea.Model, cmd tea.Cmd, limit time.Duration, observe func(tea.Model)) tea.Model {
	t.Helper()
	msgs := make(chan tea.Msg, 256)
	var outstanding atomic.Int64
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		outstanding.Add(1)
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
			} else if msg != nil {
				msgs <- msg
			}
			outstanding.Add(-1)
		}()
	}
	run(cmd)
	deadline := time.After(limit)
	for {
		select {
		case msg := <-msgs:
			var next tea.Cmd
			m, next = m.Update(msg)
			run(next)
			if observe != nil {
				observe(m)
			}
		case <-deadline:
			t.Fatalf("model did not settle within %v (%d commands outstanding)", limit, outstanding.Load())
		case <-time.After(20 * time.Millisecond):
			if outstanding.Load() == 0 && len(msgs) == 0 {
				return m
			}
		}
	}
}

func fixtureCover() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 60, 90))
	for y := range 90 {
		for x := range 60 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 2), 120, 255})
		}
	}
	return img
}

// A slow network must show a moving spinner, deliver the selected cover without
// waiting behind neighbour prefetches, fade it in, and then go quiet.
func TestCoverLoadingPipelineAnimatesAndSettles(t *testing.T) {
	const delay = 300 * time.Millisecond
	var mu sync.Mutex
	started := map[string]time.Time{}
	var inFlight, maxInFlight int
	fetch := func(_ context.Context, url string) (image.Image, error) {
		mu.Lock()
		started[url] = time.Now()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		time.Sleep(delay)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return fixtureCover(), nil
	}
	books := make([]zlib.Book, 14)
	for i := range books {
		books[i] = zlib.Book{ID: fmt.Sprint(i), Hash: "h", Cover: fmt.Sprintf("https://example.com/cover-%d.png", i)}
	}
	m := searchSelectModel{client: zlib.NewClient(), books: books, cursor: 6, width: 120, height: 30, state: searchStateResults}
	m.details.coverFetcher = fetch
	selected := books[m.cursor]
	ref := bookCommandID(selected)

	begin := time.Now()
	var arrived time.Time
	frames := map[string]bool{}
	observe := func(model tea.Model) {
		d := model.(searchSelectModel).details
		if d.covers[selected.Cover] != "" {
			if arrived.IsZero() {
				arrived = time.Now()
			}
			return
		}
		frames[ansi.Strip(d.coverView(ref, selected.Cover))] = true
	}
	cmd := m.queueSelectedCover()
	observe(m) // the first frame, before any message arrives
	final := pumpCmds(t, m, cmd, 5*time.Second, observe).(searchSelectModel)

	if arrived.IsZero() || arrived.Sub(begin) > delay+250*time.Millisecond {
		t.Fatalf("selected cover took %v, want about one fetch (%v)", arrived.Sub(begin), delay)
	}
	if len(frames) < 2 {
		t.Fatalf("loading placeholder never animated: %d distinct frames", len(frames))
	}
	if maxInFlight > coverConcurrency+1 {
		t.Fatalf("%d concurrent fetches exceeds cap %d (+1 urgent)", maxInFlight, coverConcurrency)
	}
	if final.details.coverTicking {
		t.Fatal("animation clock still running after everything loaded")
	}
	card := ansi.Strip(selectedBookCardWithCover(final.selectedBook(), 120, 30, final.details.coverView(ref, selected.Cover)))
	if got := strings.Count(card, "\n") + 1; got != selectedBookCardRows(120, 30) {
		t.Fatalf("card has %d rows, want %d", got, selectedBookCardRows(120, 30))
	}
	for i := 6 - coverPrefetchRadius; i <= 6+coverPrefetchRadius; i++ {
		if final.details.covers[books[i].Cover] == "" {
			t.Fatalf("neighbour %d was not prefetched", i)
		}
	}
}
