package cli

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

// fakeWeReadSettingsKey has the shape of a WeRead API key but is not one.
const fakeWeReadSettingsKey = "wrk-settings-fake-key-0000"

// The WeRead API key is a credential like the Google Books key: validated
// without echoing it, hidden in the editor and on every screen, saved with an
// empty box that cannot delete it by accident, and removed only on request.
func TestWeReadKeySettingIsValidatedMaskedAndRemovable(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(wereadAPIKeyEnv, "")
	t.Chdir(t.TempDir())

	if v, err := validateSetting(wereadAPIKeyEnv, "  "+fakeWeReadSettingsKey+"\t"); err != nil || v != fakeWeReadSettingsKey {
		t.Fatalf("a valid key was refused or not trimmed: %q, %v", v, err)
	}
	for _, bad := range []string{"short", "has space in it", "bad$chars-0123456789"} {
		if _, err := validateSetting(wereadAPIKeyEnv, bad); err == nil || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}

	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "metadata")
	m.width, m.height = 90, 40
	if v := m.value(wereadAPIKeyEnv); v.Value != wereadKeyNotSet || v.Source != "Default" {
		t.Fatalf("default row = %+v", v)
	}
	m = cursorTo(t, m, wereadAPIKeyEnv)
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || m.key != wereadAPIKeyEnv || m.input.EchoMode != tea.EchoPassword || m.input.Value() != "" {
		t.Fatalf("the editor: page %q key %q echo %v value %q", m.page, m.key, m.input.EchoMode, m.input.Value())
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "WeRead API key") || !strings.Contains(view, "No key is set") {
		t.Fatalf("editor:\n%s", view)
	}
	// An invalid key is refused in place; a valid one is saved and applied.
	m.input.SetValue("nope")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || !strings.Contains(m.message, "8 to 128") || strings.Contains(m.message, "nope") {
		t.Fatalf("page %q message %q", m.page, m.message)
	}
	m.input.SetValue(fakeWeReadSettingsKey)
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "metadata" || m.message != "Saved" || os.Getenv(wereadAPIKeyEnv) != fakeWeReadSettingsKey {
		t.Fatalf("page %q message %q env %q", m.page, m.message, os.Getenv(wereadAPIKeyEnv))
	}
	if saved, _ := readSettingsEnv(s.global); saved[wereadAPIKeyEnv] != fakeWeReadSettingsKey {
		t.Fatalf("saved = %v", saved)
	}
	if view := ansi.Strip(m.View().Content); strings.Contains(view, fakeWeReadSettingsKey) || !strings.Contains(view, "Configured (hidden)") {
		t.Fatalf("after saving:\n%s", view)
	}
	previous := settingsEnvironment
	settingsEnvironment = map[string]string{wereadAPIKeyEnv: fakeWeReadSettingsKey}
	t.Cleanup(func() { settingsEnvironment = previous })
	out, err := executeRootForTest(t, "config", "metadata", "weread-key", "--json")
	if err != nil || strings.Contains(out, fakeWeReadSettingsKey) || !strings.Contains(out, "Configured (hidden)") {
		t.Fatalf("--json: %v\n%s", err, out)
	}
	// The weread source on the lookup page can run now.
	for _, c := range metaSourceChoices(nil) {
		if c.name == "weread" && (!c.available || !strings.Contains(c.note, "key set")) {
			t.Errorf("the weread source does not see the saved key: %+v", c)
		}
	}

	// Enter on the empty box keeps the key; Ctrl+R then Enter removes it.
	m, _ = settingsStep(t, m, key("enter"))
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || !strings.Contains(m.message, "Ctrl+R") {
		t.Fatalf("an empty Enter: page %q message %q", m.page, m.message)
	}
	m, _ = settingsStep(t, m, ctrlR())
	m, _ = settingsStep(t, m, key("enter"))
	if saved, _ := readSettingsEnv(s.global); m.page != "metadata" || saved[wereadAPIKeyEnv] != "" {
		t.Fatalf("the key was not removed: page %q saved %v", m.page, saved)
	}
	if v := m.value(wereadAPIKeyEnv); v.Value != wereadKeyNotSet {
		t.Errorf("after removing: %+v", v)
	}
}
