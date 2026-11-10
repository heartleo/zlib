package zlib

import (
	"fmt"
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
)

type Client struct {
	httpClient *http.Client
	domain     string
	// coverCacheDir holds validated cover bytes; empty disables the disk cache.
	coverCacheDir string
	// cookiesMu guards cookies shared by concurrent book-detail requests.
	cookiesMu sync.RWMutex
	cookies   map[string]string
	loggedIn  bool
	mode      ClientMode
	// downloadRetries is how many times a download request is reissued after
	// a transient failure.
	downloadRetries int
}

// ClientMode identifies the upstream transport used by high-level operations.
type ClientMode string

const (
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
		domain:  domain,
		cookies: make(map[string]string),
		mode:    ClientModeEAPI,
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

// WithTransportFrom shares source's HTTP connection pool without sharing its
// cookie jar or account state. Both clients must use the same proxy settings.
// Configure source before sharing it; do not change its transport concurrently.
func WithTransportFrom(source *Client) ClientOption {
	return func(c *Client) {
		c.httpClient.Transport = source.httpClient.Transport
	}
}

// CloseIdleConnections releases idle connections owned by the client's transport.
// Active requests are not interrupted.
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

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
	return c.LoginEAPI(email, password)
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
	c.mode = ClientModeEAPI
}

func (c *Client) SetDomain(domain string) {
	domain = normalizeDomain(domain)
	if domain == "" {
		domain = CurrentDefaultDomain()
	}

	c.domain = domain

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
	// Secure keeps the jar from replaying an HTTPS session over plain HTTP,
	// for example after a redirect that downgrades the scheme.
	secure := u != nil && u.Scheme == "https"
	var httpCookies []*http.Cookie
	for k, v := range cookies {
		httpCookies = append(httpCookies, &http.Cookie{Name: k, Value: v, Secure: secure})
	}
	c.httpClient.Jar.SetCookies(u, httpCookies)
}

func isChallengePage(html string) bool {
	return len(html) < 20000 && challengeRe.MatchString(html)
}

var challengeRe = regexp.MustCompile(`'([0-9A-Fa-f]{40})','c_token=`)

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

// validateStatus rejects a non-2xx response. Callers that handle a specific
// non-2xx body, such as an EAPI error envelope, inspect it before letting the
// status fail the request.
func validateStatus(resp *http.Response) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &HTTPStatusError{StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}
	}
	return nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: exceeded %d redirects while requesting %s", ErrRedirectLoop, maxRedirects, req.URL)
	}
	stripEAPICredentialsOffAllowlist(req)
	return nil
}

// stripEAPICredentialsOffAllowlist removes the EAPI credential headers and the
// manually attached Cookie header when a redirect leaves the allowed
// Z-Library HTTPS origins.
//
// eapiRequest carries remix-userid/remix-userkey as custom headers, and
// net/http only strips Authorization, Cookie and WWW-Authenticate on a
// cross-host redirect. Without this, a mirror answering 30x with an
// attacker-controlled Location would receive the user key verbatim, which is
// exactly what the ParseAllowedDomain allowlist exists to prevent. net/http
// also keeps Cookie on a same-host HTTPS-to-HTTP redirect; ParseAllowedDomain
// rejects plain HTTP, so that hop loses the cookies too instead of sending
// them in clear text. Downloads legitimately redirect to CDN hosts that are
// not on the allowlist, so the redirect itself is still followed; only the
// credentials are dropped.
func stripEAPICredentialsOffAllowlist(req *http.Request) {
	if req.URL == nil {
		return
	}
	if _, err := ParseAllowedDomain(req.URL.Scheme + "://" + req.URL.Host); err == nil {
		return
	}
	req.Header.Del("remix-userid")
	req.Header.Del("remix-userkey")
	req.Header.Del("Cookie")
}
