package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"rsc.io/qr"
)

// A terminal cell is about twice as tall as it is wide, so one half-block
// character (▀ ▄ █) holds two vertically stacked modules and each module is
// one column wide and half a row tall: square. The white panel around a code
// keeps that shape by having as many modules across as down.
const (
	// qrQuietSpec is the quiet zone the QR specification asks for, in modules.
	qrQuietSpec = 4
	// qrQuietMin is the smallest quiet zone that still scans against a dark
	// terminal, used when the terminal cannot hold more.
	qrQuietMin = 2
	// qrReserveRows are the rows the login page keeps around the code: title,
	// subtitle, status and key hints.
	qrReserveRows = 4
)

// encodeQR encodes content for display on a screen. Level L is enough there,
// since a screen shows the code undamaged, and it needs fewer modules than
// the higher levels, so more terminals can show it whole.
func encodeQR(content string) (*qr.Code, error) {
	code, err := qr.Encode(content, qr.L)
	if err != nil {
		return nil, fmt.Errorf("render QR code: %w", err)
	}
	return code, nil
}

// qrSide is the panel's width and height in modules: the code plus a quiet
// zone on each side, rounded up to an even number so it fills whole rows.
// When the code's size is odd, which it always is, the right and bottom
// margins are one module wider than the left and top ones.
func qrSide(size, quiet int) int {
	n := size + 2*quiet
	return n + n%2
}

// qrFit returns the largest quiet zone that lets the panel and the rows around
// it fit in width × height, and false when even the smallest does not.
func qrFit(size, width, height int) (int, bool) {
	for quiet := qrQuietSpec; quiet >= qrQuietMin; quiet-- {
		side := qrSide(size, quiet)
		if side <= width && side/2+qrReserveRows <= height {
			return quiet, true
		}
	}
	return qrQuietMin, false
}

// qrSizeNeeded reports the smallest terminal that shows a code of size.
func qrSizeNeeded(size int) (width, height int) {
	side := qrSide(size, qrQuietMin)
	return side, side/2 + qrReserveRows
}

// renderQRCode draws code on a white panel, black on white so it scans on dark
// terminals too. The panel is qrSide(code.Size, quiet) columns wide and half
// that many rows tall.
func renderQRCode(code *qr.Code, quiet int) string {
	side := qrSide(code.Size, quiet)
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFFFFF"))
	lines := make([]string, 0, side/2)
	for y := 0; y < side; y += 2 {
		var row strings.Builder
		for x := range side {
			switch top, bottom := dark(x, y), dark(x, y+1); {
			case top && bottom:
				row.WriteString("█")
			case top:
				row.WriteString("▀")
			case bottom:
				row.WriteString("▄")
			default:
				row.WriteString(" ")
			}
		}
		lines = append(lines, style.Render(row.String()))
	}
	return strings.Join(lines, "\n")
}

// renderQR draws content with the quiet zone the specification asks for, for
// output that is not sized to a terminal.
func renderQR(content string) (string, error) {
	code, err := encodeQR(content)
	if err != nil {
		return "", err
	}
	return renderQRCode(code, qrQuietSpec), nil
}

// fitText returns the first candidate that fits in width columns, or the last,
// which the caller's view then truncates.
func fitText(width int, candidates ...string) string {
	for _, c := range candidates {
		if ansi.StringWidth(c) <= width {
			return c
		}
	}
	return candidates[len(candidates)-1]
}

// qrGap separates the code from the instructions beside it.
const qrGap = 4

// layoutQR places the code lines in a body of width columns. When the
// instructions fit beside it, the pair is centered and the instructions are
// centered vertically against the code; otherwise only the code is centered.
// The second result reports which layout was used.
func layoutQR(art, info []string, width int) ([]string, bool) {
	codeW := ansi.StringWidth(art[0])
	infoW := 0
	for _, line := range info {
		infoW = max(infoW, ansi.StringWidth(line))
	}
	if len(info) == 0 || codeW+qrGap+infoW > width-2 {
		pad := strings.Repeat(" ", max(0, (width-codeW)/2))
		out := make([]string, len(art))
		for i, line := range art {
			out[i] = pad + line
		}
		return out, false
	}
	pad := strings.Repeat(" ", (width-(codeW+qrGap+infoW))/2)
	top := (len(art) - len(info)) / 2
	out := make([]string, len(art))
	for i, line := range art {
		out[i] = pad + line
		if j := i - top; j >= 0 && j < len(info) {
			out[i] += strings.Repeat(" ", qrGap) + info[j]
		}
	}
	return out, true
}
