package zlib

import "errors"

var (
	ErrLoginFailed    = errors.New("zlibrary: login failed")
	ErrNotLoggedIn    = errors.New("zlibrary: not logged in, call Login first")
	ErrNoDomain       = errors.New("zlibrary: no working domain found")
	ErrEmptyQuery     = errors.New("zlibrary: search query is empty")
	ErrNoID           = errors.New("zlibrary: no book ID provided")
	ErrInvalidProxy   = errors.New("zlibrary: proxy_list must be a non-empty slice")
	ErrParseFailed    = errors.New("zlibrary: failed to parse page")
	ErrPhraseMinWords = errors.New("zlibrary: phrase search requires at least 2 words")
)
