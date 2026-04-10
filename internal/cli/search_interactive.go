package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	zlib "github.com/heartleo/zlib"
	"github.com/mattn/go-runewidth"
)

// — interactive search bubbletea model —

type searchState int

const (
	searchStateInput   searchState = iota // waiting for query input (handled outside model)
	searchStateLoading                    // fetching a page
	searchStateResults                    // showing results
)

type searchSelectModel struct {
	client     *zlib.Client
	query      string
	books      []zlib.Book
	page       int
	totalPages int
	cursor     int
	state      searchState
	spinner    spinner.Model
	selectedID string // set when user presses Enter
	err        error
	quitting   bool
}

// messages
type searchResultMsg struct {
	books      []zlib.Book
	page       int
	totalPages int
}

func newSearchSelectModel(query string, c *zlib.Client) searchSelectModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	return searchSelectModel{
		client:  c,
		query:   query,
		page:    1,
		state:   searchStateLoading,
		spinner: s,
	}
}

func (m searchSelectModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchPage(1))
}

func (m searchSelectModel) fetchPage(page int) tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.Search(m.query, page, 25, nil)
		if err != nil {
			return errMsg{err}
		}
		return searchResultMsg{
			books:      result.Books,
			page:       result.Page,
			totalPages: result.TotalPages,
		}
	}
}

func (m searchSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case searchResultMsg:
		m.books = msg.books
		m.page = msg.page
		m.totalPages = msg.totalPages
		m.cursor = 0
		m.state = searchStateResults
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case searchStateResults:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.books)-1 {
					m.cursor++
				}
			case "left", "h":
				if m.page > 1 {
					m.state = searchStateLoading
					return m, tea.Batch(m.spinner.Tick, m.fetchPage(m.page-1))
				}
			case "right", "l":
				if m.page < m.totalPages {
					m.state = searchStateLoading
					return m, tea.Batch(m.spinner.Tick, m.fetchPage(m.page+1))
				}
			case "enter":
				if len(m.books) > 0 && m.cursor < len(m.books) {
					id := bookIDFromURL(m.books[m.cursor].URL)
					if id != m.books[m.cursor].URL {
						m.selectedID = id
						return m, tea.Quit
					}
				}
			case "q", "esc", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			}
		case searchStateLoading:
			if msg.String() == "q" || msg.String() == "esc" || msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		if m.state == searchStateLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m searchSelectModel) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	pageStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)

	b.WriteString(fmt.Sprintf("  %s %s",
		headerStyle.Render("Search:"),
		lipgloss.NewStyle().Foreground(currentTheme.Title).Render(fmt.Sprintf("%q", m.query)),
	))
	if m.totalPages > 0 {
		b.WriteString(fmt.Sprintf("  %s", pageStyle.Render(fmt.Sprintf("Page %d / %d", m.page, m.totalPages))))
	}
	b.WriteString("\n\n")

	if m.state == searchStateLoading {
		b.WriteString(fmt.Sprintf("  %s Searching...\n", m.spinner.View()))
		return b.String()
	}

	if len(m.books) == 0 {
		b.WriteString(fmt.Sprintf("  %s No results found.\n", colorRed(symbolError)))
		return b.String()
	}

	// Column widths
	const (
		colNum    = 2
		colID     = 10
		colTitle  = 40
		colAuthor = 15
		colFmt    = 4
		colSize   = 10
	)

	// Table header
	hdrStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(currentTheme.Surface)

	header := fmt.Sprintf("    %-*s  %-*s  %-*s  %-*s  %-*s  %*s",
		colNum, "#", colID, "ID", colTitle, "Title", colAuthor, "Author", colFmt, "Fmt", colSize, "Size")
	b.WriteString(hdrStyle.Render(header))
	b.WriteString("\n")

	sepLen := 4 + colNum + 2 + colID + 2 + colTitle + 2 + colAuthor + 2 + colFmt + 2 + colSize
	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", sepLen)))
	b.WriteString("\n")

	// Rows
	cursorStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	selectedBg := lipgloss.NewStyle().Background(currentTheme.Surface)

	for i, book := range m.books {
		id := bookIDFromURL(book.URL)
		if id == book.URL {
			id = "-"
		}
		authors := strings.Join(book.Authors, ", ")
		ext := strings.ToUpper(book.Extension)
		if ext == "" {
			ext = "-"
		}
		size := book.Size
		if size == "" {
			size = "-"
		}

		selected := i == m.cursor
		cursor := "  "
		if selected {
			cursor = cursorStyle.Render("❯ ")
		}

		numStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		idStyle := lipgloss.NewStyle().Foreground(currentTheme.Link)
		titleStyle := lipgloss.NewStyle().Foreground(currentTheme.Title)
		authorStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		extStyle := lipgloss.NewStyle().Foreground(extLipglossColor(ext))
		sizeStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		if selected {
			numStyle = numStyle.Background(currentTheme.Surface)
			idStyle = idStyle.Background(currentTheme.Surface)
			titleStyle = titleStyle.Background(currentTheme.Surface)
			authorStyle = authorStyle.Background(currentTheme.Surface)
			extStyle = extStyle.Background(currentTheme.Surface)
			sizeStyle = sizeStyle.Background(currentTheme.Surface)
		}

		content := fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			numStyle.Render(fmt.Sprintf("%-*d", colNum, i+1)),
			idStyle.Render(runewidth.FillRight(runewidth.Truncate(id, colID, ""), colID)),
			titleStyle.Render(runewidth.FillRight(runewidth.Truncate(book.Name, colTitle, ""), colTitle)),
			authorStyle.Render(runewidth.FillRight(runewidth.Truncate(authors, colAuthor, ""), colAuthor)),
			extStyle.Render(runewidth.FillRight(runewidth.Truncate(ext, colFmt, ""), colFmt)),
			sizeStyle.Render(fmt.Sprintf("%*s", colSize, runewidth.Truncate(size, colSize, ""))),
		)
		if selected {
			// Pad trailing space with background color for full-row highlight
			content = selectedBg.Render(content)
		}

		b.WriteString(cursor + content + "\n")
	}

	// Help bar
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	helpKeys := lipgloss.NewStyle().Foreground(currentTheme.Accent)
	help := fmt.Sprintf("  %s select  %s page  %s download  %s quit",
		helpKeys.Render("↑/↓"), helpKeys.Render("←/→"), helpKeys.Render("enter"), helpKeys.Render("q"))
	b.WriteString(helpStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

// interactiveSearch prompts for a query then runs the interactive search model.
// Returns the selected book ID or "" if cancelled.
func interactiveSearch(c *zlib.Client) (string, error) {
	var query string
	inputForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Search books").
				Placeholder("enter title, author, or ISBN...").
				Value(&query),
		),
	).WithTheme(huhTheme())
	if err := inputForm.Run(); err != nil {
		return "", err
	}
	if strings.TrimSpace(query) == "" {
		return "", nil
	}

	p := tea.NewProgram(newSearchSelectModel(query, c))
	result, err := p.Run()
	if err != nil {
		return "", err
	}

	sm := result.(searchSelectModel)
	if sm.err != nil {
		return "", sm.err
	}
	return sm.selectedID, nil
}
