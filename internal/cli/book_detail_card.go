package cli

import (
	"context"
	"image"
	"time"

	tea "charm.land/bubbletea/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

type bookDetailReadyMsg struct {
	ref      string
	sequence uint64
}

type bookDetailResultMsg struct {
	ref  string
	book zlib.Book
	err  error
}

type bookDetailState struct {
	coverImages     map[string]image.Image
	covers          map[string]string
	coverPending    map[string]bool
	coverOrder      []string
	coverWanted     string
	coverPrefetched map[string]bool
	// coverFetcher replaces Client.FetchCover in tests.
	coverFetcher func(context.Context, string) (image.Image, error)
	// Loading spinner clock; see startCoverTick.
	coverFrame   int
	coverTicking bool
	sequence     uint64
	cache        map[string]bookDetailResultMsg
	pending      map[string]bool
}

func (d *bookDetailState) schedule(client *zlib.Client, ref string) tea.Cmd {
	d.sequence++
	if client == nil || ref == "" {
		return nil
	}
	sequence := d.sequence
	return tea.Tick(140*time.Millisecond, func(time.Time) tea.Msg {
		return bookDetailReadyMsg{ref: ref, sequence: sequence}
	})
}

func (d *bookDetailState) fetch(client *zlib.Client, ref string) tea.Cmd {
	if _, loaded := d.cache[ref]; client == nil || ref == "" || loaded || d.pending[ref] {
		return nil
	}
	if d.pending == nil {
		d.pending = make(map[string]bool)
	}
	d.pending[ref] = true
	return func() tea.Msg {
		book, err := client.FetchBook(ref)
		return bookDetailResultMsg{ref: ref, book: book, err: err}
	}
}

func (d *bookDetailState) store(msg bookDetailResultMsg) {
	delete(d.pending, msg.ref)
	if d.cache == nil {
		d.cache = make(map[string]bookDetailResultMsg)
	}
	d.cache[msg.ref] = msg
}

func (d bookDetailState) book(ref string, fallback zlib.Book) zlib.Book {
	result, ok := d.cache[ref]
	if !ok || result.err != nil {
		return fallback
	}
	book := result.book
	if book.Name != "" {
		fallback.Name = book.Name
	}
	if len(book.Authors) > 0 {
		fallback.Authors = book.Authors
	}
	for _, field := range []struct {
		target *string
		value  string
	}{
		{&fallback.Cover, book.Cover}, {&fallback.Extension, book.Extension}, {&fallback.Size, book.Size},
		{&fallback.Year, book.Year}, {&fallback.Edition, book.Edition},
		{&fallback.Pages, book.Pages}, {&fallback.Language, book.Language},
		{&fallback.Publisher, book.Publisher}, {&fallback.Categories, book.Categories},
		{&fallback.ISBN, book.ISBN}, {&fallback.ISBN10, book.ISBN10},
		{&fallback.ISBN13, book.ISBN13},
	} {
		if field.value != "" {
			*field.target = field.value
		}
	}
	return fallback
}

func (m searchSelectModel) selectedBookRef() string {
	if m.cursor < 0 || m.cursor >= len(m.books) {
		return ""
	}
	return bookCommandID(m.books[m.cursor])
}

func (m searchSelectModel) selectedBook() zlib.Book {
	book := m.books[m.cursor]
	return m.details.book(bookCommandID(book), book)
}

func (m *searchSelectModel) queueSelectedDetail() tea.Cmd {
	return tea.Batch(m.details.schedule(m.client, m.selectedBookRef()), m.queueSelectedCover())
}

func (m historySelectModel) selectedBookRef() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return historyCommandID(m.items[m.cursor])
}

func (m *historySelectModel) queueSelectedDetail() tea.Cmd {
	return tea.Batch(m.details.schedule(m.client, m.selectedBookRef()), m.queueSelectedCover())
}
