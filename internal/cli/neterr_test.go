package cli

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// tlsTimeout mimics net/http's unexported TLS handshake timeout error.
type tlsTimeout struct{}

func (tlsTimeout) Error() string   { return "net/http: TLS handshake timeout" }
func (tlsTimeout) Timeout() bool   { return true }
func (tlsTimeout) Temporary() bool { return true }

func historyTLSError() error {
	return &url.Error{Op: "Get", URL: "https://z-lib.gd/eapi/user/book/downloaded?limit=50&page=1", Err: tlsTimeout{}}
}

func TestDescribeNetworkErrorHidesRawError(t *testing.T) {
	f, ok := describeNetworkError(historyTLSError())
	if !ok || f.host != "z-lib.gd" || !strings.Contains(f.reason, "TLS handshake") {
		t.Fatalf("describe = %+v ok=%v", f, ok)
	}
	for _, canSwitch := range []bool{true, false} {
		body := ansi.Strip(networkErrorBody(f, 80, canSwitch))
		flat := strings.Join(strings.Fields(body), " ") // wrapping may split a phrase
		for _, leaked := range []string{"https://", "/eapi/", "limit=50", "Get \""} {
			if strings.Contains(body, leaked) {
				t.Fatalf("raw error leaked %q:\n%s", leaked, body)
			}
		}
		for _, want := range []string{"Can't reach z-lib.gd", "may be blocked or down", "net/http: TLS handshake timeout"} {
			if !strings.Contains(flat, want) {
				t.Fatalf("missing %q:\n%s", want, body)
			}
		}
		if canSwitch != strings.Contains(body, "Press d") || canSwitch == strings.Contains(body, "zlib config domain") {
			t.Fatalf("recovery hint wrong for canSwitch=%v:\n%s", canSwitch, body)
		}
	}
}

func TestDescribeNetworkErrorOnlyMatchesConnectionProblems(t *testing.T) {
	for name, err := range map[string]error{
		"session":  zlib.ErrSessionExpired,
		"login":    errNotLoggedIn,
		"cancel":   errCancelled,
		"context":  context.Canceled,
		"server":   errors.New("book not found"),
		"download": zlib.ErrDownloadFailed,
	} {
		if isNetworkError(err) {
			t.Fatalf("%s misclassified as a connection problem", name)
		}
	}
	for name, err := range map[string]error{
		"bot":     zlib.ErrBotProtection,
		"refused": errors.New("dial tcp: connection refused"),
		"dns":     errors.New("dial tcp: lookup z-lib.gd: no such host"),
		"timeout": &url.Error{Op: "Get", URL: "https://z-lib.gd/x", Err: context.DeadlineExceeded},
	} {
		if !isNetworkError(err) {
			t.Fatalf("%s should be a connection problem", name)
		}
	}
}

func TestHistoryConnectionErrorOffersDomainSwitch(t *testing.T) {
	m := historySelectModel{err: historyTLSError(), width: 100, height: 24, inShell: true}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Download history", "Connection problem", "Can't reach z-lib.gd", "d change domain", "r retry", "esc back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Could not load history") || strings.Contains(view, "https://") {
		t.Fatalf("raw error shown:\n%s", view)
	}

	next, cmd := m.Update(key("d"))
	got := next.(historySelectModel)
	if !got.switchDomain || cmd == nil {
		t.Fatal("d must ask the shell to switch domain")
	}
	if resume, wanted := got.domainSwitchRequest(); resume != "/history" || !wanted {
		t.Fatalf("resume = %q wanted=%v", resume, wanted)
	}

	// Outside the shell nothing can open the domain page: name the CLI command.
	m.inShell = false
	view = ansi.Strip(m.View().Content)
	if strings.Contains(view, "d change domain") || !strings.Contains(view, "zlib config domain") {
		t.Fatalf("standalone view should point to the CLI command:\n%s", view)
	}
	if next, _ := m.Update(key("d")); next.(historySelectModel).switchDomain {
		t.Fatal("d must not switch domain outside the shell")
	}

	// Other failures keep their own message and no domain key.
	m.inShell, m.err = true, errors.New("history unavailable")
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Could not load history") || strings.Contains(view, "change domain") {
		t.Fatalf("non-network error view:\n%s", view)
	}
	if next, _ := m.Update(key("d")); next.(historySelectModel).switchDomain {
		t.Fatal("d must only act on connection problems")
	}
}

func noBooks() ([]zlib.Book, error) { return nil, nil }

func TestSearchAndFeedListsOfferDomainSwitch(t *testing.T) {
	for name, tc := range map[string]struct {
		model  searchSelectModel
		resume string
	}{
		"search":      {searchSelectModel{query: "dune"}, "/search dune"},
		"favorites":   {newSavedBooksSelectModel(func(int) (zlib.SearchResult, error) { return zlib.SearchResult{}, nil }), "/favorites"},
		"recommended": {newBookFeedSelectModel("Recommended", noBooks), "/recommended"},
		"popular":     {newBookFeedSelectModel("Most popular", noBooks), "/most-popular"},
	} {
		m := tc.model
		m.err, m.width, m.height, m.inShell, m.retryPage = historyTLSError(), 100, 24, true, 1
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "d change domain") || strings.Contains(view, "https://") {
			t.Fatalf("%s view:\n%s", name, view)
		}
		next, _ := m.Update(key("d"))
		resume, wanted := next.(searchSelectModel).domainSwitchRequest()
		if !wanted || resume != tc.resume {
			t.Fatalf("%s resume = %q wanted=%v, want %q", name, resume, wanted, tc.resume)
		}
	}
	// A list no command can recreate must not promise a domain switch.
	m := searchSelectModel{feed: func() ([]zlib.Book, error) { return nil, nil }, feedTitle: "Custom", err: historyTLSError(), width: 100, height: 24, inShell: true}
	if strings.Contains(ansi.Strip(m.View().Content), "change domain") {
		t.Fatal("unresumable list offered a domain switch")
	}
}

func TestProfileConnectionErrorOffersDomainSwitch(t *testing.T) {
	m := newProfilePageModel(nil)
	m.width, m.height, m.inShell = 100, 24, true
	next, _ := m.Update(profileLoadedMsg{err: historyTLSError()})
	m = next.(profilePageModel)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Can't reach z-lib.gd") || !strings.Contains(view, "d change domain") || strings.Contains(view, "https://") {
		t.Fatalf("profile error view:\n%s", view)
	}
	next, _ = m.Update(key("d"))
	if resume, wanted := next.(profilePageModel).domainSwitchRequest(); resume != "/profile" || !wanted {
		t.Fatal("profile did not request a domain switch")
	}
}

func TestShellErrorTextWordsConnectionProblems(t *testing.T) {
	text := shellErrorText(historyTLSError())
	if !strings.Contains(text, "Can't reach z-lib.gd") || !strings.Contains(text, "/config domain") ||
		strings.Contains(text, "https://") || strings.Contains(text, "eapi") {
		t.Fatalf("shell text = %q", text)
	}
}

// Choosing "change domain" opens the domain page, and closing it reruns the
// command that failed, so the user lands back on the page they left.
func TestShellDomainSwitchOpensDomainPageThenResumes(t *testing.T) {
	isolateShellSession(t)
	m := newShellModel()
	m.signedIn = true
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.open("list", historySelectModel{err: historyTLSError(), switchDomain: true})

	m.completed()
	if m.kind != "config" || m.resume != "/history" {
		t.Fatalf("after d: kind=%q resume=%q", m.kind, m.resume)
	}
	if _, ok := m.page.(settingsModel); !ok {
		t.Fatalf("domain page not opened: %T", m.page)
	}

	m.completed()
	if m.resume != "" || m.kind == "config" {
		t.Fatalf("domain page did not resume the command: kind=%q resume=%q", m.kind, m.resume)
	}
}

func TestShellDownloadConnectionErrorIsFriendly(t *testing.T) {
	isolateShellSession(t)
	m := newShellModel()
	m.signedIn = true
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.page, m.kind = downloadModel{bookName: "Dune", err: historyTLSError()}, "download"
	m.completed()
	view := strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
	for _, want := range []string{"Can't reach z-lib.gd", "TLS handshake", "/config domain", "r retry"} {
		if !strings.Contains(view, want) {
			t.Fatalf("download error view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "https://") || strings.Contains(view, "Download failed") {
		t.Fatalf("raw download error shown:\n%s", view)
	}
}

// Regression: after picking a domain, Esc on the reopened page looped back into
// the domain page, because the failed page (still flagged switchDomain) was kept
// as m.previous and restored.
func TestShellEscAfterDomainSwitchReturnsHome(t *testing.T) {
	path := isolateShellSession(t)
	session := `{"cookies":{"remix_userid":"1","remix_userkey":"fixture"},"domain":"https://z-lib.gd"}`
	if err := os.WriteFile(path, []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zlib.EnvDomain, "")
	m := newShellModel()
	m.signedIn = true
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.open("list", historySelectModel{err: historyTLSError(), switchDomain: true})

	m.completed() // d: open the domain page
	if m.previous != nil {
		t.Fatalf("failed page kept for restore: %T", m.previous)
	}
	m.completed() // domain page closed: the command reruns
	if _, ok := m.page.(historySelectModel); !ok || m.kind != "list" {
		t.Fatalf("history did not reopen: kind=%q page=%T status=%q", m.kind, m.page, m.status)
	}
	if m.previous != nil {
		t.Fatalf("stale page kept as previous after resume: %T", m.previous)
	}

	// The reopened history fails again and the user presses Esc.
	page := m.page.(historySelectModel)
	page.err = historyTLSError()
	m.page = page
	m.completed()
	if m.page != nil || m.kind != "" {
		t.Fatalf("Esc did not return to the main page: kind=%q page=%T", m.kind, m.page)
	}
	if s, ok := m.page.(domainSwitcher); ok {
		if _, wanted := s.domainSwitchRequest(); wanted {
			t.Fatal("a stale page still asks for the domain page")
		}
	}
}

func TestShellResumeIsDroppedWhenAnotherPageOpens(t *testing.T) {
	isolateShellSession(t)
	m := newShellModel()
	m.resume = "/history"
	m.open("theme", newThemePicker("", ""))
	if m.resume != "" {
		t.Fatal("stale resume survived opening another page")
	}
}
