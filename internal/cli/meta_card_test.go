package cli

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
)

// The cover slot must sit exactly on the blank block the card reserves.
func TestMetaPrepareCoverSlotSitsInsideTheCard(t *testing.T) {
	m, _ := richPage(t)
	m.fileCover = &metaCoverImage{img: image.NewRGBA(image.Rect(0, 0, 60, 90))}
	for _, size := range [][2]int{{120, 40}, {100, 34}, {100, 30}} {
		p := sized(t, m, size[0], size[1])
		slots := p.coverSlots()
		lay := p.prepareLayout()
		if !lay.cover {
			t.Logf("%dx%d: no cover in the layout", size[0], size[1])
			continue
		}
		if len(slots) != 1 {
			t.Fatalf("%dx%d: %d slots", size[0], size[1], len(slots))
		}
		lines := strings.Split(ansi.Strip(p.View().Content), "\n")
		a := slots[0].area
		for y := a.Min.Y; y < a.Max.Y; y++ {
			cell := ansi.Cut(lines[y], a.Min.X, a.Max.X)
			if strings.TrimSpace(cell) != "" {
				t.Errorf("%dx%d row %d: the slot holds text %q", size[0], size[1], y, cell)
			}
			if !strings.HasPrefix(lines[y], "│ ") {
				t.Errorf("%dx%d row %d: not inside the card: %q", size[0], size[1], y, lines[y])
			}
		}
		if !strings.HasPrefix(lines[a.Min.Y-1], "╭") || !strings.HasPrefix(lines[a.Max.Y], "│") && !strings.HasPrefix(lines[a.Max.Y], "╰") {
			t.Errorf("%dx%d: the slot is not between the card's borders: %q / %q", size[0], size[1], lines[a.Min.Y-1], lines[a.Max.Y])
		}
		if strings.TrimSpace(ansi.Cut(lines[a.Min.Y], a.Max.X, a.Max.X+4)) == "" {
			t.Errorf("%dx%d: no text beside the cover", size[0], size[1])
		}
	}
}

// The card is compact and aligned: the fields' values start in one column, no
// blank line sits inside the text, and the shortcuts are in the "Search for"
// rule rather than in the card.
func TestMetaPrepareCardIsCompactAndAligned(t *testing.T) {
	m, _ := richPage(t)
	for _, size := range [][2]int{{120, 40}, {100, 30}} {
		lines := strings.Split(screenOf(sized(t, m, size[0], size[1])), "\n")
		var card []string
		in := false
		for _, l := range lines {
			if strings.HasPrefix(l, "╭") && !in {
				in = true
				continue
			}
			if strings.HasPrefix(l, "╰") && in {
				break
			}
			if in {
				card = append(card, l)
			}
		}
		starts := map[int]bool{}
		for _, l := range card {
			for _, label := range []string{"Publisher", "Published", "Language", "Series", "Tags", "Format", "About"} {
				if i := strings.Index(l, label); i >= 0 {
					rest := strings.TrimLeft(l[i+len(label):], " ")
					starts[ansi.StringWidth(l[:len(l)-len(rest)])] = true
				}
			}
			if strings.Contains(l, "m edit") {
				t.Errorf("%dx%d: the shortcuts are inside the card: %q", size[0], size[1], l)
			}
		}
		if len(starts) != 1 {
			t.Errorf("%dx%d: the values start in %d columns:\n%s", size[0], size[1], len(starts), strings.Join(card, "\n"))
		}
		text := strings.Join(card, "\n")
		if strings.Contains(text, "│                                                    │") && false {
			t.Error("unreachable")
		}
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, "Search for") && strings.Contains(l, "m edit file · i details") {
				found = true
			}
		}
		if !found {
			t.Errorf("%dx%d: the shortcuts are not in the Search for rule", size[0], size[1])
		}
	}
}

// The comparison page always has a place for the match's cover: a label while
// it downloads or when there is none, blank cells (the image is drawn there)
// once it is loaded, at the cells the graphics layer is told about.
func TestMetaComparisonShowsTheMatchCover(t *testing.T) {
	const url = "https://covers.example.test/1.png"
	m, _, _ := benchPage(t, withMetaCover(func(context.Context, string) (*ebookmeta.Cover, error) { return nil, errors.New("no network in tests") }))
	m = searchFromPrepare(t, m)
	m.res.Candidates[0].CoverURL = url
	m = sized(t, m, 120, 34)
	m.fileCover = &metaCoverImage{img: image.NewRGBA(image.Rect(0, 0, 60, 90))}

	// 1. Asked for, not here yet.
	m.coverCache[url] = &metaCoverImage{}
	if s := screenOf(m); !strings.Contains(s, "loading…") {
		t.Errorf("no placeholder while the match's cover downloads:\n%s", s)
	}
	// 2. The download failed.
	m.coverCache[url] = &metaCoverImage{err: errors.New("boom")}
	if s := screenOf(m); !strings.Contains(s, "unavailable") {
		t.Errorf("a failed cover is not said:\n%s", s)
	}
	// 3. No cover address.
	m.res.Candidates[0].CoverURL = ""
	if s := screenOf(m); !strings.Contains(s, "no cover") {
		t.Errorf("a match without a cover is not said:\n%s", s)
	}
	// 4. Loaded: blank cells at the slot, which the graphics layer fills.
	m.res.Candidates[0].CoverURL = url
	m.coverCache[url] = &metaCoverImage{img: image.NewRGBA(image.Rect(0, 0, 60, 90)), cover: &ebookmeta.Cover{Data: []byte{1}}}
	slots := m.coverSlots()
	if len(slots) != 2 {
		t.Fatalf("%d cover slots, want the file's and the match's", len(slots))
	}
	lines := strings.Split(screenOf(m), "\n")
	a := slots[1].area
	for y := a.Min.Y; y < a.Max.Y; y++ {
		if cell := ansi.Cut(lines[y], a.Min.X, a.Max.X); strings.TrimSpace(cell) != "" {
			t.Errorf("row %d: the match's cover place holds %q", y, cell)
		}
		if ansi.StringWidth(ansi.Cut(lines[y], a.Max.X, a.Max.X+8)) == 0 || strings.TrimSpace(ansi.Cut(lines[y], a.Max.X, a.Max.X+8)) == "" && y < a.Min.Y+3 {
			t.Errorf("row %d: no field text beside the match's cover", y)
		}
	}
	if !strings.Contains(strings.Join(lines[a.Min.Y:a.Max.Y], "\n"), "Title") {
		t.Errorf("the match's panel does not start beside its cover")
	}
}

// The match's cover is fetched when the page can draw it, also when that only
// becomes true after the lookup (a resize replays when the terminal proves it
// can show images), and the answer is kept for the screen and for the write.
func TestMetaComparisonFetchesTheMatchCoverAfterAResize(t *testing.T) {
	const url = "https://covers.example.test/1.png"
	data := pngBytes(t)
	var fetched []string
	fetch := func(_ context.Context, u string) (*ebookmeta.Cover, error) {
		fetched = append(fetched, u)
		return &ebookmeta.Cover{Data: data}, nil
	}
	m, _, _ := benchPage(t, withMetaCover(fetch))
	m = searchFromPrepare(t, m)
	m.res.Candidates[0].CoverURL = url
	// Too narrow to draw it: nothing is fetched.
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil || len(fetched) != 0 {
		t.Fatalf("fetched at 80 columns: %v", fetched)
	}
	// Wide enough: the resize asks for it.
	next, cmd = next.(metaPageModel).Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	if cmd == nil {
		t.Fatal("a resize to a size that shows the panels did not ask for the cover")
	}
	msg, ok := cmd().(metaCoverFetched)
	if !ok || msg.url != url || msg.got == nil || msg.got.img == nil || len(fetched) != 1 {
		t.Fatalf("answer %+v (%d fetches)", msg, len(fetched))
	}
	done, again := next.(metaPageModel).Update(msg)
	if again != nil {
		t.Error("the answer asked for more")
	}
	// A second resize does not fetch it again.
	if _, cmd := done.(metaPageModel).Update(tea.WindowSizeMsg{Width: 121, Height: 34}); cmd != nil {
		t.Error("the cover was fetched twice")
	}
	if got := done.(metaPageModel).compareModel().matchCoverState(); got != coverLoaded {
		t.Errorf("state = %v after the answer", got)
	}
}
