package zlib

import (
	"fmt"
	"net/url"
	"strings"
)

func (c *Client) Search(query string, page, count int, opts *SearchOptions) (SearchResult, error) {
	if !c.loggedIn {
		return SearchResult{}, ErrNotLoggedIn
	}
	if query == "" {
		return SearchResult{}, ErrEmptyQuery
	}
	if count <= 0 {
		count = 10
	}
	if count > 50 {
		count = 50
	}
	if page <= 0 {
		page = 1
	}

	u := BuildSearchURL(c.domain, query)
	if opts != nil {
		u += buildSearchParams(opts.Exact, opts.FromYear, opts.ToYear, opts.Languages, opts.Extensions)
	}
	u += fmt.Sprintf("&page=%d", page)

	html, err := c.get(u)
	if err != nil {
		return SearchResult{}, err
	}

	books, totalPages, err := parseSearchResults(html, c.domain)
	if err != nil {
		return SearchResult{}, err
	}

	if len(books) > count {
		books = books[:count]
	}

	return SearchResult{
		Books:      books,
		Page:       page,
		TotalPages: totalPages,
	}, nil
}

func (c *Client) FullTextSearch(query string, page, count int, opts *FullTextSearchOptions) (SearchResult, error) {
	if !c.loggedIn {
		return SearchResult{}, ErrNotLoggedIn
	}
	if query == "" {
		return SearchResult{}, ErrEmptyQuery
	}

	searchType := "words"
	if opts != nil && opts.Phrase {
		words := strings.Fields(query)
		if len(words) < 2 {
			return SearchResult{}, ErrPhraseMinWords
		}
		searchType = "phrase"
	}

	if count <= 0 {
		count = 10
	}
	if count > 50 {
		count = 50
	}
	if page <= 0 {
		page = 1
	}

	u := BuildFullTextSearchURL(c.domain, query, searchType)
	if opts != nil {
		u += buildSearchParams(opts.Exact, opts.FromYear, opts.ToYear, opts.Languages, opts.Extensions)
	}
	u += fmt.Sprintf("&page=%d", page)

	html, err := c.get(u)
	if err != nil {
		return SearchResult{}, err
	}

	books, totalPages, err := parseSearchResults(html, c.domain)
	if err != nil {
		return SearchResult{}, err
	}

	if len(books) > count {
		books = books[:count]
	}

	return SearchResult{
		Books:      books,
		Page:       page,
		TotalPages: totalPages,
	}, nil
}

func buildSearchParams(exact bool, fromYear, toYear int, langs []Language, exts []Extension) string {
	var sb strings.Builder
	if exact {
		sb.WriteString("&e=1")
	}
	if fromYear > 0 {
		fmt.Fprintf(&sb, "&yearFrom=%d", fromYear)
	}
	if toYear > 0 {
		fmt.Fprintf(&sb, "&yearTo=%d", toYear)
	}
	for _, l := range langs {
		fmt.Fprintf(&sb, "&languages%%5B%%5D=%s", url.QueryEscape(string(l)))
	}
	for _, e := range exts {
		fmt.Fprintf(&sb, "&extensions%%5B%%5D=%s", url.QueryEscape(string(e)))
	}
	return sb.String()
}
