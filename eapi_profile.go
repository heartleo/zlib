package zlib

import (
	"fmt"
	"net/url"
	"strconv"
)

func (c *Client) getLimitsEAPI() (DownloadLimit, error) {
	body, err := c.eapiGet("/eapi/user/profile", nil)
	if err != nil {
		return DownloadLimit{}, err
	}
	var result struct {
		Success int    `json:"success"`
		Error   string `json:"error"`
		User    struct {
			DownloadsToday      any `json:"downloads_today"`
			DownloadsLimit      any `json:"downloads_limit"`
			DailyDownloadsCount any `json:"dailyDownloadsCount"`
			DailyDownloadLimit  any `json:"dailyDownloadLimit"`
		} `json:"user"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return DownloadLimit{}, fmt.Errorf("zlibrary: invalid EAPI profile response: %w", err)
	}
	if result.Success != 1 {
		if result.Error == "" {
			result.Error = "profile request was rejected"
		}
		return DownloadLimit{}, fmt.Errorf("zlibrary: EAPI profile failed: %s", result.Error)
	}

	daily := eapiInt(result.User.DownloadsToday)
	if daily == 0 {
		daily = eapiInt(result.User.DailyDownloadsCount)
	}
	allowed := eapiInt(result.User.DownloadsLimit)
	if allowed == 0 {
		allowed = eapiInt(result.User.DailyDownloadLimit)
	}
	remaining := allowed - daily
	if remaining < 0 {
		remaining = 0
	}
	return DownloadLimit{
		DailyAmount:    daily,
		DailyAllowed:   allowed,
		DailyRemaining: remaining,
	}, nil
}

func (c *Client) downloadHistoryEAPI(page int) (DownloadHistoryResult, error) {
	body, err := c.eapiGet("/eapi/user/book/downloaded", url.Values{
		"page":  {strconv.Itoa(page)},
		"limit": {"50"},
	})
	if err != nil {
		return DownloadHistoryResult{}, err
	}
	var result struct {
		Success    int            `json:"success"`
		Error      string         `json:"error"`
		Books      []eapiBook     `json:"books"`
		Pagination eapiPagination `json:"pagination"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return DownloadHistoryResult{}, fmt.Errorf("zlibrary: invalid EAPI history response: %w", err)
	}
	if result.Success != 1 {
		if result.Error == "" {
			result.Error = "history request was rejected"
		}
		return DownloadHistoryResult{}, fmt.Errorf("zlibrary: EAPI history failed: %s", result.Error)
	}

	items := make([]DownloadHistoryItem, 0, len(result.Books))
	for _, raw := range result.Books {
		book := c.mapEAPIBook(raw)
		date := raw.Date
		if date == "" {
			date = raw.DownloadedAt
		}
		items = append(items, DownloadHistoryItem{
			ID:          book.ID,
			Hash:        book.Hash,
			Name:        book.Name,
			URL:         book.URL,
			DownloadURL: book.DownloadURL,
			Extension:   book.Extension,
			Size:        book.Size,
			Date:        date,
		})
	}
	current := result.Pagination.Current
	if current == 0 {
		current = page
	}
	return DownloadHistoryResult{
		Items:      items,
		Page:       current,
		TotalPages: result.Pagination.TotalPages,
	}, nil
}

func eapiInt(value any) int {
	n, _ := strconv.Atoi(eapiString(value))
	return n
}
