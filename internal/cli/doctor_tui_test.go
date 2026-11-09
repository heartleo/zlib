package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func doctorAvailable(domain string) zlib.DomainCheck {
	return zlib.DomainCheck{
		Domain: domain, Status: zlib.DomainStatusHealthy,
		FinalURL: domain + "/eapi/info/domains",
	}
}

func TestDoctorPageListsOnlyAvailableDomains(t *testing.T) {
	m := newDoctorPageModel([]string{"one", "two", "three"}, "", "https://z-lib.gd", nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(doctorPageModel)
	next, _ = m.Update(doctorResultMsg{sequence: m.sequence, checks: []zlib.DomainCheck{
		doctorAvailable("https://z-lib.gd"),
		{Domain: "https://z-lib.sk", Status: zlib.DomainStatusChallenged},
		doctorAvailable("https://z-lib.gl"),
	}})
	m = next.(doctorPageModel)
	if len(m.list.Items()) != 2 || m.loading {
		t.Fatal("doctor did not keep only available domains")
	}
	view := m.View()
	text := ansi.Strip(view.Content)
	if !view.AltScreen ||
		!strings.Contains(text, "https://z-lib.gd  · Current domain") ||
		!strings.Contains(text, "│ https://z-lib.gd") ||
		!strings.Contains(text, "z-lib.gd") || !strings.Contains(text, "z-lib.gl") || strings.Contains(text, "z-lib.sk") {
		t.Fatalf("doctor page displayed incorrect domains: %q", text)
	}
	if strings.Contains(text, "Available ·") || strings.Contains(text, " available") || strings.Contains(text, " selected") {
		t.Fatalf("doctor repeats availability on each row: %q", text)
	}
	if len(strings.Split(view.Content, "\n")) != 24 {
		t.Fatal("doctor page does not fill the terminal")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(doctorPageModel)
	if m.list.Index() != 1 {
		t.Fatal("doctor list did not move its selection")
	}
	text = ansi.Strip(m.View().Content)
	if !strings.Contains(text, "│ https://z-lib.gl") || !strings.Contains(text, "https://z-lib.gd  · Current domain") {
		t.Fatalf("doctor cursor changed the current domain marker: %q", text)
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 35, Height: 14})
	m = next.(doctorPageModel)
	for _, line := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(line) > 35 {
			t.Fatalf("doctor exceeds compact terminal width: %q", line)
		}
	}
}

func TestDoctorPageRetryRejectsOldResultsAndCancelsOnExit(t *testing.T) {
	m := newDoctorPageModel([]string{"https://z-lib.gd"}, "", "", nil)
	oldContext, oldSequence := m.ctx, m.sequence
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = next.(doctorPageModel)
	if cmd == nil || oldContext.Err() == nil || m.sequence == oldSequence || !m.loading {
		t.Fatal("retry did not cancel the old probe and start a new one")
	}
	next, _ = m.Update(doctorResultMsg{sequence: oldSequence, checks: []zlib.DomainCheck{doctorAvailable("https://z-lib.gd")}})
	m = next.(doctorPageModel)
	if len(m.list.Items()) != 0 || !m.loading {
		t.Fatal("stale probe result changed the active page")
	}
	next, _ = m.Update(doctorResultMsg{sequence: m.sequence})
	m = next.(doctorPageModel)
	if !strings.Contains(ansi.Strip(m.View().Content), "No available EAPI domains") {
		t.Fatal("empty result did not explain how to retry")
	}
	currentContext := m.ctx
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(doctorPageModel)
	if cmd == nil || currentContext.Err() == nil {
		t.Fatal("leaving doctor did not cancel its probe context")
	}
}

func TestDoctorPageInitiallyFocusesCurrentDomain(t *testing.T) {
	m := newDoctorPageModel(nil, "", "https://z-lib.gl", nil)
	defer m.stop()
	next, _ := m.Update(doctorResultMsg{sequence: m.sequence, checks: []zlib.DomainCheck{
		doctorAvailable("https://z-lib.gd"), doctorAvailable("https://z-lib.gl"),
	}})
	m = next.(doctorPageModel)
	if m.list.Index() != 1 {
		t.Fatal("current domain did not receive initial focus")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = next.(doctorPageModel)
	if m.list.Index() != 0 || m.current != "https://z-lib.gl" {
		t.Fatal("moving focus changed the configured domain")
	}
}

func TestDoctorPageScrollsLongDomainList(t *testing.T) {
	m := newDoctorPageModel(nil, "", "", nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m = next.(doctorPageModel)
	checks := make([]zlib.DomainCheck, 12)
	for i := range checks {
		checks[i] = doctorAvailable(fmt.Sprintf("https://%d.z-lib.gd", i))
	}
	next, _ = m.Update(doctorResultMsg{sequence: m.sequence, checks: checks})
	m = next.(doctorPageModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = next.(doctorPageModel)
	if m.list.Index() == 0 || !strings.Contains(ansi.Strip(m.View().Content), checks[m.list.Index()].Domain) {
		t.Fatal("doctor list did not page through available domains")
	}
}

// Available domains join the list the moment they answer, in candidate order,
// under one loading animation; unavailable ones never appear.
func TestDoctorPageShowsDomainsAsTheyAnswer(t *testing.T) {
	candidates := []string{"https://z-lib.gd", "https://z-lib.sk", "https://z-lib.gl", "https://2.z-lib.gd"}
	m := newDoctorPageModel(candidates, "", "https://z-lib.gl", nil)
	defer m.stop()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(doctorPageModel)
	step := func(index int, check zlib.DomainCheck) {
		t.Helper()
		next, _ := m.Update(doctorCheckMsg{sequence: m.sequence, index: index, check: check, next: func() tea.Msg { return nil }})
		m = next.(doctorPageModel)
	}
	text := func() string { return ansi.Strip(m.View().Content) }

	if v := text(); !strings.Contains(v, "Checking domains… 0 of 4") {
		t.Fatalf("no loading animation before the first result:\n%s", v)
	}
	step(3, doctorAvailable("https://2.z-lib.gd")) // answers first
	step(1, zlib.DomainCheck{Domain: "https://z-lib.sk", Status: zlib.DomainStatusChallenged})
	step(0, doctorAvailable("https://z-lib.gd"))
	v := text()
	if !m.loading || strings.Count(v, "Checking") != 1 || !strings.Contains(v, "Checking domains… 3 of 4") {
		t.Fatalf("one progress line expected while checking:\n%s", v)
	}
	if strings.Contains(v, "z-lib.sk") || strings.Index(v, "https://z-lib.gd") > strings.Index(v, "https://2.z-lib.gd") || strings.Contains(v, "No available") {
		t.Fatalf("list must show only available domains, in candidate order:\n%s", v)
	}

	// The current domain takes the cursor when it arrives, until the user moves.
	step(2, doctorAvailable("https://z-lib.gl"))
	if item := m.list.SelectedItem().(doctorDomainItem); item.check.Domain != "https://z-lib.gl" {
		t.Fatalf("cursor on %q, want the current domain", item.check.Domain)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = next.(doctorPageModel)
	moved := m.list.SelectedItem().(doctorDomainItem).check.Domain
	next, _ = m.Update(doctorCheckMsg{sequence: m.sequence, index: 9, check: doctorAvailable("https://9.z-lib.gd"), next: func() tea.Msg { return nil }})
	m = next.(doctorPageModel)
	if got := m.list.SelectedItem().(doctorDomainItem).check.Domain; got != moved {
		t.Fatalf("a late result stole the cursor: %q -> %q", moved, got)
	}

	next, _ = m.Update(doctorDoneMsg{sequence: m.sequence})
	m = next.(doctorPageModel)
	if v := text(); m.loading || strings.Contains(v, "Checking") || !strings.Contains(v, "check again") {
		t.Fatalf("finished page still looks busy:\n%s", v)
	}

	// A result from an earlier run is ignored.
	next, _ = m.Update(doctorCheckMsg{sequence: m.sequence - 1, index: 0, check: doctorAvailable("https://old.z-lib.gd")})
	if strings.Contains(ansi.Strip(next.(doctorPageModel).View().Content), "old.z-lib.gd") {
		t.Fatal("stale result shown")
	}
}

// Drives the page like the runtime: results land at different times and the
// list grows step by step instead of appearing all at once.
func TestDoctorPageListGrowsWhileChecksRun(t *testing.T) {
	candidates := []string{"https://z-lib.gd", "https://z-lib.gl", "https://z-lib.sk"}
	delays := []time.Duration{40 * time.Millisecond, 160 * time.Millisecond, 280 * time.Millisecond}
	probe := func(ctx context.Context, domains []string, _ string) <-chan domainCheckEvent {
		ch := make(chan domainCheckEvent, len(domains))
		var wg sync.WaitGroup
		for i, d := range domains {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(delays[i])
				ch <- domainCheckEvent{i, doctorAvailable(d)}
			}()
		}
		go func() { wg.Wait(); close(ch) }()
		return ch
	}
	m := newDoctorPageModel(candidates, "", "", probe)
	defer m.stop()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(doctorPageModel)

	var counts []int
	sawOneIndicator := true
	observe := func(model tea.Model) {
		page := model.(doctorPageModel)
		if n := len(page.list.Items()); len(counts) == 0 || counts[len(counts)-1] != n {
			counts = append(counts, n)
		}
		if page.loading && strings.Count(ansi.Strip(page.View().Content), "Checking") != 1 {
			sawOneIndicator = false
		}
	}
	final := pumpCmds(t, m, m.Init(), 5*time.Second, observe).(doctorPageModel)
	if final.loading || len(final.list.Items()) != 3 {
		t.Fatalf("finished with loading=%v items=%d", final.loading, len(final.list.Items()))
	}
	if !slices.Equal(counts, []int{0, 1, 2, 3}) && !slices.Equal(counts, []int{1, 2, 3}) {
		t.Fatalf("list sizes over time = %v, want 1, 2, 3 as results arrive", counts)
	}
	if !sawOneIndicator {
		t.Fatal("more or fewer than one loading indicator while checking")
	}
}

func TestShellDoctorOpensDedicatedPage(t *testing.T) {
	t.Setenv(zlib.EnvDomain, "https://z-lib.gd/")
	m := newShellModel()
	if cmd := m.dispatch("/doctor"); cmd == nil {
		t.Fatal("doctor did not start a probe")
	}
	page, ok := m.page.(doctorPageModel)
	if !ok || m.kind != "doctor" {
		t.Fatalf("doctor opened %T (%q)", m.page, m.kind)
	}
	if page.current != "https://z-lib.gd" {
		t.Fatalf("doctor current domain = %q", page.current)
	}
	ctx := page.ctx
	m, cmd := shellStep(m, key("esc"))
	if cmd == nil {
		t.Fatal("doctor did not return to shell")
	}
	m, _ = shellStep(m, cmd())
	if m.kind != "" || m.page != nil || ctx.Err() == nil {
		t.Fatal("doctor exit did not restore home and cancel the probe")
	}
}

func TestShellDoctorCancelsWhenAnotherCommandOpens(t *testing.T) {
	m := newShellModel()
	m.dispatch("/doctor")
	ctx := m.page.(doctorPageModel).ctx
	m.dispatch("/theme")
	if ctx.Err() == nil || m.kind != "theme" {
		t.Fatal("switching away from doctor kept its probe running")
	}
}
