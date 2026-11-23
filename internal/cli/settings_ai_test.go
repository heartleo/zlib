package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/aimatch"
	"github.com/heartleo/zlib/internal/config"
)

// aiKeyForTest is built at run time so no key-shaped literal sits in source.
var aiKeyForTest = strings.Repeat("k", 24)

// resolveAI runs the commands a step returned and feeds the answers back,
// skipping the spinner's ticks.
func resolveAI(m aiPageModel, cmd tea.Cmd) aiPageModel {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = resolveAI(m, c)
		}
	case aiModelsMsg, aiTestMsg:
		var next tea.Cmd
		m, next = m.update(msg)
		m = resolveAI(m, next)
	}
	return m
}

func aiStep(m aiPageModel, k tea.KeyPressMsg) (aiPageModel, tea.Cmd) { return m.update(k) }

func aiPress(t *testing.T, m aiPageModel, keys ...string) aiPageModel {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = aiStep(m, key(k))
		m = resolveAI(m, cmd)
	}
	return m
}

func aiType(m aiPageModel, text string) aiPageModel {
	for _, r := range text {
		m, _ = aiStep(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func aiPageFor(t *testing.T, s aimatch.Settings) aiPageModel {
	t.Helper()
	isolateConfigHome(t)
	if s.BaseURL != "" {
		if err := config.SaveAI(s); err != nil {
			t.Fatal(err)
		}
	}
	m := newAIPage(t.Context(), 100, 30)
	return m
}

func aiServer(t *testing.T, models ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			var data []map[string]string
			for _, id := range models {
				data = append(data, map[string]string{"id": id})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/v1/chat/completions":
			fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAIPageConfiguresTestsAndVerifies(t *testing.T) {
	srv := aiServer(t, "model-b", "model-a")
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest})

	// Model: discovery lists the endpoint's models; the filter narrows them.
	m.cursor = int(aiRowModel)
	m = aiPress(t, m, "enter")
	if m.mode != aiPick || len(m.pickAll) != 2 {
		t.Fatalf("mode=%v models=%v message=%q", m.mode, m.pickAll, m.message)
	}
	m = aiType(m, "model-a")
	m = aiPress(t, m, "enter")
	if m.s.Model != "model-a" {
		t.Fatalf("model = %q", m.s.Model)
	}
	if m.s.Verified() {
		t.Fatal("verified before any test")
	}

	// Test connection verifies exactly these settings and saves that.
	m.cursor = int(aiRowTest)
	m = aiPress(t, m, "enter")
	if !m.s.Verified() || !strings.Contains(m.message, "works") {
		t.Fatalf("verified=%v message=%q", m.s.Verified(), m.message)
	}
	saved, _ := config.LoadAI()
	if !saved.Verified() || saved.Model != "model-a" {
		t.Fatalf("saved = %v", saved)
	}

	// Any later edit makes AI Match unavailable again.
	m.cursor = int(aiRowSearch)
	m = aiPress(t, m, "enter")
	saved, _ = config.LoadAI()
	if saved.Verified() || !saved.WebSearch {
		t.Fatalf("toggling web search kept the verification: %v", saved)
	}
}

func TestAIPageTypedModelNameIsOfferedWhenNotListed(t *testing.T) {
	srv := aiServer(t, "listed")
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest})
	m.cursor = int(aiRowModel)
	m = aiPress(t, m, "enter")
	m = aiType(m, "my-private-model")
	labels, _ := m.pickRows()
	if len(labels) != 1 || labels[0] != `Use "my-private-model"` {
		t.Fatalf("labels = %v", labels)
	}
	m = aiPress(t, m, "enter")
	if m.s.Model != "my-private-model" {
		t.Fatalf("model = %q", m.s.Model)
	}
}

func TestAIPageModelListFailureStillAllowsAManualName(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler()) // no /models endpoint
	t.Cleanup(srv.Close)
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest})
	m.cursor = int(aiRowModel)
	m = aiPress(t, m, "enter")
	if m.mode != aiPick || !strings.Contains(m.message, "Type a model name") {
		t.Fatalf("mode=%v message=%q", m.mode, m.message)
	}
	m = aiType(m, "typed")
	m = aiPress(t, m, "enter")
	if m.s.Model != "typed" {
		t.Fatalf("model = %q", m.s.Model)
	}
}

func TestAIPageFailedTestLeavesAIMatchOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key `+aiKeyForTest+`"}}`)
	}))
	t.Cleanup(srv.Close)
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest, Model: "m"})
	m.cursor = int(aiRowTest)
	m = aiPress(t, m, "enter")
	if m.s.Verified() || !strings.Contains(m.message, "401") {
		t.Fatalf("verified=%v message=%q", m.s.Verified(), m.message)
	}
	if strings.Contains(m.message, aiKeyForTest) {
		t.Fatal("the key is in the message")
	}
}

func TestAIPageKeyIsMaskedEverywhereAndKeptOnEmptyEnter(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	for _, v := range []string{ansi.Strip(m.view().Content), settingsValueText(aiSettingValue())} {
		if strings.Contains(v, aiKeyForTest) {
			t.Fatalf("key shown: %q", v)
		}
	}
	m.cursor = int(aiRowKey)
	m = aiPress(t, m, "enter")
	if m.mode != aiEdit || m.input.Value() != "" {
		t.Fatalf("the editor must open empty: mode=%v value=%q", m.mode, m.input.Value())
	}
	if strings.Contains(ansi.Strip(m.view().Content), aiKeyForTest) {
		t.Fatal("key shown in the editor")
	}
	m = aiPress(t, m, "enter") // empty Enter must not delete the key
	if m.s.APIKey != aiKeyForTest || m.mode != aiEdit {
		t.Fatalf("empty Enter changed the key: mode=%v", m.mode)
	}
	m = aiType(m, "pasted")
	m = aiPress(t, m, "enter")
	if m.s.APIKey != "pasted" || strings.Contains(ansi.Strip(m.view().Content), "pasted") {
		t.Fatalf("key = %q", m.s.APIKey)
	}
	m.cursor = int(aiRowKey)
	m = aiPress(t, m, "enter")
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = aiPress(t, m, "enter")
	if m.s.APIKey != "" {
		t.Fatal("Ctrl+R then Enter did not remove the key")
	}
}

func TestAIPageRejectsAnInsecureBaseURL(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{})
	m.cursor = int(aiRowURL)
	m = aiPress(t, m, "enter")
	m = aiType(m, "http://api.example.com/v1")
	m = aiPress(t, m, "enter")
	if m.mode != aiEdit || m.s.BaseURL != "" || !strings.Contains(m.message, "https") {
		t.Fatalf("mode=%v url=%q message=%q", m.mode, m.s.BaseURL, m.message)
	}
}

func TestAIPageProviderPresetSetsEndpointAndClearsTheOldModelAndKey(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "old"})
	m.cursor = int(aiRowProvider)
	m = aiPress(t, m, "enter")
	m = aiType(m, "Anthropic")
	m = aiPress(t, m, "enter")
	if m.s.Protocol != aimatch.ProtocolAnthropic || m.s.BaseURL != "https://api.anthropic.com" || m.s.Model != "" || m.s.APIKey != "" {
		t.Fatalf("settings = %v", m.s)
	}
	if m.provider() != "Anthropic" {
		t.Fatalf("provider = %q", m.provider())
	}
}

func TestAIPageEscCancelsABusyTestAndLateAnswersAreDropped(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	m.busy, m.gen = true, 3
	m, _ = aiStep(m, key("esc"))
	if m.busy || m.done {
		t.Fatalf("busy=%v done=%v: Esc while busy cancels the work only", m.busy, m.done)
	}
	m, _ = m.update(aiTestMsg{gen: 3})
	if m.s.Verified() {
		t.Fatal("an answer to a cancelled test verified the settings")
	}
}

func TestAIPageEscLeavesAndRemoveNeedsConfirmation(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	m.cursor = int(aiRowRemove)
	m = aiPress(t, m, "enter", "n")
	if saved, _ := config.LoadAI(); !saved.Configured() {
		t.Fatal("settings removed without a yes")
	}
	m = aiPress(t, m, "enter", "y")
	if saved, _ := config.LoadAI(); saved.Configured() {
		t.Fatal("settings not removed")
	}
	m = aiPress(t, m, "esc")
	if !m.done {
		t.Fatal("Esc did not leave the page")
	}
}

func TestSettingsHomeOpensTheAIPageAndReturnsToIt(t *testing.T) {
	isolateConfigHome(t)
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "home")
	if err != nil {
		t.Fatal(err)
	}
	if v := m.value(aiModelKey); v.Value != "Not configured" {
		t.Fatalf("AI row = %+v", v)
	}
	m.cursor = indexOfKey(settingsPageKeys("home"), aiModelKey)
	next, _ := m.Update(key("enter"))
	m = next.(settingsModel)
	if m.page != "ai-page" || m.aiPage == nil {
		t.Fatalf("page = %q", m.page)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "AI model") {
		t.Fatal("the AI page is not drawn")
	}
	next, _ = m.Update(key("esc"))
	m = next.(settingsModel)
	if m.page != "home" || m.aiPage != nil {
		t.Fatalf("page = %q", m.page)
	}
}

func TestAIPageFitsCompactTerminals(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	for _, size := range [][2]int{{80, 24}, {60, 12}, {40, 8}} {
		m.width, m.height = size[0], size[1]
		m.resize()
		for line := range strings.SplitSeq(ansi.Strip(m.view().Content), "\n") {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, line)
			}
		}
	}
}

func settingsValueText(v settingValue) string { return v.Value + v.Saved + v.Source }

func indexOfKey(keys []string, key string) int {
	for i, k := range keys {
		if k == key {
			return i
		}
	}
	return -1
}
