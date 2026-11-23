package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
)

func tab(m aiPageModel) aiPageModel {
	m, _ = aiStep(m, tea.KeyPressMsg{Code: tea.KeyTab})
	return m
}

// editorOn opens the editor of row on a configured page.
func editorOn(t *testing.T, row aiAction) aiPageModel {
	t.Helper()
	m := aiPageFor(t, configured(aiServer(t)))
	m.cursor = int(row)
	return aiPress(t, m, "enter")
}

func TestAIEditorShowsClearCancelSaveButtonsAndTheirFocus(t *testing.T) {
	m := editorOn(t, actURL)
	screen := ansi.Strip(m.view().Content)
	for _, want := range []string{"Clear", "Cancel", "Save", "tab buttons"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
	if !m.input.Focused() || m.editAction != 0 {
		t.Fatal("the text box must have the focus when the editor opens")
	}
	boxFocused := m.view().Content
	m = tab(m) // Clear
	if m.input.Focused() || m.editAction != 1 {
		t.Fatalf("focus did not move to Clear: focused=%v action=%d", m.input.Focused(), m.editAction)
	}
	if m.view().Content == boxFocused {
		t.Fatal("moving the focus changed nothing on screen")
	}
}

func TestAIClearButtonEmptiesTheBoxInOneEnterAndReturnsTheFocus(t *testing.T) {
	m := editorOn(t, actURL)
	if m.input.Value() == "" {
		t.Fatal("the editor should open on the current address")
	}
	m = aiPress(t, tab(m), "enter")
	if m.input.Value() != "" || m.mode != aiEdit {
		t.Fatalf("value=%q mode=%v", m.input.Value(), m.mode)
	}
	if !m.input.Focused() || m.editAction != 0 {
		t.Fatal("the focus did not return to the box after clearing")
	}
	m = aiType(m, "https://new.example.com/v1")
	if m.input.Value() != "https://new.example.com/v1" {
		t.Fatalf("typing after Clear: %q", m.input.Value())
	}
}

func TestAIClearButtonOnTheKeyMarksItForRemoval(t *testing.T) {
	m := editorOn(t, actKey)
	m = aiPress(t, tab(m), "enter") // Clear
	if m.input.Value() != "" || !m.resetKey {
		t.Fatalf("value=%q resetKey=%v", m.input.Value(), m.resetKey)
	}
	m = aiPress(t, m, "enter")
	if m.s.APIKey != "" {
		t.Fatal("Enter after Clear did not remove the saved key")
	}
}

func TestAICancelAndSaveButtons(t *testing.T) {
	m := editorOn(t, actURL)
	m = aiPress(t, tab(tab(m)), "enter") // Cancel
	if m.mode != aiRows {
		t.Fatalf("Cancel left mode %v", m.mode)
	}
	m = editorOn(t, actKey)
	m = aiType(m, "pasted")
	m = aiPress(t, tab(tab(tab(m))), "enter") // Save
	if m.s.APIKey != "pasted" {
		t.Fatalf("Save did not store the key: %q", m.s.APIKey)
	}
}

func TestAITypingGoesToTheBoxOnlyWhileItHasTheFocus(t *testing.T) {
	m := editorOn(t, actURL)
	before := m.input.Value()
	m = aiType(tab(m), "xyz")
	if m.input.Value() != before {
		t.Fatalf("typing on a button changed the box: %q", m.input.Value())
	}
	m = tab(tab(tab(m))) // around to the box again
	if !m.input.Focused() || m.editAction != 0 {
		t.Fatalf("Tab did not cycle back to the box: action=%d", m.editAction)
	}
	if m = aiType(m, "z"); !strings.HasSuffix(m.input.Value(), "z") {
		t.Fatal("the box does not take typing again")
	}
}

func TestAIEditorButtonsAreDroppedOnShortTerminalsButCtrlRStillClears(t *testing.T) {
	m := editorOn(t, actURL)
	m.height = 12
	if strings.Contains(ansi.Strip(m.view().Content), "Cancel") {
		t.Fatal("the buttons need rows a short terminal does not have")
	}
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input.Value() != "" {
		t.Fatal("Ctrl+R no longer clears")
	}
}

func TestAIHeaderEditorHasTheSameButtons(t *testing.T) {
	m := aiPageFor(t, configured(aiServer(t)))
	m.s.Headers = []aimatch.Header{{Name: "x-a", Value: "111111"}}
	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter", "enter")
	m = aiType(m, "111111")
	m = aiPress(t, tab(m), "enter") // Clear
	if m.input.Value() != "" || m.field != fieldHeader {
		t.Fatalf("value=%q field=%v", m.input.Value(), m.field)
	}
}
