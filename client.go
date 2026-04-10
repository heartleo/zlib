package zlib

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	defaultTimeout   = 180 * time.Second
)

type Client struct {
	httpClient  *http.Client
	domain      string
	loginDomain string
	cookies     map[string]string
	loggedIn    bool
}

func NewClient(opts ...ClientOption) *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: defaultTimeout,
		},
		domain:      CurrentDefaultDomain(),
		loginDomain: buildLoginURL(CurrentDefaultDomain()),
		cookies:     make(map[string]string),
	}
	for _, opt := range opts {
		opt(c)
	}
	if proxyURL := strings.TrimSpace(os.Getenv(EnvProxy)); proxyURL != "" {
		WithProxy(proxyURL)(c)
	}
	return c
}

type ClientOption func(*Client)

func WithDomain(domain string) ClientOption {
	return func(c *Client) {
		c.SetDomain(domain)
	}
}

func WithProxy(proxyURL string) ClientOption {
	return func(c *Client) {
		u, err := url.Parse(proxyURL)
		if err != nil {
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
		c.cookies[cookie.Name] = cookie.Value
	}

	c.loggedIn = true
	return nil
}

func (c *Client) Logout() {
	c.cookies = make(map[string]string)
	c.loggedIn = false
	jar, _ := cookiejar.New(nil)
	c.httpClient.Jar = jar
}

func (c *Client) Domain() string {
	return c.domain
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
	for k, v := range c.cookies {
		httpCookies = append(httpCookies, &http.Cookie{Name: k, Value: v})
	}
	if len(httpCookies) > 0 {
		c.httpClient.Jar.SetCookies(u, httpCookies)
	}
}

func (c *Client) Cookies() map[string]string {
	return c.cookies
}

func (c *Client) SetCookies(cookies map[string]string) {
	c.cookies = cookies
	c.loggedIn = true
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

func (c *Client) get(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	html := string(body)

	// Auto-solve JS challenge and retry once
	if isChallengePage(html) {
		token, err := solveChallenge(html)
		if err != nil {
			return "", fmt.Errorf("challenge solve failed: %w", err)
		}
		c.cookies["c_token"] = token
		return c.get(rawURL)
	}

	return html, nil
}
