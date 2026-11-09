package cli

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func domainPage(t *testing.T, mirrors ...string) settingsModel {
	t.Helper()
	isolateMirrors(t, mirrors...)
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "domain")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 40
	return m
}

func settingsStep(t *testing.T, m settingsModel, msg tea.Msg) (settingsModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(settingsModel), cmd
}

func typeText(t *testing.T, m settingsModel, text string) settingsModel {
	t.Helper()
	for _, r := range text {
		m, _ = settingsStep(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func healthy(origin string) zlib.DomainCheck {
	return zlib.DomainCheck{Domain: origin, Status: zlib.DomainStatusHealthy, FinalURL: origin + "/eapi/info/domains"}
}

func TestDomainPageListsMirrorsFirstWhileChecking(t *testing.T) {
	m := domainPage(t, "https://mirror.example")

	if m.domains[0] != "https://mirror.example" || !slices.Contains(m.domains, "https://z-lib.gd") {
		t.Fatalf("domains = %v, want the mirror first, then built-ins", m.domains)
	}
	m, _ = settingsStep(t, m, settingsStartProbeMsg{}) // the check starts on open
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"YOUR MIRRORS", "BUILT-IN", "mirror.example", "Checking domains… 0 of "} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "Checking"); n != 1 {
		t.Errorf("one loading indicator expected, got %d:\n%s", n, view)
	}
	if strings.Index(view, "mirror.example") > strings.Index(view, "BUILT-IN") {
		t.Error("mirror is not listed above the built-in section")
	}
	if _, ok := m.Init()().(settingsStartProbeMsg); !ok {
		t.Error("domain page does not start its check on open")
	}
}

// Each row shows its result the moment that domain answers; the others stay
// blank, and one footer animation covers the rest.
func TestDomainPageShowsResultsAsTheyArrive(t *testing.T) {
	m := domainPage(t, "https://mirror.example")
	m.busy = true
	total := len(m.domains)
	var next tea.Cmd
	followed := false
	m, next = settingsStep(t, m, settingsDomainCheckMsg{
		generation: m.generation, domain: "https://mirror.example", check: healthy("https://mirror.example"),
		next: func() tea.Msg { followed = true; return nil },
	})
	if next == nil || next() != nil || !followed {
		t.Fatal("the page did not continue reading results")
	}
	view := ansi.Strip(m.View().Content)
	if !regexp.MustCompile(`mirror\.example\s+✓`).MatchString(view) {
		t.Errorf("finished row missing its result:\n%s", view)
	}
	if want := fmt.Sprintf("Checking domains… 1 of %d", total); !strings.Contains(view, want) {
		t.Errorf("progress %q missing:\n%s", want, view)
	}
	if strings.Count(view, "Checking") != 1 || strings.Contains(view, "✗") {
		t.Errorf("pending rows must stay quiet:\n%s", view)
	}

	// A late result from an earlier check must not touch the page.
	m, _ = settingsStep(t, m, settingsDomainCheckMsg{generation: m.generation - 1, domain: "https://z-lib.gd", check: healthy("https://z-lib.gd")})
	if _, ok := m.checks["https://z-lib.gd"]; ok {
		t.Error("a stale result was applied")
	}
}

func TestDomainPageWithoutMirrorsInvitesAdding(t *testing.T) {
	m := domainPage(t)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "None yet · press a") {
		t.Errorf("view lacks the empty-mirrors hint:\n%s", view)
	}
}

func TestDomainPageShowsEveryResultAndUsesOnlyAvailableRows(t *testing.T) {
	m := domainPage(t, "https://mirror.example", "https://hop.example")
	t.Setenv(zlib.EnvDomain, "")
	checks := map[string]zlib.DomainCheck{}
	for _, d := range m.domains {
		checks[d] = zlib.DomainCheck{Domain: d, Status: zlib.DomainStatusDiamWallBlocked}
	}
	checks["https://mirror.example"] = healthy("https://mirror.example")
	checks["https://hop.example"] = zlib.DomainCheck{Domain: "https://hop.example", Status: zlib.DomainStatusHealthy, FinalURL: "https://zlib.bz/eapi/info/domains"}
	m, _ = settingsStep(t, m, settingsProbeMsg{generation: m.generation, checks: checks})

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"✓", "✗", "✓ via zlib.bz", "2 of "} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	m.cursor = slices.Index(m.domains, "https://z-lib.gd")
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "domain" || !strings.Contains(m.message, "is unavailable") {
		t.Fatalf("enter on a blocked row: page %q, message %q", m.page, m.message)
	}

	m.cursor = slices.Index(m.domains, "https://hop.example")
	m, _ = settingsStep(t, m, key("enter"))
	if got := m.value(zlib.EnvDomain).Value; got != "https://zlib.bz" {
		t.Errorf("saved domain = %q, want the redirect target https://zlib.bz", got)
	}
}

func TestDomainPageEnterWhileUncheckedExplains(t *testing.T) {
	m := domainPage(t)
	m, _ = settingsStep(t, m, key("enter"))
	if m.page != "domain" || !strings.Contains(m.message, "Still checking") {
		t.Errorf("page %q, message %q", m.page, m.message)
	}
}

func TestDomainPageAddMirrorChecksThenAsksForTrust(t *testing.T) {
	m := domainPage(t)
	calls := stubVerifyMirror(t, nil)

	m.busy = true // a domain check is still running
	m, _ = settingsStep(t, m, key("a"))
	if m.page != "mirror" || m.busy {
		t.Fatalf("a opened %q (busy %v), want an idle mirror form", m.page, m.busy)
	}
	m = typeText(t, m, "Mirror.Example")
	m, cmd := settingsStep(t, m, key("enter"))
	if !m.busy || cmd == nil {
		t.Fatal("enter did not start the check")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Checking Mirror.Example") {
		t.Errorf("view lacks the checking status:\n%s", view)
	}
	m, _ = settingsStep(t, m, cmd().(tea.BatchMsg)[1]())
	if want := []string{"https://mirror.example"}; !slices.Equal(*calls, want) {
		t.Fatalf("verified %v, want %v", *calls, want)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Trust this mirror?") || !strings.Contains(view, "login") {
		t.Fatalf("no trust confirmation:\n%s", view)
	}
	if len(savedMirrors(t)) != 0 {
		t.Fatal("mirror saved before confirmation")
	}

	m, _ = settingsStep(t, m, key("y"))
	if want := []string{"https://mirror.example"}; !slices.Equal(savedMirrors(t), want) {
		t.Fatalf("saved %v, want %v", savedMirrors(t), want)
	}
	if m.page != "domain" || m.domains[m.cursor] != "https://mirror.example" {
		t.Errorf("page %q, cursor on %q; want the new mirror selected", m.page, m.domains[m.cursor])
	}
	if label, ok := domainStatus(m.domains[m.cursor], m.checks[m.domains[m.cursor]], true); !ok {
		t.Errorf("new mirror reads %q, want available from its check", label)
	}
}

func TestDomainPageAddMirrorCancelAndRejections(t *testing.T) {
	m := domainPage(t, "https://mirror.example")
	calls := stubVerifyMirror(t, nil)
	m, _ = settingsStep(t, m, key("a"))

	for input, want := range map[string]string{"z-lib.gd": "built in", "mirror.example": "already one of your mirrors", "http://x.example": "HTTPS"} {
		m.input.SetValue("")
		m = typeText(t, m, input)
		m, _ = settingsStep(t, m, key("enter"))
		if !strings.Contains(m.message, want) {
			t.Errorf("%q: message %q, want %q", input, m.message, want)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("checked %v for rejected input", *calls)
	}

	m.input.SetValue("")
	m = typeText(t, m, "new.example")
	m, cmd := settingsStep(t, m, key("enter"))
	m, _ = settingsStep(t, m, cmd().(tea.BatchMsg)[1]())
	m, _ = settingsStep(t, m, key("esc"))
	if m.mirrorConfirm != "" || m.page != "mirror" {
		t.Fatalf("esc in the dialog: confirm %q, page %q", m.mirrorConfirm, m.page)
	}
	if slices.Contains(savedMirrors(t), "https://new.example") {
		t.Fatal("declined mirror saved")
	}
	m, _ = settingsStep(t, m, key("esc"))
	if m.page != "domain" {
		t.Errorf("esc on the form returned to %q, want domain", m.page)
	}
}

func TestDomainPageAddMirrorShowsFailedCheck(t *testing.T) {
	m := domainPage(t)
	stubVerifyMirror(t, errors.New("https://new.example answered, but not with the Z-Library EAPI"))
	m, _ = settingsStep(t, m, key("a"))
	m = typeText(t, m, "new.example")
	m, cmd := settingsStep(t, m, key("enter"))
	m, _ = settingsStep(t, m, cmd().(tea.BatchMsg)[1]())
	if m.busy || m.mirrorConfirm != "" || !strings.Contains(m.message, "not with the Z-Library EAPI") {
		t.Errorf("busy %v, confirm %q, message %q", m.busy, m.mirrorConfirm, m.message)
	}
}

func TestDomainPageRemoveMirror(t *testing.T) {
	m := domainPage(t, "https://mirror.example")

	m.cursor = slices.Index(m.domains, "https://z-lib.gd")
	m, _ = settingsStep(t, m, key("d"))
	if m.mirrorConfirm != "" || !strings.Contains(m.message, "cannot be removed") {
		t.Fatalf("d on a built-in: confirm %q, message %q", m.mirrorConfirm, m.message)
	}

	m.cursor = 0
	m, _ = settingsStep(t, m, key("d"))
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Remove this mirror?") {
		t.Fatalf("no remove confirmation:\n%s", view)
	}
	m, _ = settingsStep(t, m, key("enter")) // Cancel has the focus.
	if len(savedMirrors(t)) != 1 {
		t.Fatal("enter on Cancel removed the mirror")
	}

	m, _ = settingsStep(t, m, key("d"))
	m, _ = settingsStep(t, m, key("y"))
	if len(savedMirrors(t)) != 0 || slices.Contains(m.domains, "https://mirror.example") {
		t.Errorf("saved %v, listed %v after removal", savedMirrors(t), m.domains)
	}
	if !strings.Contains(m.message, "Removed mirror.example") {
		t.Errorf("message %q", m.message)
	}
}

func TestDomainPageFitsSmallTerminals(t *testing.T) {
	m := domainPage(t, "https://a-rather-long-personal-mirror-name.example")
	for _, size := range [][2]int{{40, 12}, {60, 16}, {100, 9}} {
		m.width, m.height = size[0], size[1]
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("line exceeds width %d at %dx%d: %q", ansi.StringWidth(line), size[0], size[1], ansi.Strip(line))
			}
		}
	}
}

func TestDomainPageSortsAvailableFirstWithinEachSection(t *testing.T) {
	m := domainPage(t, "https://down.example", "https://up.example")
	m.cursor = slices.Index(m.domains, "https://z-lib.gd")
	checks := map[string]zlib.DomainCheck{}
	for _, d := range m.domains {
		checks[d] = zlib.DomainCheck{Domain: d, Status: zlib.DomainStatusNetworkError}
	}
	checks["https://up.example"] = healthy("https://up.example")
	checks["https://zlib.bz"] = healthy("https://zlib.bz")
	m, _ = settingsStep(t, m, settingsProbeMsg{generation: m.generation, checks: checks})

	if want := []string{"https://up.example", "https://down.example", "https://zlib.bz"}; !slices.Equal(m.domains[:3], want) {
		t.Errorf("first rows = %v, want %v", m.domains[:3], want)
	}
	if m.domains[m.cursor] != "https://z-lib.gd" {
		t.Errorf("cursor moved to %q, want it to stay on z-lib.gd", m.domains[m.cursor])
	}
	view := ansi.Strip(m.View().Content)
	if strings.Index(view, "down.example") > strings.Index(view, "BUILT-IN") {
		t.Error("an unavailable mirror dropped below the built-in section")
	}
}

func TestDomainPageMirrorFormKeepsParentCursor(t *testing.T) {
	isolateMirrors(t)
	m, err := newSettingsModel(t.Context(), testSettingsStore(t), "network")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 40
	m, _ = settingsStep(t, m, key("enter")) // Domain row opens the domain page.
	if m.page != "domain" {
		t.Fatalf("enter opened %q, want domain", m.page)
	}
	m.cursor = 3
	m, _ = settingsStep(t, m, key("a"))
	m, _ = settingsStep(t, m, key("esc"))
	if m.page != "domain" || m.cursor != 3 {
		t.Fatalf("closing the form: page %q cursor %d, want domain row 3", m.page, m.cursor)
	}
	m, _ = settingsStep(t, m, key("esc"))
	if m.page != "network" || m.cursor != 0 {
		t.Fatalf("leaving the domain page: page %q cursor %d, want network row 0", m.page, m.cursor)
	}
}
