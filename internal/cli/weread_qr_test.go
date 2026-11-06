package cli

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const loginURL = "https://open.weixin.qq.com/connect/confirm?uuid=0123456789abcdef"

// The panel must have as many modules across as down, with the code
// centered to within one module and a quiet zone of at least quiet on every
// side. A module is one column wide and half a row tall.
func TestQRPanelIsSquareWithQuietZone(t *testing.T) {
	for _, content := range []string{loginURL, loginURL + "0123456789", "x", strings.Repeat("a", 120)} {
		code, err := encodeQR(content)
		if err != nil {
			t.Fatal(err)
		}
		for quiet := qrQuietMin; quiet <= qrQuietSpec; quiet++ {
			lines := strings.Split(ansi.Strip(renderQRCode(code, quiet)), "\n")
			modulesDown, modulesAcross := 2*len(lines), ansi.StringWidth(lines[0])
			if modulesDown != modulesAcross || modulesAcross != qrSide(code.Size, quiet) {
				t.Fatalf("size %d quiet %d: %d modules across, %d down", code.Size, quiet, modulesAcross, modulesDown)
			}
			// Margin of the first and last dark module on each side.
			left, right, top, bottom := modulesAcross, modulesAcross, modulesDown, modulesDown
			for row, line := range lines {
				for col, r := range []rune(line) {
					for half, dark := range []bool{r == '█' || r == '▀', r == '█' || r == '▄'} {
						if !dark {
							continue
						}
						y := 2*row + half
						left, right = min(left, col), min(right, modulesAcross-1-col)
						top, bottom = min(top, y), min(bottom, modulesDown-1-y)
					}
				}
			}
			for side, got := range map[string]int{"left": left, "right": right, "top": top, "bottom": bottom} {
				if got < quiet || got > quiet+1 {
					t.Fatalf("size %d quiet %d: %s margin = %d modules", code.Size, quiet, side, got)
				}
			}
			if left != top || right != bottom {
				t.Fatalf("size %d quiet %d: margins left/top %d/%d, right/bottom %d/%d differ", code.Size, quiet, left, top, right, bottom)
			}
		}
	}
}

func TestQRFitPicksLargestQuietZoneThatFits(t *testing.T) {
	code, err := encodeQR(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	size := code.Size
	for _, tt := range []struct {
		name          string
		width, height int
		quiet         int
		ok            bool
	}{
		{"roomy", 200, 100, qrQuietSpec, true},
		{"standard 80x24", 80, 24, 3, true},
		{"one row short of spec at 80", 80, qrSide(size, qrQuietSpec)/2 + qrReserveRows - 1, 3, true},
		{"just the minimum", qrSide(size, qrQuietMin), qrSide(size, qrQuietMin)/2 + qrReserveRows, qrQuietMin, true},
		{"too narrow", qrSide(size, qrQuietMin) - 1, 100, qrQuietMin, false},
		{"too short", 200, qrSide(size, qrQuietMin)/2 + qrReserveRows - 1, qrQuietMin, false},
	} {
		quiet, ok := qrFit(size, tt.width, tt.height)
		if quiet != tt.quiet || ok != tt.ok {
			t.Errorf("%s: qrFit = %d, %v; want %d, %v", tt.name, quiet, ok, tt.quiet, tt.ok)
		}
	}
	if needW, needH := qrSizeNeeded(size); needW != qrSide(size, qrQuietMin) || needH != needW/2+qrReserveRows {
		t.Errorf("qrSizeNeeded = %d×%d", needW, needH)
	}
}

// qrLoginView renders the login page showing a code at width × height.
func qrLoginView(t *testing.T, width, height int, scanned bool) string {
	t.Helper()
	code, err := encodeQR(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	m := newWeReadLoginModel(nil)
	m.code, m.scanned = code, scanned
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return ansi.Strip(next.(wereadLoginModel).View().Content)
}

// qrBlockRows returns the rows of view that draw the code.
func qrBlockRows(view string) []string {
	var rows []string
	for _, line := range strings.Split(view, "\n") {
		if strings.ContainsAny(line, "█▀▄") {
			rows = append(rows, line)
		}
	}
	return rows
}

func TestWeReadLoginShowsWholeCodeOnStandardTerminal(t *testing.T) {
	view := qrLoginView(t, 80, 24, false)
	if strings.Contains(view, "Enlarge") {
		t.Fatalf("an 80×24 terminal should show the code:\n%s", view)
	}
	if lines := strings.Split(view, "\n"); len(lines) != 24 {
		t.Fatalf("view has %d rows, want 24", len(lines))
	}
	code, _ := encodeQR(loginURL)
	quiet, _ := qrFit(code.Size, 80, 24)
	// The panel's rows are all blocks or blanks of one width, so find them
	// by the white panel's width instead of the text beside them.
	rows := qrBlockRows(view)
	if len(rows) == 0 {
		t.Fatalf("no QR rows:\n%s", view)
	}
	side := qrSide(code.Size, quiet)
	for _, want := range []string{"WeRead login", "Log in with WeChat", "Open WeChat and scan this code", "Confirm the login on your phone", "The code expires in 5 minutes.", "Waiting for the scan", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("login page lacks %q:\n%s", want, view)
		}
	}
	if len(rows) > side/2 {
		t.Fatalf("%d block rows for a panel %d rows tall", len(rows), side/2)
	}
}

func TestWeReadLoginCentersCodeWhenInstructionsDoNotFit(t *testing.T) {
	code, _ := encodeQR(loginURL)
	quiet, _ := qrFit(code.Size, 45, 30)
	side := qrSide(code.Size, quiet)
	view := qrLoginView(t, 45, 30, false)
	if strings.Contains(view, "Log in with WeChat") {
		t.Fatalf("instructions squeezed in beside the code:\n%s", view)
	}
	// The finder patterns start at the quiet zone's edge, so the leftmost
	// block in any row sits quiet columns inside the panel.
	lead := 1 << 30
	for _, row := range qrBlockRows(view) {
		lead = min(lead, len(row)-len(strings.TrimLeft(row, " ")))
	}
	if want := (45-side)/2 + quiet; lead != want {
		t.Fatalf("leftmost block at column %d, want %d (panel centered):\n%s", lead, want, view)
	}
	if !strings.Contains(view, "Scan with WeChat, then confirm on your phone.") || strings.Contains(view, "…") {
		t.Fatalf("narrow page should show a whole status sentence:\n%s", view)
	}
	// One spare row at most separates the code from the page's edges.
	rows := strings.Split(view, "\n")
	first := slices.IndexFunc(rows, func(r string) bool { return strings.ContainsAny(r, "█▀▄") })
	last := first
	for i, r := range rows {
		if strings.ContainsAny(r, "█▀▄") {
			last = i
		}
	}
	if above, below := first-2, len(rows)-2-1-last; above-below > 1 || below-above > 1 {
		t.Fatalf("code is not centered vertically: %d blank rows above, %d below", above, below)
	}
}

func TestWeReadLoginReportsScan(t *testing.T) {
	for size, want := range map[[2]int]string{
		{100, 30}: symbolSuccess + " Scanned. Confirm the login in WeChat on your phone.",
		{45, 30}:  symbolSuccess + " Scanned. Confirm on your phone.",
	} {
		if view := qrLoginView(t, size[0], size[1], true); !strings.Contains(view, want) {
			t.Fatalf("%v: scanned page lacks %q:\n%s", size, want, view)
		}
	}
	if view := qrLoginView(t, 100, 30, true); !strings.Contains(view, symbolSuccess+" Open WeChat and scan this code") {
		t.Fatalf("the scan step is not marked done:\n%s", view)
	}
}

// At every terminal size the page either draws the code at its square size
// or says how large the terminal must be; it never draws a clipped code.
func TestWeReadLoginNeverClipsTheCode(t *testing.T) {
	code, _ := encodeQR(loginURL)
	needW, needH := qrSizeNeeded(code.Size)
	for width := 20; width <= 120; width += 5 {
		for height := 6; height <= 45; height += 3 {
			view := qrLoginView(t, width, height, false)
			rows := qrBlockRows(view)
			fits := width >= needW && height >= needH
			switch {
			case fits && len(rows) == 0:
				t.Fatalf("%d×%d fits the code but none was drawn:\n%s", width, height, view)
			case !fits && len(rows) != 0:
				t.Fatalf("%d×%d is too small but code rows were drawn:\n%s", width, height, view)
			case !fits && height >= 7 && !strings.Contains(view, fmt.Sprintf("%d×%d", needW, needH)):
				t.Fatalf("%d×%d: the hint does not name the size needed:\n%s", width, height, view)
			}
			if fits && len(rows)*2 < needW-2*(qrQuietMin+1) {
				t.Fatalf("%d×%d: only %d code rows drawn:\n%s", width, height, len(rows), view)
			}
			for _, line := range strings.Split(view, "\n") {
				if w := ansi.StringWidth(line); w > width {
					t.Fatalf("%d×%d: line is %d columns wide", width, height, w)
				}
			}
			if n := len(strings.Split(view, "\n")); n > height {
				t.Fatalf("%d×%d: view has %d rows", width, height, n)
			}
		}
	}
}
