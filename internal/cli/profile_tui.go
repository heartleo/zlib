package cli

import (
	"fmt"
	"strings"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

type profileLoadedMsg struct {
	profile zlib.UserProfile
	err     error
}

type profilePageModel struct {
	client   *zlib.Client
	viewport viewport.Model
	spinner  spinner.Model
	profile  zlib.UserProfile
	loaded   bool
	err      error
	width    int
	height   int

	inShell      bool
	switchDomain bool // the user asked to change domain from a connection error
}

func (m profilePageModel) domainSwitchRequest() (string, bool) { return "/profile", m.switchDomain }

func newProfilePageModel(client *zlib.Client) profilePageModel {
	return profilePageModel{client: client, spinner: newLoadingSpinner(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(18)), width: 80, height: 24}
}

func (m profilePageModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.load())
}

func (m profilePageModel) load() tea.Cmd {
	return func() tea.Msg {
		profile, err := m.client.GetProfile()
		return profileLoadedMsg{profile: profile, err: err}
	}
}

func (m profilePageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loaded {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport.SetWidth(max(1, msg.Width))
		m.viewport.SetHeight(max(1, msg.Height-6))
		m.updateContent()
		return m, nil
	case profileLoadedMsg:
		m.profile, m.err, m.loaded = msg.profile, msg.err, true
		m.updateContent()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		case "d":
			if m.err != nil && offersDomainSwitch(m.err, m.inShell, "/profile") {
				m.switchDomain = true
				return m, tea.Quit
			}
		case "r":
			if m.err != nil {
				m.err, m.loaded = nil, false
				m.updateContent()
				return m, tea.Batch(m.spinner.Tick, m.load())
			}
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *profilePageModel) updateContent() {
	content := ""
	if m.loaded && m.err == nil {
		content = renderProfilePage(m.profile, m.width)
	}
	m.viewport.SetContent(content)
}

func (m profilePageModel) View() tea.View {
	if !m.loaded {
		return screenView(m.width, m.height, "Profile", "", m.spinner.View(), "", uiKey("esc", "back"))
	}
	if m.err != nil {
		body, subtitle, bindings := loadErrorView(m.err, m.width, offersDomainSwitch(m.err, m.inShell, "/profile"),
			"Could not load profile", uiKey("r", "retry"), uiKey("esc", "back"))
		return screenView(m.width, m.height, "Profile", subtitle, body, "", bindings...)
	}
	status := ""
	bindings := []keys.Binding{uiKey("esc", "back")}
	if m.viewport.TotalLineCount() > m.viewport.Height() {
		status = fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))
		bindings = append([]keys.Binding{uiKey("↑/↓", "scroll")}, bindings...)
	}
	return screenView(m.width, m.height, "Profile", "", m.viewport.View(), status, bindings...)
}

type profileField struct{ label, value string }

func profileSection(title string, width int) string {
	section := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	line := lipgloss.NewStyle().Foreground(currentTheme.Surface)
	remaining := width - ansi.StringWidth(title) - 1
	if remaining < 2 {
		return section.Render(title)
	}
	return section.Render(title) + " " + line.Render(strings.Repeat("─", remaining))
}

func profileFieldBlock(field profileField, width int) string {
	primary := lipgloss.NewStyle().Foreground(currentTheme.Title)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	lines := strings.Split(ansi.Hardwrap(bookCell(field.value), max(1, width), false), "\n")
	for i := range lines {
		lines[i] = primary.Render(lines[i])
	}
	return muted.Render(field.label) + "\n" + strings.Join(lines, "\n")
}

func renderProfilePage(profile zlib.UserProfile, width int) string {
	width = max(1, width)
	primary := lipgloss.NewStyle().Foreground(currentTheme.Title)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	rows := []string{}
	if profile.Name != "" {
		rows = append(rows, primary.Bold(true).Render(bookCell(profile.Name)), "")
	}
	quotaTitle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true).Render("DAILY DOWNLOADS")
	if !profile.QuotaKnown {
		rows = append(rows, profileSection("DAILY DOWNLOADS", width), muted.Render("Quota unavailable"))
	} else {
		limits := profile.DownloadLimit
		usage := primary.Bold(true).Render(fmt.Sprintf("%d / %d used", limits.DailyAmount, limits.DailyAllowed))
		gap := width - ansi.StringWidth(quotaTitle) - ansi.StringWidth(usage)
		if gap >= 2 {
			rows = append(rows, quotaTitle+strings.Repeat(" ", gap)+usage)
		} else {
			rows = append(rows, quotaTitle, usage)
		}
		bar := progress.New(progress.WithWidth(width),
			progress.WithColors(remainingColor(limits.DailyRemaining), currentTheme.Surface),
			progress.WithoutPercentage(), progress.WithFillCharacters('━', '─'))
		fraction := 0.0
		if limits.DailyAllowed > 0 {
			fraction = min(1, max(0, float64(limits.DailyAmount)/float64(limits.DailyAllowed)))
		}
		rows = append(rows, bar.ViewAs(fraction))
		if limits.DailyReset != "" {
			rows = append(rows, muted.Render("Resets at ")+primary.Render(bookCell(limits.DailyReset)))
		}
	}
	fields := []profileField{}
	addField := func(label, value string) {
		if value != "" {
			fields = append(fields, profileField{label, value})
		}
	}
	addField("Email", profile.Email)
	addField("Kindle email", profile.KindleEmail)
	if profile.Premium != nil {
		if *profile.Premium {
			addField("Plan", "Premium")
		} else {
			addField("Plan", "Standard")
		}
	}
	if profile.Confirmed != nil {
		if *profile.Confirmed {
			addField("Email confirmed", "Yes")
		} else {
			addField("Email confirmed", "No")
		}
	}
	if profile.DonationsActive != nil {
		if *profile.DonationsActive {
			addField("Donation", "Active")
		} else {
			addField("Donation", "Inactive")
		}
	}
	addField("Donation expires", profile.DonationsExpire)
	if len(fields) > 0 {
		rows = append(rows, "", profileSection("ACCOUNT", width))
		if width >= 72 {
			columnWidth := (width - 3) / 2
			for i := 0; i < len(fields); i += 2 {
				left := lipgloss.NewStyle().Width(columnWidth).Render(profileFieldBlock(fields[i], columnWidth))
				if i+1 < len(fields) {
					right := profileFieldBlock(fields[i+1], width-columnWidth-3)
					rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right))
				} else {
					rows = append(rows, left)
				}
				if i+2 < len(fields) {
					rows = append(rows, "")
				}
			}
		} else {
			for _, field := range fields {
				rows = append(rows, profileFieldBlock(field, width), "")
			}
		}
	}
	if len(profile.PersonalDomains) > 0 {
		rows = append(rows, "", profileSection("PERSONAL DOMAINS", width))
		for _, domain := range profile.PersonalDomains {
			for _, line := range strings.Split(ansi.Hardwrap(bookCell(domain), width, false), "\n") {
				rows = append(rows, primary.Render(line))
			}
		}
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}
