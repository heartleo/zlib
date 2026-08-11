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
	ErrSessionExpired = errors.New("zlibrary: session expired, run `zlib login` to re-authenticate")
	ErrPhraseMinWords = errors.New("zlibrary: phrase search requires at least 2 words")
	ErrDownloadFailed = errors.New("zlibrary: download failed")
	// ErrBookUnavailable reports a book that is still listed in search results
	// but whose record the server no longer serves: the detail page answers
	// HTTP 204 with an empty body, and its /dl/ link answers 204 as well.
	ErrBookUnavailable = errors.New("zlibrary: book is no longer available on the server")
)
