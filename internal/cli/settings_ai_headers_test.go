package cli

import (
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

// gatewayNeedingHeader is an endpoint that answers 400 unless a session header
// is sent, like the gateway that motivated custom headers.
func gatewayNeedingHeader(t *testing.T, header string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(header) == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"Request is missing %s"}}`, header)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func headersPage(t *testing.T, srv *httptest.Server) aiPageModel {
	t.Helper()
	m := aiPageFor(t, configured(srv))
	m.cursor = int(actHeaders)
	return aiPress(t, m, "enter")
}

func TestAIHeadersRowIsLastOfTheConfigurationAndSaysNoneWhenEmpty(t *testing.T) {
	m := aiPageFor(t, configured(aiServer(t)))
	if actHeaders != actSearch+1 || actTest != actHeaders+1 {
		t.Fatal("the headers row must follow web search and come before the actions")
	}
	if screen := ansi.Strip(m.view().Content); !strings.Contains(screen, "Custom headers") || !strings.Contains(screen, "None (optional)") {
		t.Fatalf("no headers row:\n%s", screen)
	}
}

func TestAIAddingAHeaderFixesAGatewayThatNeedsOneAndTestsAtOnce(t *testing.T) {
	srv := gatewayNeedingHeader(t, "X-Session-Id")
	m := aiPageFor(t, configured(srv))
	m.cursor = int(actTest)
	m = aiPress(t, m, "enter")
	if m.s.Verified() || !strings.Contains(m.message, "missing") {
		t.Fatalf("the gateway should refuse without the header: verified=%v message=%q", m.s.Verified(), m.message)
	}

	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter")
	if m.mode != aiHeaders {
		t.Fatalf("mode = %v", m.mode)
	}
	m = aiPress(t, m, "enter") // the "Add a header" row
	if m.mode != aiEdit || m.field != fieldHeader {
		t.Fatalf("mode=%v field=%v", m.mode, m.field)
	}
	m = aiType(m, "X-Session-Id: "+strings.Repeat("s", 12))
	m = aiPress(t, m, "enter")
	if m.mode != aiHeaders || len(m.s.Headers) != 1 || !strings.Contains(m.message, "Saved") {
		t.Fatalf("mode=%v headers=%v message=%q", m.mode, m.s.Headers, m.message)
	}
	m = aiPress(t, m, "esc") // leaving the list tests again
	if !m.s.Verified() {
		t.Fatalf("not verified after adding the header: %q", m.message)
	}
	saved, _ := config.LoadAI()
	if len(saved.Headers) != 1 || !saved.Verified() {
		t.Fatalf("saved = %v", saved)
	}
}

func TestAIHeaderValuesAreNeverShown(t *testing.T) {
	value := strings.Repeat("v", 16)
	s := configured(aiServer(t))
	s.Headers = []aimatch.Header{{Name: "x-session-id", Value: value}}
	m := aiPageFor(t, s)
	summary := ansi.Strip(m.view().Content)
	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter")
	list := ansi.Strip(m.view().Content)
	m = aiPress(t, m, "enter") // edit the existing header
	edit := ansi.Strip(m.view().Content)
	for name, screen := range map[string]string{"summary": summary, "list": list, "editor": edit, "settings row": settingsValueText(aiSettingValue())} {
		if strings.Contains(screen, value) {
			t.Errorf("%s shows the header value:\n%s", name, screen)
		}
	}
	if !strings.Contains(list, "x-session-id") || !strings.Contains(list, "16 characters") {
		t.Fatalf("the list should name the header and hide its value:\n%s", list)
	}
	if m.input.Value() != "x-session-id: " {
		t.Fatalf("the editor opens with %q", m.input.Value())
	}
}

func TestAIEditingAHeaderWithoutAValueKeepsTheSavedOne(t *testing.T) {
	s := configured(aiServer(t))
	s.Headers = []aimatch.Header{{Name: "x-session-id", Value: "keep-this-value"}}
	m := aiPageFor(t, s)
	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter", "enter", "enter") // list, edit, save as it was
	if len(m.s.Headers) != 1 || m.s.Headers[0].Value != "keep-this-value" {
		t.Fatalf("headers = %v", m.s.Headers)
	}
	// A new value replaces it.
	m = aiPress(t, m, "enter")
	m = aiType(m, "new-value-123")
	m = aiPress(t, m, "enter")
	if m.s.Headers[0].Value != "new-value-123" {
		t.Fatalf("headers = %v", m.s.Headers)
	}
}

func TestAIHeadersCanBeDeletedByKeyOrByClearing(t *testing.T) {
	s := configured(aiServer(t))
	s.Headers = []aimatch.Header{{Name: "x-a", Value: "111111"}, {Name: "x-b", Value: "222222"}, {Name: "x-c", Value: "333333"}}
	m := aiPageFor(t, s)
	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter", "d") // delete x-a
	if len(m.s.Headers) != 2 || m.s.Headers[0].Name != "x-b" || !strings.Contains(m.message, "Removed x-a") {
		t.Fatalf("headers = %v message=%q", m.s.Headers, m.message)
	}
	// Edit x-b, clear the box with Ctrl+R, Enter: deleted.
	m = aiPress(t, m, "enter")
	m, _ = aiStep(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input.Value() != "" {
		t.Fatalf("Ctrl+R left %q in the box", m.input.Value())
	}
	m = aiPress(t, m, "enter")
	if len(m.s.Headers) != 1 || m.s.Headers[0].Name != "x-c" || m.mode != aiHeaders {
		t.Fatalf("headers = %v mode=%v", m.s.Headers, m.mode)
	}
}

func TestAIHeaderEditorRejectsWhatIsNotAHeader(t *testing.T) {
	m := headersPage(t, aiServer(t))
	m = aiPress(t, m, "enter")
	for line, want := range map[string]string{
		"no colon here":      "Name: value",
		"Authorization: abc": "cannot be overridden",
		"bad name: v":        "not a valid header name",
		"x-empty:":           "needs a value",
	} {
		m.input.SetValue(line)
		m = aiPress(t, m, "enter")
		if m.mode != aiEdit || !strings.Contains(m.message, want) || len(m.s.Headers) != 0 {
			t.Errorf("%q: mode=%v message=%q headers=%v", line, m.mode, m.message, m.s.Headers)
		}
	}
	m = aiPress(t, m, "esc")
	if m.mode != aiHeaders {
		t.Fatalf("Esc in the editor should return to the list, mode=%v", m.mode)
	}
}

func TestAIHeadersListFitsCompactTerminals(t *testing.T) {
	s := configured(aiServer(t))
	s.Headers = []aimatch.Header{{Name: "x-a-rather-long-header-name", Value: "111111"}}
	m := aiPageFor(t, s)
	m.cursor = int(actHeaders)
	m = aiPress(t, m, "enter")
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
