package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func choiceModel(t *testing.T) (settingsModel, settingsStore) {
	t.Helper()
	isolateConfigHome(t)
	t.Setenv(nameLayoutEnv, "")
	t.Setenv(metaResultsEnv, "")
	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "metadata")
	m.width, m.height = 90, 30
	return m, s
}

func settingsPress(t *testing.T, m settingsModel, keys ...string) settingsModel {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(settingsModel)
	}
	return m
}

func settingsScreen(m settingsModel) string { return ansi.Strip(m.View().Content) }

// File name format is a list of the presets with the current one marked, and a
// custom row that opens the text box with what it takes and a preview.
func TestFileNameFormatIsAChoiceListWithACustomRow(t *testing.T) {
	m, s := choiceModel(t)
	m.edit(nameLayoutEnv)
	if m.page != "choice" {
		t.Fatalf("the setting did not open a choice page: %q", m.page)
	}
	screen := settingsScreen(m)
	for _, want := range []string{"File name format", "(•) Title - Author", "( ) Author - Title", "( ) Title (Author)", "Custom pattern…", "Dune - Frank Herbert.epub", "Enter choose"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the page lacks %q:\n%s", want, screen)
		}
	}

	// Choosing a preset saves it and returns to the list; the default clears it.
	m = settingsPress(t, m, "down", "enter")
	if m.page != "metadata" || os.Getenv(nameLayoutEnv) != "author-title" {
		t.Fatalf("page %q env %q", m.page, os.Getenv(nameLayoutEnv))
	}
	m.edit(nameLayoutEnv)
	if !strings.Contains(settingsScreen(m), "(•) Author - Title") {
		t.Errorf("the saved preset is not marked:\n%s", settingsScreen(m))
	}
	m = settingsPress(t, m, "up", "enter")
	if saved, _ := readSettingsEnv(s.global); saved[nameLayoutEnv] != "" || os.Getenv(nameLayoutEnv) != "" {
		t.Errorf("the default left a value: %v, env %q", saved, os.Getenv(nameLayoutEnv))
	}

	// The custom row: a box with the rules and a preview, refused while invalid.
	m.edit(nameLayoutEnv)
	m = settingsPress(t, m, "end", "enter")
	if m.page != "edit" || !m.fromChoice {
		t.Fatalf("the custom row did not open the box: page %q", m.page)
	}
	m.input.SetValue("{title} [{authors}]")
	screen = settingsScreen(m)
	for _, want := range []string{"{title} and {authors}", "Preview: Dune by Frank Herbert → Dune [Frank Herbert].epub"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the box lacks %q:\n%s", want, screen)
		}
	}
	m.input.SetValue("{authors} only")
	m = settingsPress(t, m, "enter")
	if m.page != "edit" || !strings.Contains(m.message, "{title}") {
		t.Fatalf("a pattern without {title} was saved: page %q message %q", m.page, m.message)
	}
	m.input.SetValue("{title} [{authors}]")
	m = settingsPress(t, m, "enter")
	if m.page != "metadata" || os.Getenv(nameLayoutEnv) != "{title} [{authors}]" {
		t.Fatalf("page %q env %q", m.page, os.Getenv(nameLayoutEnv))
	}
	if saved, _ := readSettingsEnv(s.global); saved[nameLayoutEnv] != "{title} [{authors}]" {
		t.Fatalf("the pattern did not survive the .env: %v", saved)
	}

	// Reopened, the custom row is marked with the pattern; Esc in the box comes
	// back to the choices, and Esc there to the list.
	m.edit(nameLayoutEnv)
	if screen := settingsScreen(m); !strings.Contains(screen, "(•) {title} [{authors}]") {
		t.Fatalf("the custom value is not marked:\n%s", screen)
	}
	m = settingsPress(t, m, "enter")
	if m.input.Value() != "{title} [{authors}]" {
		t.Errorf("the box does not hold the custom value: %q", m.input.Value())
	}
	m = settingsPress(t, m, "esc")
	if m.page != "choice" {
		t.Fatalf("Esc in the box: page %q", m.page)
	}
	m = settingsPress(t, m, "esc")
	if m.page != "metadata" {
		t.Fatalf("Esc on the choices: page %q", m.page)
	}
}

// Lookup results and the Kindle settings are lists too, with a custom row where
// another number makes sense and none where only the listed values are valid.
func TestOtherSettingsWithShortListsAreChoicePages(t *testing.T) {
	m, s := choiceModel(t)
	m.edit(metaResultsEnv)
	if screen := settingsScreen(m); m.page != "choice" || !strings.Contains(screen, "(•) 10") || !strings.Contains(screen, "Other number…") {
		t.Fatalf("results:\n%s", screen)
	}
	m = settingsPress(t, m, "down", "enter") // 20
	if os.Getenv(metaResultsEnv) != "20" || m.page != "metadata" {
		t.Fatalf("env %q page %q", os.Getenv(metaResultsEnv), m.page)
	}
	m.edit(metaResultsEnv)
	m = settingsPress(t, m, "end", "enter")
	m.input.SetValue("99")
	m = settingsPress(t, m, "enter")
	if m.page != "edit" || !strings.Contains(m.message, "1 to 50") {
		t.Fatalf("99 was accepted: page %q message %q", m.page, m.message)
	}
	m.input.SetValue("25")
	m = settingsPress(t, m, "enter")
	if os.Getenv(metaResultsEnv) != "25" {
		t.Fatalf("env %q", os.Getenv(metaResultsEnv))
	}
	m.edit(metaResultsEnv)
	if screen := settingsScreen(m); !strings.Contains(screen, "(•) 25  (your own value)") {
		t.Errorf("the own value is not marked:\n%s", screen)
	}

	// SMTP security has three rows and no custom one; the default row is the
	// saved empty value, not the "starttls" the row reads as.
	k, _ := newSettingsModel(t.Context(), s, "kindle")
	k.width, k.height = 90, 30
	k.edit(kindleSecurityKey)
	screen := settingsScreen(k)
	if k.page != "choice" || !strings.Contains(screen, "(•) Follow the port") || strings.Contains(screen, "…") && strings.Contains(screen, "Custom") {
		t.Fatalf("security:\n%s", screen)
	}
	k = settingsPress(t, k, "end", "enter")
	if k.kindle.SMTPSecurity != zlib.SMTPSecurityTLS || k.page != "kindle" {
		t.Fatalf("security %q page %q", k.kindle.SMTPSecurity, k.page)
	}
}
