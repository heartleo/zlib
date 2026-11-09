package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func TestSettingsCommandRoutesAndJSON(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(t.TempDir())
	previous := settingsEnvironment
	settingsEnvironment = map[string]string{zlib.EnvProxy: "http://reader:secret@localhost:7890"}
	t.Cleanup(func() { settingsEnvironment = previous })
	for _, tc := range []struct {
		args  []string
		count int
	}{
		{[]string{"config"}, 11}, {[]string{"config", "network"}, 2},
		{[]string{"config", "network", "domain"}, 1}, {[]string{"config", "network", "proxy"}, 1},
		{[]string{"config", "download"}, 2}, {[]string{"config", "download", "directory"}, 1},
		{[]string{"config", "download", "retries"}, 1},
		{[]string{"config", "kindle"}, 5}, {[]string{"config", "kindle", "to"}, 1},
		{[]string{"config", "kindle", "from"}, 1}, {[]string{"config", "kindle", "host"}, 1},
		{[]string{"config", "kindle", "port"}, 1}, {[]string{"config", "kindle", "security"}, 1},
		{[]string{"config", "weread"}, 2}, {[]string{"config", "weread", "account"}, 1},
		{[]string{"config", "weread", "proxy"}, 1},
	} {
		out, err := executeRootForTest(t, append(tc.args, "--json")...)
		if err != nil {
			t.Fatal(err)
		}
		var values []settingValue
		if err = json.Unmarshal([]byte(out), &values); err != nil || len(values) != tc.count {
			t.Fatalf("%v: %s, %v", tc.args, out, err)
		}
		if strings.Contains(out, "secret") || strings.Contains(out, "reader") {
			t.Fatal("credentials leaked")
		}
	}
	configCmd, _, _ := rootCmd.Find([]string{"config"})
	configCmd.PersistentFlags().Set("json", "false")
	configCmd.PersistentFlags().Lookup("json").Changed = false
}

func TestSettingsSaveValidationAndLiveReload(t *testing.T) {
	s := testSettingsStore(t)
	t.Setenv(zlib.EnvRetries, "")
	m, _ := newSettingsModel(t.Context(), s, "download")
	m.edit(zlib.EnvRetries)
	m.input.SetValue("-1")
	next, _ := m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || !strings.Contains(m.message, "non-negative") {
		t.Fatal("invalid value saved")
	}
	m.input.SetValue("5")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.value(zlib.EnvRetries).Value != "5" || os.Getenv(zlib.EnvRetries) != "5" {
		t.Fatal("saved setting not active")
	}
	m.edit(zlib.EnvRetries)
	m.input.SetValue("")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.value(zlib.EnvRetries).Value != "3" || os.Getenv(zlib.EnvRetries) != "" {
		t.Fatal("reset failed")
	}
	if err := s.save(zlib.EnvDomain, "https://example.com"); err == nil {
		t.Fatal("untrusted domain saved")
	}
	if err := s.save(downloadDirEnvVar, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("nonexistent directory saved")
	}
}

func TestSettingsSaveFailureKeepsDraft(t *testing.T) {
	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "download")
	m.edit(zlib.EnvRetries)
	m.input.SetValue("8")
	if err := os.Mkdir(s.global, 0700); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || m.input.Value() != "8" || !strings.Contains(m.message, "Could not save") {
		t.Fatal("failed save lost draft")
	}
}

func testSettingsStore(t *testing.T) settingsStore {
	t.Helper()
	dir := t.TempDir()
	return settingsStore{global: filepath.Join(dir, "global.env"), local: filepath.Join(dir, "local.env"), kindle: filepath.Join(dir, "kindle.json"), env: map[string]string{}}
}

func TestSettingsSaveRoundTripAndReset(t *testing.T) {
	s := testSettingsStore(t)
	original := "# retain comment\nUNRELATED=keep\nZLIB_PROXY=old\nZLIB_PROXY=duplicate\n"
	if err := os.WriteFile(s.global, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	proxy := "http://user:p%40ss@localhost:7890"
	if err := s.save(zlib.EnvProxy, proxy); err != nil {
		t.Fatal(err)
	}
	v, err := readSettingsEnv(s.global)
	if err != nil {
		t.Fatal(err)
	}
	if v[zlib.EnvProxy] != proxy || v["UNRELATED"] != "keep" {
		t.Fatalf("round trip failed")
	}
	data, _ := os.ReadFile(s.global)
	if !strings.Contains(string(data), "# retain comment") || strings.Count(string(data), "ZLIB_PROXY=") != 1 {
		t.Fatal("comment lost or duplicate remained")
	}
	dir := filepath.Join(t.TempDir(), "spaces $dollar")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.save(downloadDirEnvVar, dir); err != nil {
		t.Fatal(err)
	}
	v, err = readSettingsEnv(s.global)
	if err != nil || v[downloadDirEnvVar] != dir {
		t.Fatalf("path round trip: %q, %v", v[downloadDirEnvVar], err)
	}
	if err := s.save(zlib.EnvProxy, ""); err != nil {
		t.Fatal(err)
	}
	v, _ = readSettingsEnv(s.global)
	if _, ok := v[zlib.EnvProxy]; ok {
		t.Fatal("reset did not remove key")
	}
}

func TestSettingsSaveSMTPPasswordWithoutChangingOtherKeys(t *testing.T) {
	s := testSettingsStore(t)
	if err := os.WriteFile(s.global, []byte("# keep this\nZLIB_PROXY=http://localhost:7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	password := ` pass$word\"value `
	if err := s.save(kindleSMTPPasswordEnv, password); err != nil {
		t.Fatal(err)
	}
	values, err := readSettingsEnv(s.global)
	if err != nil || values[kindleSMTPPasswordEnv] != password || values[zlib.EnvProxy] != "http://localhost:7890" {
		t.Fatal("SMTP password did not round trip with other settings intact")
	}
	data, err := os.ReadFile(s.global)
	if err != nil || !strings.Contains(string(data), "# keep this") {
		t.Fatal("saving SMTP password removed an unrelated comment")
	}
	if err := s.save(kindleSMTPPasswordEnv, " \t "); err == nil {
		t.Fatal("blank SMTP password was accepted")
	}
}

func TestSettingsSourceAndRedaction(t *testing.T) {
	s := testSettingsStore(t)
	if err := s.save(zlib.EnvRetries, "2"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(s.local, []byte("ZLIB_DOWNLOAD_RETRIES=4\n"), 0600)
	s.env[zlib.EnvRetries] = "5"
	values, err := s.values()
	if err != nil {
		t.Fatal(err)
	}
	if values[3].Value != "5" || values[3].Saved != "2" || values[3].Source != "Process environment" {
		t.Fatalf("precedence: %+v", values[3])
	}
	delete(s.env, zlib.EnvRetries)
	values, _ = s.values()
	if values[3].Source != "Working-directory .env" {
		t.Fatal("local source lost")
	}
	if strings.Contains(displaySetting(zlib.EnvProxy, "http://user:password@localhost:7890"), "password") {
		t.Fatal("proxy password exposed")
	}
}

func TestSettingsNavigationCancelAndViewport(t *testing.T) {
	for _, page := range []string{"home", "network", "download", "kindle", "domain"} {
		for _, size := range [][2]int{{80, 24}, {40, 16}, {32, 12}, {20, 5}} {
			m, err := newSettingsModel(t.Context(), testSettingsStore(t), page)
			if err != nil {
				t.Fatal(err)
			}
			m.width, m.height = size[0], size[1]
			for _, line := range strings.Split(m.View().Content, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("view too wide")
				}
			}
			if len(strings.Split(m.View().Content, "\n")) > size[1] {
				t.Fatal("view too tall")
			}
		}
	}
	s := testSettingsStore(t)
	m, _ := newSettingsModel(t.Context(), s, "download")
	m.edit(zlib.EnvRetries)
	m.input.SetValue("7")
	next, _ := m.Update(key("esc"))
	m = next.(settingsModel)
	if !m.discard {
		t.Fatal("dirty form exited without confirmation")
	}
	next, _ = m.Update(key("y"))
	m = next.(settingsModel)
	if _, err := os.Stat(s.global); !os.IsNotExist(err) {
		t.Fatal("cancel wrote settings")
	}
	if m.page != "download" {
		t.Fatal("cancel did not return to parent")
	}
}

func TestSettingsHomeShowsEveryFieldAndReturnsToSelection(t *testing.T) {
	s := testSettingsStore(t)
	s.env[zlib.EnvProxy] = "http://reader:secret@localhost:7890"
	m, err := newSettingsModel(t.Context(), s, "home")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {40, 16}, {32, 12}} {
		m.width, m.height = size[0], size[1]
		view := ansi.Strip(m.View().Content)
		labels := []string{"NETWORK", "Domain", "Proxy URL", "DOWNLOADS", "Default directory", "Download retries", "KINDLE", "Kindle address", "From address", "SMTP host", "SMTP port", "SMTP security"}
		if size[1] < 20 {
			labels = labels[:3]
		}
		for _, label := range labels {
			if !strings.Contains(view, label) {
				t.Fatalf("%v: missing %q", size, label)
			}
		}
		if strings.Contains(view, "Saved to:") || strings.Contains(view, "Source:") {
			t.Fatal("settings overview has redundant status")
		}
		if strings.Contains(view, "reader") || strings.Contains(view, "secret") {
			t.Fatal("proxy credentials visible on settings home")
		}
	}
	next, _ := m.Update(key("down"))
	m = next.(settingsModel)
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || m.key != zlib.EnvProxy {
		t.Fatal("proxy row did not open its editor")
	}
	next, _ = m.Update(key("esc"))
	m = next.(settingsModel)
	if m.page != "home" || m.cursor != 1 {
		t.Fatal("editor did not return to the selected home row")
	}
	next, _ = m.Update(key("home"))
	m = next.(settingsModel)
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "domain" {
		t.Fatal("domain row did not open the domain picker")
	}
	next, _ = m.Update(key("esc"))
	m = next.(settingsModel)
	if m.page != "home" || m.cursor != 0 {
		t.Fatal("domain picker did not return to settings home")
	}
}

func TestSettingsKindleSaveAndNavigation(t *testing.T) {
	s := testSettingsStore(t)
	m, err := newSettingsModel(t.Context(), s, "home")
	if err != nil {
		t.Fatal(err)
	}
	m.cursor = 4
	next, _ := m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || m.key != kindleToKey {
		t.Fatal("Kindle row did not open")
	}
	m.input.SetValue("reader@kindle.com")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "home" || m.cursor != 4 || m.value(kindleToKey).Value != "reader@kindle.com" {
		t.Fatal("Kindle save did not return to selection")
	}
	data, err := os.ReadFile(s.kindle)
	if err != nil || !strings.Contains(string(data), "reader@kindle.com") || strings.Contains(string(data), "ZLIB_SMTP_PWD") {
		t.Fatalf("Kindle config was not saved safely: %v", err)
	}
	m.edit(kindlePortKey)
	m.input.SetValue("70000")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || !strings.Contains(m.message, "1 to 65535") {
		t.Fatal("invalid SMTP port was accepted")
	}
	m.input.SetValue("465")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.value(kindlePortKey).Value != "465" {
		t.Fatal("SMTP port not saved")
	}
	if v := m.value(kindleSecurityKey); v.Value != zlib.SMTPSecurityTLS || v.Source != "Default" {
		t.Fatalf("port 465 should imply tls by default, got %+v", v)
	}
	// smtp.163.com documents 994 as an SSL/TLS port; IANA does not, so the
	// mode has to be set explicitly.
	m.edit(kindlePortKey)
	m.input.SetValue("994")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	m.edit(kindleSecurityKey)
	m.input.SetValue("ssl")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "edit" || !strings.Contains(m.message, "starttls") {
		t.Fatal("invalid SMTP security was accepted")
	}
	m.input.SetValue("TLS")
	next, _ = m.Update(key("enter"))
	m = next.(settingsModel)
	if v := m.value(kindleSecurityKey); v.Value != zlib.SMTPSecurityTLS || v.Saved != zlib.SMTPSecurityTLS {
		t.Fatalf("SMTP security not saved: %+v", v)
	}
	loaded, err := newSettingsModel(t.Context(), s, "kindle")
	if err != nil || loaded.value(kindleToKey).Value != "reader@kindle.com" || loaded.value(kindlePortKey).Value != "994" || loaded.value(kindleSecurityKey).Value != zlib.SMTPSecurityTLS {
		t.Fatal("Kindle values did not reload")
	}
	data, err = os.ReadFile(s.kindle)
	if err != nil || !strings.Contains(string(data), `"smtp_security": "tls"`) {
		t.Fatalf("smtp_security not written to kindle.json: %s, %v", data, err)
	}
}

func TestSettingsStaleProbeAndDirectorySelection(t *testing.T) {
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "domain")
	m.generation = 2
	next, _ := m.Update(settingsProbeMsg{generation: 1, checks: map[string]zlib.DomainCheck{"stale": {}}})
	m = next.(settingsModel)
	if len(m.checks) != 0 {
		t.Fatal("stale probe updated page")
	}
	m.edit(downloadDirEnvVar)
	dir := t.TempDir()
	m.browse(dir)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = next.(settingsModel)
	if m.page != "edit" || m.input.Value() != dir {
		t.Fatal("directory not returned to editor")
	}
	if _, err := os.Stat(m.store.global); !os.IsNotExist(err) {
		t.Fatal("browse saved prematurely")
	}
}

func TestSettingsEditorsFitAndButtonsSave(t *testing.T) {
	for _, key := range []string{zlib.EnvProxy, downloadDirEnvVar, zlib.EnvRetries} {
		for _, size := range [][2]int{{32, 12}, {40, 16}, {60, 20}, {80, 24}} {
			m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "home")
			m.width, m.height = size[0], size[1]
			m.edit(key)
			view := m.View().Content
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("editor exceeds height")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("editor exceeds width: %q", line)
				}
			}
		}
	}
	t.Setenv(zlib.EnvRetries, "")
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "download")
	m.edit(zlib.EnvRetries)
	m.input.SetValue("2")
	for range 3 {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(settingsModel)
	}
	if m.editAction != 4 {
		t.Fatal("Tab did not reach Save")
	}
	next, _ := m.Update(key("enter"))
	m = next.(settingsModel)
	if m.value(zlib.EnvRetries).Saved != "2" {
		t.Fatal("Save button failed")
	}
}

func TestSettingsProxyEditorShowsURL(t *testing.T) {
	const proxy = "http://reader:sample@localhost:7890"
	s := testSettingsStore(t)
	s.env[zlib.EnvProxy] = proxy
	m, err := newSettingsModel(t.Context(), s, "home")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ansi.Strip(m.View().Content), "sample") {
		t.Fatal("settings overview exposed proxy credentials")
	}
	m.edit(zlib.EnvProxy)
	if m.input.EchoMode != textinput.EchoNormal {
		t.Fatal("proxy URL input is masked")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, proxy) {
		t.Fatal("proxy URL is not visible in its editor")
	}
	for _, size := range [][2]int{{32, 12}, {40, 16}} {
		m.width, m.height = size[0], size[1]
		m.input.SetWidth(max(1, min(56, m.width-ansi.StringWidth(m.input.Prompt)-4)))
		m.editAction = 4
		view := m.View().Content
		if !strings.Contains(ansi.Strip(view), "Save") {
			t.Fatalf("save action hidden at %dx%d", size[0], size[1])
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("editor exceeds width at %dx%d", size[0], size[1])
			}
		}
	}
}

func TestSettingsProbeFailureAndCancel(t *testing.T) {
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "domain")
	cmd := m.probe("http://127.0.0.1:1", "domain")
	batch := cmd().(tea.BatchMsg)
	// Results stream one by one; follow each next command until the final message.
	msg := batch[1]()
	for {
		next, follow := m.Update(msg)
		m = next.(settingsModel)
		if _, done := msg.(settingsProbeMsg); done || follow == nil {
			break
		}
		msg = follow()
	}
	view := m.View().Content
	if m.busy || len(m.domains) == 0 || !strings.Contains(view, "0 of ") || !strings.Contains(view, "✗") {
		t.Fatal("failed probe has no recovery state")
	}
	m.probe("http://127.0.0.1:1", "domain")
	old := m.generation
	m.back()
	next, _ := m.Update(settingsProbeMsg{generation: old, checks: map[string]zlib.DomainCheck{"stale": {}}})
	m = next.(settingsModel)
	if _, stale := m.checks["stale"]; m.page != "network" || stale {
		t.Fatal("cancelled probe updated page")
	}
}
