package zlib

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	defaultTimeout   = 180 * time.Second
	maxRedirects     = 10

	// maxChallengeAttempts bounds how many times get() re-requests a URL after
	// solving the JS challenge.
	//
	// Clearing the challenge is probabilistic, so this budget is sized from
	// measurement rather than taste. Sampled against the live site with a 5s
	// gap, the attempt that finally succeeded was 2, 2, 2, 3, 5, 5, 6, 7, and
	// twice not within 8. A budget near the median fails about half the time,
	// which is why it is 10 and not 4.
	//
	// It must stay bounded. This retry was once plain recursion, and because
	// each retry restarts the HTTP timeout, a stubborn endpoint turned into a
	// command that printed nothing and never returned.
	maxChallengeAttempts = 10
)

// Challenge retry backoff. Vars, not consts, so tests can shorten them.
var (
	challengeRetryBase    = 1500 * time.Millisecond
	challengeRetryMaxWait = 15 * time.Second
)

type Client struct {
	httpClient  *http.Client
	domain      string
	loginDomain string
	// cookiesMu guards cookies. A Client is shared across the goroutines
	// FetchBookDetails fans out, and every request can rewrite c_token, so the
	// map is never touched directly — go through cookieSnapshot/setCookie.
	cookiesMu sync.RWMutex
	cookies   map[string]string
	loggedIn  bool
	mode      ClientMode
	// downloadRetries is how many times a download request is reissued after
	// a transient failure.
	downloadRetries int
}

// ClientMode selects the upstream transport used by high-level operations.
type ClientMode string

const (
	ClientModeHTML ClientMode = "html"
	ClientModeEAPI ClientMode = "eapi"
)

func NewClient(opts ...ClientOption) *Client {
	jar, _ := cookiejar.New(nil)
	domain := CurrentDefaultDomain()
	c := &Client{
		httpClient: &http.Client{
			Jar:           jar,
			Timeout:       defaultTimeout,
			CheckRedirect: checkRedirect,
		},
		domain:      domain,
		loginDomain: buildLoginURL(domain),
		cookies:     make(map[string]string),
		mode:        ClientModeHTML,
		// Read at construction, not package init: the CLI loads .env after
		// all init functions have run.
		downloadRetries: resolveDownloadRetries(os.Getenv(EnvRetries)),
	}
	// Environment first, explicit options second: a caller-supplied option is
	// more specific than ZLIB_PROXY and must win.
	if proxyURL := strings.TrimSpace(os.Getenv(EnvProxy)); proxyURL != "" {
		WithProxy(proxyURL)(c)
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type ClientOption func(*Client)

func WithDomain(domain string) ClientOption {
	return func(c *Client) {
		c.SetDomain(domain)
	}
}

// WithProxy routes requests through proxyURL. It clones http.DefaultTransport
// so the connection pooling, HTTP/2 negotiation, and dial timeouts the stdlib
// tunes for us survive; only the proxy function is replaced.
func WithProxy(proxyURL string) ClientOption {
	return func(c *Client) {
		proxyURL = strings.TrimSpace(proxyURL)
		if proxyURL == "" {
			return
		}
		u, err := url.Parse(proxyURL)
		if err != nil {
			return
		}
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport := defaultTransport.Clone()
			transport.Proxy = http.ProxyURL(u)
			c.httpClient.Transport = transport
			return
		}
		c.httpClient.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	}
}

func WithOnion(proxyURL string) ClientOption {
	return func(c *Client) {
		c.SetDomain(TorDomain)
		WithProxy(proxyURL)(c)
	}
}

func (c *Client) Login(email, password string) error {
	data := url.Values{
		"isModal":       {"true"},
		"email":         {email},
		"password":      {password},
		"site_mode":     {"books"},
		"action":        {"login"},
		"isSingleLogin": {"1"},
		"redirectUrl":   {""},
		"gg_json_mode":  {"1"},
	}

	req, err := http.NewRequest("POST", c.loginDomain, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}
	if err := validateResponse(resp, body); err != nil {
		return fmt.Errorf("%w: %w", ErrLoginFailed, err)
	}

	var result struct {
		Response struct {
			ValidationError interface{} `json:"validationError"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("%w: invalid response: %v", ErrLoginFailed, err)
	}
	if result.Response.ValidationError != nil {
		return fmt.Errorf("%w: %v", ErrLoginFailed, result.Response.ValidationError)
	}

	// Extract cookies
	u, _ := url.Parse(c.loginDomain)
	for _, cookie := range c.httpClient.Jar.Cookies(u) {
		c.setCookie(cookie.Name, cookie.Value)
	}

	c.loggedIn = true
	return nil
}

func (c *Client) Logout() {
	c.cookiesMu.Lock()
	c.cookies = make(map[string]string)
	c.cookiesMu.Unlock()
	c.loggedIn = false
	jar, _ := cookiejar.New(nil)
	c.httpClient.Jar = jar
}

func (c *Client) Domain() string {
	return c.domain
}

func (c *Client) Mode() ClientMode {
	return c.mode
}

func (c *Client) SetMode(mode ClientMode) {
	if mode == ClientModeEAPI {
		c.mode = ClientModeEAPI
		return
	}
	c.mode = ClientModeHTML
}

func (c *Client) SetDomain(domain string) {
	domain = normalizeDomain(domain)
	if domain == "" {
		domain = CurrentDefaultDomain()
	}

	c.domain = domain
	c.loginDomain = buildLoginURL(domain)

	if c.httpClient == nil || c.httpClient.Jar == nil {
		return
	}

	u, err := url.Parse(domain)
	if err != nil {
		return
	}

	var httpCookies []*http.Cookie
	for k, v := range c.cookieSnapshot() {
		httpCookies = append(httpCookies, &http.Cookie{Name: k, Value: v})
	}
	if len(httpCookies) > 0 {
		c.httpClient.Jar.SetCookies(u, httpCookies)
	}
}

// cookieSnapshot returns a copy of the cookie map safe to range over while
// another goroutine is updating it.
func (c *Client) cookieSnapshot() map[string]string {
	c.cookiesMu.RLock()
	defer c.cookiesMu.RUnlock()
	snapshot := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		snapshot[k] = v
	}
	return snapshot
}

func (c *Client) setCookie(name, value string) {
	c.cookiesMu.Lock()
	defer c.cookiesMu.Unlock()
	if c.cookies == nil {
		c.cookies = make(map[string]string)
	}
	c.cookies[name] = value
}

func (c *Client) Cookies() map[string]string {
	return c.cookieSnapshot()
}

func (c *Client) SetCookies(cookies map[string]string) {
	c.cookiesMu.Lock()
	c.cookies = make(map[string]string, len(cookies))
	for k, v := range cookies {
		c.cookies[k] = v
	}
	c.loggedIn = true
	c.cookiesMu.Unlock()

	u, _ := url.Parse(c.domain)
	var httpCookies []*http.Cookie
	for k, v := range cookies {
		httpCookies = append(httpCookies, &http.Cookie{Name: k, Value: v})
	}
	c.httpClient.Jar.SetCookies(u, httpCookies)
}

func isChallengePage(html string) bool {
	return len(html) < 20000 && challengeRe.MatchString(html)
}

// diamWallTitleRe matches DiamWall's interstitial <title>. The title is the
// only body marker that reliably separates a block page from ordinary content,
// so detection anchors on it rather than on a bare substring search.
var diamWallTitleRe = regexp.MustCompile(`(?is)<title[^>]*>[^<]*diamwall[^<]*</title>`)

// isDiamWallStatus reports whether statusCode is one of the non-standard
// statuses DiamWall serves when it refuses a request outright: 513 for
// "Verifying your browser" and 517 for "Access Denied".
func isDiamWallStatus(statusCode int) bool {
	return statusCode == 513 || statusCode == 517
}

// isHTMLBody reports whether a body with this Content-Type could be a DiamWall
// interstitial. An unset or unparseable type is treated as HTML so a block page
// served without a type is still caught; a declared non-HTML type (notably the
// EAPI's application/json) is not scanned at all.
func isHTMLBody(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return true
	}
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

// isBotProtectionResponse reports whether a response is DiamWall refusing the
// request. DiamWall serves its page either with a terminal status or with a
// nominally successful one, so both are checked.
//
// Detection is deliberately narrow, because the obvious broad checks produce
// false positives on healthy traffic:
//
//   - The __diamwall cookie is a CLEARANCE cookie. DiamWall sets it on requests
//     it ALLOWS, so its presence is evidence the request got through, not that
//     it was blocked.
//   - A bare body substring search for the vendor name matches legitimate
//     content: a search page echoes the query back into the results, and a book
//     title or description may contain the word.
//
// Only a DiamWall status, or an HTML body whose <title> is the interstitial,
// counts as a block.
func isBotProtectionResponse(statusCode int, header http.Header, body []byte) bool {
	if isDiamWallStatus(statusCode) {
		return true
	}
	if !isHTMLBody(header.Get("Content-Type")) {
		return false
	}
	return diamWallTitleRe.Match(body)
}

// validateStatus rejects a non-2xx response. It is separate from
// validateResponse because callers that handle specific non-2xx bodies (the JS
// challenge, an EAPI error envelope) must inspect the body before the status
// is allowed to fail the request.
func validateStatus(resp *http.Response) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &HTTPStatusError{StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}
	}
	return nil
}

func validateResponse(resp *http.Response, body []byte) error {
	if isBotProtectionResponse(resp.StatusCode, resp.Header, body) {
		return &BotProtectionError{StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}
	}
	return validateStatus(resp)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: exceeded %d redirects while requesting %s", ErrRedirectLoop, maxRedirects, req.URL)
	}
	stripEAPICredentialsOffAllowlist(req)
	return nil
}

// stripEAPICredentialsOffAllowlist removes the EAPI credential headers when a
// redirect leaves the allowed Z-Library hosts.
//
// eapiRequest carries remix-userid/remix-userkey as custom headers, and
// net/http only strips Authorization, Cookie and WWW-Authenticate on a
// cross-host redirect. Without this, a mirror answering 30x with an
// attacker-controlled Location would receive the user key verbatim, which is
// exactly what the ParseAllowedDomain allowlist exists to prevent. Downloads
// legitimately redirect to CDN hosts that are not on the allowlist, so the
// redirect itself is still followed; only the credentials are dropped.
func stripEAPICredentialsOffAllowlist(req *http.Request) {
	if req.URL == nil {
		return
	}
	if _, err := ParseAllowedDomain(req.URL.Scheme + "://" + req.URL.Host); err == nil {
		return
	}
	req.Header.Del("remix-userid")
	req.Header.Del("remix-userkey")
}

// isLoginPage reports whether html is Z-Library's login page, which the server
// returns (with status 200) for authenticated requests once the session has
// expired. The login form carries a stable id we can match on.
func isLoginPage(html string) bool {
	return strings.Contains(html, `id="loginForm"`)
}

// get fetches rawURL as a browser would, transparently clearing the JS
// challenge when the server serves it instead of the page.
//
// Clearing the challenge is probabilistic, so the request is reissued with a
// freshly solved c_token until it succeeds or maxChallengeAttempts is reached.
// The budget must stay bounded: each retry restarts the HTTP timeout, so an
// unbounded loop turns a stubborn endpoint into a command that never returns.
func (c *Client) get(rawURL string) (string, error) {
	wait := challengeRetryBase
	for attempt := 1; ; attempt++ {
		html, challenged, err := c.fetchOnce(rawURL)
		if err != nil {
			return "", err
		}
		if !challenged {
			return html, nil
		}
		if attempt >= maxChallengeAttempts {
			return "", fmt.Errorf("%w: still challenged after %d attempts at %s", ErrChallengeFailed, attempt, rawURL)
		}
		token, err := solveChallenge(html)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrChallengeFailed, err)
		}
		c.setCookie("c_token", token)

		time.Sleep(wait)
		if wait *= 2; wait > challengeRetryMaxWait {
			wait = challengeRetryMaxWait
		}
	}
}

// fetchOnce performs a single GET. It reports challenged=true, along with the
// interstitial HTML, when the server answered with the JS challenge instead of
// the requested page.
func (c *Client) fetchOnce(rawURL string) (html string, challenged bool, err error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	for k, v := range c.cookieSnapshot() {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	if isUnexpectedBookRedirect(req.URL, resp.Request.URL) {
		return "", false, fmt.Errorf("%w: requested %s but received %s", ErrParseFailed, req.URL.Path, resp.Request.URL.Path)
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", false, err
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", false, err
	}

	// DiamWall refuses the request outright: there is no challenge to solve and
	// no content to parse, so this is checked first.
	if isBotProtectionResponse(resp.StatusCode, resp.Header, body) {
		return "", false, &BotProtectionError{StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}
	}

	html = string(body)

	// Z-Library serves the "Checking your browser" interstitial with HTTP 503,
	// so the challenge must be recognised BEFORE the status check below. Doing
	// it the other way round makes the solver unreachable and fails every
	// request to the primary mirror with an opaque 503.
	if isChallengePage(html) {
		return html, true, nil
	}

	// Once the session expires, the server serves the login page (HTTP 200)
	// in place of the requested authenticated page.
	if isLoginPage(html) {
		return "", false, ErrSessionExpired
	}

	if err := validateStatus(resp); err != nil {
		return "", false, err
	}

	return html, false, nil
}

func isUnexpectedBookRedirect(requested, received *url.URL) bool {
	if requested == nil || received == nil {
		return false
	}
	if !strings.HasPrefix(requested.Path, bookPathPrefix) {
		return false
	}
	return !strings.HasPrefix(received.Path, bookPathPrefix)
}
