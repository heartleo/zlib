package cli

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// captureConfiguredSources replaces the source builder with one that records
// which sources the command line resolved for the run.
func captureConfiguredSources(t *testing.T, got *[]string) {
	t.Helper()
	old := newMetaSources
	newMetaSources = func(http.RoundTripper) []metasource.Source {
		*got = configuredMetaSources()
		return []metasource.Source{&fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}}}
	}
	t.Cleanup(func() { newMetaSources = old })
}

// --source decides the sources for one run and beats ZLIB_METADATA_SOURCES; the
// run after it goes back to the environment.
func TestMetaFetchSourceFlagBeatsTheEnvironmentForOneRun(t *testing.T) {
	var got []string
	captureConfiguredSources(t, &got)
	t.Setenv(metaSourcesEnv, "openlibrary")
	p := namedEPUB(t, "Before (Frank Herbert).epub")

	if _, _, err := runMetaWithStderr(t, p, "--fetch", "--json", "--source", "douban,weread"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"douban", "weread"}; !reflect.DeepEqual(got, want) {
		t.Errorf("--source resolved %v, want %v", got, want)
	}
	if _, _, err := runMetaWithStderr(t, p, "--fetch", "--json", "--source", "weread", "--source", "Google"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"google", "weread"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a repeated --source resolved %v, want %v", got, want)
	}
	if _, _, err := runMetaWithStderr(t, p, "--fetch", "--json"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"openlibrary"}; !reflect.DeepEqual(got, want) {
		t.Errorf("without --source the run used %v, want %v", got, want)
	}
	if metaSourcesFlag != "" {
		t.Errorf("the flag leaked past its run: %q", metaSourcesFlag)
	}
}

func TestMetaSourceFlagIsValidated(t *testing.T) {
	var got []string
	captureConfiguredSources(t, &got)
	interactiveMeta(t, false)
	p := namedEPUB(t, "Before (Frank Herbert).epub")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{p, "--source", "douban"}, "only applies with --fetch"},
		{[]string{p, "--fetch", "--source", "douban,duoban"}, `unknown source "duoban"`},
		{[]string{p, "--fetch", "--source", ""}, "at least one of"},
	} {
		_, _, err := runMetaWithStderr(t, tc.args...)
		if !isUsage(err) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want a usage error containing %q", tc.args, err, tc.want)
		}
		if got != nil {
			t.Errorf("%v: a lookup ran with %v", tc.args, got)
		}
	}
}

// With no file the picker opens, and --source rides along with --fetch only.
func TestMetaSourceFlagWorksWithThePicker(t *testing.T) {
	interactiveMeta(t, true)
	_, _, err := runMetaWithStderr(t, "--fetch", "--source", "douban")
	if err == nil || isUsage(err) || !strings.Contains(err.Error(), "requires a terminal") {
		t.Errorf("--fetch --source: err = %v, want the command to reach the interactive program", err)
	}
	if metaSourcesFlag != "" {
		t.Errorf("the flag leaked past its run: %q", metaSourcesFlag)
	}
	for _, args := range [][]string{{"--source", "douban"}, {"--fetch", "--source", "douban", "--json"}, {"--fetch", "--source", "douban", "-t", "x"}} {
		if _, _, err := runMetaWithStderr(t, args...); !isUsage(err) {
			t.Errorf("%v: err = %v, want a usage error (exit 2)", args, err)
		}
	}
}

// The prepare page starts from what --source chose.
func TestMetaSourceFlagSetsThePageDefaults(t *testing.T) {
	t.Setenv(metaSourcesEnv, "")
	metaSourcesFlag = "douban"
	t.Cleanup(func() { metaSourcesFlag = "" })
	if got := enabledMetaSources(defaultMetaSourceChoices()); !reflect.DeepEqual(got, []string{"douban"}) {
		t.Errorf("the page starts with %v", got)
	}
}

func TestSaveMetaSourcesWritesTheGlobalEnvAndAppliesIt(t *testing.T) {
	s := testSettingsStore(t)
	t.Setenv(metaSourcesEnv, "") // restored after the save below sets it
	if err := os.WriteFile(s.global, []byte("KEEP=me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	metaSourcesFlag = "weread"
	t.Cleanup(func() { metaSourcesFlag = "" })

	overridden, err := saveMetaSourcesTo(s, []string{"weread", "openlibrary"})
	if err != nil || overridden != "" {
		t.Fatalf("overridden = %q, err = %v", overridden, err)
	}
	saved, _ := readSettingsEnv(s.global)
	if saved[metaSourcesEnv] != "openlibrary,weread" || saved["KEEP"] != "me" {
		t.Errorf("saved = %v", saved)
	}
	if got := os.Getenv(metaSourcesEnv); got != "openlibrary,weread" {
		t.Errorf("this process did not pick the list up: %q", got)
	}
	if metaSourcesFlag != "" {
		t.Errorf("--source still overrides the saved list: %q", metaSourcesFlag)
	}
}

// A real environment variable or a .env in the working directory still wins:
// the list is saved, the process keeps what it had, and the caller is told.
func TestSaveMetaSourcesReportsWhatOverridesIt(t *testing.T) {
	t.Run("process environment", func(t *testing.T) {
		s := testSettingsStore(t)
		s.env[metaSourcesEnv] = "douban"
		t.Setenv(metaSourcesEnv, "douban")
		overridden, err := saveMetaSourcesTo(s, []string{"google"})
		if err != nil || overridden != "Process environment" {
			t.Fatalf("overridden = %q, err = %v", overridden, err)
		}
		if saved, _ := readSettingsEnv(s.global); saved[metaSourcesEnv] != "google" {
			t.Errorf("not saved: %v", saved)
		}
		if got := os.Getenv(metaSourcesEnv); got != "douban" {
			t.Errorf("the override was replaced: %q", got)
		}
	})
	t.Run("working directory", func(t *testing.T) {
		s := testSettingsStore(t)
		t.Setenv(metaSourcesEnv, "douban")
		if err := os.WriteFile(s.local, []byte(metaSourcesEnv+"=douban\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		overridden, err := saveMetaSourcesTo(s, []string{"google"})
		if err != nil || overridden != "Working-directory .env" {
			t.Fatalf("overridden = %q, err = %v", overridden, err)
		}
	})
}

// The saved list has a settings row that opens a checklist, like the default
// search formats, so it can be seen, changed and cleared without editing .env by
// hand: nothing ticked is the built-in default, by book language.
func TestSettingsPageEditsAndClearsTheLookupSources(t *testing.T) {
	s := testSettingsStore(t)
	t.Setenv(metaSourcesEnv, "")
	t.Setenv(googleBooksKeyEnv, "")
	t.Setenv(googleBooksKeyAliasEnv, "")
	t.Setenv(wereadAPIKeyEnv, "wrk-fake-key-0000")
	m, _ := newSettingsModel(t.Context(), s, "metadata")
	m.width, m.height = 80, 30
	if v := m.value(metaSourcesEnv); v.Value != metaSourcesByLanguage || v.Source != "Default" {
		t.Fatalf("default row = %+v", v)
	}
	press := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			next, _ := m.Update(key(k))
			m = next.(settingsModel)
		}
	}
	ticked := func() string { return m.sourcesValue() }

	m.edit(metaSourcesEnv)
	if m.page != "sources" || ticked() != "" {
		t.Fatalf("the row did not open an empty checklist: page %q, ticked %q", m.page, ticked())
	}
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Lookup sources", "[ ] Open Library", "[ ] Goodreads", "[ ] Douban", "[ ] WeRead", "By book language", "Space toggle"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the checklist lacks %q:\n%s", want, screen)
		}
	}
	// Tick Douban and WeRead (down three times to Douban, then once more), then save.
	press("down", "down", "down", "space", "down", "space")
	if ticked() != "douban,weread" || !strings.Contains(ansi.Strip(m.View().Content), "[✓] Douban") {
		t.Fatalf("ticked %q:\n%s", ticked(), ansi.Strip(m.View().Content))
	}
	press("enter")
	if m.page != "metadata" {
		t.Fatalf("saving did not return to the list: page %q", m.page)
	}
	if v := m.value(metaSourcesEnv); v.Value != "douban,weread" || v.Source != "Global configuration" || os.Getenv(metaSourcesEnv) != "douban,weread" {
		t.Fatalf("after saving: %+v, env %q", v, os.Getenv(metaSourcesEnv))
	}

	// Reopened, the saved list is ticked; Esc leaves it as it was.
	m.edit(metaSourcesEnv)
	if ticked() != "douban,weread" {
		t.Fatalf("the saved list is not ticked: %q", ticked())
	}
	press("space", "esc") // the cursor is on Open Library: tick it, then cancel
	if v := m.value(metaSourcesEnv); v.Value != "douban,weread" {
		t.Errorf("Esc saved the change: %+v", v)
	}

	// WeRead cannot be ticked without its key, but can be unticked while listed.
	t.Setenv(wereadAPIKeyEnv, "")
	m.edit(metaSourcesEnv)
	press("down", "down", "down", "down", "space")
	if ticked() != "douban" {
		t.Errorf("WeRead was not unticked: %q", ticked())
	}
	press("space")
	if ticked() != "douban" || !strings.Contains(m.message, "WeRead") {
		t.Errorf("WeRead was ticked without its key: %q (%q)", ticked(), m.message)
	}

	// Ctrl+A ticks what can run, again clears; nothing ticked saves the default.
	press("ctrl+a")
	if ticked() != "openlibrary,google,goodreads,douban" {
		t.Errorf("ctrl+a ticked %q", ticked())
	}
	press("ctrl+a", "enter")
	if v := m.value(metaSourcesEnv); v.Value != metaSourcesByLanguage || v.Source != "Default" || os.Getenv(metaSourcesEnv) != "" {
		t.Fatalf("after clearing: %+v, env %q", v, os.Getenv(metaSourcesEnv))
	}
	if saved, _ := readSettingsEnv(s.global); saved[metaSourcesEnv] != "" {
		t.Errorf("the cleared list is still saved: %v", saved)
	}
}

func TestMetaSourcesSettingIsValidatedAndHasADefault(t *testing.T) {
	if v, err := validateSetting(metaSourcesEnv, " Douban , openlibrary,douban "); err != nil || v != "openlibrary,douban" {
		t.Errorf("normalized = %q, %v", v, err)
	}
	if _, err := validateSetting(metaSourcesEnv, "openlibrary,librarything"); err == nil || !strings.Contains(err.Error(), "librarything") {
		t.Errorf("a typo was accepted: %v", err)
	}
	if v, err := validateSetting(metaSourcesEnv, "goodreads,openlibrary"); err != nil || v != "openlibrary,goodreads" {
		t.Errorf("goodreads = %q, %v", v, err)
	}
	if v, err := validateSetting(metaSourcesEnv, ""); err != nil || v != "" {
		t.Errorf("empty = %q, %v", v, err)
	}
	s := testSettingsStore(t)
	values, err := s.values()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if v.Key == metaSourcesEnv && (v.Value != metaSourcesByLanguage || v.Source != "Default") {
			t.Errorf("default row = %+v", v)
		}
	}
}
