package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// The domain page lists every domain zlib may use, the user's own mirrors
// first, each with the result of an unauthenticated check, and lets the user
// pick one, add a mirror (verified, then confirmed) or remove one.

type settingsMirrorCheckMsg struct {
	generation int
	origin     string
	check      zlib.DomainCheck
	err        error
}

// settingsStartProbeMsg starts the domain check when the model opens on the
// domain page (see settingsModel.Init).
type settingsStartProbeMsg struct{}

// domainCandidates lists the user's mirrors, then the built-in domains, plus
// the current domain when it is trusted but not otherwise listed.
func (m settingsModel) domainCandidates() []string {
	mirrors := zlib.UserMirrors()
	candidates := slices.Clone(mirrors)
	for _, d := range zlib.KnownDomains() {
		if !slices.Contains(candidates, d) {
			candidates = append(candidates, d)
		}
	}
	if current := m.currentDomain(); current != "" && !slices.Contains(candidates, current) {
		candidates = append(candidates, current)
	}
	return candidates
}

// currentDomain is the saved domain in origin form, or "" when unset.
func (m settingsModel) currentDomain() string {
	current, err := zlib.ParseAllowedDomain(m.value(zlib.EnvDomain).Value)
	if err != nil {
		return ""
	}
	return current
}

func (m settingsModel) currentDomainIndex() int {
	return max(0, slices.Index(m.domains, m.currentDomain()))
}

func isUserMirror(origin string) bool {
	return slices.Contains(zlib.UserMirrors(), origin)
}

// usableOrigin is the trusted origin a check ended on, which is what gets
// saved: a redirector is usable through its trusted target.
func usableOrigin(check zlib.DomainCheck) (string, bool) {
	if check.Status != zlib.DomainStatusHealthy {
		return "", false
	}
	final, err := url.Parse(check.FinalURL)
	if err != nil || final.Host == "" {
		return "", false
	}
	origin, err := zlib.ParseAllowedDomain(final.Scheme + "://" + final.Host)
	return origin, err == nil
}

// availabilityRank orders rows within a section: available, still checking,
// then unavailable.
func (m settingsModel) availabilityRank(domain string) int {
	check, checked := m.checks[domain]
	if !checked {
		return 1
	}
	if _, ok := usableOrigin(check); ok {
		return 0
	}
	return 2
}

// sortDomains puts available rows first within each section, keeping the
// mirrors above the built-in domains and the cursor on the same domain.
func (m *settingsModel) sortDomains() {
	selected := ""
	if m.cursor < len(m.domains) {
		selected = m.domains[m.cursor]
	}
	mirrors := zlib.UserMirrors()
	section := func(d string) int {
		if slices.Contains(mirrors, d) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(m.domains, func(a, b string) int {
		if c := section(a) - section(b); c != 0 {
			return c
		}
		return m.availabilityRank(a) - m.availabilityRank(b)
	})
	if i := slices.Index(m.domains, selected); i >= 0 {
		m.cursor = i
	}
}

// domainStatus marks a check result and reports whether it is good news.
// Every failure uses a cross: the reason does not change what the user
// can do, which is to pick another domain; `zlib doctor --json` has the detail.
func domainStatus(domain string, check zlib.DomainCheck, checked bool) (string, bool) {
	if !checked {
		// Rows fill in as their checks finish; the footer carries the one
		// loading animation, so a pending row stays quiet.
		return "", false
	}
	origin, ok := usableOrigin(check)
	if !ok {
		return "✗", false
	}
	if origin != domain {
		return "✓ via " + strings.TrimPrefix(origin, "https://"), true
	}
	return "✓", true
}

func (m *settingsModel) openDomains() tea.Cmd {
	m.page = "domain"
	m.message = ""
	m.domains = m.domainCandidates()
	m.cursor = m.currentDomainIndex()
	m.checks = nil
	return m.probe(m.effectiveProxy(), "domain")
}

// resumeDomainCheck restarts the check when leaving the add form interrupted
// it, so no row is left without a result.
func (m *settingsModel) resumeDomainCheck() tea.Cmd {
	if m.busy {
		return nil
	}
	for _, d := range m.domains {
		if _, ok := m.checks[d]; !ok {
			return m.probe(m.effectiveProxy(), "domain")
		}
	}
	return nil
}

func (m *settingsModel) verifyMirror(origin string) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.generation++
	generation := m.generation
	m.busy = true
	m.message = ""
	m.spinner = newLoadingSpinner()
	proxy := m.effectiveProxy()
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		check, err := verifyMirror(ctx, origin, zlib.DomainProbeOptions{ProxyURL: proxy})
		return settingsMirrorCheckMsg{generation: generation, origin: origin, check: check, err: err}
	})
}

func (m settingsModel) handleMirrorCheck(msg settingsMirrorCheckMsg) (settingsModel, tea.Cmd) {
	if msg.generation != m.generation {
		return m, nil
	}
	m.busy = false
	m.cancel = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.message = safeText(msg.err.Error())
		return m, nil
	}
	m.pendingCheck = msg.check
	m.mirrorConfirm = "trust"
	m.mirrorTarget = msg.origin
	m.mirrorFocused = false
	return m, nil
}

// updateDomainKeys handles keys for the domain page, the add-mirror form and
// their confirmations. handled is false for keys the generic list handles.
func (m settingsModel) updateDomainKeys(msg tea.KeyPressMsg) (settingsModel, tea.Cmd, bool) {
	k := msg.String()
	if m.mirrorConfirm != "" {
		switch k {
		case "left", "right", "tab", "shift+tab", "h", "l":
			m.mirrorFocused = !m.mirrorFocused
		case "y":
			return m.confirmMirror()
		case "enter":
			if m.mirrorFocused {
				return m.confirmMirror()
			}
			m.mirrorConfirm = ""
		case "n", "esc":
			m.mirrorConfirm = ""
		}
		return m, nil, true
	}

	if m.page == "mirror" {
		switch k {
		case "esc":
			m.back()
			return m, m.resumeDomainCheck(), true
		case "enter":
			if m.busy {
				return m, nil, true
			}
			origin, err := zlib.ParseMirrorOrigin(m.input.Value())
			switch {
			case err != nil:
				m.message = err.Error()
			case zlib.IsBuiltInDomain(origin):
				m.message = strings.TrimPrefix(origin, "https://") + " is already built in; there is no need to add it"
			case isUserMirror(origin):
				m.message = strings.TrimPrefix(origin, "https://") + " is already one of your mirrors"
			default:
				return m, m.verifyMirror(origin), true
			}
			return m, nil, true
		}
		if m.busy {
			return m, nil, true
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd, true
	}

	if m.page != "domain" {
		return m, nil, false
	}
	switch k {
	case "r":
		m.checks = nil
		return m, m.probe(m.effectiveProxy(), "domain"), true
	case "a":
		// Pause the domain check so it cannot hold the form busy;
		// resumeDomainCheck finishes it on the way back.
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.generation++
		m.busy = false
		m.mirrorCursor = m.cursor
		m.page = "mirror"
		m.message = ""
		m.input.SetValue("")
		m.input.Placeholder = "mirror.example"
		m.input.EchoMode = 0
		return m, m.input.Focus(), true
	case "d", "delete", "backspace":
		if len(m.domains) == 0 {
			return m, nil, true
		}
		target := m.domains[m.cursor]
		if !isUserMirror(target) {
			m.message = "Built-in domains cannot be removed; only your mirrors can"
			return m, nil, true
		}
		m.mirrorConfirm = "remove"
		m.mirrorTarget = target
		m.mirrorFocused = false
		return m, nil, true
	case "enter":
		if len(m.domains) == 0 {
			return m, nil, true
		}
		domain := m.domains[m.cursor]
		check, checked := m.checks[domain]
		if !checked {
			m.message = "Still checking " + strings.TrimPrefix(domain, "https://") + "; press r to check again"
			return m, nil, true
		}
		origin, ok := usableOrigin(check)
		if !ok {
			m.message = strings.TrimPrefix(domain, "https://") + " is unavailable. Choose an available domain or press r."
			return m, nil, true
		}
		m.key = zlib.EnvDomain
		m.save(origin)
		return m, nil, true
	}
	return m, nil, false
}

func (m settingsModel) confirmMirror() (settingsModel, tea.Cmd, bool) {
	target, action := m.mirrorTarget, m.mirrorConfirm
	m.mirrorConfirm = ""
	host := strings.TrimPrefix(target, "https://")
	switch action {
	case "trust":
		if err := addUserMirror(target); err != nil {
			m.message = "Could not add the mirror: " + safeText(err.Error())
			return m, nil, true
		}
		if m.checks == nil {
			m.checks = map[string]zlib.DomainCheck{}
		}
		m.checks[target] = m.pendingCheck
		m.page = "domain"
		m.domains = m.domainCandidates()
		m.cursor = max(0, slices.Index(m.domains, target))
		m.sortDomains()
		m.message = "Trusted " + host + " · Enter uses it"
		return m, m.resumeDomainCheck(), true
	case "remove":
		current := isCurrentDomain(target)
		if err := removeUserMirror(target); err != nil {
			m.message = "Could not remove the mirror: " + safeText(err.Error())
			return m, nil, true
		}
		delete(m.checks, target)
		m.domains = m.domainCandidates()
		m.cursor = min(m.cursor, max(0, len(m.domains)-1))
		m.sortDomains()
		m.message = "Removed " + host
		if current {
			m.message += " · it was your current domain; choose another"
		}
	}
	return m, nil, true
}

func (m settingsModel) domainView(w, h int) tea.View {
	if m.page == "mirror" {
		lines := []string{"Mirror domain", m.input.View(), ""}
		muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		for _, line := range []string{
			"zlib checks that the domain answers as Z-Library before adding it,",
			"then asks you to confirm. It trusts that exact host only.",
		} {
			lines = append(lines, muted.Render(ansi.Truncate(line, max(1, w-2), "…")))
		}
		status := m.message
		if m.busy {
			status = m.spinner.View() + " Checking " + strings.TrimPrefix(strings.TrimSpace(m.input.Value()), "https://") + "…"
		}
		controls := []keys.Binding{uiKey("esc", "cancel"), uiKey("enter", "check")}
		v := settingsCompactView(w, h, "Settings / Add mirror", "Add a mirror you trust", lines, status, controls)
		return m.withMirrorConfirm(v, w, h)
	}

	current := m.currentDomain()
	subtitle := "Current: Automatic"
	if current != "" {
		subtitle = "Current: " + bookCell(strings.TrimPrefix(current, "https://")) + " (●)"
	}
	section := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)

	domainWidth := 0
	for _, d := range m.domains {
		domainWidth = max(domainWidth, ansi.StringWidth(strings.TrimPrefix(d, "https://")))
	}
	domainWidth = min(domainWidth, max(8, w/2))

	var lines []string
	selected := 0
	mirrorCount := 0
	for _, d := range m.domains {
		if isUserMirror(d) {
			mirrorCount++
		}
	}
	lines = append(lines, section.Render("  YOUR MIRRORS"))
	if mirrorCount == 0 {
		lines = append(lines, muted.Render("  None yet · press a to add a mirror you trust"))
	}
	for i, d := range m.domains {
		if i == mirrorCount {
			lines = append(lines, "", section.Render("  BUILT-IN"))
		}
		check, checked := m.checks[d]
		label, good := domainStatus(d, check, checked)
		marker := "  "
		if d == current {
			marker = "● "
		}
		name := ansi.Truncate(strings.TrimPrefix(d, "https://"), domainWidth, "…")
		pad := strings.Repeat(" ", max(0, domainWidth-ansi.StringWidth(name))+2)
		statusStyle := muted
		if checked {
			statusStyle = lipgloss.NewStyle().Foreground(currentTheme.Error)
			if good {
				statusStyle = statusStyle.Foreground(currentTheme.Success)
			}
		}
		if i == m.cursor {
			selected = len(lines)
			statusStyle = statusStyle.Background(currentTheme.Surface)
			lines = append(lines, styledListRow(marker+name+pad+statusStyle.Render(label), true, w, currentTheme))
			continue
		}
		row := "  " + lipgloss.NewStyle().Foreground(currentTheme.Accent).Render(marker) +
			lipgloss.NewStyle().Foreground(currentTheme.Title).Render(name) + pad + statusStyle.Render(label)
		lines = append(lines, ansi.Truncate(row, max(1, w), "…"))
	}

	available := max(3, h-8)
	if len(lines) > available {
		from := min(max(0, selected-available+2), len(lines)-available)
		lines = lines[from : from+available]
	}

	status := m.message
	if m.busy {
		checked := 0
		for _, d := range m.domains {
			if _, ok := m.checks[d]; ok {
				checked++
			}
		}
		status = fmt.Sprintf("%s Checking domains… %d of %d", m.spinner.View(), checked, len(m.domains))
	}
	controls := []keys.Binding{uiKey("esc", "back"), uiKey("enter", "use"), uiKey("a", "add mirror"), uiKey("d", "remove"), uiKey("r", "check again")}
	v := screenView(w, h, "Settings / Domains", subtitle, strings.Join(lines, "\n"), status, controls...)
	return m.withMirrorConfirm(v, w, h)
}

func (m settingsModel) withMirrorConfirm(v tea.View, w, h int) tea.View {
	host := strings.TrimPrefix(m.mirrorTarget, "https://")
	switch m.mirrorConfirm {
	case "trust":
		v.Content = confirmationDialog(v.Content, w, h, "Trust this mirror?", host, "",
			"zlib will send your login to this host.", "Trust", m.mirrorFocused, true)
	case "remove":
		note := "zlib will stop sending your login to it."
		if isCurrentDomain(m.mirrorTarget) {
			note = "It is your current domain; choose another after removing it."
		}
		v.Content = confirmationDialog(v.Content, w, h, "Remove this mirror?", host, "", note, "Remove", m.mirrorFocused, true)
	}
	return v
}
