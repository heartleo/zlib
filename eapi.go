package zlib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultEAPIDomain = "https://z-lib.gd"

	eapiLoginPath  = "/eapi/user/login"
	eapiSearchPath = "/eapi/book/search"
)

type eapiBook struct {
	ID             any    `json:"id"`
	Hash           string `json:"hash"`
	Title          string `json:"title"`
	Author         string `json:"author"`
	Year           any    `json:"year"`
	Publisher      string `json:"publisher"`
	Identifier     any    `json:"identifier"`
	ISBN           any    `json:"isbn"`
	Language       string `json:"language"`
	Cover          string `json:"cover"`
	Extension      string `json:"extension"`
	Filesize       any    `json:"filesize"`
	FilesizeString string `json:"filesizeString"`
	Href           string `json:"href"`
	DownloadPath   string `json:"dl"`
	Description    string `json:"description"`
	Date           string `json:"date"`
	DownloadedAt   string `json:"downloaded_at"`
	InterestScore  any    `json:"interestScore"`
	QualityScore   any    `json:"qualityScore"`
}

type eapiPagination struct {
	Current    int `json:"current"`
	TotalPages int `json:"total_pages"`
}

func (c *Client) LoginEAPI(email, password string) error {
	c.SetMode(ClientModeEAPI)
	body, err := c.eapiPost(eapiLoginPath, url.Values{
		"email":    {email},
		"password": {password},
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLoginFailed, err)
	}

	var result struct {
		Success int    `json:"success"`
		Error   string `json:"error"`
		User    struct {
			ID             json.Number `json:"id"`
			RemixUserKey   string      `json:"remix_userkey"`
			LegacyRemixKey string      `json:"remix_user_key"`
		} `json:"user"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return fmt.Errorf("%w: invalid EAPI response: %v", ErrLoginFailed, err)
	}
	if result.Success != 1 {
		return c.loginEAPIViaHTML(email, password, result.Error)
	}

	key := result.User.RemixUserKey
	if key == "" {
		key = result.User.LegacyRemixKey
	}
	if result.User.ID == "" || key == "" {
		return fmt.Errorf("%w: EAPI response did not contain remix credentials", ErrLoginFailed)
	}
	c.SetCookies(map[string]string{
		"remix_userid":  result.User.ID.String(),
		"remix_userkey": key,
	})
	return nil
}

// loginEAPIViaHTML obtains the EAPI credentials through the HTML login form
// when /eapi/user/login refuses to issue them.
//
// The EAPI authenticates on nothing but remix_userid/remix_userkey, and
// /rpc.php sets both as cookies. Measured 2026-09-04 against a live account:
// /eapi/user/login answered "Authorization failed" while the same account and
// password drove /eapi/user/profile, /eapi/book/search and
// /eapi/user/book/downloaded without complaint using the keys /rpc.php handed
// out. The password itself was accepted -- a wrong one comes back "Incorrect
// email or password" instead -- so the login endpoint is gated on something
// the rest of the EAPI does not check. Falling back to the form keeps EAPI
// mode usable rather than failing the whole session on that one endpoint.
func (c *Client) loginEAPIViaHTML(email, password, eapiError string) error {
	if err := c.Login(email, password); err != nil {
		return err
	}

	cookies := c.cookieSnapshot()
	if cookies["remix_userid"] == "" || cookies["remix_userkey"] == "" {
		if eapiError == "" {
			eapiError = "login was rejected"
		}
		return fmt.Errorf("%w: EAPI login failed (%s) and the HTML login returned no remix credentials", ErrLoginFailed, eapiError)
	}
	// Login is the HTML path and leaves the mode alone, but say it outright:
	// the caller asked for EAPI and the credentials it just seated are EAPI
	// credentials.
	c.SetMode(ClientModeEAPI)
	return nil
}

func (c *Client) searchEAPI(query string, page, count int, opts *SearchOptions) (SearchResult, error) {
	values := url.Values{
		"message": {query},
		"page":    {strconv.Itoa(page)},
		"limit":   {strconv.Itoa(count)},
	}
	if opts != nil {
		if opts.Exact {
			values.Set("e", "1")
		}
		if opts.FromYear > 0 {
			values.Set("yearFrom", strconv.Itoa(opts.FromYear))
		}
		if opts.ToYear > 0 {
			values.Set("yearTo", strconv.Itoa(opts.ToYear))
		}
		for _, language := range opts.Languages {
			values.Add("languages[]", strings.ToLower(string(language)))
		}
		for _, extension := range opts.Extensions {
			values.Add("extensions[]", strings.ToLower(extension.String()))
		}
	}

	body, err := c.eapiPost(eapiSearchPath, values)
	if err != nil {
		return SearchResult{}, err
	}
	var result struct {
		Success    int            `json:"success"`
		Error      string         `json:"error"`
		Books      []eapiBook     `json:"books"`
		Pagination eapiPagination `json:"pagination"`
	}
	if err := decodeEAPIJSON(body, &result); err != nil {
		return SearchResult{}, fmt.Errorf("zlibrary: invalid EAPI search response: %w", err)
	}
	if result.Success != 1 {
		if result.Error == "" {
			result.Error = "search was rejected"
		}
		return SearchResult{}, fmt.Errorf("zlibrary: EAPI search failed: %s", result.Error)
	}

	books := make([]Book, 0, len(result.Books))
	for _, item := range result.Books {
		books = append(books, c.mapEAPIBook(item))
	}

	// Fall back to the requested page when the server omits pagination.current.
	// A zero Page freezes the interactive pager: "previous" is disabled by the
	// page > 1 guard while "next" refetches page 1.
	current := result.Pagination.Current
	if current == 0 {
		current = page
	}

	return SearchResult{
		Books:      books,
		Page:       current,
		TotalPages: result.Pagination.TotalPages,
	}, nil
}

func (c *Client) eapiPost(path string, values url.Values) ([]byte, error) {
	return c.eapiRequest(context.Background(), http.MethodPost, path, values)
}

func (c *Client) eapiGet(path string, values url.Values) ([]byte, error) {
	return c.eapiRequest(context.Background(), http.MethodGet, path, values)
}

func (c *Client) eapiRequest(ctx context.Context, method, path string, values url.Values) ([]byte, error) {
	endpoint := c.domain + path
	var requestBody io.Reader
	if method == http.MethodGet {
		if len(values) > 0 {
			endpoint += "?" + values.Encode()
		}
	} else {
		requestBody = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return nil, err
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)
	cookies := c.cookieSnapshot()
	if userID := cookies["remix_userid"]; userID != "" {
		req.Header.Set("remix-userid", userID)
	}
	if userKey := cookies["remix_userkey"]; userKey != "" {
		req.Header.Set("remix-userkey", userKey)
	}
	for name, value := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// EAPI answers with application/json, so isBotProtectionResponse skips the
	// body scan entirely and only a DiamWall status can trip here. That is what
	// keeps a search for a book whose title or query text mentions the vendor
	// from being misreported as a block.
	if isBotProtectionResponse(resp.StatusCode, resp.Header, responseBody) {
		return nil, &BotProtectionError{StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}
	}

	// A non-2xx EAPI response still carries {"success":0,"error":"..."}. Hand
	// that body to the caller so the server's message ("daily limit reached",
	// "not authorized") survives instead of being flattened into an opaque
	// status error.
	if err := validateStatus(resp); err != nil && !isEAPIEnvelope(responseBody) {
		return nil, err
	}
	return responseBody, nil
}

// isEAPIEnvelope reports whether body is a JSON object carrying EAPI's
// "success" field, meaning the caller can extract a real error message from it.
func isEAPIEnvelope(body []byte) bool {
	var envelope struct {
		Success *int `json:"success"`
	}
	if err := decodeEAPIJSON(body, &envelope); err != nil {
		return false
	}
	return envelope.Success != nil
}

func (c *Client) mapEAPIBook(item eapiBook) Book {
	id := eapiString(item.ID)
	isbn := eapiString(item.Identifier)
	if isbn == "" {
		isbn = eapiString(item.ISBN)
	}
	size := item.FilesizeString
	if size == "" {
		size = eapiString(item.Filesize)
	}
	book := Book{
		ID:          id,
		Hash:        item.Hash,
		ISBN:        isbn,
		URL:         absolutizeURL(c.domain, item.Href),
		Cover:       item.Cover,
		Name:        item.Title,
		Publisher:   item.Publisher,
		Year:        eapiString(item.Year),
		Language:    item.Language,
		Extension:   item.Extension,
		Size:        size,
		Rating:      eapiString(item.InterestScore),
		Quality:     eapiString(item.QualityScore),
		Description: item.Description,
	}
	if id != "" && item.Hash != "" {
		book.DownloadURL = c.domain + "/eapi/book/" + url.PathEscape(id) + "/" + url.PathEscape(item.Hash) + "/file"
	} else {
		book.DownloadURL = absolutizeURL(c.domain, item.DownloadPath)
	}
	if item.Author != "" {
		book.Authors = []string{item.Author}
	}
	return book
}

func decodeEAPIJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func eapiString(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}
