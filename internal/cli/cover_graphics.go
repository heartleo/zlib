package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/sixel"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

const coverGraphicsID = 5917697
const coverProbe = "\x1b_Ga=q,i=5917697,s=1,v=1,f=24,t=d;AAAA\x1b\\\x1b[c\x1b[16t\x1b[14t"
const coverDelete = "\x1b_Ga=d,d=I,i=5917697,q=2\x1b\\"
const syncBegin = "\x1b[?2026h"
const syncEnd = "\x1b[?2026l"

type coverCapabilities struct {
	kitty, sixel            bool
	cellWidth, cellHeight   int
	pixelWidth, pixelHeight int
	cols, rows              int
}

func (c coverCapabilities) cellSize() (int, int) {
	if c.cellWidth > 0 && c.cellHeight > 0 {
		return c.cellWidth, c.cellHeight
	}
	if c.cols > 0 && c.rows > 0 {
		return c.pixelWidth / c.cols, c.pixelHeight / c.rows
	}
	return 0, 0
}

func (c coverCapabilities) protocol() string {
	w, h := c.cellSize()
	if w <= 0 || h <= 0 || w > 512 || h > 512 {
		return ""
	}
	if c.kitty {
		return "Kitty"
	}
	if c.sixel {
		return "Sixel"
	}
	return ""
}

type coverSource struct {
	url           string
	img           image.Image
	width, height int
	area          image.Rectangle
}

func (s coverSource) bounds() image.Rectangle {
	if !s.area.Empty() {
		return s.area
	}
	return image.Rect(2, 1, s.width-2, s.height-2)
}

type coverFrame struct {
	data, protocol          string
	x, y, cols, rows        int
	pixelWidth, pixelHeight int
}
type coverEncodedMsg struct {
	generation uint64
	frame      *coverFrame
}

// The outer model owns terminal capabilities across shell page generations.
// Network requests and keyboard navigation remain in the existing shared pages.
type coverGraphicsModel struct {
	inner            tea.Model
	output           *coverOutput
	caps             coverCapabilities
	generation       uint64
	key              string
	frame            *coverFrame
	encoding         bool
	supported        bool
	failedGeneration uint64
}

func runCoverProgram(model tea.Model) (tea.Model, error) {
	coverGraphicsAvailable.Store(false) // until the terminal proves otherwise
	out := &coverOutput{File: os.Stdout, target: os.Stdout}
	defer out.cleanup()
	defer coverGraphicsAvailable.Store(false)
	m := &coverGraphicsModel{inner: model, output: out}
	result, err := tea.NewProgram(m, tea.WithOutput(out)).Run()
	if wrapped, ok := result.(*coverGraphicsModel); ok {
		return wrapped.inner, err
	}
	return model, err
}

func (m *coverGraphicsModel) Init() tea.Cmd { return tea.Batch(m.inner.Init(), tea.Raw(coverProbe)) }

func (m *coverGraphicsModel) source() coverSource {
	model := m.inner
	if shell, ok := model.(shellModel); ok {
		if shell.typing || shell.kind != "list" {
			return coverSource{}
		}
		model = shell.page
	}
	switch page := model.(type) {
	case searchSelectModel:
		if !page.quitting && page.state == searchStateResults && len(page.books) > 0 && !page.confirmDownload && (page.detailOpen || page.hasBookCard()) {
			return cardCoverSource(page.details, page.selectedBook().Cover, page.width, page.height)
		}
	case historySelectModel:
		if !page.quitting && page.state == historyStateResults && !page.confirmDownload {
			book := page.details.book(page.selectedBookRef(), zlib.Book{})
			return cardCoverSource(page.details, book.Cover, page.width, page.height)
		}
	}
	return coverSource{}
}

// cardCoverSource is the loaded cover for the book card, or no source while
// the cover is still missing.
func cardCoverSource(d bookDetailState, url string, width, height int) coverSource {
	if d.covers[url] == "" {
		return coverSource{}
	}
	return coverSource{url: url, img: d.coverImages[url], width: width, height: height, area: bookCardCoverBounds()}
}

func (m *coverGraphicsModel) sourceKey() string {
	s := m.source()
	if s.img == nil || !coverSizeFits(s.width, s.height) || m.caps.protocol() == "" {
		return ""
	}
	w, h := m.caps.cellSize()
	return fmt.Sprintf("%s/%d/%d/%d/%d/%s/%v", s.url, s.width, s.height, w, h, m.caps.protocol(), s.bounds())
}

func (m *coverGraphicsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd, query, wake tea.Cmd
	switch event := msg.(type) {
	case uv.KittyGraphicsEvent:
		if event.Options.ID == coverGraphicsID {
			m.caps.kitty = string(event.Payload) == "OK"
		}
	case uv.PrimaryDeviceAttributesEvent:
		m.caps.sixel = slices.Contains(event, 4)
	case uv.CellSizeEvent:
		m.caps.cellWidth, m.caps.cellHeight = event.Width, event.Height
	case uv.PixelSizeEvent:
		m.caps.pixelWidth, m.caps.pixelHeight = event.Width, event.Height
	case coverEncodedMsg:
		m.encoding = false
		if event.generation == m.generation {
			m.frame = event.frame
			wake = tea.Raw("\x1b7\x1b8")
			if event.frame == nil {
				m.failedGeneration = event.generation
			}
		}
	default:
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.caps.cols, m.caps.rows = size.Width, size.Height
			m.caps.cellWidth, m.caps.cellHeight = 0, 0
			m.caps.pixelWidth, m.caps.pixelHeight = 0, 0
			query = tea.Raw("\x1b[16t\x1b[14t")
		}
		m.inner, cmd = m.inner.Update(msg)
	}
	// Tell the pages whether covers exist at all. They stay hidden until a
	// protocol and pixel metrics are confirmed; on a change, replay the window
	// size so the pages relayout and start their cover requests.
	if supported := m.caps.protocol() != ""; supported != m.supported {
		m.supported = supported
		coverGraphicsAvailable.Store(supported)
		if m.caps.cols > 0 && m.caps.rows > 0 {
			var resize tea.Cmd
			m.inner, resize = m.inner.Update(tea.WindowSizeMsg{Width: m.caps.cols, Height: m.caps.rows})
			cmd = tea.Batch(cmd, resize)
		}
	}
	key := m.sourceKey()
	if key != m.key {
		m.key = key
		m.generation++
		m.frame = nil
		wake = tea.Raw("\x1b7\x1b8")
	}
	if key != "" && m.frame == nil && !m.encoding && m.failedGeneration != m.generation {
		m.encoding = true
		s, c, generation := m.source(), m.caps, m.generation
		encode := func() tea.Msg { return coverEncodedMsg{generation, encodeCoverFrame(s, c)} }
		return m, tea.Batch(cmd, query, wake, encode)
	}
	return m, tea.Batch(cmd, query, wake)
}

func (m *coverGraphicsModel) View() tea.View {
	v := m.inner.View()
	frame := m.frame
	if m.key == "" || m.key != m.sourceKey() {
		frame = nil
	}
	source := m.source()
	// A capable terminal should show the final pixel cover once, rather than
	// briefly drawing a block thumbnail while its native encoding is pending.
	waitingCard := !source.area.Empty() && m.sourceKey() != "" && m.failedGeneration != m.generation
	if frame != nil || waitingCard {
		// Blank only the reserved cover cells; keep adjacent metadata and borders.
		area := m.source().bounds()
		lines := strings.Split(v.Content, "\n")
		for i, line := range lines {
			if i >= area.Min.Y && i < area.Max.Y {
				lines[i] = ansi.Cut(line, 0, area.Min.X) + strings.Repeat(" ", area.Dx()) + ansi.Cut(line, area.Max.X, ansi.StringWidth(line))
			}
		}
		v.Content = strings.Join(lines, "\n")
	}
	m.output.set(frame)
	return v
}

func encodeCoverFrame(s coverSource, c coverCapabilities) *coverFrame {
	cw, ch := c.cellSize()
	if cw <= 0 || ch <= 0 || s.img == nil {
		return nil
	}
	b := s.img.Bounds()
	if b.Empty() {
		return nil
	}
	// Keep source pixels when they fit. Downsample only to fit the viewport;
	// never invent detail by enlarging a small source bitmap.
	w, h := b.Dx(), b.Dy()
	area := s.bounds()
	maxW, maxH := area.Dx()*cw, area.Dy()*ch
	if w > maxW {
		h = max(1, h*maxW/w)
		w = maxW
	}
	if h > maxH {
		w = max(1, w*maxH/h)
		h = maxH
	}
	if w <= 0 || h <= 0 {
		return nil
	}
	img := s.img
	if w != b.Dx() || h != b.Dy() {
		img = resizeCover(s.img, w, h)
	}
	cols, rows := (w+cw-1)/cw, (h+ch-1)/ch
	f := &coverFrame{protocol: c.protocol(), x: area.Min.X + (area.Dx()-cols)/2, y: area.Min.Y + (area.Dy()-rows)/2, cols: cols, rows: rows}
	f.pixelWidth, f.pixelHeight = w, h
	var data bytes.Buffer
	if f.protocol == "Kitty" {
		if err := png.Encode(&data, img); err != nil {
			return nil
		}
		payload := base64.StdEncoding.EncodeToString(data.Bytes())
		var out strings.Builder
		for offset := 0; offset < len(payload); offset += 4096 {
			end := min(offset+4096, len(payload))
			more := 0
			if end < len(payload) {
				more = 1
			}
			opts := fmt.Sprintf("m=%d", more)
			if offset == 0 {
				opts = fmt.Sprintf("a=T,f=100,t=d,i=%d,q=2,C=1,m=%d", coverGraphicsID, more)
			}
			out.WriteString(ansi.KittyGraphics([]byte(payload[offset:end]), opts))
		}
		f.data = out.String()
	} else if f.protocol == "Sixel" {
		if err := (&sixel.Encoder{}).Encode(&data, img); err != nil {
			return nil
		}
		f.data = ansi.SixelGraphics(0, 1, 0, data.Bytes())
	} else {
		return nil
	}
	return f
}

// Graphics bypass the cell renderer but share its output writer. Each picture
// follows the text frame in the same write; no independent stdout writes race
// the renderer. Repaint after every write since Sixel has no retained placement.
type coverOutput struct {
	*os.File
	target      io.Writer
	mu          sync.Mutex
	want, shown *coverFrame
}

func (w *coverOutput) set(f *coverFrame) { w.mu.Lock(); w.want = f; w.mu.Unlock() }

func eraseCover(f *coverFrame) string {
	if f == nil {
		return ""
	}
	if f.protocol == "Kitty" {
		return coverDelete
	}
	var out strings.Builder
	out.WriteString("\x1b7")
	for y := range f.rows {
		fmt.Fprintf(&out, "\x1b[%d;%dH%s", f.y+y+1, f.x+1, strings.Repeat(" ", f.cols))
	}
	out.WriteString("\x1b8")
	return out.String()
}

func (w *coverOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.want == nil && w.shown == nil {
		return w.target.Write(p)
	}
	want := w.want
	if bytes.Contains(p, []byte("\x1b[?1049l")) {
		want = nil
		w.want = nil
	}
	var out bytes.Buffer
	out.WriteString(syncBegin)
	if w.shown != want {
		out.WriteString(eraseCover(w.shown))
	}
	// Append graphics before an existing synchronized-update terminator.
	end := bytes.HasSuffix(p, []byte(syncEnd))
	if end {
		p = p[:len(p)-len(syncEnd)]
	}
	out.Write(p)
	// Kitty keeps a placed image until it is deleted, so resending the PNG on
	// every text frame (spinner ticks, cursor moves) only burns bandwidth.
	// Sixel has no retained placement and a screen clear drops Kitty images too.
	retained := want != nil && want == w.shown && want.protocol == "Kitty" &&
		!bytes.Contains(p, []byte("\x1b[2J")) && !bytes.Contains(p, []byte("\x1b[3J"))
	if want != nil && !retained {
		fmt.Fprintf(&out, "\x1b7\x1b[%d;%dH%s\x1b8", want.y+1, want.x+1, want.data)
	}
	out.WriteString(syncEnd)
	n, err := w.target.Write(out.Bytes())
	if err != nil {
		return 0, err
	}
	if n != out.Len() {
		return 0, io.ErrShortWrite
	}
	w.shown = want
	length := len(p)
	if end {
		length += len(syncEnd)
	}
	return length, nil
}

func (w *coverOutput) cleanup() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = io.WriteString(w.target, eraseCover(w.shown))
	w.shown = nil
	w.want = nil
}
