package cli

import (
	"context"
	"fmt"
	"slices"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// doctorProbeFunc starts the checks and yields each result as it finishes.
type doctorProbeFunc func(context.Context, []string, string) <-chan domainCheckEvent

// doctorResultMsg carries a complete set of checks at once.
type doctorResultMsg struct {
	sequence uint64
	checks   []zlib.DomainCheck
}

// doctorCheckMsg is one domain's finished check, sent while the others still
// run. next reads the following result, or reports doctorDoneMsg at the end.
type doctorCheckMsg struct {
	sequence uint64
	index    int
	check    zlib.DomainCheck
	next     tea.Cmd
}

type doctorDoneMsg struct{ sequence uint64 }

type doctorDomainItem struct {
	check   zlib.DomainCheck
	current bool
}

func (i doctorDomainItem) Title() string {
	if i.current {
		return i.check.Domain + "  · Current domain"
	}
	return i.check.Domain
}
func (i doctorDomainItem) Description() string { return "" }
func (i doctorDomainItem) FilterValue() string { return i.check.Domain }

type doctorPageModel struct {
	list       list.Model
	spinner    spinner.Model
	probe      doctorProbeFunc
	candidates []string
	proxy      string
	current    string
	ctx        context.Context
	cancel     context.CancelFunc
	sequence   uint64
	loading    bool
	width      int
	height     int

	// Results arrive one at a time. found keeps the available ones in
	// candidate order, checked counts every finished probe, and moved records
	// that the user chose a row, so later arrivals no longer steal the cursor.
	found   map[int]zlib.DomainCheck
	checked int
	moved   bool
}

func newDoctorPageModel(candidates []string, proxy, current string, probe doctorProbeFunc) doctorPageModel {
	if probe == nil {
		probe = streamDomainChecks
	}
	items := list.New(nil, doctorListDelegate(80), 80, 18)
	items.SetShowTitle(false)
	items.SetShowHelp(false)
	items.SetShowStatusBar(false)
	items.SetShowFilter(false)
	items.SetFilteringEnabled(false)
	items.DisableQuitKeybindings()
	items.Styles.PaginationStyle = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	items.Styles.ActivePaginationDot = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	items.Styles.InactivePaginationDot = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	ctx, cancel := context.WithCancel(context.Background())
	return doctorPageModel{
		list: items, spinner: s, probe: probe, candidates: candidates, proxy: proxy,
		current: current, ctx: ctx, cancel: cancel, sequence: 1, loading: true, width: 80, height: 24,
	}
}

func doctorListDelegate(width int) list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().Foreground(currentTheme.Title).PaddingLeft(2)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(currentTheme.Accent).
		PaddingLeft(1).
		Foreground(currentTheme.Accent).
		Background(currentTheme.Surface).
		Bold(true).
		Width(max(1, width-2))
	return delegate
}

func (m doctorPageModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.check())
}

func (m doctorPageModel) check() tea.Cmd {
	return nextDoctorCheck(m.sequence, m.probe(m.ctx, m.candidates, m.proxy))
}

func nextDoctorCheck(sequence uint64, ch <-chan domainCheckEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return doctorDoneMsg{sequence}
		}
		return doctorCheckMsg{sequence: sequence, index: event.index, check: event.check, next: nextDoctorCheck(sequence, ch)}
	}
}

// showFound rebuilds the list from the available results in candidate order.
// The cursor stays on the same domain once the user has moved it; until then
// it follows the current domain as soon as that one is found.
func (m *doctorPageModel) showFound() tea.Cmd {
	indexes := make([]int, 0, len(m.found))
	for i := range m.found {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)
	previous := ""
	if item, ok := m.list.SelectedItem().(doctorDomainItem); ok {
		previous = item.check.Domain
	}
	items := make([]list.Item, 0, len(indexes))
	selected := 0
	for _, i := range indexes {
		check := m.found[i]
		if (m.moved && check.Domain == previous) || (!m.moved && check.Domain == m.current) {
			selected = len(items)
		}
		items = append(items, doctorDomainItem{check: check, current: check.Domain == m.current})
	}
	cmd := m.list.SetItems(items)
	m.list.Select(selected)
	return cmd
}

func (m *doctorPageModel) stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m doctorPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(max(1, msg.Width), max(1, msg.Height-6))
		m.list.SetDelegate(doctorListDelegate(max(1, msg.Width)))
		return m, nil
	case doctorCheckMsg:
		if msg.sequence != m.sequence || !m.loading {
			return m, nil
		}
		m.checked++
		if len(availableDomains([]zlib.DomainCheck{msg.check})) == 1 {
			if m.found == nil {
				m.found = map[int]zlib.DomainCheck{}
			}
			m.found[msg.index] = msg.check
			// Call before returning: the returned m is copied before its
			// operands to the right are evaluated.
			show := m.showFound()
			return m, tea.Batch(show, msg.next)
		}
		return m, msg.next
	case doctorDoneMsg:
		if msg.sequence == m.sequence {
			m.loading = false
		}
		return m, nil
	case doctorResultMsg:
		if msg.sequence != m.sequence || !m.loading {
			return m, nil
		}
		m.loading = false
		available := availableDomains(msg.checks)
		items := make([]list.Item, 0, len(available))
		selected := 0
		for _, check := range available {
			if check.Domain == m.current {
				selected = len(items)
			}
			items = append(items, doctorDomainItem{check: check, current: check.Domain == m.current})
		}
		cmd := m.list.SetItems(items)
		m.list.Select(selected)
		return m, cmd
	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			m.stop()
			return m, tea.Quit
		case "r":
			m.stop()
			m.ctx, m.cancel = context.WithCancel(context.Background())
			m.sequence++
			m.loading = true
			m.found, m.checked, m.moved = nil, 0, false
			m.list.SetItems(nil)
			return m, tea.Batch(m.spinner.Tick, m.check())
		case "up", "down", "k", "j", "pgup", "pgdown", "home", "end":
			m.moved = true
		}
	}
	if len(m.list.Items()) > 0 {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m doctorPageModel) View() tea.View {
	if m.loading {
		// One animation for the whole check; available domains appear above it
		// the moment they answer.
		progress := fmt.Sprintf("%s Checking domains… %d of %d", m.spinner.View(), m.checked, len(m.candidates))
		if len(m.list.Items()) == 0 {
			return screenView(m.width, m.height, "Doctor", "", "  "+progress, "", uiKey("esc", "back"))
		}
		return screenView(m.width, m.height, "Doctor", "", m.list.View(), progress,
			uiKey("↑/↓", "select"), uiKey("esc", "back"))
	}
	body := m.list.View()
	if len(m.list.Items()) == 0 {
		body = "No available EAPI domains."
	}
	return screenView(m.width, m.height, "Doctor", "", body, "",
		uiKey("↑/↓", "select"), uiKey("r", "check again"), uiKey("esc", "back"))
}
