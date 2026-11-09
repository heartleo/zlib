package cli

import (
	"context"
	"image"
	"image/color"
	"maps"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

const coverWidth = 18
const coverRows = 9
const coverConcurrency = 8
const coverPrefetchRadius = 4

// Lists and Book details both put the card after two heading rows. Its border
// and one-cell padding place the cover at column two, row three (zero based).
func bookCardCoverBounds() image.Rectangle { return image.Rect(2, 3, 2+coverWidth, 3+coverRows) }

func (m *searchSelectModel) queueSelectedCover() tea.Cmd {
	if len(m.books) == 0 || m.cursor < 0 || m.cursor >= len(m.books) {
		return nil
	}
	prefetch := m.details.prefetchCovers(m.client, len(m.books), m.cursor, m.width, m.height, func(i int) zlib.Book {
		book := m.books[i]
		return m.details.book(bookCommandID(book), book)
	})
	ref, url := m.selectedCoverKey()
	return tea.Batch(prefetch, m.details.startCoverTick(m.client, m.width, m.height, ref, url))
}

func (m *historySelectModel) queueSelectedCover() tea.Cmd {
	ref := m.selectedBookRef()
	if ref == "" {
		return nil
	}
	// History rows carry no cover URL until their details load, so warm the
	// neighbours' details first; each result re-enters here and queues covers.
	var details []tea.Cmd
	for _, offset := range []int{1, -1, 2, -2} {
		i := m.cursor + offset
		if i >= 0 && i < len(m.items) && len(m.details.pending) < 3 {
			details = append(details, m.details.fetch(m.client, historyCommandID(m.items[i])))
		}
	}
	covers := m.details.prefetchCovers(m.client, len(m.items), m.cursor, m.width, m.height, func(i int) zlib.Book {
		return m.details.book(historyCommandID(m.items[i]), zlib.Book{})
	})
	_, url := m.selectedCoverKey()
	return tea.Batch(append(details, covers, m.details.startCoverTick(m.client, m.width, m.height, ref, url))...)
}

// Start the selected cover first, then a small window in navigation order.
// Results call this again, so at most eight requests run even while the user scrolls.
func (d *bookDetailState) prefetchCovers(client *zlib.Client, count, cursor, width, height int, bookAt func(int) zlib.Book) tea.Cmd {
	if client == nil || !coverFits(width, height) || cursor < 0 || cursor >= count {
		return nil
	}
	d.coverWanted = bookAt(cursor).Cover
	d.touchCover(d.coverWanted)
	window := make(map[string]bool)
	for i := max(0, cursor-coverPrefetchRadius); i < min(count, cursor+coverPrefetchRadius+1); i++ {
		window[bookAt(i).Cover] = true
	}
	if d.coverPrefetched == nil {
		d.coverPrefetched = make(map[string]bool)
	}
	maps.DeleteFunc(d.coverPrefetched, func(url string, _ bool) bool { return !window[url] })
	var commands []tea.Cmd
	for _, offset := range []int{0, 1, -1, 2, -2, 3, -3, 4, -4} {
		i := cursor + offset
		if i >= 0 && i < count {
			book := bookAt(i)
			if offset != 0 && d.coverPrefetched[book.Cover] {
				continue
			}
			// The selected cover is never starved by neighbour prefetches.
			if cmd := d.queueCoverAt(client, book, width, height, offset == 0); cmd != nil {
				if offset != 0 {
					d.coverPrefetched[book.Cover] = true
				}
				commands = append(commands, cmd)
			}
		}
	}
	return tea.Batch(commands...)
}

const coverTickInterval = 80 * time.Millisecond

type coverTickMsg struct{}

// coverLoading reports whether the slot for ref/url is still waiting on the
// network: either the cover itself, or the details that name its URL.
func (d bookDetailState) coverLoading(ref, url string) bool {
	if url == "" {
		_, loaded := d.cache[ref]
		return ref != "" && !loaded
	}
	_, done := d.covers[url]
	return !done
}

// startCoverTick runs one spinner clock while the selected slot is loading, and
// stops on its own once there is nothing left to animate.
func (d *bookDetailState) startCoverTick(client *zlib.Client, width, height int, ref, url string) tea.Cmd {
	if client == nil || d.coverTicking || searchReducedMotion || !coverFits(width, height) ||
		!d.coverLoading(ref, url) {
		return nil
	}
	d.coverTicking = true
	return tea.Tick(coverTickInterval, func(time.Time) tea.Msg { return coverTickMsg{} })
}

func (d *bookDetailState) advanceCoverTick(client *zlib.Client, width, height int, ref, url string) tea.Cmd {
	d.coverFrame++
	d.coverTicking = false
	return d.startCoverTick(client, width, height, ref, url)
}

func (m *searchSelectModel) selectedCoverKey() (string, string) {
	if len(m.books) == 0 || m.cursor < 0 || m.cursor >= len(m.books) {
		return "", ""
	}
	return m.selectedBookRef(), m.selectedBook().Cover
}

func (m *historySelectModel) selectedCoverKey() (string, string) {
	ref := m.selectedBookRef()
	return ref, m.details.book(ref, zlib.Book{}).Cover
}

// coverView returns the reserved cover slot. A loaded cover is a blank block the
// pixel image is drawn over. A pending fetch shows only a centered spinner
// glyph, with no frame or text; a missing or failed cover gets a muted,
// bordered label.
func (d bookDetailState) coverView(ref, url string) string {
	if view := d.covers[url]; view != "" {
		return view
	}
	slot := lipgloss.NewStyle().Foreground(currentTheme.Muted).Width(coverWidth).Height(coverRows).
		Align(lipgloss.Center, lipgloss.Center)
	if d.coverLoading(ref, url) {
		return slot.Render(strings.TrimSpace(spinner.Dot.Frames[d.coverFrame%len(spinner.Dot.Frames)]))
	}
	label := "no cover"
	if url != "" {
		label = "cover unavailable"
	}
	return slot.Border(lipgloss.RoundedBorder()).BorderForeground(currentTheme.Muted).Render(label)
}

func (d *bookDetailState) touchCover(url string) {
	if url == "" {
		return
	}
	if _, cached := d.covers[url]; !cached {
		return
	}
	d.coverOrder = slices.DeleteFunc(d.coverOrder, func(value string) bool { return value == url })
	d.coverOrder = append(d.coverOrder, url)
}

type bookCoverResultMsg struct {
	url, view string
	image     image.Image
}

// coverGraphicsAvailable is true while the terminal has answered a Kitty or
// Sixel query with usable pixel metrics. The outer coverGraphicsModel owns it;
// without it covers are hidden entirely: no column, no placeholder, no requests.
var coverGraphicsAvailable atomic.Bool

func coverSizeFits(width, height int) bool { return width >= 100 && height >= 20 }

func coverFits(width, height int) bool {
	return coverGraphicsAvailable.Load() && coverSizeFits(width, height)
}

// resizeCover uses bilinear interpolation only when fitting a source bitmap
// to a smaller pixel viewport. Native-size previews keep the original pixels.
func resizeCover(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		sy := max(0, (float64(y)+0.5)*float64(b.Dy())/float64(h)-0.5)
		y0 := int(sy)
		fy := sy - math.Floor(sy)
		wy := [2]float64{1 - fy, fy}
		for x := range w {
			sx := max(0, (float64(x)+0.5)*float64(b.Dx())/float64(w)-0.5)
			x0 := int(sx)
			fx := sx - math.Floor(sx)
			wx := [2]float64{1 - fx, fx}
			var channels [4]float64
			for dy := range 2 {
				for dx := range 2 {
					weight := wx[dx] * wy[dy]
					r, g, blue, a := img.At(b.Min.X+min(x0+dx, b.Dx()-1), b.Min.Y+min(y0+dy, b.Dy()-1)).RGBA()
					for i, value := range [4]uint32{r, g, blue, a} {
						channels[i] += float64(value) * weight
					}
				}
			}
			out.Set(x, y, color.RGBA64{R: uint16(channels[0] + 0.5), G: uint16(channels[1] + 0.5), B: uint16(channels[2] + 0.5), A: uint16(channels[3] + 0.5)})
		}
	}
	return out
}

func (d *bookDetailState) queueCoverAt(client *zlib.Client, book zlib.Book, width, height int, urgent bool) tea.Cmd {
	if client == nil || !coverFits(width, height) || book.Cover == "" {
		return nil
	}
	if d.covers == nil {
		d.covers = make(map[string]string)
	}
	if d.coverPending == nil {
		d.coverPending = make(map[string]bool)
	}
	if _, ok := d.covers[book.Cover]; ok || d.coverPending[book.Cover] || (!urgent && len(d.coverPending) >= coverConcurrency) {
		return nil
	}
	d.coverPending[book.Cover] = true
	fetch := client.FetchCover
	if d.coverFetcher != nil {
		fetch = d.coverFetcher
	}
	return func() tea.Msg {
		img, err := fetch(context.Background(), book.Cover)
		view := ""
		if err == nil {
			view = renderBookCover(img)
		} else {
			img = nil
		}
		return bookCoverResultMsg{url: book.Cover, view: view, image: img}
	}
}

func (d *bookDetailState) storeCover(msg bookCoverResultMsg) {
	if d.covers == nil {
		d.covers = make(map[string]string)
	}
	delete(d.coverPending, msg.url)
	if d.coverImages == nil {
		d.coverImages = make(map[string]image.Image)
	}
	d.covers[msg.url] = msg.view
	d.coverImages[msg.url] = msg.image
	d.touchCover(msg.url)
	for {
		pixels := 0
		for _, img := range d.coverImages {
			if img != nil {
				pixels += img.Bounds().Dx() * img.Bounds().Dy()
			}
		}
		if len(d.covers) <= 12 && pixels <= 8_000_000 {
			break
		}
		victim := ""
		for _, url := range d.coverOrder {
			if url != d.coverWanted {
				if _, ok := d.covers[url]; ok {
					victim = url
					break
				}
			}
		}
		if victim == "" {
			break
		}
		delete(d.covers, victim)
		delete(d.coverImages, victim)
		d.coverOrder = slices.DeleteFunc(d.coverOrder, func(url string) bool { return url == victim })
	}
}

// renderBookCover returns the blank block reserved for the pixel image. Covers
// are never drawn with text characters: a terminal without an image protocol
// gets no cover at all, so the cell view carries only "loaded" and the footprint.
func renderBookCover(img image.Image) string {
	if img == nil || img.Bounds().Empty() {
		return ""
	}
	row := strings.Repeat(" ", coverWidth)
	return strings.Join(slices.Repeat([]string{row}, coverRows), "\n")
}
