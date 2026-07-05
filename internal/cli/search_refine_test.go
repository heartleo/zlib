package cli

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	zlib "github.com/heartleo/zlib"
)

func resultsModel(query string) searchSelectModel {
	m := newSearchSelectModel(query, nil, nil, false)
	m.state = searchStateResults
	m.books = []zlib.Book{{Name: "A"}, {Name: "B"}}
	m.totalPages = 3
	m.page = 2
	m.cursor = 1
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(m searchSelectModel, k tea.KeyMsg) searchSelectModel {
	next, _ := m.Update(k)
	return next.(searchSelectModel)
}

func TestSlashEntersFilterPrefilled(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	if m.state != searchStateFilter {
		t.Fatalf("state = %v, want filter", m.state)
	}
	if m.input.Value() != "golang" {
		t.Errorf("input = %q, want prefilled %q", m.input.Value(), "golang")
	}
}

func TestFilterEnterRerunsWithNewQuery(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m.input.SetValue("rust")
	m = step(m, key("enter"))
	if m.state != searchStateLoading {
		t.Fatalf("state = %v, want loading", m.state)
	}
	if m.query != "rust" {
		t.Errorf("query = %q, want rust", m.query)
	}
	if m.page != 1 {
		t.Errorf("page = %d, want reset to 1", m.page)
	}
	if m.totalPages != 0 {
		t.Errorf("totalPages = %d, want reset to 0 while loading", m.totalPages)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want reset to 0", m.cursor)
	}
}

func TestFilterEnterPreservesNonEmptyWhitespace(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m.input.SetValue(" golang ")
	m = step(m, key("enter"))
	if m.state != searchStateLoading {
		t.Fatalf("state = %v, want loading", m.state)
	}
	if m.query != " golang " {
		t.Errorf("query = %q, want whitespace preserved", m.query)
	}
}

func TestFilterEnterUnchangedQueryReturnsToResults(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m = step(m, key("enter")) // value still "golang"
	if m.state != searchStateResults {
		t.Fatalf("state = %v, want results", m.state)
	}
	if m.query != "golang" {
		t.Errorf("query = %q, want unchanged golang", m.query)
	}
}

func TestFilterEscCancels(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m.input.SetValue("rust")
	m = step(m, key("esc"))
	if m.state != searchStateResults {
		t.Fatalf("state = %v, want results", m.state)
	}
	if m.query != "golang" {
		t.Errorf("query = %q, want unchanged golang after cancel", m.query)
	}
}

func TestFilterCtrlCQuits(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m = step(m, key("ctrl+c"))
	if !m.quitting {
		t.Errorf("ctrl+c while filtering should quit")
	}
	if m.state != searchStateFilter {
		t.Errorf("state = %v, want still filter while quitting", m.state)
	}
}

func TestFilterTypingDoesNotQuitOnQ(t *testing.T) {
	m := step(resultsModel("golang"), key("/"))
	m.input.SetValue("")
	m = step(m, key("q")) // must be treated as text, not quit
	if m.quitting {
		t.Errorf("typing 'q' while filtering should not quit")
	}
	if m.state != searchStateFilter {
		t.Errorf("state = %v, want still filter", m.state)
	}
}
