package metasource

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ParseQueryEdit reads what the user typed to change a search: a title,
// authors separated by "&" and ISBNs separated by commas, semicolons or line
// breaks. It returns the new query, or an error saying what cannot be searched.
//
// Text is cleaned of control characters, and each ISBN must be valid (an
// ISBN-10 is read as its ISBN-13). An ISBN the user typed is confirmed, like
// one passed with --isbn. An ISBN the old query held as unconfirmed, such as a
// download's hint or one read from the book, stays unconfirmed for as long as
// it is left in the field: editing only the title must not promote it.
func ParseQueryEdit(old Query, title, authors, isbns string) (Query, error) {
	q := Query{Title: clean(title), Authors: cleanList(strings.Split(authors, "&"))}
	split := func(r rune) bool { return r == ',' || r == ';' || r == '\n' }
	for _, token := range strings.FieldsFunc(isbns, split) {
		token = clean(token)
		if token == "" {
			continue
		}
		isbn := NormalizeISBN(token)
		if isbn == "" {
			return Query{}, fmt.Errorf("%q is not a valid ISBN: check the digits and the check digit", shorten(token, 24))
		}
		if slices.Contains(q.ISBNs, isbn) {
			continue
		}
		q.ISBNs = append(q.ISBNs, isbn)
		if slices.Contains(old.Unconfirmed, isbn) {
			q.Unconfirmed = append(q.Unconfirmed, isbn)
		}
	}
	if q.Empty() {
		return Query{}, errors.New("Enter a title, an author or an ISBN to search for")
	}
	return q, nil
}

// shorten cuts s to n runes, so a long pasted value cannot fill an error line.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
