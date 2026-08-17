package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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
	searchStateFilter                     // editing the query inline (/)
)

type searchSelectModel struct {
	client       *zlib.Client
	query        string
	opts         *zlib.SearchOptions
	books        []zlib.Book
	page         int
	totalPages   int
	cursor       int
	state        searchState
	spinner      spinner.Model
	selectedID   string    // set when user presses Enter
	selectedBook zlib.Book // the card the ID came from, carries its /dl/ URL
	err          error
	quitting     bool
	fullTitle    bool            // do not truncate titles
	width        int             // terminal width, from tea.WindowSizeMsg
	input        textinput.Model // inline query editor, shown in searchStateFilter
}

// messages
type searchResultMsg struct {
	books      []zlib.Book
	page       int
	totalPages int
}

func newSearchSelectModel(query string, c *zlib.Client, opts *zlib.SearchOptions, fullTitle bool) searchSelectModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)

	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "refine search…"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	ti.TextStyle = lipgloss.NewStyle().Foreground(currentTheme.Title)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(currentTheme.Muted)

	return searchSelectModel{
		client:    c,
		query:     query,
		opts:      opts,
		page:      1,
		state:     searchStateLoading,
		spinner:   s,
		fullTitle: fullTitle,
		input:     ti,
	}
}

func (m searchSelectModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchPage(1))
}

func (m searchSelectModel) fetchPage(page int) tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.Search(m.query, page, 25, m.opts)
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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

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
						m.selectedBook = m.books[m.cursor]
						return m, tea.Quit
					}
				}
			case "/":
				// Enter inline query editing, pre-filled with the current
				// query so it can be tweaked rather than retyped.
				m.state = searchStateFilter
				m.input.SetValue(m.query)
				m.input.CursorEnd()
				return m, m.input.Focus()
			case "q", "esc", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			}
		case searchStateFilter:
			switch msg.String() {
			case "enter":
				q := m.input.Value()
				m.input.Blur()
				if strings.TrimSpace(q) == "" || q == m.query {
					// Nothing to re-run; return to the current results.
					m.state = searchStateResults
					return m, nil
				}
				m.query = q
				m.page = 1
				m.totalPages = 0
				m.cursor = 0
				m.state = searchStateLoading
				return m, tea.Batch(m.spinner.Tick, m.fetchPage(1))
			case "esc":
				m.input.Blur()
				m.state = searchStateResults
				return m, nil
			case "ctrl+c":
				m.input.Blur()
				m.quitting = true
				return m, tea.Quit
			}
			// Any other key edits the query; fall through to input.Update below.
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

	// While editing, route remaining messages (typed keys, cursor blink) to
	// the text input so it updates and keeps blinking.
	if m.state == searchStateFilter {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
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
	b.WriteString("\n")
	if m.state == searchStateFilter {
		// Show the inline editor above the current (stale) results.
		b.WriteString("  " + m.input.View() + "\n")
	}
	b.WriteString("\n")

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
		colRating = 6
	)

	// Title column width: fixed by default, widened to the longest
	// title when --full-title is set so nothing gets truncated.
	titleWidth := colTitle
	if m.fullTitle {
		for _, book := range m.books {
			if w := runewidth.StringWidth(book.Name); w > titleWidth {
				titleWidth = w
			}
		}
		// Cap to the terminal width so the table doesn't wrap and break
		// alignment. fixedCols is the width of everything except the title.
		const fixedCols = 61
		if maxTitle := m.width - fixedCols; m.width > 0 && titleWidth > maxTitle && maxTitle >= colTitle {
			titleWidth = maxTitle
		}
	}

	// Table header
	hdrStyle := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(currentTheme.Surface)

	header := fmt.Sprintf("    %-*s  %-*s  %-*s  %-*s  %-*s  %*s  %*s",
		colNum, "#", colID, "ID", titleWidth, "Title", colAuthor, "Author", colFmt, "Fmt", colSize, "Size", colRating, "Rating")
	b.WriteString(hdrStyle.Render(header))
	b.WriteString("\n")

	sepLen := 4 + colNum + 2 + colID + 2 + titleWidth + 2 + colAuthor + 2 + colFmt + 2 + colSize + 2 + colRating
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
		rating := book.Rating
		if rating == "" {
			rating = "-"
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
		ratingStyle := lipgloss.NewStyle().Foreground(currentTheme.Success)
		if selected {
			numStyle = numStyle.Background(currentTheme.Surface)
			idStyle = idStyle.Background(currentTheme.Surface)
			titleStyle = titleStyle.Background(currentTheme.Surface)
			authorStyle = authorStyle.Background(currentTheme.Surface)
			extStyle = extStyle.Background(currentTheme.Surface)
			sizeStyle = sizeStyle.Background(currentTheme.Surface)
			ratingStyle = ratingStyle.Background(currentTheme.Surface)
		}

		content := fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s",
			numStyle.Render(fmt.Sprintf("%-*d", colNum, i+1)),
			idStyle.Render(runewidth.FillRight(runewidth.Truncate(id, colID, ""), colID)),
			titleStyle.Render(runewidth.FillRight(runewidth.Truncate(book.Name, titleWidth, ""), titleWidth)),
			authorStyle.Render(runewidth.FillRight(runewidth.Truncate(authors, colAuthor, ""), colAuthor)),
			extStyle.Render(runewidth.FillRight(runewidth.Truncate(ext, colFmt, ""), colFmt)),
			sizeStyle.Render(fmt.Sprintf("%*s", colSize, runewidth.Truncate(size, colSize, ""))),
			ratingStyle.Render(fmt.Sprintf("%*s", colRating, runewidth.Truncate(rating, colRating, ""))),
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
	var help string
	if m.state == searchStateFilter {
		help = fmt.Sprintf("  %s search  %s cancel",
			helpKeys.Render("enter"), helpKeys.Render("esc"))
	} else {
		help = fmt.Sprintf("  %s select  %s page  %s search  %s download  %s quit",
			helpKeys.Render("↑/↓"), helpKeys.Render("←/→"), helpKeys.Render("/"),
			helpKeys.Render("enter"), helpKeys.Render("q"))
	}
	b.WriteString(helpStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

// interactiveSearch prompts for a query then runs the interactive search model.
// Returns the selected book — its ID is empty if the user cancelled. The book
// carries the card's /dl/ URL when the search page exposed one.
func interactiveSearch(c *zlib.Client, opts *zlib.SearchOptions, fullTitle bool) (zlib.Book, error) {
	var query string
	var selectedExts []string

	// Pre-select extensions from CLI --ext flags
	if opts != nil {
		for _, e := range opts.Extensions {
			selectedExts = append(selectedExts, string(e))
		}
	}

	inputForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Search books").
				Placeholder("enter title, author, or ISBN...").
				Value(&query),
			huh.NewMultiSelect[string]().
				Title("Filter extensions").
				Description("(skip to search all formats)").
				Options(
					huh.NewOption("EPUB", "EPUB"),
					huh.NewOption("AZW", "AZW"),
					huh.NewOption("AZW3", "AZW3"),
					huh.NewOption("MOBI", "MOBI"),
					huh.NewOption("PDF", "PDF"),
					huh.NewOption("RTF", "RTF"),
					huh.NewOption("TXT", "TXT"),
				).
				Value(&selectedExts),
		),
	).WithTheme(huhTheme())
	if err := inputForm.Run(); err != nil {
		return zlib.Book{}, err
	}
	if strings.TrimSpace(query) == "" {
		return zlib.Book{}, nil
	}

	// Merge selected extensions into opts
	if len(selectedExts) > 0 {
		if opts == nil {
			opts = &zlib.SearchOptions{}
		}
		opts.Extensions = nil
		for _, e := range selectedExts {
			opts.Extensions = append(opts.Extensions, zlib.Extension(e))
		}
	}

	p := tea.NewProgram(newSearchSelectModel(query, c, opts, fullTitle))
	result, err := p.Run()
	if err != nil {
		return zlib.Book{}, err
	}

	sm := result.(searchSelectModel)
	if sm.err != nil {
		return zlib.Book{}, sm.err
	}
	sm.selectedBook.ID = sm.selectedID
	return sm.selectedBook, nil
}
