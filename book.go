package zlib

import "sync"

// detailFetchConcurrency bounds the book-detail fan-out. An unbounded burst
// makes every goroutine clear its own challenge instead of sharing a token.
const detailFetchConcurrency = 4

// FetchBookDetails resolves ids concurrently, returning an entry for every id;
// ids that fail map to a zero Book.
//
// The first id is fetched alone as a warm-up. Z-Library challenges /book/ pages
// harder than list pages, so a cold burst makes each goroutine solve its own
// challenge and serve out its own backoff; clearing one first lets the rest
// share the c_token.
//
// If that warm-up fails, the remaining ids are skipped. When the server is in
// that mood every fetch fails the same slow way and backfills nothing —
// measured on a live account: 21 ids, 21 failures, zero fields filled — so
// paying the backoff N more times buys nothing.
func (c *Client) FetchBookDetails(ids []string) map[string]Book {
	out := make(map[string]Book, len(ids))
	if len(ids) == 0 {
		return out
	}

	first, err := c.FetchBook(ids[0])
	if err != nil {
		for _, id := range ids {
			out[id] = Book{}
		}
		return out
	}
	out[ids[0]] = first

	type entry struct {
		id   string
		book Book
	}
	rest := ids[1:]
	results := make(chan entry, len(rest))
	slots := make(chan struct{}, detailFetchConcurrency)
	var wg sync.WaitGroup
	for _, id := range rest {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			book, err := c.FetchBook(id)
			if err != nil {
				results <- entry{id: id}
				return
			}
			results <- entry{id: id, book: book}
		}()
	}
	wg.Wait()
	close(results)

	for e := range results {
		out[e.id] = e.book
	}
	return out
}

func (c *Client) FetchBook(id string) (Book, error) {
	if !c.loggedIn {
		return Book{}, ErrNotLoggedIn
	}
	if id == "" {
		return Book{}, ErrNoID
	}
	if c.mode == ClientModeEAPI {
		return c.fetchBookEAPI(id)
	}

	bookURL := BuildBookURL(c.domain, id)
	html, err := c.get(bookURL)
	if err != nil {
		return Book{}, err
	}

	book, err := parseBookDetail(html, c.domain)
	if err != nil {
		return Book{}, err
	}
	book.URL = bookURL
	return book, nil
}
