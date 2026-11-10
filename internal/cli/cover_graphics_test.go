package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/ansi/sixel"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

type coverTerminalCapture struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	events chan string
}

type gatedCoverImage struct {
	image.Image
	ready   <-chan struct{}
	started chan struct{}
	once    *sync.Once
}

func (img gatedCoverImage) At(x, y int) color.Color {
	img.once.Do(func() { close(img.started) })
	<-img.ready
	return img.Image.At(x, y)
}

func TestCoverEncodedFrameWakesUnchangedView(t *testing.T) {
	page := pixelCoverFixture()
	var buf bytes.Buffer
	m := &coverGraphicsModel{inner: page, output: &coverOutput{target: &buf}, caps: coverCapabilities{kitty: true, cellWidth: 8, cellHeight: 16}, generation: 1, encoding: true}
	m.key = m.sourceKey()
	before := m.View().Content
	frame := encodeCoverFrame(m.source(), m.caps)
	_, cmd := m.Update(coverEncodedMsg{generation: m.generation, frame: frame})
	after := m.View().Content
	if before != after {
		t.Fatal("fixture must exercise identical placeholder and ready text frames")
	}
	var wakes func(tea.Cmd) bool
	wakes = func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		switch msg := cmd().(type) {
		case tea.RawMsg:
			return fmt.Sprint(msg.Msg) != ""
		case tea.BatchMsg:
			for _, c := range msg {
				if wakes(c) {
					return true
				}
			}
		}
		return false
	}
	if !wakes(cmd) {
		t.Fatal("ready cover has identical text frame and never wakes output writer")
	}
}

func (c *coverTerminalCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	for _, token := range []string{"a=q,", "a=T,", "\x1bP0;1q", coverDelete, "\x1b]777;cover-test-barrier\x1b\\"} {
		if bytes.Contains(p, []byte(token)) {
			select {
			case c.events <- token:
			default:
			}
		}
	}
	return len(p), nil
}

func TestCoverGraphicsTerminalRoundTrip(t *testing.T) {
	for _, protocol := range []string{"Kitty", "Sixel"} {
		t.Run(protocol, func(t *testing.T) {
			fixture := pixelCoverFixture()
			page := newBookFeedSelectModel("Recommended", func() ([]zlib.Book, error) { return fixture.books, nil })
			page.details = fixture.details
			ready := make(chan struct{})
			started := make(chan struct{})
			var release sync.Once
			defer release.Do(func() { close(ready) })
			page.details.coverImages["cover"] = gatedCoverImage{Image: fixture.details.coverImages["cover"], ready: ready, started: started, once: new(sync.Once)}
			page.reducedMotion = true
			capture := &coverTerminalCapture{events: make(chan string, 100)}
			out := &coverOutput{target: capture}
			r, w := io.Pipe()
			defer r.Close()
			defer w.Close()
			model := &coverGraphicsModel{inner: page, output: out}
			p := tea.NewProgram(model, tea.WithInput(r), tea.WithOutput(out), tea.WithWindowSize(120, 24), tea.WithContext(t.Context()), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() { _, err := p.Run(); done <- err }()
			defer func() { release.Do(func() { close(ready) }); p.Kill() }()
			wait := func(token string) {
				t.Helper()
				timer := time.NewTimer(8 * time.Second)
				defer timer.Stop()
				for {
					select {
					case got := <-capture.events:
						if got == token {
							return
						}
					case err := <-done:
						t.Fatalf("program ended while waiting for %q: %v", token, err)
					case <-timer.C:
						t.Fatalf("no terminal output %q", token)
					}
				}
			}
			wait("a=q,")
			reply := "\x1b[6;16;8t"
			token := "a=T,"
			if protocol == "Kitty" {
				reply += "\x1b_Gi=5917697;OK\x1b\\"
			} else {
				reply += "\x1b[?62;4c"
				token = "\x1bP0;1q"
			}
			if _, err := io.WriteString(w, reply); err != nil {
				t.Fatal(err)
			}
			// Finish a blank placeholder frame before allowing the encoder to
			// finish. No subsequent text change may be required to show pixels.
			select {
			case <-started:
			case <-time.After(8 * time.Second):
				t.Fatal("encoder did not start")
			}
			const barrier = "\x1b]777;cover-test-barrier\x1b\\"
			for range 2 {
				p.Send(tea.RawMsg{Msg: barrier})
				wait(barrier)
			}
			release.Do(func() { close(ready) })
			wait(token)
			io.WriteString(w, "q")
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("terminal program did not exit")
			}
			out.cleanup()
			if out.shown != nil {
				t.Fatal("terminal image remained after exit")
			}
		})
	}
}

func pixelCoverFixture() searchSelectModel {
	book := zlib.Book{ID: "1", Hash: "fixture", Cover: "cover"}
	img := image.NewRGBA(image.Rect(0, 0, 180, 270))
	for y := range 270 {
		for x := range 180 {
			img.SetRGBA(x, y, color.RGBA{R: byte(x), G: byte(y), A: 255})
		}
	}
	m := searchSelectModel{width: 120, height: 24, state: searchStateResults, books: []zlib.Book{book}}
	m.details.storeCover(bookCoverResultMsg{url: "cover", view: renderBookCover(img), image: img})
	return m
}

func TestCoverGraphicsDetectionAndFallback(t *testing.T) {
	var buf bytes.Buffer
	m := &coverGraphicsModel{inner: pixelCoverFixture(), output: &coverOutput{target: &buf}}
	if strings.Contains(m.View().Content, "▀") {
		t.Fatal("covers must never be drawn with text characters")
	}
	m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: 42}, Payload: []byte("OK")})
	if m.caps.kitty {
		t.Fatal("unrelated reply enabled graphics")
	}
	m.Update(uv.PrimaryDeviceAttributesEvent{62, 4})
	if m.caps.protocol() != "" {
		t.Fatal("pixels without cell metrics")
	}
	m.Update(uv.CellSizeEvent{Width: 8, Height: 16})
	if m.caps.protocol() != "Sixel" {
		t.Fatal("Sixel not detected")
	}
	m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: coverGraphicsID}, Payload: []byte("OK")})
	if m.caps.protocol() != "Kitty" {
		t.Fatal("Kitty not preferred")
	}
	// An in-flight Sixel encoder must not replace a newer Kitty request.
	m.Update(coverEncodedMsg{generation: m.generation - 1, frame: &coverFrame{protocol: "Sixel"}})
	if m.frame != nil {
		t.Fatal("stale encoder result accepted")
	}
	frame := encodeCoverFrame(m.source(), m.caps)
	m.Update(coverEncodedMsg{generation: m.generation, frame: frame})
	v := m.View()
	if strings.Contains(v.Content, "▀") {
		t.Fatal("pixel preview did not replace fallback")
	}
	if _, err := m.output.Write([]byte("TEXT")); err != nil {
		t.Fatal(err)
	}
	if strings.Index(buf.String(), "TEXT") > strings.Index(buf.String(), "a=T") {
		t.Fatal("graphics preceded text")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.View()
	buf.Reset()
	m.output.Write([]byte("LIST"))
	if !strings.Contains(buf.String(), coverDelete) || strings.Contains(buf.String(), "a=T") {
		t.Fatal("preview image survived exit")
	}
	for _, size := range [][2]int{{99, 24}, {120, 19}} {
		page := pixelCoverFixture()
		m.inner = page
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.sourceKey() != "" || m.frame != nil {
			t.Fatal("narrow resize retained pixel preview")
		}
	}
}

func TestCoverGraphicsEncodePreservesPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 336, 336))
	img.SetRGBA(25, 30, color.RGBA{R: 80, G: 120, B: 160, A: 255})
	s := coverSource{url: "cover", img: img, width: 120, height: 24}
	for _, protocol := range []string{"Kitty", "Sixel"} {
		c := coverCapabilities{kitty: protocol == "Kitty", sixel: protocol == "Sixel", cellWidth: 8, cellHeight: 16}
		f := encodeCoverFrame(s, c)
		if f == nil || f.x < 0 || f.y < 1 || f.x+f.cols > 120 || f.y+f.rows > 23 {
			t.Fatal("image outside viewport")
		}
		var decoded image.Image
		if protocol == "Kitty" {
			var payload strings.Builder
			for chunk := range strings.SplitSeq(f.data, "\x1b_G") {
				if chunk == "" {
					continue
				}
				head, body, ok := strings.Cut(strings.TrimSuffix(chunk, "\x1b\\"), ";")
				if !ok || len(body) > 4096 || len(body)%4 != 0 {
					t.Fatal("invalid Kitty chunk")
				}
				if !strings.Contains(head, "m=") {
					t.Fatal("missing continuation flag")
				}
				payload.WriteString(body)
			}
			raw, err := base64.StdEncoding.DecodeString(payload.String())
			if err != nil {
				t.Fatal(err)
			}
			decoded, err = png.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.At(25, 30) != img.At(25, 30) { // Different concrete color types can represent identical values.
				r, g, b, a := decoded.At(25, 30).RGBA()
				rr, gg, bb, aa := img.At(25, 30).RGBA()
				if r != rr || g != gg || b != bb || a != aa {
					t.Fatal("Kitty changed native source pixels")
				}
			}
		} else {
			payload := strings.TrimSuffix(strings.TrimPrefix(f.data, "\x1bP0;1q"), "\x1b\\")
			var err error
			decoded, err = (&sixel.Decoder{}).Decode(strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
		}
		if decoded.Bounds().Dx() != 336 || decoded.Bounds().Dy() != 336 {
			t.Fatal("source resolution lost")
		}
		// A smaller pixel viewport must produce a fitted, non-clipped bitmap.
		c.cellWidth, c.cellHeight = 1, 2
		f = encodeCoverFrame(s, c)
		if f == nil || f.rows > 21 || f.cols > 116 {
			t.Fatal("downsample overflow")
		}
	}
}

func TestCoverDownsampleInterpolation(t *testing.T) {
	img := image.NewRGBA(image.Rect(10, 20, 12, 22))
	img.SetRGBA(10, 20, color.RGBA{A: 255})
	img.SetRGBA(11, 20, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetRGBA(10, 21, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetRGBA(11, 21, color.RGBA{A: 255})
	r, g, b, a := resizeCover(img, 1, 1).At(0, 0).RGBA()
	if r < 32000 || r > 33500 || g != r || b != r || a != 65535 {
		t.Fatal("downsampling discarded blended source detail")
	}
}

func TestCoverOutputOrderingCleanupAndShutdown(t *testing.T) {
	for _, protocol := range []string{"Kitty", "Sixel"} {
		var buf bytes.Buffer
		w := &coverOutput{target: &buf}
		f := &coverFrame{protocol: protocol, data: "PICTURE", x: 3, y: 2, cols: 10, rows: 5}
		w.set(f)
		text := []byte("TEXT" + syncEnd)
		if n, err := w.Write(text); n != len(text) || err != nil {
			t.Fatal("writer length contract")
		}
		if !strings.Contains(buf.String(), "TEXT\x1b7") || !strings.HasSuffix(buf.String(), syncEnd) {
			t.Fatal("picture not in synchronized frame")
		}
		buf.Reset()
		w.set(nil)
		w.Write([]byte("NEW PAGE"))
		erase := eraseCover(f)
		if !strings.Contains(buf.String(), erase) || strings.Index(buf.String(), erase) > strings.Index(buf.String(), "NEW PAGE") {
			t.Fatal("old image not erased before new page")
		}
		w.set(f)
		w.Write([]byte("TEXT"))
		buf.Reset()
		w.Write([]byte("\x1b[?1049l"))
		if strings.Contains(buf.String(), "PICTURE") {
			t.Fatal("image redrawn after alternate screen exit")
		}
		buf.Reset()
		w.cleanup()
		if buf.Len() != 0 {
			t.Fatal("cleanup drew in normal screen")
		}
	}
}

func TestCoverGraphicsShellOverlayAndPixelMetrics(t *testing.T) {
	page := pixelCoverFixture()
	var buf bytes.Buffer
	m := &coverGraphicsModel{inner: shellModel{kind: "list", page: page}, output: &coverOutput{target: &buf}}
	m.Update(uv.CellSizeEvent{Width: 8, Height: 16})
	m.Update(uv.PrimaryDeviceAttributesEvent{4})
	if m.sourceKey() == "" {
		t.Fatal("shell preview missing")
	}
	shell := m.inner.(shellModel)
	shell.typing = true
	m.inner = shell
	if m.sourceKey() != "" {
		t.Fatal("image would cover command palette")
	}
	c := coverCapabilities{kitty: true, cols: 120, rows: 24, pixelWidth: 960, pixelHeight: 384}
	if c.protocol() != "Kitty" {
		t.Fatal("window pixel size fallback missing")
	}
}

func TestBookCardAutomaticallyUsesPixelCover(t *testing.T) {
	for _, protocol := range []string{"Kitty", "Sixel"} {
		for _, detailOpen := range []bool{false, true} {
			page := pixelCoverFixture()
			page.detailOpen = detailOpen
			page.books[0].Name = "Preserved title"
			page.books[0].Authors = []string{"Preserved author"}
			before := strings.Split(page.View().Content, "\n")
			var buf bytes.Buffer
			m := &coverGraphicsModel{inner: page, output: &coverOutput{target: &buf}}
			m.Update(uv.CellSizeEvent{Width: 8, Height: 16})
			if protocol == "Kitty" {
				m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: coverGraphicsID}, Payload: []byte("OK")})
			} else {
				m.Update(uv.PrimaryDeviceAttributesEvent{4})
			}
			if m.sourceKey() == "" || m.source().bounds() != bookCardCoverBounds() {
				t.Fatal("card did not select automatic pixel rendering")
			}
			if strings.Contains(m.View().Content, "▀") {
				t.Fatal("character thumbnail flashed while native card was encoding")
			}
			f := encodeCoverFrame(m.source(), m.caps)
			area := bookCardCoverBounds()
			if f.x < area.Min.X || f.y < area.Min.Y || f.x+f.cols > area.Max.X || f.y+f.rows > area.Max.Y {
				t.Fatal("card image overlaps metadata or border")
			}
			if f.pixelHeight <= coverRows*2 || f.pixelWidth <= coverWidth {
				t.Fatal("card still uses character-grid resolution")
			}
			m.Update(coverEncodedMsg{generation: m.generation, frame: f})
			after := strings.Split(m.View().Content, "\n")
			if strings.Contains(strings.Join(after, "\n"), "▀") {
				t.Fatal("character thumbnail remained under pixel cover")
			}
			if len(before) != len(after) {
				t.Fatal("pixel rendering changed page height")
			}
			for i, line := range after {
				if ansi.StringWidth(line) != ansi.StringWidth(before[i]) {
					t.Fatal("pixel rendering changed page width")
				}
				if i >= area.Min.Y && i < area.Max.Y {
					if ansi.Strip(ansi.Cut(line, 0, area.Min.X)) != ansi.Strip(ansi.Cut(before[i], 0, area.Min.X)) || ansi.Strip(ansi.Cut(line, area.Max.X, page.width)) != ansi.Strip(ansi.Cut(before[i], area.Max.X, page.width)) {
						t.Fatal("metadata or border erased with thumbnail")
					}
				} else if line != before[i] {
					t.Fatal("non-cover row changed")
				}
			}
			m.output.Write([]byte("CARD"))
			if !strings.Contains(buf.String(), f.data) {
				t.Fatal("card pixels not written")
			}

		}
	}
}

func TestHistoryCardPixelsAndOverlays(t *testing.T) {
	fixture := pixelCoverFixture()
	details := fixture.details
	details.cache = map[string]bookDetailResultMsg{"1:fixture": {book: fixture.books[0]}}
	h := historySelectModel{width: 120, height: 24, state: historyStateResults, items: []zlib.DownloadHistoryItem{{ID: "1", Hash: "fixture"}}, details: details, detailOpen: true}
	var buf bytes.Buffer
	m := &coverGraphicsModel{inner: h, output: &coverOutput{target: &buf}, caps: coverCapabilities{kitty: true, cellWidth: 8, cellHeight: 16}}
	if m.sourceKey() == "" || m.source().bounds() != bookCardCoverBounds() {
		t.Fatal("history card missing pixel cover")
	}
	h.confirmDownload = true
	m.inner = h
	if m.sourceKey() != "" {
		t.Fatal("card image would overlap download confirmation")
	}
	h.confirmDownload = false
	h.state = historyStateConfirmDelete
	m.inner = h
	if m.sourceKey() != "" {
		t.Fatal("card image would overlap delete confirmation")
	}
	s := shellModel{kind: "list", page: fixture, typing: true}
	m.inner = s
	if m.sourceKey() != "" {
		t.Fatal("card image would overlap command dialog")
	}
}

func TestEncodeCoverFrameScalesSmallSourceToFillSlot(t *testing.T) {
	// Retina cells (16x34 physical pixels) with a source smaller than the slot.
	img := image.NewRGBA(image.Rect(0, 0, 100, 150))
	src := coverSource{img: img, width: 120, height: 24, area: bookCardCoverBounds()}
	f := encodeCoverFrame(src, coverCapabilities{kitty: true, cellWidth: 16, cellHeight: 34})
	if f == nil {
		t.Fatal("no frame")
	}
	maxW, maxH := coverWidth*16, coverRows*34
	if f.pixelWidth != maxW && f.pixelHeight != maxH {
		t.Fatalf("cover %dx%d does not fill %dx%d slot on either axis", f.pixelWidth, f.pixelHeight, maxW, maxH)
	}
	if f.pixelWidth > maxW || f.pixelHeight > maxH {
		t.Fatalf("cover %dx%d overflows %dx%d slot", f.pixelWidth, f.pixelHeight, maxW, maxH)
	}
	if f.pixelWidth*150 != f.pixelHeight*100 && abs(f.pixelWidth*150-f.pixelHeight*100) > 150 {
		t.Fatalf("aspect ratio changed: %dx%d", f.pixelWidth, f.pixelHeight)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestITermScaleConvertsPointsToPixels(t *testing.T) {
	scale, ok := parseITermScale("\x1b]1337;ReportCellSize=18.0;9.0;2.0\x1b\\")
	if !ok || scale != 2 {
		t.Fatalf("scale = %v, %v", scale, ok)
	}
	for _, bad := range []string{"1337;ReportCellSize=18.0;9.0", "1337;ReportCellSize=a;b;c", "1337;Other=1;2;3", "1337;ReportCellSize=18;9;0"} {
		if _, ok := parseITermScale(bad); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
	m := &coverGraphicsModel{inner: pixelCoverFixture(), output: &coverOutput{target: &bytes.Buffer{}}}
	m.Update(uv.CellSizeEvent{Width: 9, Height: 18})
	m.Update(uv.PrimaryDeviceAttributesEvent{4})
	m.Update(uv.UnknownOscEvent("\x1b]1337;ReportCellSize=18.0;9.0;2.0\x1b\\"))
	if w, h := m.caps.cellSize(); w != 18 || h != 36 {
		t.Fatalf("cell size = %dx%d, want 18x36", w, h)
	}
	f := encodeCoverFrame(m.source(), m.caps)
	if f == nil || f.pixelHeight != coverRows*36 {
		t.Fatalf("cover not sized in physical pixels: %+v", f)
	}
}
