package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbles/v2/textinput"
	bubbletea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/metasource"
)

// secretKey has the shape of a Google API key but is not one.
const secretKey = "AIzaSecretKeyNotReal0123456789abcdefg"

func ctrlR() bubbletea.KeyPressMsg { return bubbletea.KeyPressMsg{Code: 'r', Mod: bubbletea.ModCtrl} }

func TestGoogleKeyIsMaskedInJSONAndOnScreen(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(t.TempDir())
	previous := settingsEnvironment
	settingsEnvironment = map[string]string{googleBooksKeyEnv: secretKey}
	t.Cleanup(func() { settingsEnvironment = previous })

	out, err := executeRootForTest(t, "config", "metadata", "google-key", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, secretKey) || strings.Contains(out, "AIza") {
		t.Fatalf("the key is in the JSON output: %s", out)
	}
	var values []settingValue
	if err := json.Unmarshal([]byte(out), &values); err != nil || len(values) != 1 ||
		values[0].Value != "Configured (hidden)" || values[0].Source != "Process environment" {
		t.Fatalf("values = %+v, err = %v", values, err)
	}

	m, _ := newSettingsModel(t.Context(), settingsStore{global: t.TempDir() + "/g.env", local: t.TempDir() + "/l.env", kindle: t.TempDir() + "/k.json", env: settingsEnvironment}, "metadata")
	m.width, m.height = 80, 40 // tall enough that the home list is not cut off
	for _, page := range []string{"metadata", "home"} {
		m.page = page
		if view := ansi.Strip(m.View().Content); strings.Contains(view, secretKey) || !strings.Contains(view, "Google Books key") {
			t.Fatalf("%s page:\n%s", page, view)
		}
	}
	m = cursorTo(t, m, googleBooksKeyEnv)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Configured (hidden)") {
		t.Fatalf("the key row does not say a key is configured:\n%s", view)
	}
}

func TestGoogleKeyRowWithoutAKeySaysSo(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(googleBooksKeyEnv, "")
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "metadata")
	if v := m.value(googleBooksKeyEnv); v.Value != googleKeyNotSet || v.Source != "Default" {
		t.Fatalf("value = %+v", v)
	}
	if got := displaySetting(googleBooksKeyEnv, googleKeyNotSet); got != googleKeyNotSet {
		t.Fatalf("display = %q", got)
	}
}

func TestGoogleKeyValidationNeverEchoesTheValue(t *testing.T) {
	if v, err := validateSetting(googleBooksKeyEnv, "  "+secretKey+"\t"); err != nil || v != secretKey {
		t.Fatalf("a valid key was refused or not trimmed: %q, %v", v, err)
	}
	if v, err := validateSetting(googleBooksKeyEnv, ""); err != nil || v != "" {
		t.Fatalf("empty removes the key: %q, %v", v, err)
	}
	for _, bad := range []string{"short", "has space in the middle 0123456789", "bad$chars0123456789012345", secretKey + "!"} {
		_, err := validateSetting(googleBooksKeyEnv, bad)
		if err == nil || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
	if _, err := validateSetting(googleBooksKeyEnv, secretKey+"\nINJECTED=1"); err == nil {
		t.Error("a multi-line value was accepted")
	}
}

func TestGoogleKeyEditorIsMaskedEmptyAndSafeToEnter(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(googleBooksKeyEnv, "") // restores the process environment after the save below
	s := testSettingsStore(t)
	if err := s.save(googleBooksKeyEnv, secretKey); err != nil {
		t.Fatal(err)
	}
	m, _ := newSettingsModel(t.Context(), s, "metadata")
	m = cursorTo(t, m, googleBooksKeyEnv)

	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || m.key != googleBooksKeyEnv {
		t.Fatalf("page = %q", m.page)
	}
	if m.input.EchoMode != tea.EchoPassword || m.input.Value() != "" {
		t.Fatalf("the editor shows or prefills the key: echo = %v, value = %q", m.input.EchoMode, m.input.Value())
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, secretKey) || !strings.Contains(view, "A key is set (hidden)") || !strings.Contains(view, "Google Books API key") {
		t.Fatalf("editor:\n%s", view)
	}

	// Enter on the empty box must not delete the saved key.
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || !strings.Contains(m.message, "Ctrl+R") {
		t.Fatalf("page = %q, message = %q", m.page, m.message)
	}
	if saved, _ := readSettingsEnv(s.global); saved[googleBooksKeyEnv] != secretKey {
		t.Fatalf("an empty Enter changed the saved key: %v", saved)
	}

	// Typing something invalid is refused and the saved key stays.
	m.input.SetValue("nope")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || !strings.Contains(m.message, "20 to 128") || strings.Contains(m.message, "nope") {
		t.Fatalf("page = %q, message = %q", m.page, m.message)
	}
	if saved, _ := readSettingsEnv(s.global); saved[googleBooksKeyEnv] != secretKey {
		t.Fatalf("an invalid key replaced the saved one: %v", saved)
	}
}

func TestGoogleKeyEditorSavesReplacesAndRemoves(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(googleBooksKeyEnv, "")
	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "metadata")
	m = cursorTo(t, m, googleBooksKeyEnv)

	// Save a key.
	m, _ = settingsStep(t, m, key("enter"))
	m.input.SetValue(secretKey)
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "metadata" || m.message != "Saved" {
		t.Fatalf("page = %q, message = %q", m.page, m.message)
	}
	data, _ := os.ReadFile(s.global)
	if !strings.Contains(string(data), googleBooksKeyEnv+`="`+secretKey+`"`) {
		t.Fatalf("saved .env = %q", data)
	}
	if os.Getenv(googleBooksKeyEnv) != secretKey {
		t.Fatal("the saved key did not apply to this process")
	}
	if view := ansi.Strip(m.View().Content); strings.Contains(view, secretKey) || !strings.Contains(view, "Configured (hidden)") {
		t.Fatalf("after saving:\n%s", view)
	}
	if info, err := os.Stat(s.global); err == nil && info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("the .env holding a key is readable by others: %v", info.Mode())
	}

	// Remove it: Ctrl+R then Enter. Typing after Ctrl+R cancels the removal.
	m, _ = settingsStep(t, m, key("enter"))
	m, _ = settingsStep(t, m, ctrlR())
	if !m.resetSecret || !strings.Contains(m.message, "removal selected") {
		t.Fatalf("resetSecret = %v, message = %q", m.resetSecret, m.message)
	}
	m, _ = settingsStep(t, m, key("x"))
	if m.resetSecret {
		t.Fatal("typing did not cancel the pending removal")
	}
	m.input.SetValue("")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" {
		t.Fatalf("an empty Enter left the editor: page = %q", m.page)
	}
	m, _ = settingsStep(t, m, ctrlR())
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "metadata" || m.message != "Saved" {
		t.Fatalf("page = %q, message = %q", m.page, m.message)
	}
	if saved, _ := readSettingsEnv(s.global); saved[googleBooksKeyEnv] != "" {
		t.Fatalf("the key was not removed: %v", saved)
	}
	if v := m.value(googleBooksKeyEnv); v.Value != googleKeyNotSet {
		t.Fatalf("value after removal = %+v", v)
	}
}

// captureRT answers every request with an empty Google Books result and keeps
// the requests, so a test can see what was sent without a network.
type captureRT struct{ reqs []*http.Request }

func (c *captureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.reqs = append(c.reqs, r)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}, Request: r}, nil
}

func googleSource(t *testing.T, rt http.RoundTripper) metasource.Source {
	t.Helper()
	t.Setenv(metaSourcesEnv, "google") // Google is not a default without a key
	for _, s := range newMetaSources(rt) {
		if s.Name() == "Google Books" {
			return s
		}
	}
	t.Fatal("no Google Books source")
	return nil
}

func TestLookupsUseTheConfiguredGoogleKey(t *testing.T) {
	q := metasource.Query{Title: "Dune"}

	t.Setenv(googleBooksKeyEnv, "  "+secretKey+"\n")
	rt := &captureRT{}
	if _, err := googleSource(t, rt).Identify(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if len(rt.reqs) != 1 || rt.reqs[0].Header.Get("X-Goog-Api-Key") != secretKey || strings.Contains(rt.reqs[0].URL.String(), secretKey) {
		t.Fatalf("requests = %+v", rt.reqs)
	}

	t.Setenv(googleBooksKeyEnv, "")
	rt = &captureRT{}
	if _, err := googleSource(t, rt).Identify(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if len(rt.reqs) != 1 || rt.reqs[0].Header.Get("X-Goog-Api-Key") != "" {
		t.Fatalf("a key header was sent without a key: %+v", rt.reqs[0].Header)
	}

	// A hand-edited, malformed value fails loudly and sends nothing.
	t.Setenv(googleBooksKeyEnv, "not a key")
	rt = &captureRT{}
	if _, err := googleSource(t, rt).Identify(context.Background(), q); !errors.Is(err, metasource.ErrKeyMalformed) || len(rt.reqs) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(rt.reqs))
	}
}

func TestRejectedKeyErrorsPointToTheSetting(t *testing.T) {
	for _, err := range []error{metasource.ErrKeyRejected, metasource.ErrKeyMalformed} {
		got := metaSourceError(metasource.SourceError{Source: "Google Books", Err: err})
		if !strings.Contains(got, "Google Books: ") || !strings.Contains(got, "zlib config metadata") || strings.Contains(got, "AIza") {
			t.Errorf("%v: %q", err, got)
		}
	}
	if got := metaSourceError(metasource.SourceError{Source: "Google Books", Err: metasource.ErrRateLimited}); strings.Contains(got, "zlib config") {
		t.Errorf("a plain rate limit should not send the user to settings: %q", got)
	}
}

func TestGoogleKeyRowIsReachableOnAStandardTerminal(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(googleBooksKeyEnv, "")
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "home")
	m.width, m.height = 80, 24
	m = cursorTo(t, m, googleBooksKeyEnv)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "> Google Books key") || !strings.Contains(view, googleKeyNotSet) {
		t.Fatalf("the key row is not visible when selected at 80x24:\n%s", view)
	}
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "edit" || m.key != googleBooksKeyEnv {
		t.Fatalf("Enter did not open the editor from the home page: page = %q", m.page)
	}
}
