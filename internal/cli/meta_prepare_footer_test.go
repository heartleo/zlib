package cli

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// The shell builds the page with a way to save the sources, and a file whose
// name and title disagree offers the other title, so its footer has every key.
// It must still fit, and keep enter and esc, on the terminals people use.
func TestMetaPagePrepareFooterWithEveryKeyFitsNarrowTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	m := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m.saveSources = func([]string) (string, error) { return "", nil }
	m.extraction.AltTitle = "Another title"
	for _, tc := range []struct {
		width int
		want  []string
	}{
		{150, []string{"move", "toggle", "edit search", "other title", "save sources", "rename", "file info", "search", "back"}},
		{100, []string{"space toggle", "n rename", "enter search", "esc back"}},
		{80, []string{"toggle", "enter", "esc"}},
		{60, []string{"?", "enter", "esc"}}, // ctrl+d is in the ? list
		{40, []string{"enter", "esc"}},
		{24, []string{"enter", "esc"}},
	} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
		got := footer(ansi.Strip(next.(metaPageModel).View().Content))
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%d columns: footer %q lacks %q", tc.width, got, want)
			}
		}
		if ansi.StringWidth(got) > tc.width || strings.Contains(got, "…") {
			t.Errorf("%d columns: footer %q is cut off", tc.width, got)
		}
	}
}

// footerKeys are the keys a footer shows, whatever their labels.
func footerKeys(footer string) []string {
	var out []string
	for _, part := range strings.Split(footer, "•") {
		if f := strings.Fields(part); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// At every width from 40 up, each page shows the rename key and Esc, not only at
// the widths where a long wording fits.
func TestMetaPagesShowRenameAndEscAtEveryWidth(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	prepare := preparedSourcesPage(t, fakeSourceChoices(), &asked, &queries)
	prepare.saveSources = func([]string) (string, error) { return "", nil }
	prepare.extraction.AltTitle = "Another title"
	candidates := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	failed := candidates
	failed.stage, failed.res = metaFailed, metasource.Result{}
	fields := candidates.openFields()
	for name, page := range map[string]metaPageModel{"prepare": prepare, "candidates": candidates, "failed": failed, "fields": fields} {
		for width := 40; width <= 130; width++ {
			next, _ := page.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			got := footer(ansi.Strip(next.(metaPageModel).View().Content))
			keys := footerKeys(got)
			if !slices.Contains(keys, "n") || !slices.Contains(keys, "?") || !slices.Contains(keys, "esc") || ansi.StringWidth(got) > width || strings.Contains(got, "…") {
				t.Errorf("%s at %d columns: footer %q", name, width, got)
			}
		}
	}
}
