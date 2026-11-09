package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

func bookCell(s string) string {
	return strings.Join(strings.Fields(ansi.Strip(s)), " ")
}

func (m searchSelectModel) feedRows() int {
	height := m.height
	if height == 0 {
		height = 24
	}
	if m.hasBookCard() {
		cardRows := selectedBookCardRows(m.width, height)
		if height >= 15 {
			return max(1, height-cardRows-6)
		}
		return max(1, height-cardRows-5)
	}
	return max(1, height-12)
}

func (m searchSelectModel) hasBookCard() bool {
	return m.feed == nil || m.showBookCard
}

// Keep the viewport still while the cursor moves within its visible rows.
func (m *searchSelectModel) ensureCursorVisible() {
	m.listOffset = visibleListOffset(m.listOffset, m.cursor, len(m.books), m.feedRows())
}

func visibleListOffset(offset, cursor, count, rows int) int {
	offset = min(max(0, offset), max(0, count-rows))
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+rows {
		return cursor - rows + 1
	}
	return offset
}

func (m searchSelectModel) feedView() string {
	return m.feedViewWith("Author", "No books found.", m.positionLabel(), "")
}

// pageLabel names the server page, e.g. "Page 1 of 10". The EAPI reports only
// a page count, so lists never claim a total number of books.
func pageLabel(page, totalPages int) string {
	if totalPages > 0 {
		return fmt.Sprintf("Page %d of %d", page, totalPages)
	}
	return fmt.Sprintf("Page %d", page)
}

// positionLabel names the selected row within the loaded page, e.g. "Book 5 of
// 50". It is the list's only position indicator: the title row carries no
// counter and there is no progress bar, which used to repeat the same number
// and read as progress through all pages.
func (m searchSelectModel) positionLabel() string {
	if len(m.books) == 0 {
		return ""
	}
	return fmt.Sprintf("Book %d of %d", min(max(0, m.cursor), len(m.books)-1)+1, len(m.books))
}

// joinStatus joins the non-empty parts of a list status line.
func joinStatus(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func (m searchSelectModel) feedViewWith(column, empty, status, navigation string) string {
	width, height := m.width, m.height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	if width < 30 || height < 14 {
		return ansi.Truncate("Enlarge terminal · q quit", max(1, width), "…")
	}
	accent := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	title := strings.TrimPrefix(m.feedTitle, "Browse ")
	if title != "" {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	lines := []string{accent.Render(title), muted.Render(status)}
	if m.state == searchStateLoading {
		return strings.Join(append([]string{accent.Render(title), ""},
			m.spinner.View(),
			"",
			muted.Render(m.exitHint())), "\n")
	}
	if len(m.books) == 0 {
		help := navigation + m.exitHint()
		if strings.Contains(navigation, "/ search") {
			lines = append(lines, ansi.Truncate("No books found for this search.", width, "…"), ansi.Truncate("Try another title, author, or ISBN.", width, "…"), "")
			help = "/ search · " + m.exitHint()
			if m.inShell {
				help = "Ctrl+F search · " + m.exitHint()
			}
			if m.state == searchStateFilter {
				help = "enter search · esc cancel"
			}
			return strings.Join(append(lines, muted.Render(ansi.Truncate(help, width, "…"))), "\n")
		}
		if m.state == searchStateFilter {
			help = "enter search · esc cancel"
		}
		return strings.Join(append(lines, empty, "", ansi.Truncate(help, width, "…")), "\n")
	}
	cursor := min(max(0, m.cursor), len(m.books)-1)

	if m.detailOpen {
		book := m.selectedBook()
		return "Book details\n\n" + selectedBookCardWithCover(book, width, height, m.details.coverView(m.selectedBookRef(), book.Cover)) + "\n\n" + "Enter download · Esc back"
	}
	showCard := m.hasBookCard()
	if showCard {
		lines = append(lines, selectedBookCardWithCover(m.selectedBook(), width, height, m.details.coverView(m.selectedBookRef(), m.selectedBook().Cover)), "")
	}
	count := m.feedRows()
	start := min(max(0, m.listOffset), max(0, len(m.books)-count))
	if cursor < start {
		start = cursor
	} else if cursor >= start+count {
		start = cursor - count + 1
	}
	end := min(len(m.books), start+count)
	numberWidth := len(fmt.Sprint(len(m.books)))
	titleNeed, detailNeed := len("Title"), ansi.StringWidth(column)
	for _, book := range m.books {
		titleNeed = max(titleNeed, ansi.StringWidth(bookCell(book.Name)))
		detailNeed = max(detailNeed, ansi.StringWidth(bookCell(strings.Join(book.Authors, ", "))))
	}
	titleWidth, authorWidth, gapWidth := bookColumnWidths(width, numberWidth, titleNeed, detailNeed)
	gap := strings.Repeat(" ", gapWidth)
	cell := func(s string, w int) string {
		s = ansi.Truncate(bookCell(s), w, "…")
		return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
	}
	row := func(number, title, author string) string {
		s := "  " + strings.Repeat(" ", max(0, numberWidth-ansi.StringWidth(number))) + number + gap + cell(title, titleWidth)
		if authorWidth > 0 {
			s += gap + cell(author, authorWidth)
		}
		return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
	}
	lines = append(lines, muted.Render(row("#", "Title", column)))
	for i := start; i < end; i++ {
		book := m.books[i]
		s := row(fmt.Sprint(i+1), book.Name, strings.Join(book.Authors, ", "))
		if i == cursor {
			s = styledListRow(s[2:], true, width, currentTheme)
		} else {
			s = styledListRow(s[2:], false, width, currentTheme)
		}
		lines = append(lines, s)
	}
	for range count - (end - start) {
		lines = append(lines, "")
	}
	book := m.books[cursor]
	if !showCard {
		lines = append(lines, "")
		preview := strings.Split(ansi.Wrap(bookCell(book.Name), width, ""), "\n")
		for i := range 3 {
			line := ""
			if i < len(preview) {
				line = preview[i]
				if i == 2 && len(preview) > 3 {
					line = ansi.Truncate(line+"…", width, "…")
				}
			}
			lines = append(lines, accent.Render(line))
		}
		lines = append(lines, muted.Render(ansi.Truncate(bookCell(strings.Join(book.Authors, ", ")), width, "…")))
		lines = append(lines, ansi.Truncate(feedBookMetadata(book), width, "…"), "")
	}
	help := "↑/↓ select · PgUp/PgDn scroll · i details · enter download · " + m.exitHint()
	lines[0] = accent.Render(ansi.Truncate(title, width, "…"))
	if navigation != "" {
		help = "↑/↓ select · ←/→ page · i details · enter download · " + m.exitHint()
	}
	if strings.Contains(navigation, "/ search") {
		help = "↑/↓ select · ←/→ page · / search · i details · enter download · " + m.exitHint()
		if m.inShell {
			help = strings.Replace(help, "/ search", "Ctrl+F search", 1)
		}
	}
	if width < 70 {
		switch {
		case strings.Contains(navigation, "/ search") && m.inShell:
			help = "↑↓ · enter · Ctrl+F · " + m.exitHint()
		case strings.Contains(navigation, "/ search"):
			help = "↑↓ · enter · / · " + m.exitHint()
		case navigation != "":
			help = "↑↓ · ←→ · enter · " + m.exitHint()
		default:
			help = "↑↓ · enter · " + m.exitHint()
		}
	}
	if m.listAction != "" {
		if width < 70 {
			help = "↑↓ · enter · " + m.listAction + " · " + m.exitHint()
		} else {
			help = "↑/↓ select · ←/→ page · i details · enter download · " + m.listAction + " · " + m.exitHint()
		}
	}
	if m.state == searchStateFilter {
		help = "enter search · esc cancel"
	}
	if showCard && height >= 15 {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(help))
	return strings.Join(lines, "\n")
}

func (m searchSelectModel) exitHint() string {
	if m.inShell {
		return "q back"
	}
	return "q quit"
}

// Measure the whole page, not the selected row, to keep columns stationary.
// Spend spare cells on content before truncating either column.
func bookColumnWidths(width, numberWidth, titleNeed, detailNeed int) (int, int, int) {
	gap := 3
	if 2+numberWidth+2*gap+titleNeed+detailNeed > width {
		gap = 2
	}
	available := max(1, width-2-numberWidth-2*gap)
	if titleNeed+detailNeed <= available {
		return titleNeed, detailNeed, gap
	}
	titleMin, detailMin := min(16, titleNeed), min(8, detailNeed)
	if available < titleMin+detailMin {
		return min(titleNeed, max(1, width-2-numberWidth-gap)), 0, gap
	}
	// Reserve a readable detail column, favor titles, then redistribute leftovers.
	detail := min(detailNeed, max(detailMin, available/3))
	title := min(titleNeed, available-detail)
	detail = min(detailNeed, available-title)
	return title, detail, gap
}

// Reserve the same space for every book so selection never moves the list.
func selectedBookCard(book zlib.Book, width int) string {
	return selectedBookCardBorder(book, width, 0)
}

func selectedBookCardRows(width, height int) int {
	if width == 0 {
		width = 80
	}
	if width < 78 {
		if height >= 24 {
			return 13
		}
	} else if height >= 20 {
		return 11
	}
	return 8
}

func selectedBookCardBorder(book zlib.Book, width, height int) string {
	return selectedBookCardWithCover(book, width, height, "")
}

func selectedBookCardWithCover(book zlib.Book, width, height int, cover string) string {
	inner := max(1, width-6)
	showCover := coverFits(width, height)
	if showCover {
		inner -= coverWidth + 2
	}
	accent := lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	primary := lipgloss.NewStyle().Foreground(currentTheme.Title)
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	title := strings.Split(ansi.Wrap(bookCell(book.Name), inner, ""), "\n")
	var lines []string
	for i := range 2 {
		line := ""
		if i < len(title) {
			line = title[i]
			if i == 1 && len(title) > 2 {
				line = ansi.Truncate(line, max(1, inner-1), "") + "…"
			}
		}
		lines = append(lines, accent.Render(line))
	}
	lines = append(lines, primary.Render(ansi.Truncate(bookCell(strings.Join(book.Authors, ", ")), inner, "…")))
	var details []string
	fileDetails := []string{strings.ToUpper(book.Extension), book.Size}
	if selectedBookCardRows(width, height) == 8 {
		fileDetails = append(fileDetails, book.Year)
	}
	for _, value := range fileDetails {
		if value = bookCell(value); value != "" {
			details = append(details, value)
		}
	}
	lines = append(lines, muted.Render(ansi.Truncate(strings.Join(details, " · "), inner, "…")))
	if selectedBookCardRows(width, height) == 8 {
		details = details[:0]
		for _, value := range []string{book.Language, book.Publisher} {
			if value = bookCell(value); value != "" {
				details = append(details, value)
			}
		}
		lines = append(lines, muted.Render(ansi.Truncate(strings.Join(details, " · "), inner, "…")))
	} else {
		line := func(parts ...string) string {
			var present []string
			for _, part := range parts {
				if part != "" {
					present = append(present, part)
				}
			}
			return muted.Render(ansi.Truncate(strings.Join(present, " · "), inner, "…"))
		}
		field := func(label, value string) string {
			if value == "" {
				return ""
			}
			return label + ": " + bookCell(value)
		}
		category := field("Categories", book.Categories)
		if width < 78 {
			lines = append(lines,
				line(field("Year", book.Year), field("Ed", book.Edition), bookCell(book.Pages)),
				line(bookCell(book.Language), field("Pub", book.Publisher)),
			)
			wrapped := strings.Split(ansi.Hardwrap(category, inner, false), "\n")
			for i := range 2 {
				value := ""
				if i < len(wrapped) {
					value = wrapped[i]
				}
				lines = append(lines, muted.Render(ansi.Truncate(value, inner, "…")))
			}
			lines = append(lines, line(field("ISBN 10", book.ISBN10)), line(field("ISBN 13", book.ISBN13)))
		} else {
			lines = append(lines,
				line(field("Year", book.Year), field("Edition", book.Edition), field("Pages", book.Pages)),
				line(field("Language", book.Language), field("Publisher", book.Publisher)),
			)
			lines = append(lines, line(category), line(field("ISBN 10", book.ISBN10), field("ISBN 13", book.ISBN13)))
		}
	}
	lines = append(lines, muted.Render(ansi.Truncate("ID: "+bookCommandID(book), inner, "…")))
	body := strings.Join(lines, "\n")
	if showCover {
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(coverWidth).Height(coverRows).Render(cover), "  ", lipgloss.NewStyle().Width(inner).Render(body))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(currentTheme.Accent).
		Padding(0, 1).Width(width - 2).Render(body)
}

func feedBookMetadata(book zlib.Book) string {
	parts := []string{"ID: " + bookCommandID(book)}
	for _, value := range []string{strings.ToUpper(book.Extension), book.Size} {
		if value != "" {
			parts = append(parts, bookCell(value))
		}
	}
	return strings.Join(parts, " · ")
}
