package zlib

import (
	"context"
	"fmt"
	"net/http"
)

const (
	recommendedBooksPath = "/eapi/user/book/recommended"
	mostPopularBooksPath = "/eapi/book/most-popular"
)

func (c *Client) RecommendedBooks() ([]Book, error) {
	return c.RecommendedBooksContext(context.Background())
}

// RecommendedBooksContext is RecommendedBooks with cancellation support.
func (c *Client) RecommendedBooksContext(ctx context.Context) ([]Book, error) {
	return c.bookFeedContext(ctx, recommendedBooksPath)
}

// MostPopularBooks returns the EAPI most-popular list.
func (c *Client) MostPopularBooks() ([]Book, error) {
	return c.MostPopularBooksContext(context.Background())
}

// MostPopularBooksContext is MostPopularBooks with cancellation support.
func (c *Client) MostPopularBooksContext(ctx context.Context) ([]Book, error) {
	return c.bookFeedContext(ctx, mostPopularBooksPath)
}

func (c *Client) bookFeed(path string) ([]Book, error) {
	return c.bookFeedContext(context.Background(), path)
}

func (c *Client) bookFeedContext(ctx context.Context, path string) ([]Book, error) {
	if !c.loggedIn {
		return nil, ErrNotLoggedIn
	}
	body, err := c.eapiRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Success int        `json:"success"`
		Error   string     `json:"error"`
		Books   []eapiBook `json:"books"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return nil, fmt.Errorf("zlibrary: invalid EAPI book list response: %w", err)
	}
	if result.Success != 1 {
		if result.Error == "" {
			result.Error = "book list request was rejected"
		}
		return nil, fmt.Errorf("zlibrary: EAPI book list failed: %s", result.Error)
	}
	books := make([]Book, 0, len(result.Books))
	for _, item := range result.Books {
		book := c.mapEAPIBook(item)
		if book.ID == "" || book.Hash == "" {
			return nil, fmt.Errorf("zlibrary: EAPI book list entry is missing id or hash")
		}
		books = append(books, book)
	}
	return books, nil
}
