package zlib

func (c *Client) GetLimits() (DownloadLimit, error) {
	if !c.loggedIn {
		return DownloadLimit{}, ErrNotLoggedIn
	}

	html, err := c.get(BuildDownloadsURL(c.domain))
	if err != nil {
		return DownloadLimit{}, err
	}
	return parseDownloadLimits(html)
}

func (c *Client) DownloadHistory(page int) (DownloadHistoryResult, error) {
	if !c.loggedIn {
		return DownloadHistoryResult{}, ErrNotLoggedIn
	}
	if page <= 0 {
		page = 1
	}

	urls := []string{
		BuildHistoryURL(c.domain, page),
		BuildDownloadsPageURL(c.domain, page),
	}

	var (
		items   []DownloadHistoryItem
		lastErr error
	)
	for _, u := range urls {
		html, err := c.get(u)
		if err != nil {
			lastErr = err
			continue
		}

		var totalPages int
		items, totalPages, err = parseDownloadHistory(html, c.domain)
		if err == nil {
			return DownloadHistoryResult{
				Items:      items,
				Page:       page,
				TotalPages: totalPages,
			}, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return DownloadHistoryResult{}, lastErr
	}

	return DownloadHistoryResult{
		Items: items,
		Page:  page,
	}, nil
}
