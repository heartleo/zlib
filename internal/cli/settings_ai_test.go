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

// aiPageFor opens the page over saved settings (none: the first-run setup).
func aiPageFor(t *testing.T, s aimatch.Settings) aiPageModel {
	t.Helper()
	isolateConfigHome(t)
	if s.BaseURL != "" {
		if err := config.SaveAI(s); err != nil {
			t.Fatal(err)
		}
	}
	return newAIPage(t.Context(), 100, 30)
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

func configured(srv *httptest.Server) aimatch.Settings {
	return aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest, Model: "m"}
}

// aiSetURL replaces the base URL the editor offers with url and presses Enter.
func aiSetURL(t *testing.T, m aiPageModel, url string) aiPageModel {
	t.Helper()
	m.input.SetValue("")
	return aiPress(t, aiType(m, url), "enter")
}

func TestAIFirstRunWalksThroughTheSetupAndVerifiesAtTheEnd(t *testing.T) {
	srv := aiServer(t, "model-b", "model-a")
	m := aiPageFor(t, aimatch.Settings{})
	if m.mode != aiPick || !m.wizard || m.field != fieldProtocol {
		t.Fatalf("first run must open on the protocol list: mode=%v wizard=%v", m.mode, m.wizard)
	}
	screen := ansi.Strip(m.view().Content)
	for _, want := range []string{"Step 1 of 4", "OpenAI-compatible", "Anthropic-compatible"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
	m = aiPress(t, m, "enter") // OpenAI-compatible
	if m.mode != aiEdit || m.field != fieldURL || m.input.Value() != "https://api.openai.com/v1" {
		t.Fatalf("the URL step must offer the protocol's own address: mode=%v value=%q", m.mode, m.input.Value())
	}
	m = aiSetURL(t, m, srv.URL+"/v1") // loopback: no key step, straight to the models
	if m.mode != aiPick || m.field != fieldModel || len(m.pickAll) != 2 {
		t.Fatalf("mode=%v field=%v models=%v message=%q", m.mode, m.field, m.pickAll, m.message)
	}
	if !strings.Contains(ansi.Strip(m.view().Content), "Step 3 of 3") {
		t.Fatalf("a local endpoint has no key step:\n%s", ansi.Strip(m.view().Content))
	}
	m = aiType(m, "model-a")
	m = aiPress(t, m, "enter") // choosing the last step tests the connection
	if m.wizard || !m.s.Verified() || !strings.Contains(m.message, "works") {
		t.Fatalf("wizard=%v verified=%v message=%q", m.wizard, m.s.Verified(), m.message)
	}
	if saved, _ := config.LoadAI(); !saved.Verified() || saved.Model != "model-a" {
		t.Fatalf("saved = %v", saved)
	}
}

func TestAIAnthropicProtocolOffersItsAddressThenAsksForTheKey(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{})
	m = aiType(m, "Anthropic")
	m = aiPress(t, m, "enter")
	if m.mode != aiEdit || m.field != fieldURL || m.input.Value() != "https://api.anthropic.com" || m.s.Protocol != aimatch.ProtocolAnthropic {
		t.Fatalf("mode=%v field=%v value=%q", m.mode, m.field, m.input.Value())
	}
	m = aiPress(t, m, "enter") // keep the official address
	if m.mode != aiEdit || m.field != fieldKey || !strings.Contains(ansi.Strip(m.view().Content), "Step 3 of 4") {
		t.Fatalf("mode=%v field=%v\n%s", m.mode, m.field, ansi.Strip(m.view().Content))
	}
}

func TestAIChangingTheURLDropsTheOldModelAndKeyAndAsksAgain(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "old"})
	if m.mode != aiRows {
		t.Fatalf("a configured page opens on the summary, mode=%v", m.mode)
	}
	m.cursor = int(actURL)
	m = aiPress(t, m, "enter")
	m = aiSetURL(t, m, "https://other.example.com/v1")
	if m.s.Model != "" || m.s.APIKey != "" || m.s.BaseURL != "https://other.example.com/v1" {
		t.Fatalf("settings = %v", m.s)
	}
	if m.mode != aiEdit || m.field != fieldKey || !m.wizard {
		t.Fatalf("the key step did not follow: mode=%v field=%v", m.mode, m.field)
	}
}

func TestAIChangingTheProtocolSwapsTheOfficialAddressButKeepsACustomOne(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: aimatch.DefaultBaseURL(aimatch.ProtocolOpenAI), APIKey: aiKeyForTest, Model: "m"})
	m.cursor = int(actProtocol)
	m = aiPress(t, aiType(aiPress(t, m, "enter"), "Anthropic"), "enter")
	if m.s.BaseURL != "https://api.anthropic.com" || m.s.Model != "" || m.s.APIKey != "" {
		t.Fatalf("settings = %v", m.s)
	}
	custom := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://gateway.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	custom.cursor = int(actProtocol)
	custom = aiPress(t, aiType(aiPress(t, custom, "enter"), "Anthropic"), "enter")
	if custom.s.BaseURL != "https://gateway.example.com/v1" || custom.s.Protocol != aimatch.ProtocolAnthropic {
		t.Fatalf("a custom address was replaced: %v", custom.s)
	}
}

func TestAIEscPausesTheSetupAndKeepsWhatWasSaved(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{})
	m = aiType(m, "Anthropic")
	m = aiPress(t, m, "enter", "esc") // Esc in the URL step
	if m.mode != aiRows || m.wizard || !strings.Contains(m.message, "paused") {
		t.Fatalf("mode=%v wizard=%v message=%q", m.mode, m.wizard, m.message)
	}
	if saved, _ := config.LoadAI(); saved.Protocol != aimatch.ProtocolAnthropic {
		t.Fatalf("the protocol step was not kept: %v", saved)
	}
	m = aiPress(t, m, "esc")
	if !m.done {
		t.Fatal("Esc on the summary did not leave")
	}
}

func TestAIChoosingAModelTestsAtOnce(t *testing.T) {
	srv := aiServer(t, "model-b", "model-a")
	s := configured(srv)
	s.Model = "old-model"
	m := aiPageFor(t, s)
	m.cursor = int(actModel)
	m = aiPress(t, m, "enter")
	if m.mode != aiPick || len(m.pickAll) != 2 {
		t.Fatalf("mode=%v models=%v message=%q", m.mode, m.pickAll, m.message)
	}
	m = aiType(m, "model-a")
	m = aiPress(t, m, "enter")
	if m.s.Model != "model-a" || !m.s.Verified() {
		t.Fatalf("model=%q verified=%v message=%q", m.s.Model, m.s.Verified(), m.message)
	}
}

func TestAIModelListStartsOnTheCurrentModel(t *testing.T) {
	srv := aiServer(t, "aaa", "current", "zzz")
	s := configured(srv)
	s.Model = "current"
	m := aiPageFor(t, s)
	m.cursor = int(actModel)
	m = aiPress(t, m, "enter")
	if m.pickCursor != 1 || !strings.Contains(ansi.Strip(m.view().Content), "current  ✓ current") {
		t.Fatalf("cursor=%d\n%s", m.pickCursor, ansi.Strip(m.view().Content))
	}
}

func TestAITypedModelNameIsOfferedWhenNotListed(t *testing.T) {
	srv := aiServer(t, "listed")
	m := aiPageFor(t, configured(srv))
	m.cursor = int(actModel)
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

func TestAIModelListFailureStillAllowsAManualName(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler()) // no /models endpoint
	t.Cleanup(srv.Close)
	m := aiPageFor(t, configured(&httptest.Server{URL: srv.URL}))
	m.cursor = int(actModel)
	m = aiPress(t, m, "enter")
	if m.mode != aiPick || !strings.Contains(m.message, "Type a model name") {
		t.Fatalf("mode=%v message=%q", m.mode, m.message)
	}
	m = aiType(m, "typed")
	m = aiPress(t, m, "enter")
	if m.s.Model != "typed" {
		t.Fatalf("model = %q", m.s.Model)
	}
	if m.s.Verified() {
		t.Fatal("verified although the test could not pass")
	}
}

func TestAIWebSearchToggleTestsAgainAndFailsWhereUnsupported(t *testing.T) {
	srv := aiServer(t) // chat works, the Responses route does not exist
	s := configured(srv)
	s.MarkVerified()
	m := aiPageFor(t, s)
	m.cursor = int(actSearch)
	m = aiPress(t, m, "enter")
	if !m.s.WebSearch || m.s.Verified() || !strings.Contains(m.message, "web search") {
		t.Fatalf("search=%v verified=%v message=%q", m.s.WebSearch, m.s.Verified(), m.message)
	}
	if saved, _ := config.LoadAI(); saved.Verified() {
		t.Fatal("a failed test left the old verification in place")
	}
	m = aiPress(t, m, "enter") // off again: the plain test passes
	if m.s.WebSearch || !m.s.Verified() {
		t.Fatalf("search=%v verified=%v message=%q", m.s.WebSearch, m.s.Verified(), m.message)
	}
}

func TestAIFailedTestLeavesAIMatchOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key `+aiKeyForTest+`"}}`)
	}))
	t.Cleanup(srv.Close)
	s := aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: aiKeyForTest, Model: "m"}
	m := aiPageFor(t, s)
	m.cursor = int(actTest)
	m = aiPress(t, m, "enter")
	if m.s.Verified() || !strings.Contains(m.message, "401") {
		t.Fatalf("verified=%v message=%q", m.s.Verified(), m.message)
	}
	if strings.Contains(m.message, aiKeyForTest) {
		t.Fatal("the key is in the message")
	}
}

func TestAIKeyIsMaskedEverywhereKeptOnEmptyEnterAndTestedWhenReplaced(t *testing.T) {
	srv := aiServer(t)
	m := aiPageFor(t, configured(srv))
	for _, v := range []string{ansi.Strip(m.view().Content), settingsValueText(aiSettingValue())} {
		if strings.Contains(v, aiKeyForTest) {
			t.Fatalf("key shown: %q", v)
		}
	}
	m.cursor = int(actKey)
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
	if !m.s.Verified() {
		t.Fatalf("replacing the key did not test again: %q", m.message)
	}
	m.cursor = int(actKey)
	m = aiPress(t, m, "enter")
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = aiPress(t, m, "enter")
	if m.s.APIKey != "" {
		t.Fatal("Ctrl+R then Enter did not remove the key")
	}
}

func TestAIRejectsAnInsecureBaseURL(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{})
	m = aiPress(t, m, "enter") // OpenAI-compatible
	m = aiSetURL(t, m, "http://api.example.com/v1")
	if m.mode != aiEdit || m.field != fieldURL || !strings.Contains(m.message, "https") {
		t.Fatalf("mode=%v url=%q message=%q", m.mode, m.s.BaseURL, m.message)
	}
}

func TestAIEscCancelsABusyTestAndLateAnswersAreDropped(t *testing.T) {
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

func TestAIEscLeavesAndRemoveNeedsConfirmation(t *testing.T) {
	m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: "https://api.example.com/v1", APIKey: aiKeyForTest, Model: "m"})
	m.cursor = int(actRemove)
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
	next, _ = m.Update(key("esc")) // pauses the first-run setup
	m = next.(settingsModel)
	next, _ = m.Update(key("esc"))
	m = next.(settingsModel)
	if m.page != "home" || m.aiPage != nil {
		t.Fatalf("page = %q", m.page)
	}
}

func TestAIPagesFitCompactTerminals(t *testing.T) {
	srv := aiServer(t, "model-a")
	pages := map[string]aiPageModel{
		"summary":  aiPageFor(t, configured(srv)),
		"protocol": aiPageFor(t, aimatch.Settings{}),
	}
	key := aiPageFor(t, aimatch.Settings{})
	key = aiPress(t, aiType(key, "Anthropic"), "enter", "enter")
	pages["key step"] = key
	for name, m := range pages {
		for _, size := range [][2]int{{80, 24}, {60, 12}, {40, 8}} {
			m.width, m.height = size[0], size[1]
			m.resize()
			for line := range strings.SplitSeq(ansi.Strip(m.view().Content), "\n") {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%s %dx%d: line is %d wide: %q", name, size[0], size[1], w, line)
				}
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

func TestAICtrlRClearsTheBaseURLAndTheKeyInOneKey(t *testing.T) {
	srv := aiServer(t)
	m := aiPageFor(t, configured(srv))
	// Base URL: the box empties, and an empty address is not saved.
	m.cursor = int(actURL)
	m = aiPress(t, m, "enter")
	if m.input.Value() == "" {
		t.Fatal("the editor should open on the current address")
	}
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input.Value() != "" || m.mode != aiEdit {
		t.Fatalf("value=%q mode=%v", m.input.Value(), m.mode)
	}
	m = aiPress(t, m, "enter")
	if m.mode != aiEdit || !strings.Contains(m.message, "required") || m.s.BaseURL == "" {
		t.Fatalf("an empty address was accepted: mode=%v message=%q url=%q", m.mode, m.message, m.s.BaseURL)
	}
	m = aiPress(t, m, "esc")
	// API key: Ctrl+R then Enter removes it at once.
	m.cursor = int(actKey)
	m = aiPress(t, m, "enter")
	m = aiType(m, "typed")
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input.Value() != "" {
		t.Fatalf("the key box was not emptied: %q", m.input.Value())
	}
	if !strings.Contains(ansi.Strip(m.view().Content), "ctrl+r clear") {
		t.Fatalf("the footer does not offer the clear key:\n%s", ansi.Strip(m.view().Content))
	}
}

func TestAIEditorShowsAFocusedBoxAndAVisibleCursor(t *testing.T) {
	m := aiPageFor(t, configured(aiServer(t)))
	m.cursor = int(actKey)
	m = aiPress(t, m, "enter")
	if !m.input.VirtualCursor() || !m.input.Focused() {
		t.Fatalf("virtual cursor=%v focused=%v", m.input.VirtualCursor(), m.input.Focused())
	}
	if screen := ansi.Strip(m.view().Content); !strings.Contains(screen, "╭") || !strings.Contains(screen, "> ") {
		t.Fatalf("no framed input:\n%s", screen)
	}
	// A short terminal drops the frame but keeps the input.
	m.height = 10
	if screen := ansi.Strip(m.view().Content); strings.Contains(screen, "╭") || !strings.Contains(screen, "> ") {
		t.Fatalf("compact view:\n%s", screen)
	}
}

func TestAIDefaultModelIsSuggestedWhenNoneIsSet(t *testing.T) {
	// Without a model the page opens the setup; the protocol and address are
	// kept (same protocol), so two Enters reach the model step.
	toModelStep := func(base string) aiPageModel {
		m := aiPageFor(t, aimatch.Settings{Protocol: aimatch.ProtocolOpenAI, BaseURL: base + "/v1", APIKey: aiKeyForTest})
		m = aiPress(t, m, "enter", "enter")
		if m.mode != aiPick || m.field != fieldModel {
			t.Fatalf("not on the model step: mode=%v field=%v", m.mode, m.field)
		}
		return m
	}

	// Listed: the cursor starts on it, and Enter takes it.
	srv := aiServer(t, "aaa", aimatch.DefaultModel, "zzz")
	m := toModelStep(srv.URL)
	_, values := m.pickRows()
	if values[m.pickCursor] != aimatch.DefaultModel {
		t.Fatalf("cursor on %q, want %q", values[m.pickCursor], aimatch.DefaultModel)
	}
	if m = aiPress(t, m, "enter"); m.s.Model != aimatch.DefaultModel {
		t.Fatalf("model = %q", m.s.Model)
	}

	// No model list at all: the box is prefilled, so Enter takes it as well.
	bad := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(bad.Close)
	m = toModelStep(bad.URL)
	if m.input.Value() != aimatch.DefaultModel {
		t.Fatalf("the default model is not suggested: value=%q message=%q", m.input.Value(), m.message)
	}
	if m = aiPress(t, m, "enter"); m.s.Model != aimatch.DefaultModel {
		t.Fatalf("model = %q", m.s.Model)
	}
}
