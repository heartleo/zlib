package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestDefaultFormatPickerPreselectsAndCancels(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "epub,pdf")
	store := testSettingsStore(t)
	if err := store.save(searchExtensionsEnv, "epub,pdf"); err != nil {
		t.Fatal(err)
	}
	m, err := newSettingsModel(t.Context(), store, "home")
	if err != nil {
		t.Fatal(err)
	}
	m.cursor = slices.Index(settingsPageKeys("home"), searchExtensionsEnv)
	previousCursor := m.cursor
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "formats" || m.input.Focused() || m.formatValue() != "epub,pdf" {
		t.Fatal("format row did not open a preselected checklist")
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"[✓] EPUB", "[✓] PDF", "[ ] AZW", "Space toggle", "Ctrl+A all/none", "Enter save", "Esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	for _, gone := range []string{"Reset", "Cancel", "Tab"} {
		if strings.Contains(view, gone) {
			t.Errorf("picker still shows %q:\n%s", gone, view)
		}
	}
	m, _ = settingsStep(t, m, key("space"))
	if m.formatValue() != "pdf" || os.Getenv(searchExtensionsEnv) != "epub,pdf" {
		t.Fatal("toggling wrote before Enter")
	}
	m, _ = settingsStep(t, m, key("esc"))
	if m.page != "home" || m.cursor != previousCursor {
		t.Fatal("cancel did not restore the settings selection")
	}
	m.edit(searchExtensionsEnv)
	if m.formatValue() != "epub,pdf" {
		t.Fatal("cancel retained draft changes")
	}
	m, _ = settingsStep(t, m, key("ctrl+a")) // Select all.
	m, _ = settingsStep(t, m, key("ctrl+a")) // All set, so clear.
	stored, err := readSettingsEnv(store.global)
	if err != nil || stored[searchExtensionsEnv] != "epub,pdf" || len(m.formats) != 0 {
		t.Fatal("clearing must change only the draft")
	}
	m, _ = settingsStep(t, m, key("esc"))
	m.edit(searchExtensionsEnv)
	if m.formatValue() != "epub,pdf" {
		t.Fatal("Esc saved the cleared draft")
	}
	m, _ = settingsStep(t, m, key("ctrl+a"))
	m, _ = settingsStep(t, m, key("ctrl+a"))
	m, _ = settingsStep(t, m, key("enter"))
	stored, err = readSettingsEnv(store.global)
	if err != nil || stored[searchExtensionsEnv] != "" || m.page != "home" || m.value(searchExtensionsEnv).Value != "Any format" {
		t.Fatal("Enter did not persist an unrestricted format selection")
	}
}

func TestDefaultFormatPickerCtrlASelectsEveryFormat(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "")
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "search")
	if err != nil {
		t.Fatal(err)
	}
	m.edit(searchExtensionsEnv)
	m, _ = settingsStep(t, m, key("ctrl+a"))
	if len(m.formats) != len(searchFormats) {
		t.Fatalf("Ctrl+A selected %d of %d formats", len(m.formats), len(searchFormats))
	}
	m, _ = settingsStep(t, m, key("space")) // Uncheck one; Ctrl+A fills the rest again.
	m, _ = settingsStep(t, m, key("ctrl+a"))
	if len(m.formats) != len(searchFormats) {
		t.Fatal("Ctrl+A with a partial selection must select all")
	}
}

func TestDefaultFormatPickerSaveFailureRetainsSelection(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "")
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "search")
	if err != nil {
		t.Fatal(err)
	}
	m.edit(searchExtensionsEnv)
	m, _ = settingsStep(t, m, key("end"))
	m, _ = settingsStep(t, m, key("space"))
	m.store.global = t.TempDir() // A directory cannot be replaced by the .env writer.
	m, _ = settingsStep(t, m, key("ctrl+s"))
	if m.page != "formats" || m.message == "" || !slices.Contains(m.formats, zlib.ExtLIT) || os.Getenv(searchExtensionsEnv) != "" {
		t.Fatal("failed Save lost the draft or changed the live defaults")
	}
}

func TestDefaultFormatPickerEnterSavesWithoutToggling(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "any format", true: "epub"}[selected], func(t *testing.T) {
			t.Setenv(searchExtensionsEnv, "")
			store := testSettingsStore(t)
			m, err := newSettingsModel(t.Context(), store, "search")
			if err != nil {
				t.Fatal(err)
			}
			m.edit(searchExtensionsEnv)
			want := ""
			if selected {
				m, _ = settingsStep(t, m, key("space"))
				want = "epub"
			}
			m, _ = settingsStep(t, m, key("enter"))
			stored, err := readSettingsEnv(store.global)
			if err != nil || m.page != "search" || stored[searchExtensionsEnv] != want || os.Getenv(searchExtensionsEnv) != want {
				t.Fatal("Enter did not save the selection unchanged")
			}
		})
	}
}

func TestDefaultFormatPickerPreservesProcessOverride(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "pdf")
	store := testSettingsStore(t)
	store.env = map[string]string{searchExtensionsEnv: "pdf"}
	m, err := newSettingsModel(t.Context(), store, "search")
	if err != nil {
		t.Fatal(err)
	}
	m.edit(searchExtensionsEnv)
	if m.formatValue() != "pdf" || !strings.Contains(ansi.Strip(m.View().Content), "Using Process environment") {
		t.Fatal("picker did not explain the active override")
	}
	m, _ = settingsStep(t, m, key("space")) // Add EPUB to the saved defaults.
	m, _ = settingsStep(t, m, key("ctrl+s"))
	stored, err := readSettingsEnv(store.global)
	if err != nil || stored[searchExtensionsEnv] != "epub,pdf" || os.Getenv(searchExtensionsEnv) != "pdf" || !strings.Contains(m.message, "overridden") {
		t.Fatal("picker overwrote the process setting or lost the saved preference")
	}
}

func TestDefaultFormatPickerFitsAndScrollsInEveryTheme(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "epub")
	originalTheme := currentTheme
	t.Cleanup(func() { currentTheme = originalTheme })
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "search")
	if err != nil {
		t.Fatal(err)
	}
	m.edit(searchExtensionsEnv)
	for _, name := range selectableThemes() {
		currentTheme, _ = namedTheme(name)
		for _, size := range [][2]int{{120, 32}, {80, 24}, {40, 16}, {36, 14}, {30, 14}, {20, 8}, {20, 5}} {
			m, _ = settingsStep(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m, _ = settingsStep(t, m, key("end"))
			view := ansi.Strip(m.View().Content)
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatalf("%s at %v exceeds terminal height", name, size)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%s at %v exceeds terminal width", name, size)
				}
			}
			if size[1] >= 7 {
				for _, want := range []string{"LIT", "save", "Esc cancel"} {
					if !strings.Contains(view, want) {
						t.Fatalf("%s at %v hides %q:\n%s", name, size, want, view)
					}
				}
			}
		}
	}
}
