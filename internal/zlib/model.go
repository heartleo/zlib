package zlib

import (
	"encoding/json"
	"strings"
)

// MarshalJSON prints the ID as the id:hash reference `zlib download` takes, so
// no command that lists books can print a bare ID by forgetting a rewrite.
func (b Book) MarshalJSON() ([]byte, error) {
	type plain Book
	if b.ID != "" && b.Hash != "" && !strings.Contains(b.ID, ":") {
		b.ID += ":" + b.Hash
	}
	return json.Marshal(plain(b))
}

type Book struct {
	ID          string   `json:"id"`
	Hash        string   `json:"hash,omitempty"`
	ISBN        string   `json:"isbn,omitempty"`
	ISBN10      string   `json:"isbn_10,omitempty"`
	ISBN13      string   `json:"isbn_13,omitempty"`
	URL         string   `json:"url"`
	Cover       string   `json:"cover,omitempty"`
	Name        string   `json:"name"`
	Authors     []string `json:"authors,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Year        string   `json:"year,omitempty"`
	Language    string   `json:"language,omitempty"`
	Extension   string   `json:"extension,omitempty"`
	Size        string   `json:"size,omitempty"`
	Rating      string   `json:"rating,omitempty"`
	Quality     string   `json:"quality,omitempty"`
	Description string   `json:"description,omitempty"`
	Categories  string   `json:"categories,omitempty"`
	Edition     string   `json:"edition,omitempty"`
	Pages       string   `json:"pages,omitempty"`
	DownloadURL string   `json:"download_url,omitempty"`
}

type SearchResult struct {
	Books      []Book `json:"books"`
	Page       int    `json:"page"`
	TotalPages int    `json:"total_pages"`
}

type DownloadLimit struct {
	DailyAmount    int `json:"daily_amount"`
	DailyAllowed   int `json:"daily_allowed"`
	DailyRemaining int `json:"daily_remaining"`
}

// UserProfile contains the account fields safe to display or serialize.
// Authentication credentials returned by EAPI are intentionally excluded.
type UserProfile struct {
	DownloadLimit
	ID              string   `json:"id,omitempty"`
	Name            string   `json:"name,omitempty"`
	Email           string   `json:"email,omitempty"`
	KindleEmail     string   `json:"kindle_email,omitempty"`
	Premium         *bool    `json:"premium,omitempty"`
	Confirmed       *bool    `json:"confirmed,omitempty"`
	DonationsActive *bool    `json:"donations_active,omitempty"`
	DonationsExpire string   `json:"donations_expire,omitempty"`
	PersonalDomains []string `json:"personal_domains,omitempty"`
	QuotaKnown      bool     `json:"quota_known"`
}

type DownloadHistoryItem struct {
	// ID and Hash identify the downloaded book in EAPI responses.
	ID          string   `json:"id,omitempty"`
	Hash        string   `json:"hash,omitempty"`
	Name        string   `json:"name"`
	Authors     []string `json:"authors,omitempty"`
	URL         string   `json:"url"`
	DownloadURL string   `json:"download_url,omitempty"`
	Extension   string   `json:"extension,omitempty"`
	Size        string   `json:"size,omitempty"`
	Date        string   `json:"date"`
	// ISBN, ISBN10 and ISBN13 are what the list response says, with the same
	// meaning and normalization as on Book.
	ISBN   string `json:"isbn,omitempty"`
	ISBN10 string `json:"isbn_10,omitempty"`
	ISBN13 string `json:"isbn_13,omitempty"`
}

type DownloadHistoryResult struct {
	Items      []DownloadHistoryItem `json:"items"`
	Page       int                   `json:"page"`
	TotalPages int                   `json:"total_pages"`
}

type Booklist struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Count       string `json:"count,omitempty"`
	Views       string `json:"views,omitempty"`
}

type BooklistResult struct {
	Lists      []Booklist `json:"lists"`
	Page       int        `json:"page"`
	TotalPages int        `json:"total_pages"`
}

type SearchOptions struct {
	Exact      bool
	FromYear   int
	ToYear     int
	Languages  []Language
	Extensions []Extension
	Order      OrderOption
}

type FullTextSearchOptions struct {
	SearchOptions
	Phrase bool
}
