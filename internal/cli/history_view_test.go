package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestHistoryDeleteConfirmationAndFailure(t *testing.T) {
	m := newHistorySelectModel(nil)
	m.state = historyStateResults
	m.items = []zlib.DownloadHistoryItem{{ID: "42", Name: "A book"}}
	next, cmd := m.Update(key("d"))
	m = next.(historySelectModel)
	if cmd != nil || m.state != historyStateConfirmDelete {
		t.Fatal("deletion must require confirmation")
	}
	next, cmd = m.Update(key("n"))
	m = next.(historySelectModel)
	if cmd != nil || m.state != historyStateResults || len(m.items) != 1 {
		t.Fatal("cancel changed history")
	}
	next, _ = m.Update(key("d"))
	m = next.(historySelectModel)
	next, cmd = m.Update(key("y"))
	m = next.(historySelectModel)
	if cmd == nil || m.state != historyStateDeleting {
		t.Fatal("confirmation did not start deletion")
	}
	_, duplicate := m.Update(key("y"))
	if duplicate != nil {
		t.Fatal("duplicate delete request")
	}
	next, _ = m.Update(historyDeletedMsg{err: errors.New("denied")})
	m = next.(historySelectModel)
	if len(m.items) != 1 || !strings.Contains(m.View().Content, "Delete failed") {
		t.Fatal("failure lost history or error")
	}
	next, cmd = m.Update(historyDeletedMsg{})
	if next.(historySelectModel).state != historyStateLoading || cmd == nil {
		t.Fatal("success did not refresh history")
	}
}

func TestHistoryDeleteDialogButtons(t *testing.T) {
	m := newHistorySelectModel(nil)
	m.state, m.width, m.height = historyStateResults, 80, 24
	m.items = []zlib.DownloadHistoryItem{{ID: "42", Name: "A book"}}
	next, _ := m.Update(key("d"))
	m = next.(historySelectModel)
	view := ansi.Strip(m.View().Content)
	if m.deleteFocused || !strings.Contains(view, "Cancel") || !strings.Contains(view, "Delete") || !strings.Contains(view, "A book") {
		t.Fatal("dialog missing selection or book")
	}
	next, cmd := m.Update(key("enter"))
	if cmd != nil || next.(historySelectModel).state != historyStateResults {
		t.Fatal("default Enter must cancel")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(historySelectModel)
	if !m.deleteFocused || !strings.Contains(ansi.Strip(m.View().Content), "Delete") {
		t.Fatal("delete button not focused")
	}
	next, cmd = m.Update(key("enter"))
	if cmd == nil || next.(historySelectModel).state != historyStateDeleting {
		t.Fatal("selected delete not submitted")
	}
}

func TestHistoryViewportAndSelection(t *testing.T) {
	for _, width := range []int{30, 40, 80, 120} {
		m := newHistorySelectModel(nil)
		m.state, m.width, m.height = historyStateResults, width, 24
		for i := range 50 {
			m.items = append(m.items, zlib.DownloadHistoryItem{ID: fmt.Sprint(i + 1), Hash: "abcdef", Name: "历史书名 Chinese title", Date: "2026-10-13"})
		}
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
		m = next.(historySelectModel)
		view := m.View()
		if !view.AltScreen || !strings.Contains(ansi.Strip(view.Content), "> 50") {
			t.Fatal("last selection is not visible")
		}
		lines := strings.Split(view.Content, "\n")
		if len(lines) > m.height {
			t.Fatal("history exceeds terminal height")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("history exceeds width %d: %q", width, line)
			}
		}
		next, cmd := m.Update(key("enter"))
		if cmd != nil || next.(historySelectModel).selected != nil {
			t.Fatal("download started before confirmation")
		}
		m = next.(historySelectModel)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		m = next.(historySelectModel)
		next, cmd = m.Update(key("enter"))
		if next.(historySelectModel).selected == nil || cmd == nil {
			t.Fatal("book without direct URL cannot be selected")
		}
	}
}

func TestHistorySharesListScrollAndBookCard(t *testing.T) {
	m := newHistorySelectModel(nil)
	m.state, m.width, m.height, m.inShell = historyStateResults, 80, 24, true
	for i := range 30 {
		m.items = append(m.items, zlib.DownloadHistoryItem{
			ID: fmt.Sprint(i + 1), Hash: "abcdef", Name: fmt.Sprintf("Book %d", i+1), Authors: []string{"An Author"}, Date: "2026-10-13",
		})
	}
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	header := -1
	for i, line := range lines {
		if strings.Contains(line, "#") && strings.Contains(line, "Title") {
			header = i
			break
		}
	}
	if header < 0 || !strings.Contains(lines[header], "Author") || !strings.Contains(lines[1], "Downloaded 2026-10-13") {
		t.Fatal("history header or selected download date is missing")
	}
	card := strings.Join(lines[2:header], "\n")
	if !strings.Contains(card, "An Author") || strings.Contains(card, "2026-10-13") || !strings.Contains(view, "d delete · q back") {
		t.Fatal("history card or actions differ from other book lists")
	}
	m.width = 30
	if !strings.Contains(ansi.Strip(m.View().Content), "Page 1 · 2026-10-13") {
		t.Fatal("narrow history view hides the selected download date")
	}
	m.width = 80
	next, _ := m.Update(key("pgdown"))
	m = next.(historySelectModel)
	if m.listOffset != 1 {
		t.Fatalf("page down list offset = %d, want 1", m.listOffset)
	}
	next, _ = m.Update(key("up"))
	m = next.(historySelectModel)
	if m.listOffset != 1 {
		t.Fatal("moving within the visible rows scrolled the history list")
	}
}

// The history list response carries a book's ISBN like every other list, so
// the selected book's card shows ISBN 10 and 13 without waiting for details.
func TestHistoryCardShowsTheISBNFromTheList(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 24}} {
		m := newHistorySelectModel(nil)
		m.state, m.width, m.height, m.inShell = historyStateResults, size[0], size[1], true
		m.items = []zlib.DownloadHistoryItem{{
			ID: "1", Hash: "abcdef", Name: "A book", Authors: []string{"An Author"}, Date: "2026-10-13",
			ISBN10: "7115390576", ISBN13: "9787115390578",
		}}
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"ISBN 10: 7115390576", "ISBN 13: 9787115390578"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: history card lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
	}
}

func TestHistoryEmptyPageCanGoBack(t *testing.T) {
	m := newHistorySelectModel(nil)
	next, _ := m.Update(historyResultMsg{page: 2})
	m = next.(historySelectModel)
	if !strings.Contains(m.View().Content, "No history") || !strings.Contains(m.View().Content, "q quit") {
		t.Fatal("empty state missing message or navigation")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if next.(historySelectModel).state != historyStateLoading || cmd == nil {
		t.Fatal("cannot return from empty page")
	}
}

func TestHistoryLastPageHasNoNext(t *testing.T) {
	result := zlib.DownloadHistoryResult{Page: 2, TotalPages: 2, Items: []zlib.DownloadHistoryItem{{ID: "1"}}}
	if historyHasNext(result) {
		t.Fatal("last page should not have a next page")
	}
	result.Page = 1
	if !historyHasNext(result) {
		t.Fatal("first page should have a next page")
	}
}
