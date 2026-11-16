package zlib

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func (c *Client) getProfileEAPI() (UserProfile, error) {
	return c.getProfileEAPIContext(context.Background())
}

func (c *Client) getProfileEAPIContext(ctx context.Context) (UserProfile, error) {
	body, err := c.eapiRequest(ctx, http.MethodGet, "/eapi/user/profile", nil)
	if err != nil {
		return UserProfile{}, err
	}
	var result struct {
		Success int    `json:"success"`
		Error   string `json:"error"`
		User    *struct {
			ID                  any      `json:"id"`
			Name                string   `json:"name"`
			Email               string   `json:"email"`
			KindleEmail         string   `json:"kindle_email"`
			Premium             any      `json:"isPremium"`
			Confirmed           any      `json:"confirmed"`
			DonationsActive     any      `json:"donations_active"`
			DonationsExpire     string   `json:"donations_expire"`
			PersonalDomains     []string `json:"personalDomains"`
			DownloadsToday      any      `json:"downloads_today"`
			DownloadsLimit      any      `json:"downloads_limit"`
			DailyDownloadsCount any      `json:"dailyDownloadsCount"`
			DailyDownloadLimit  any      `json:"dailyDownloadLimit"`
		} `json:"user"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return UserProfile{}, fmt.Errorf("zlibrary: invalid EAPI profile response: %w", err)
	}
	if result.Success != 1 {
		if result.Error == "" {
			result.Error = "profile request was rejected"
		}
		return UserProfile{}, fmt.Errorf("zlibrary: EAPI profile failed: %s", result.Error)
	}
	if result.User == nil {
		return UserProfile{}, fmt.Errorf("zlibrary: EAPI profile response is missing user")
	}

	today := result.User.DownloadsToday
	if today == nil {
		today = result.User.DailyDownloadsCount
	}
	limit := result.User.DownloadsLimit
	if limit == nil {
		limit = result.User.DailyDownloadLimit
	}
	daily := eapiInt(today)
	allowed := eapiInt(limit)
	remaining := allowed - daily
	if remaining < 0 {
		remaining = 0
	}
	return UserProfile{
		DownloadLimit: DownloadLimit{
			DailyAmount:    daily,
			DailyAllowed:   allowed,
			DailyRemaining: remaining,
		},
		ID:              eapiString(result.User.ID),
		Name:            result.User.Name,
		Email:           result.User.Email,
		KindleEmail:     result.User.KindleEmail,
		Premium:         eapiFlag(result.User.Premium),
		Confirmed:       eapiFlag(result.User.Confirmed),
		DonationsActive: eapiFlag(result.User.DonationsActive),
		DonationsExpire: result.User.DonationsExpire,
		PersonalDomains: result.User.PersonalDomains,
		QuotaKnown:      limit != nil,
	}, nil
}

func eapiFlag(value any) *bool {
	if value == nil {
		return nil
	}
	if flag, ok := value.(bool); ok {
		return &flag
	}
	n, err := strconv.Atoi(eapiString(value))
	if err != nil {
		return nil
	}
	flag := n != 0
	return &flag
}

func (c *Client) downloadHistoryEAPI(page int) (DownloadHistoryResult, error) {
	return c.downloadHistoryEAPIContext(context.Background(), page)
}

func (c *Client) downloadHistoryEAPIContext(ctx context.Context, page int) (DownloadHistoryResult, error) {
	body, err := c.eapiRequest(ctx, http.MethodGet, "/eapi/user/book/downloaded", url.Values{
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
			Authors:     book.Authors,
			URL:         book.URL,
			DownloadURL: book.DownloadURL,
			Extension:   book.Extension,
			Size:        book.Size,
			Date:        date,
			ISBN:        book.ISBN,
			ISBN10:      book.ISBN10,
			ISBN13:      book.ISBN13,
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
