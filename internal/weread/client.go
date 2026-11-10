package weread

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// maxResponseBytes bounds every JSON body read from WeRead or WeChat.
	maxResponseBytes = 1 << 20

	errCodeSessionExpired = -2012
	// errCodeAuthFailed means WeRead no longer accepts the credentials; the
	// server words it as an authentication failure.
	errCodeAuthFailed   = -2013
	errCodeVerification = -2041
)

// apiTimeout bounds one API request; a var so tests can shorten it.
var apiTimeout = 30 * time.Second

// Credentials identify a WeRead login. RefreshToken and DeviceID are long
// lived; AccessToken is reissued from them when WeRead rejects it.
type Credentials struct {
	VID          string `json:"vid"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token"`
	DeviceID     string `json:"device_id"`
	// Name is the account's WeRead nickname, for display only. It is
	// cleaned server text and may be empty.
	Name string `json:"name,omitempty"`
}

// Valid reports whether c holds enough to mint an access token.
func (c Credentials) Valid() bool {
	return c.VID != "" && c.RefreshToken != "" && c.DeviceID != ""
}

// Client talks to WeRead as the e-ink app. It is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	apiBase    string
	openBase   string
	longBase   string
	// cosURL builds the upload URL for an object; tests point it at a
	// local server.
	cosURL func(bucket, key string) string

	mu    sync.Mutex // guards creds and serializes token refreshes
	creds Credentials
	save  func(Credentials) error
}

// Option configures a Client.
type Option func(*Client)

// WithCredentials starts the client with saved credentials.
func WithCredentials(creds Credentials) Option {
	return func(c *Client) { c.creds = creds }
}

// WithCredentialsSaver persists credentials whenever WeRead rotates them.
func WithCredentialsSaver(save func(Credentials) error) Option {
	return func(c *Client) { c.save = save }
}

// WithTransport sets the transport, for example to choose a proxy, and keeps
// the client's refusal to follow redirects.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.httpClient.Transport = rt }
}

// WithHTTPClient replaces the HTTP client. Its CheckRedirect should refuse
// redirects: requests carry the access token in headers.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient returns a client that honors the standard proxy environment
// variables (HTTPS_PROXY, NO_PROXY). ZLIB_PROXY is for Z-Library mirrors and
// does not apply here.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			// No redirects: the access token travels in headers, and no
			// endpoint used here is expected to move.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		apiBase:  defaultAPIBase,
		openBase: defaultOpenBase,
		longBase: defaultLongBase,
		cosURL:   defaultCOSURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Credentials returns the current credentials, including any rotation.
func (c *Client) Credentials() Credentials {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.creds
}

// envelope holds the error fields WeRead adds to a failed response. Both
// spellings occur.
type envelope struct {
	ErrCode  *int   `json:"errCode"`
	ErrCode2 *int   `json:"errcode"`
	ErrMsg   string `json:"errMsg"`
	ErrMsg2  string `json:"errmsg"`
}

func (e envelope) code() int {
	switch {
	case e.ErrCode != nil:
		return *e.ErrCode
	case e.ErrCode2 != nil:
		return *e.ErrCode2
	}
	return 0
}

func (e envelope) message() string {
	if e.ErrMsg != "" {
		return cleanText(e.ErrMsg)
	}
	return cleanText(e.ErrMsg2)
}

// call sends an authenticated API request and decodes the JSON reply into
// out. An expired access token is refreshed once and the request repeated;
// WeRead rejects such a request before acting on it.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	for attempt := 0; ; attempt++ {
		creds, err := c.authorize(ctx, attempt > 0)
		if err != nil {
			return err
		}
		status, raw, err := c.send(ctx, method, path, query, body, func(h http.Header) {
			h.Set("vid", creds.VID)
			h.Set("accessToken", creds.AccessToken)
		})
		if err != nil {
			return err
		}
		var env envelope
		_ = json.Unmarshal(raw, &env)
		code := env.code()
		if status == http.StatusUnauthorized || code == errCodeSessionExpired || code == errCodeAuthFailed {
			if attempt == 0 {
				continue
			}
			return ErrSessionExpired
		}
		if code == errCodeVerification {
			return fmt.Errorf("%w (%s)", ErrVerificationRequired, path)
		}
		if status != http.StatusOK || code < 0 {
			return &APIError{Path: path, Status: status, Code: code, Message: env.message()}
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("WeRead %s returned invalid JSON: %w", path, err)
		}
		return nil
	}
}

// authorize returns credentials with an access token, minting one when none
// is cached or force is set.
func (c *Client) authorize(ctx context.Context, force bool) (Credentials, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.creds.Valid() {
		return Credentials{}, ErrNotLoggedIn
	}
	if c.creds.AccessToken != "" && !force {
		return c.creds, nil
	}
	if err := c.refreshLocked(ctx); err != nil {
		return Credentials{}, err
	}
	return c.creds, nil
}

type loginResponse struct {
	VID          json.Number `json:"vid"`
	AccessToken  string      `json:"accessToken"`
	RefreshToken string      `json:"refreshToken"`
}

// refreshLocked mints a new access token from the refresh token. c.mu must
// be held.
func (c *Client) refreshLocked(ctx context.Context) error {
	timestamp := time.Now().UnixMilli()
	random := randomInt(1000) + 1
	body := map[string]any{
		"deviceId":     c.creds.DeviceID,
		"deviceName":   deviceName,
		"deviceType":   deviceType,
		"inBackground": 0,
		"kickType":     1,
		"random":       random,
		"refCgi":       "",
		"refreshToken": c.creds.RefreshToken,
		"signature":    loginSignature(timestamp, c.creds.DeviceID, random),
		"timestamp":    timestamp,
		"trackId":      "",
	}
	var resp loginResponse
	if err := c.postLogin(ctx, body, &resp); err != nil {
		// A rejected refresh token is final; a server error is not.
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
			return &sessionError{cause: err}
		}
		return err
	}
	if vid := resp.VID.String(); vid != "" && vid != c.creds.VID {
		return errors.New("WeRead token refresh returned a different account")
	}
	c.creds.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		c.creds.RefreshToken = resp.RefreshToken
	}
	if c.save != nil {
		if err := c.save(c.creds); err != nil {
			return fmt.Errorf("save WeRead credentials: %w", err)
		}
	}
	return nil
}

// postLogin sends a /login request, used for both the QR exchange and
// token refresh, and requires an access token in the reply.
func (c *Client) postLogin(ctx context.Context, body map[string]any, out *loginResponse) error {
	status, raw, err := c.send(ctx, http.MethodPost, "/login", nil, body, nil)
	if err != nil {
		return err
	}
	var env envelope
	_ = json.Unmarshal(raw, &env)
	if code := env.code(); code == errCodeVerification {
		return fmt.Errorf("%w (/login)", ErrVerificationRequired)
	}
	if err := json.Unmarshal(raw, out); err != nil || status != http.StatusOK || out.AccessToken == "" {
		return &APIError{Path: "/login", Status: status, Code: env.code(), Message: env.message()}
	}
	return nil
}

// send issues one request to the WeRead API with the device headers and
// returns the status and a bounded body.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any, auth func(http.Header)) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	u := c.apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return 0, nil, err
	}
	setDeviceHeaders(req.Header)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	if auth != nil {
		auth(req.Header)
	}
	return c.do(req)
}

// do runs req and reads at most maxResponseBytes of the reply.
func (c *Client) do(req *http.Request) (int, []byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(raw) > maxResponseBytes {
		return resp.StatusCode, nil, fmt.Errorf("WeRead %s: response exceeds %d bytes", req.URL.Path, maxResponseBytes)
	}
	return resp.StatusCode, raw, nil
}

// getJSON fetches a public (unauthenticated) JSON document.
func (c *Client) getJSON(ctx context.Context, rawURL string, header func(http.Header), out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if header != nil {
		header(req.Header)
	}
	status, raw, err := c.do(req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &APIError{Path: req.URL.Path, Status: status}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s returned invalid JSON: %w", req.URL.Path, err)
	}
	return nil
}

// cleanText drops control and bidi formatting characters from server text
// so it cannot reach a terminal as an escape sequence. strings.Map also
// replaces invalid UTF-8.
func cleanText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, s))
}
