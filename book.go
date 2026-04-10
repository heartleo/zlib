package zlib

func (c *Client) FetchBookDetails(ids []string) map[string]Book {
	type entry struct {
		id   string
		book Book
	}
	ch := make(chan entry, len(ids))
	for _, id := range ids {
		go func(id string) {
			book, err := c.FetchBook(id)
			if err != nil {
				ch <- entry{id: id}
				return
			}
			ch <- entry{id: id, book: book}
		}(id)
	}
	out := make(map[string]Book, len(ids))
	for range ids {
		e := <-ch
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
