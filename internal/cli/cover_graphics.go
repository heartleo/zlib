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
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/sixel"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

const coverGraphicsID = 5917697
const coverProbe = "\x1b_Ga=q,i=5917697,s=1,v=1,f=24,t=d;AAAA\x1b\\\x1b[c\x1b[16t\x1b[14t" + iTermCellSizeQuery
const coverDelete = "\x1b_Ga=d,d=I,i=5917697,q=2\x1b\\"
const syncBegin = "\x1b[?2026h"
const syncEnd = "\x1b[?2026l"

type coverCapabilities struct {
	kitty, sixel            bool
	cellWidth, cellHeight   int
	pixelWidth, pixelHeight int
	cols, rows              int
	// scale converts reported sizes to physical pixels. iTerm2 on a HiDPI Mac
	// reports cell and window sizes in points but draws images in pixels; it
	// states the factor through OSC 1337 ReportCellSize. Zero means 1.
	scale float64
}

// iTermCellSizeQuery asks iTerm2 for its pixel scale. Other terminals ignore it.
const iTermCellSizeQuery = "\x1b]1337;ReportCellSize\a"

// parseITermScale reads the scale from the reply
// "ESC ] 1337;ReportCellSize=<h>;<w>;<scale> ST". The framing may or may not be
// present, depending on how the input parser hands the sequence over.
func parseITermScale(osc string) (float64, bool) {
	osc = strings.TrimPrefix(osc, "\x1b]")
	osc = strings.TrimSuffix(strings.TrimSuffix(osc, "\x1b\\"), "\a")
	rest, ok := strings.CutPrefix(osc, "1337;ReportCellSize=")
	if !ok {
		return 0, false
	}
	parts := strings.Split(rest, ";")
	if len(parts) < 3 {
		return 0, false
	}
	scale, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil || scale < 1 || scale > 8 {
		return 0, false
	}
	return scale, true
}

func (c coverCapabilities) cellSize() (int, int) {
	w, h := 0, 0
	if c.cellWidth > 0 && c.cellHeight > 0 {
		w, h = c.cellWidth, c.cellHeight
	} else if c.cols > 0 && c.rows > 0 {
		w, h = c.pixelWidth/c.cols, c.pixelHeight/c.rows
	}
	if c.scale > 1 {
		w, h = int(float64(w)*c.scale+0.5), int(float64(h)*c.scale+0.5)
	}
	return w, h
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
	id                      int // the Kitty image id
	// more are further images drawn with this one, such as the second cover
	// of a comparison. They are erased and redrawn together.
	more []*coverFrame
}

// all is the frame and the ones drawn with it.
func (f *coverFrame) all() []*coverFrame {
	if f == nil {
		return nil
	}
	return append([]*coverFrame{f}, f.more...)
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

// coverSlotter is a page that shows covers of its own, such as the metadata
// page: it says which images go where, or none while something covers them.
type coverSlotter interface {
	coverSlots() []coverSource
}

// sources are the covers to draw now: the book card's one, or those a page
// such as the metadata page lays out itself.
func (m *coverGraphicsModel) sources() []coverSource {
	model := m.inner
	if shell, ok := model.(shellModel); ok {
		if shell.typing {
			return nil
		}
		if page, ok := shell.page.(coverSlotter); ok && shell.kind == "meta" {
			return page.coverSlots()
		}
		if shell.kind != "list" {
			return nil
		}
		model = shell.page
	}
	if s := m.cardSource(model); s.img != nil {
		return []coverSource{s}
	}
	return nil
}

func (m *coverGraphicsModel) source() coverSource {
	if s := m.sources(); len(s) > 0 {
		return s[0]
	}
	return coverSource{}
}

func (m *coverGraphicsModel) cardSource(model tea.Model) coverSource {
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
	srcs := m.sources()
	if len(srcs) == 0 || m.caps.protocol() == "" {
		return ""
	}
	w, h := m.caps.cellSize()
	var key strings.Builder
	for _, s := range srcs {
		if s.img == nil || !coverSizeFits(s.width, s.height) {
			return ""
		}
		fmt.Fprintf(&key, "%s/%d/%d/%d/%d/%s/%v|", s.url, s.width, s.height, w, h, m.caps.protocol(), s.bounds())
	}
	return key.String()
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
	case uv.UnknownOscEvent:
		if scale, ok := parseITermScale(string(event)); ok {
			m.caps.scale = scale
		}
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
			query = tea.Raw("\x1b[16t\x1b[14t" + iTermCellSizeQuery)
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
		srcs, c, generation := m.sources(), m.caps, m.generation
		encode := func() tea.Msg { return coverEncodedMsg{generation, encodeCoverFrames(srcs, c)} }
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
	srcs := m.sources()
	// A capable terminal should show the final pixel cover once, rather than
	// briefly drawing a block thumbnail while its native encoding is pending.
	waitingCard := len(srcs) > 0 && !srcs[0].area.Empty() && m.sourceKey() != "" && m.failedGeneration != m.generation
	if frame != nil || waitingCard {
		// Blank only the reserved cover cells; keep adjacent metadata and borders.
		lines := strings.Split(v.Content, "\n")
		for _, s := range srcs {
			area := s.bounds()
			for i, line := range lines {
				if i >= area.Min.Y && i < area.Max.Y {
					lines[i] = ansi.Cut(line, 0, area.Min.X) + strings.Repeat(" ", area.Dx()) + ansi.Cut(line, area.Max.X, ansi.StringWidth(line))
				}
			}
		}
		v.Content = strings.Join(lines, "\n")
	}
	m.output.set(frame)
	return v
}

// encodeCoverFrames encodes every source, each with its own Kitty image id. It
// returns nil when any of them cannot be drawn, so a pair is never half shown.
func encodeCoverFrames(srcs []coverSource, c coverCapabilities) *coverFrame {
	var first *coverFrame
	for i, s := range srcs {
		f := encodeCoverFrameID(s, c, coverGraphicsID+i)
		if f == nil {
			return nil
		}
		if first == nil {
			first = f
		} else {
			first.more = append(first.more, f)
		}
	}
	return first
}

func encodeCoverFrame(s coverSource, c coverCapabilities) *coverFrame {
	return encodeCoverFrameID(s, c, coverGraphicsID)
}

func encodeCoverFrameID(s coverSource, c coverCapabilities, id int) *coverFrame {
	cw, ch := c.cellSize()
	if cw <= 0 || ch <= 0 || s.img == nil {
		return nil
	}
	b := s.img.Bounds()
	if b.Empty() {
		return nil
	}
	// Scale to fit the reserved cells, up or down, keeping the aspect ratio.
	// Terminals report cell size in physical pixels, so on HiDPI screens such as
	// macOS Retina a small source shown 1:1 would fill only a fraction of the slot.
	w, h := b.Dx(), b.Dy()
	area := s.bounds()
	maxW, maxH := area.Dx()*cw, area.Dy()*ch
	if w*maxH >= h*maxW {
		h = max(1, h*maxW/w)
		w = maxW
	} else {
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
	f := &coverFrame{protocol: c.protocol(), x: area.Min.X + (area.Dx()-cols)/2, y: area.Min.Y + (area.Dy()-rows)/2, cols: cols, rows: rows, id: id}
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
				opts = fmt.Sprintf("a=T,f=100,t=d,i=%d,q=2,C=1,m=%d", id, more)
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
	var out strings.Builder
	for _, p := range f.all() {
		if p.protocol == "Kitty" {
			if p.id == 0 || p.id == coverGraphicsID {
				out.WriteString(coverDelete)
			} else {
				fmt.Fprintf(&out, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", p.id)
			}
			continue
		}
		out.WriteString("\x1b7")
		for y := range p.rows {
			fmt.Fprintf(&out, "\x1b[%d;%dH%s", p.y+y+1, p.x+1, strings.Repeat(" ", p.cols))
		}
		out.WriteString("\x1b8")
	}
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
		for _, p := range want.all() {
			fmt.Fprintf(&out, "\x1b7\x1b[%d;%dH%s\x1b8", p.y+1, p.x+1, p.data)
		}
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
