package cli

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// — interactive history bubbletea model —

type historyState int

const (
	historyStateLoading historyState = iota
	historyStateResults
	historyStateConfirmDelete
	historyStateDeleting
)

type historySelectModel struct {
	client          *zlib.Client
	items           []zlib.DownloadHistoryItem
	details         bookDetailState
	detailOpen      bool
	switchDomain    bool // the user asked to change domain from a connection error
	page            int
	totalPages      int
	hasNext         bool
	cursor          int
	listOffset      int
	width, height   int
	inShell         bool
	state           historyState
	spinner         spinner.Model
	selected        *zlib.DownloadHistoryItem // set when user presses Enter
	kindleShortcut  bool
	err             error
	retryPage       int
	quitting        bool
	deleteError     error
	deleteFocused   bool
	confirmDownload bool
	downloadFocused bool
}

type historyDeletedMsg struct{ err error }

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
			return listLoadError{err, page}
		}
		// Fetch missing extension/size details
		var missingIDs []string
		for _, item := range result.Items {
			if id := historyCommandID(item); id != "" && item.Extension == "" {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) > 0 {
			details := m.client.FetchBookDetails(missingIDs)
			for i, item := range result.Items {
				id := historyCommandID(item)
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
			hasNext:    historyHasNext(result),
		}
	}
}

func (m historySelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case historyDeletedMsg:
		m.state = historyStateResults
		m.deleteError = msg.err
		if msg.err != nil {
			return m, nil
		}
		page := m.page
		if len(m.items) == 1 && page > 1 {
			page--
		}
		m.state = historyStateLoading
		return m, tea.Batch(m.spinner.Tick, m.fetchPage(page))

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureCursorVisible()
		return m, m.queueSelectedDetail()

	case listLoadError:
		m.err = msg.err
		m.retryPage = msg.page
		m.state = historyStateResults
		return m, nil

	case historyResultMsg:
		m.err = nil
		m.items = msg.items
		m.page = msg.page
		m.totalPages = msg.totalPages
		m.hasNext = msg.hasNext
		m.cursor = 0
		m.listOffset = 0
		m.deleteError = nil
		m.state = historyStateResults
		return m, m.queueSelectedDetail()

	case bookDetailReadyMsg:
		if msg.sequence == m.details.sequence && msg.ref == m.selectedBookRef() && m.state == historyStateResults {
			return m, tea.Batch(m.details.fetch(m.client, msg.ref), m.queueSelectedCover())
		}
		return m, nil

	case bookDetailResultMsg:
		m.details.store(msg)
		return m, m.queueSelectedCover()

	case bookCoverResultMsg:
		m.details.storeCover(msg)
		return m, m.queueSelectedCover()

	case coverTickMsg:
		ref, url := m.selectedCoverKey()
		return m, m.details.advanceCoverTick(m.client, m.width, m.height, ref, url)

	case tea.KeyPressMsg:
		if msg.String() == "d" && m.err != nil && offersDomainSwitch(m.err, m.inShell, "/history") {
			m.switchDomain = true
			return m, tea.Quit
		}
		if m.err != nil && msg.String() != "r" && msg.String() != "esc" && msg.String() != "q" && msg.String() != "ctrl+c" {
			return m, nil
		}
		if m.confirmDownload {
			switch msg.String() {
			case "left", "right", "tab", "shift+tab":
				m.downloadFocused = !m.downloadFocused
			case "esc", "ctrl+c", "n":
				m.confirmDownload = false
			case "enter":
				m.confirmDownload = false
				if m.downloadFocused {
					item := m.items[m.cursor]
					m.selected = &item
					return m, tea.Quit
				}
			}
			return m, nil
		}
		if m.detailOpen {
			switch msg.String() {
			case "esc", "i":
				m.detailOpen = false
				return m, nil
			case "enter", "s", "ctrl+c":
			default:
				return m, nil
			}
		} else if msg.String() == "i" && m.state == historyStateResults && len(m.items) > 0 {
			m.detailOpen = true
			return m, m.queueSelectedDetail()
		}
		switch m.state {
		case historyStateConfirmDelete:
			if msg.String() == "left" || msg.String() == "right" || msg.String() == "tab" || msg.String() == "shift+tab" {
				m.deleteFocused = !m.deleteFocused
				return m, nil
			}
			if msg.String() == "y" || (msg.String() == "enter" && m.deleteFocused) {
				id := m.items[m.cursor].ID
				m.state = historyStateDeleting
				return m, tea.Batch(m.spinner.Tick, func() tea.Msg { return historyDeletedMsg{m.client.DeleteDownloadHistory(id)} })
			}
			if msg.String() == "n" || msg.String() == "esc" || msg.String() == "ctrl+c" || msg.String() == "enter" {
				m.state = historyStateResults
			}
			return m, nil
		case historyStateDeleting:
			return m, nil
		case historyStateResults:
			previousCursor := m.cursor
			switch msg.String() {
			case "r":
				page := m.page
				if m.err != nil {
					page = max(1, m.retryPage)
				}
				m.err = nil
				m.state = historyStateLoading
				return m, tea.Batch(m.spinner.Tick, m.fetchPage(page))
			case "d", "delete":
				if len(m.items) > 0 && m.items[m.cursor].ID != "" {
					m.deleteError = nil
					m.deleteFocused = false
					m.state = historyStateConfirmDelete
				}
			case "pgdown":
				m.cursor = min(max(0, len(m.items)-1), m.cursor+(searchSelectModel{width: m.width, height: m.height}).feedRows())
			case "pgup":
				m.cursor = max(0, m.cursor-(searchSelectModel{width: m.width, height: m.height}).feedRows())
			case "home":
				m.cursor = 0
			case "end":
				m.cursor = max(0, len(m.items)-1)
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
			case "s":
				if len(m.items) > 0 && m.cursor < len(m.items) {
					item := m.items[m.cursor]
					if item.DownloadURL != "" || historyCommandID(item) != "" {
						m.selected, m.kindleShortcut = &item, true
						return m, tea.Quit
					}
				}
			case "enter":
				if len(m.items) > 0 && m.cursor < len(m.items) {
					item := m.items[m.cursor]
					if item.DownloadURL != "" || historyCommandID(item) != "" {
						m.confirmDownload = true
						m.downloadFocused = false
						return m, nil
					}
				}
			case "q", "esc", "ctrl+c":
				m.err = nil
				m.quitting = true
				return m, tea.Quit
			}
			if m.cursor != previousCursor {
				m.ensureCursorVisible()
				return m, m.queueSelectedDetail()
			}
		case historyStateLoading:
			if msg.String() == "q" || msg.String() == "esc" || msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		if m.state == historyStateLoading || m.state == historyStateDeleting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func historyHasNext(result zlib.DownloadHistoryResult) bool {
	if result.TotalPages > 0 {
		return result.Page < result.TotalPages
	}
	return len(result.Items) > 0
}

func (m *historySelectModel) ensureCursorVisible() {
	rows := (searchSelectModel{width: m.width, height: m.height}).feedRows()
	m.listOffset = visibleListOffset(m.listOffset, m.cursor, len(m.items), rows)
}

func (m historySelectModel) domainSwitchRequest() (string, bool) {
	return "/history", m.switchDomain
}

func (m historySelectModel) View() tea.View {
	if m.err != nil {
		body, subtitle, bindings := loadErrorView(m.err, m.width, offersDomainSwitch(m.err, m.inShell, "/history"),
			"Could not load history", uiKey("r", "retry"), uiKey("esc", "back"))
		return screenView(m.width, m.height, "Download history", subtitle, body, "", bindings...)
	}
	list := searchSelectModel{detailOpen: m.detailOpen, feedTitle: "Download history", width: m.width, height: m.height, cursor: m.cursor, listOffset: m.listOffset, listAction: "d delete", inShell: m.inShell, spinner: m.spinner, state: searchStateResults, details: m.details}
	if m.state == historyStateLoading {
		list.state = searchStateLoading
	}
	for _, item := range m.items {
		book := zlib.Book{ID: item.ID, Hash: item.Hash, URL: item.URL, Name: item.Name, Authors: item.Authors, Extension: item.Extension, Size: item.Size}
		list.books = append(list.books, m.details.book(historyCommandID(item), book))
	}
	status := pageLabel(m.page, m.totalPages)
	if m.state == historyStateResults && m.cursor < len(m.items) {
		date := bookCell(m.items[m.cursor].Date)
		if m.width > 0 && m.width < 40 {
			// The date matters more than the position on a narrow terminal.
			status = joinStatus(status, date)
		} else {
			if date != "" {
				date = "Downloaded " + date
			}
			status = joinStatus(status, list.positionLabel(), date)
		}
	}
	v := themedView(list.feedViewWith("Author", "No history on this page.", status, "←/→ page · "))
	if m.state == historyStateDeleting || m.deleteError != nil {
		lines := strings.Split(v.Content, "\n")
		muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
		if m.state == historyStateDeleting {
			lines[len(lines)-1] = m.spinner.View()
		} else {
			lines[1] = bookCell(m.deleteError.Error())
			lines[len(lines)-1] = muted.Render("Delete failed · d retry · " + list.exitHint())
		}
		v.Content = strings.Join(lines, "\n")
	}
	if m.state == historyStateConfirmDelete {
		v.Content = m.deleteDialog(v.Content)
	}
	if m.confirmDownload {
		book := m.items[m.cursor]
		v.Content = confirmationDialog(v.Content, m.width, m.height, "Download book?", book.Name, historyCommandID(book), "", "Download", m.downloadFocused, false)
	}
	return fullScreenView(m.width, m.height, v.Content)
}

func (m historySelectModel) deleteDialog(background string) string {
	book := m.items[m.cursor]
	return confirmationDialog(background, m.width, m.height, "Delete history?", book.Name, book.ID, "Local files are kept.", "Delete", m.deleteFocused, true)
}
