package tableprinter

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// Cell holds the text content of a table cell and an optional color function
// applied after the text is fitted to the column width.
type Cell struct {
	Text    string
	ColorFn func(string) string
}

// Plain returns a Cell with no color.
func Plain(text string) Cell { return Cell{Text: text} }

// Colored returns a Cell whose fitted text is passed through fn before rendering.
func Colored(text string, fn func(string) string) Cell { return Cell{Text: text, ColorFn: fn} }

// Border renders a horizontal border row, e.g. ┌───┬───┐
func Border(left, mid, right string, widths []int) string {
	var sb strings.Builder
	sb.WriteString(left)
	for i, w := range widths {
		sb.WriteString(strings.Repeat("─", w+2))
		if i < len(widths)-1 {
			sb.WriteString(mid)
		}
	}
	sb.WriteString(right)
	return sb.String()
}

// Row renders a data row with │ separators. Each cell is fitted to its column
// width first; then the optional ColorFn is applied to the fitted text.
func Row(cells []Cell, widths []int) string {
	var sb strings.Builder
	sb.WriteString("│")
	for i, w := range widths {
		sb.WriteString(" ")
		fitted := fitCols(cells[i].Text, w)
		if cells[i].ColorFn != nil {
			fitted = cells[i].ColorFn(fitted)
		}
		sb.WriteString(fitted)
		sb.WriteString(" │")
	}
	return sb.String()
}

// CellWidth returns the terminal display width of s.
func CellWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runewidth.RuneWidth(r)
	}
	return n
}

// fitCols pads or truncates s to exactly maxCols display columns.
func fitCols(s string, maxCols int) string {
	w := CellWidth(s)
	if w <= maxCols {
		return s + strings.Repeat(" ", maxCols-w)
	}
	const ellipsis = "..."
	const ew = len(ellipsis)
	cols := 0
	var out []rune
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if cols+rw > maxCols-ew {
			break
		}
		out = append(out, r)
		cols += rw
	}
	return string(out) + ellipsis + strings.Repeat(" ", maxCols-cols-ew)
}
