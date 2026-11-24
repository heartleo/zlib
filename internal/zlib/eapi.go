package zlib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
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
	ISBN10         any    `json:"isbn10"`
	ISBN13         any    `json:"isbn13"`
	Categories     any    `json:"categories"`
	Category       any    `json:"category"`
	Edition        any    `json:"edition"`
	Pages          any    `json:"pages"`
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
	return c.LoginEAPIContext(context.Background(), email, password)
}

// LoginEAPIContext is LoginEAPI with a context, so an interactive caller can
// abandon a login that a slow mirror leaves hanging.
func (c *Client) LoginEAPIContext(ctx context.Context, email, password string) error {
	c.SetMode(ClientModeEAPI)
	body, err := c.eapiRequest(ctx, http.MethodPost, eapiLoginPath, url.Values{
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
		if result.Error == "" {
			result.Error = "login was rejected"
		}
		return fmt.Errorf("%w: %s; import a Netscape cookie file with zlib login --cookies if this account cannot use EAPI password login", ErrLoginFailed, result.Error)
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

func (c *Client) searchEAPI(query string, page, count int, opts *SearchOptions) (SearchResult, error) {
	return c.searchEAPIContext(context.Background(), query, page, count, opts)
}

func (c *Client) searchEAPIContext(ctx context.Context, query string, page, count int, opts *SearchOptions) (SearchResult, error) {
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
		if opts.Order != "" {
			values.Set("order", opts.Order.String())
		}
	}

	body, err := c.eapiRequest(ctx, http.MethodPost, eapiSearchPath, values)
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
	// Search rejects requests without a client source ("Access denied"),
	// credentials or not; the other endpoints do not check it.
	req.Header.Set("source", "android")
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
	// A revoked or expired remix_userkey is answered with HTTP 400 and
	// {"success":0,"error":"Invalid credentials"} (or "Please login") on every
	// authenticated endpoint. Classify it once here so callers can tell it
	// apart from other failures. Login is exempt: there the same envelope
	// describes the credentials being submitted, not a saved session.
	if path != eapiLoginPath && cookies["remix_userkey"] != "" && isEAPISessionRejection(responseBody) {
		return nil, ErrSessionExpired
	}
	return responseBody, nil
}

// isEAPISessionRejection reports whether body is the envelope EAPI returns
// when it does not accept the session sent with the request.
func isEAPISessionRejection(body []byte) bool {
	var envelope struct {
		Success *int   `json:"success"`
		Error   string `json:"error"`
	}
	if err := decodeEAPIJSON(body, &envelope); err != nil || envelope.Success == nil || *envelope.Success != 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(envelope.Error)) {
	case "invalid credentials", "please login":
		return true
	}
	return false
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
		ISBN10:      normalizeISBN(eapiString(item.ISBN10)),
		ISBN13:      normalizeISBN(eapiString(item.ISBN13)),
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
		Categories:  eapiCategoryText(item.Categories),
		Edition:     eapiString(item.Edition),
		Pages:       eapiString(item.Pages),
	}
	if book.Categories == "" {
		book.Categories = eapiCategoryText(item.Category)
	}
	if !validISBN10(book.ISBN10) {
		book.ISBN10 = ""
	}
	if !validISBN13(book.ISBN13) {
		book.ISBN13 = ""
	}
	// An identifier field can list several values in one string, such as
	// "9787229042066,7229042062" (an ISBN-13 and its ISBN-10), the ISBNs of
	// several editions, or an Amazon ASIN like "B07S7CCQGL", which is not an
	// ISBN. Each value is checked alone, and one with a bad check digit is
	// skipped.
	var isbn10s, isbn13s []string
	for _, field := range []string{eapiString(item.Identifier), eapiString(item.ISBN)} {
		for _, value := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' || r == ';' || r == '|' || r == '\n' }) {
			if normalized := normalizeISBN(value); validISBN10(normalized) {
				isbn10s = append(isbn10s, normalized)
			} else if validISBN13(normalized) {
				isbn13s = append(isbn13s, normalized)
			}
		}
	}
	if book.ISBN13 == "" && len(isbn13s) > 0 {
		book.ISBN13 = isbn13s[0]
	}
	// The card shows the two side by side as one book's pair, so the ISBN-10 is
	// the one that belongs to the chosen ISBN-13. When the list names several
	// editions, another edition's ISBN-10 is skipped and the pair is derived.
	for _, v := range isbn10s {
		if book.ISBN10 == "" && (book.ISBN13 == "" || isbn13FromCore("978"+v[:9]) == book.ISBN13) {
			book.ISBN10 = v
		}
	}
	if book.ISBN10 == "" && strings.HasPrefix(book.ISBN13, "978") && validISBN13(book.ISBN13) {
		book.ISBN10 = isbn10FromCore(book.ISBN13[3:12])
	}
	if book.ISBN13 == "" && validISBN10(book.ISBN10) {
		book.ISBN13 = isbn13FromCore("978" + book.ISBN10[:9])
	}
	if book.Pages == "0" {
		book.Pages = ""
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

func normalizeISBN(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), " ", ""))
}

func validISBN10(value string) bool {
	if len(value) != 10 {
		return false
	}
	sum := 0
	for i := range 9 {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		sum += int(value[i]-'0') * (10 - i)
	}
	check := value[9]
	if check == 'X' {
		sum += 10
	} else if check >= '0' && check <= '9' {
		sum += int(check - '0')
	} else {
		return false
	}
	return sum%11 == 0
}

func validISBN13(value string) bool {
	if len(value) != 13 {
		return false
	}
	sum := 0
	for i := range 13 {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += int(value[i]-'0') * weight
	}
	return sum%10 == 0
}

func isbn10FromCore(core string) string {
	sum := 0
	for i := range 9 {
		sum += int(core[i]-'0') * (10 - i)
	}
	check := (11 - sum%11) % 11
	if check == 10 {
		return core + "X"
	}
	return core + strconv.Itoa(check)
}

func isbn13FromCore(core string) string {
	sum := 0
	for i := range 12 {
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += int(core[i]-'0') * weight
	}
	return core + strconv.Itoa((10-sum%10)%10)
}

func eapiCategoryText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, entry := range value {
			if name := eapiCategoryText(entry); name != "" {
				parts = append(parts, name)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range []string{"name", "title", "path"} {
			if name := eapiCategoryText(value[key]); name != "" {
				return name
			}
		}
	}
	return ""
}

// decodeEAPIJSON decodes an EAPI response into target and strips control
// characters from every string in it (see cleanText), so no caller can print
// a terminal escape sequence the server sent.
func decodeEAPIJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	cleanText(reflect.ValueOf(target))
	return nil
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
	case bool:
		return strconv.FormatBool(value)
	default:
		// Objects and arrays are not scalar text; printing them with
		// fmt.Sprint would show server-chosen keys verbatim.
		return ""
	}
}
