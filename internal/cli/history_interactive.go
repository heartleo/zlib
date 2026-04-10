package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zlib "github.com/heartleo/zlib"
	"github.com/mattn/go-runewidth"
)

// — interactive history bubbletea model —

type historyState int

const (
	historyStateLoading historyState = iota
	historyStateResults
)

type historySelectModel struct {
	client     *zlib.Client
	items      []zlib.DownloadHistoryItem
	page       int
	totalPages int
	hasNext    bool
	cursor     int
	state      historyState
	spinner    spinner.Model
	selected   *zlib.DownloadHistoryItem // set when user presses Enter
	err        error
	quitting   bool
}

type historyResultMsg struct {
	items      []zlib.DownloadHistoryItem
	page       int
	totalPages int
	hasNext    bool
}

func newHistorySelectModel(c *zlib.Client) historySelectModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	return historySelectModel{
		client:  c,
		page:    1,
		state:   historyStateLoading,
		spinner: s,
	}
}

func (m historySelectModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchPage(1))
}

func (m historySelectModel) fetchPage(page int) tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.DownloadHistory(page)
		if err != nil {
			return errMsg{err}
		}
		// Fetch missing extension/size details
		var missingIDs []string
		for _, item := range result.Items {
			if id := bookIDFromURL(item.URL); id != "" && id != item.URL && item.Extension == "" {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) > 0 {
			details := m.client.FetchBookDetails(missingIDs)
			for i, item := range result.Items {
				id := bookIDFromURL(item.URL)
				if d, ok := details[id]; ok {
					if result.Items[i].Extension == "" {
						result.Items[i].Extension = d.Extension
					}
					if result.Items[i].Size == "" {
						result.Items[i].Size = d.Size
					}
				}
				_ = item
			}
		}
		return historyResultMsg{
			items:      result.Items,
			page:       result.Page,
			totalPages: result.TotalPages,
			hasNext:    len(result.Items) > 0,
		}
	}
}

func (m historySelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case historyResultMsg:
		m.items = msg.items
		m.page = msg.page
		m.totalPages = msg.totalPages
		m.hasNext = msg.hasNext
		m.cursor = 0
		m.state = historyStateResults
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case historyStateResults:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			case "left", "h":
				if m.page > 1 {
					m.state = historyStateLoading
					return m, tea.Batch(m.spinner.Tick, m.fetchPage(m.page-1))
				}
			case "right", "l":
				if m.hasNext {
					m.state = historyStateLoading
					return m, tea.Batch(m.spinner.Tick, m.fetchPage(m.page+1))
				}
			case "enter":
				if len(m.items) > 0 && m.cursor < len(m.items) {
					item := m.items[m.cursor]
					if item.DownloadURL != "" {
						m.selected = &item
						return m, tea.Quit
					}
				}
			case "q", "esc", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			}
		case historyStateLoading:
			if msg.String() == "q" || msg.String() == "esc" || msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		if m.state == historyStateLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m historySelectModel) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	pageStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)

	b.WriteString(fmt.Sprintf("  %s", headerStyle.Render("Download History")))
	if m.state == historyStateResults {
		if m.totalPages > 0 {
			b.WriteString(fmt.Sprintf("  %s", pageStyle.Render(fmt.Sprintf("Page %d / %d", m.page, m.totalPages))))
		} else if m.page > 0 {
			b.WriteString(fmt.Sprintf("  %s", pageStyle.Render(fmt.Sprintf("Page %d", m.page))))
		}
	}
	b.WriteString("\n\n")

	if m.state == historyStateLoading {
		b.WriteString(fmt.Sprintf("  %s Loading...\n", m.spinner.View()))
		return b.String()
	}

	if len(m.items) == 0 {
		b.WriteString(fmt.Sprintf("  %s No history found.\n", colorRed(symbolError)))
		return b.String()
	}

	// Column widths
	const (
		colNum   = 2
		colID    = 10
		colTitle = 40
		colFmt   = 4
		colSize  = 10
		colDate  = 10
	)

	// Table header
	hdrStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(currentTheme.Surface)

	header := fmt.Sprintf("    %-*s  %-*s  %-*s  %-*s  %*s  %-*s",
		colNum, "#", colID, "ID", colTitle, "Title", colFmt, "Fmt", colSize, "Size", colDate, "Date")
	b.WriteString(hdrStyle.Render(header))
	b.WriteString("\n")

	sepLen := 4 + colNum + 2 + colID + 2 + colTitle + 2 + colFmt + 2 + colSize + 2 + colDate
	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", sepLen)))
	b.WriteString("\n")

	// Rows
	cursorStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	selectedBg := lipgloss.NewStyle().Background(currentTheme.Surface)

	for i, item := range m.items {
		id := bookIDFromURL(item.URL)
		if id == item.URL {
			id = "-"
		}
		ext := strings.ToUpper(item.Extension)
		if ext == "" {
			ext = "-"
		}
		size := item.Size
		if size == "" {
			size = "-"
		}
		date := item.Date
		if date == "" {
			date = "-"
		}

		selected := i == m.cursor
		cursor := "  "
		if selected {
			cursor = cursorStyle.Render("❯ ")
		}

		numStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		idStyle := lipgloss.NewStyle().Foreground(currentTheme.Link)
		titleStyle := lipgloss.NewStyle().Foreground(currentTheme.Title)
		extStyle := lipgloss.NewStyle().Foreground(extLipglossColor(ext))
		sizeStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		dateStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		if selected {
			numStyle = numStyle.Background(currentTheme.Surface)
			idStyle = idStyle.Background(currentTheme.Surface)
			titleStyle = titleStyle.Background(currentTheme.Surface)
			extStyle = extStyle.Background(currentTheme.Surface)
			sizeStyle = sizeStyle.Background(currentTheme.Surface)
			dateStyle = dateStyle.Background(currentTheme.Surface)
		}

		content := fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			numStyle.Render(fmt.Sprintf("%-*d", colNum, i+1)),
			idStyle.Render(runewidth.FillRight(runewidth.Truncate(id, colID, ""), colID)),
			titleStyle.Render(runewidth.FillRight(runewidth.Truncate(item.Name, colTitle, ""), colTitle)),
			extStyle.Render(runewidth.FillRight(runewidth.Truncate(ext, colFmt, ""), colFmt)),
			sizeStyle.Render(fmt.Sprintf("%*s", colSize, runewidth.Truncate(size, colSize, ""))),
			dateStyle.Render(runewidth.FillRight(runewidth.Truncate(date, colDate, ""), colDate)),
		)
		if selected {
			content = selectedBg.Render(content)
		}

		b.WriteString(cursor + content + "\n")
	}

	// Help bar
	b.WriteString("\n")
	helpKeys := lipgloss.NewStyle().Foreground(currentTheme.Accent)
	help := fmt.Sprintf("  %s select  %s page  %s download  %s quit",
		helpKeys.Render("↑/↓"), helpKeys.Render("←/→"), helpKeys.Render("enter"), helpKeys.Render("q"))
	b.WriteString(lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(help))
	b.WriteString("\n")

	return b.String()
}
