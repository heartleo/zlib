package tableprinter

import (
	"os"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestMain(m *testing.M) {
	runewidth.DefaultCondition.EastAsianWidth = false
	os.Exit(m.Run())
}

func TestFitColsKeepsRequestedDisplayWidthForCJKText(t *testing.T) {
	got := fitCols("球状闪电（没有《球状闪电》，就没有《三体》）", 32)
	if width := CellWidth(got); width != 32 {
		t.Fatalf("expected display width 32, got %d for %q", width, got)
	}
}

func TestRowMatchesBorderWidthWithTruncatedCells(t *testing.T) {
	widths := []int{3, 10, 32, 16, 4, 6, 9}
	row := Row([]Cell{
		Plain("2"),
		Plain("9vqa86ZavA"),
		Plain("球状闪电（没有《球状闪电》，就没有《三体》）"),
		Plain("刘慈欣 [刘慈欣]"),
		Plain("2014"),
		Plain("EPUB"),
		Plain("554 KB"),
	}, widths)
	border := Border("┌", "┬", "┐", widths)

	if rowWidth, borderWidth := CellWidth(row), CellWidth(border); rowWidth != borderWidth {
		t.Fatalf("expected row width %d to match border width %d\nrow: %q\nborder: %q", rowWidth, borderWidth, row, border)
	}
}

func TestColorFnAppliedAfterFitting(t *testing.T) {
	prefix := "\033[36m"
	suffix := "\033[0m"
	cyan := func(s string) string { return prefix + s + suffix }

	widths := []int{10}
	row := Row([]Cell{Colored("hello", cyan)}, widths)

	// The fitted text "hello     " (10 chars) should be wrapped in color codes.
	if !strings.Contains(row, prefix) || !strings.Contains(row, suffix) {
		t.Fatal("expected color codes in output")
	}
	// Strip color codes and verify alignment still works.
	plain := strings.ReplaceAll(strings.ReplaceAll(row, prefix, ""), suffix, "")
	border := Border("┌", "┬", "┐", []int{10})
	if CellWidth(plain) != CellWidth(border) {
		t.Fatalf("alignment broken after coloring: plain width %d, border width %d", CellWidth(plain), CellWidth(border))
	}
}
