package zlib

type Book struct {
	ID          string   `json:"id"`
	ISBN        string   `json:"isbn,omitempty"`
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
	DownloadURL string   `json:"download_url,omitempty"`
}

type SearchResult struct {
	Books      []Book `json:"books"`
	Page       int    `json:"page"`
	TotalPages int    `json:"total_pages"`
}

type DownloadLimit struct {
	DailyAmount    int    `json:"daily_amount"`
	DailyAllowed   int    `json:"daily_allowed"`
	DailyRemaining int    `json:"daily_remaining"`
	DailyReset     string `json:"daily_reset"`
}

type DownloadHistoryItem struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url,omitempty"`
	Extension   string `json:"extension,omitempty"`
	Size        string `json:"size,omitempty"`
	Date        string `json:"date"`
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
}

type FullTextSearchOptions struct {
	SearchOptions
	Phrase bool
}
