package cli

import (
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
		{100, []string{"move", "toggle", "edit search", "other title", "save sources", "search", "back"}},
		{80, []string{"toggle", "enter", "esc"}},
		{60, []string{"r", "enter", "esc"}},
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
