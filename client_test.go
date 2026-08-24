package zlib

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogin_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginRPCPath && r.Method == "POST" {
			http.SetCookie(w, &http.Cookie{Name: "remix_userid", Value: "123"})
			http.SetCookie(w, &http.Cookie{Name: "remix_userkey", Value: "abc"})
			resp := map[string]interface{}{
				"response": map[string]interface{}{},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loginDomain = buildLoginURL(server.URL)

	err := c.Login("test@example.com", "testpassword")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if c.cookies["remix_userid"] != "123" {
		t.Errorf("expected userid cookie '123', got %q", c.cookies["remix_userid"])
	}
	if c.cookies["remix_userkey"] != "abc" {
		t.Errorf("expected userkey cookie 'abc', got %q", c.cookies["remix_userkey"])
	}
}

func TestFetchBook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>
		<z-cover id="123" title="Test Book"><img class="image" src="/c.jpg"></z-cover>
		<i class="authors"><a href="/a">Author A</a></i>
		<div class="bookDetailsBox">
		  <div class="bookProperty property_year"><div class="property_value">2020</div></div>
		  <div class="bookProperty property__file"><div class="property_value">PDF, 10 MB</div></div>
		</div>
		<a class="btn btn-default addDownloadedBook" href="/dl/abc">PDF</a>
		</html>`)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	book, err := c.FetchBook("123")
	if err != nil {
		t.Fatalf("FetchBook() error = %v", err)
	}
	if book.Name != "Test Book" {
		t.Errorf("Name = %q", book.Name)
	}
	if book.DownloadURL != server.URL+"/dl/abc" {
		t.Errorf("DownloadURL = %q", book.DownloadURL)
	}
}

func TestFetchBookRejectsRedirectToHome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/123":
			http.Redirect(w, r, "/", http.StatusFound)
		case "/":
			fmt.Fprint(w, `<html>
			<z-cover id="999" title="Homepage Recommendation"></z-cover>
			</html>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loggedIn = true

	if _, err := c.FetchBook("123"); !errors.Is(err, ErrParseFailed) {
		t.Fatalf("FetchBook() error = %v, want ErrParseFailed", err)
	}
}

func TestLogin_ValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"response": map[string]interface{}{
				"validationError": "Invalid credentials",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient()
	c.domain = server.URL
	c.loginDomain = buildLoginURL(server.URL)

	err := c.Login("bad@example.com", "wrong")
	if err == nil {
		t.Fatal("Login() expected error, got nil")
	}
}

func TestLoginReturnsBotProtectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(517)
		fmt.Fprint(w, `<html><title>Access Denied | DiamWall</title></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	err := c.Login("test@example.com", "testpassword")
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("Login() error = %v, want ErrLoginFailed", err)
	}
	if !errors.Is(err, ErrBotProtection) {
		t.Fatalf("Login() error = %v, want ErrBotProtection", err)
	}

	var botErr *BotProtectionError
	if !errors.As(err, &botErr) {
		t.Fatalf("Login() error = %v, want BotProtectionError", err)
	}
	if botErr.StatusCode != 517 || botErr.URL != server.URL+loginRPCPath {
		t.Errorf("BotProtectionError = %#v, want status 517 at %s", botErr, server.URL+loginRPCPath)
	}
}

func TestGetReturnsBotProtectionErrorBeforeParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(517)
		fmt.Fprint(w, `<html><title>Access Denied | DiamWall</title></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	_, err := c.get(server.URL + "/s/test")
	if !errors.Is(err, ErrBotProtection) {
		t.Fatalf("get() error = %v, want ErrBotProtection", err)
	}
}

func TestGetRecognizesDiamWallBodyWithSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>Access Denied | DiamWall</title></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	_, err := c.get(server.URL + "/s/test")
	if !errors.Is(err, ErrBotProtection) {
		t.Fatalf("get() error = %v, want ErrBotProtection", err)
	}
}

func TestGetRejectsUnexpectedHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "temporarily unavailable")
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	_, err := c.get(server.URL + "/s/test")
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("get() error = %v, want HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusServiceUnavailable || statusErr.URL != server.URL+"/s/test" {
		t.Errorf("HTTPStatusError = %#v, want status 503 at %s", statusErr, server.URL+"/s/test")
	}
}

func TestGetReportsRedirectLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	_, err := c.get(server.URL + "/loop")
	if !errors.Is(err, ErrRedirectLoop) {
		t.Fatalf("get() error = %v, want ErrRedirectLoop", err)
	}
}

// challengePageHTML builds a "Checking your browser" interstitial that
// solveChallenge can actually solve.
func challengePageHTML() string {
	return `<html><body><script>var a=['6f1e2d3c4b5a69788796a5b4c3d2e1f009182736','c_token=','array'];</script></body></html>`
}

// shortenChallengeBackoff keeps the retry tests from sleeping for real.
func shortenChallengeBackoff(t *testing.T) {
	t.Helper()
	base, max := challengeRetryBase, challengeRetryMaxWait
	challengeRetryBase, challengeRetryMaxWait = time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		challengeRetryBase, challengeRetryMaxWait = base, max
	})
}

// The challenge interstitial is served with HTTP 503. If the status check runs
// before the challenge check, the solver is unreachable and every request to
// the primary mirror fails with an opaque 503.
func TestGetSolvesChallengeServedWith503(t *testing.T) {
	shortenChallengeBackoff(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, challengePageHTML())
			return
		}
		fmt.Fprint(w, `<html><body>real content</body></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	html, err := c.get(server.URL + "/s/test")
	if err != nil {
		t.Fatalf("get() error = %v, want the challenge to be solved", err)
	}
	if !strings.Contains(html, "real content") {
		t.Errorf("get() html = %q, want the page served after the challenge", html)
	}
	if got := c.Cookies()["c_token"]; got == "" {
		t.Error("get() did not store a solved c_token")
	}
}

// The retry must stay bounded: each attempt restarts the HTTP timeout, so an
// unbounded loop turns a stubborn endpoint into a command that never returns.
func TestGetGivesUpAfterChallengeBudget(t *testing.T) {
	shortenChallengeBackoff(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, challengePageHTML())
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	_, err := c.get(server.URL + "/s/test")
	if !errors.Is(err, ErrChallengeFailed) {
		t.Fatalf("get() error = %v, want ErrChallengeFailed", err)
	}
	if got := int(requests.Load()); got != maxChallengeAttempts {
		t.Errorf("get() made %d requests, want %d", got, maxChallengeAttempts)
	}
}

// A search echoes the query into the results page, so content that merely
// mentions the vendor must not be mistaken for a block page.
func TestGetAllowsContentMentioningDiamWall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>Search results</title><body>
			<input value="diamwall"><div>Defeating DiamWall, 2nd ed.</div></body></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	html, err := c.get(server.URL + "/s/diamwall")
	if err != nil {
		t.Fatalf("get() error = %v, want the page to be returned", err)
	}
	if !strings.Contains(html, "Defeating DiamWall") {
		t.Errorf("get() html = %q, want the search results", html)
	}
}

func TestGetDetectsDiamWallStatus513(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(513)
		fmt.Fprint(w, `<html><title>Verifying your browser | DiamWall</title></html>`)
	}))
	defer server.Close()

	c := NewClient(WithDomain(server.URL))
	if _, err := c.get(server.URL + "/s/test"); !errors.Is(err, ErrBotProtection) {
		t.Fatalf("get() error = %v, want ErrBotProtection", err)
	}
}

func TestWithDomain(t *testing.T) {
	c := NewClient(WithDomain("https://example.com"))

	if c.Domain() != "https://example.com" {
		t.Fatalf("Domain() = %q, want %q", c.Domain(), "https://example.com")
	}
	if c.loginDomain != buildLoginURL("https://example.com") {
		t.Fatalf("loginDomain = %q, want %q", c.loginDomain, buildLoginURL("https://example.com"))
	}
}

func TestNewClientPrefersExplicitProxyOverEnv(t *testing.T) {
	t.Setenv(EnvProxy, "http://127.0.0.1:7890")

	client := NewClient(WithProxy("http://127.0.0.1:1080"))

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected http transport to be configured")
	}

	reqURL, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("failed to parse request url: %v", err)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: reqURL})
	if err != nil {
		t.Fatalf("proxy func returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:1080" {
		t.Fatalf("proxy url = %v, want %q", proxyURL, "http://127.0.0.1:1080")
	}
}

func TestNewClientIgnoresEmptyExplicitProxy(t *testing.T) {
	t.Setenv(EnvProxy, "http://127.0.0.1:7890")

	client := NewClient(WithProxy(""))

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected http transport to be configured")
	}

	reqURL, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("failed to parse request url: %v", err)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: reqURL})
	if err != nil {
		t.Fatalf("proxy func returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy url = %v, want %q", proxyURL, "http://127.0.0.1:7890")
	}
}

func TestWithProxyPreservesDefaultTransportSettings(t *testing.T) {
	client := NewClient(WithProxy("http://127.0.0.1:7890"))

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected http transport to be configured")
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("http.DefaultTransport is not an *http.Transport on this platform")
	}
	if transport.MaxIdleConns != defaultTransport.MaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, defaultTransport.MaxIdleConns)
	}
	if transport.IdleConnTimeout != defaultTransport.IdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, defaultTransport.IdleConnTimeout)
	}
	if transport.ForceAttemptHTTP2 != defaultTransport.ForceAttemptHTTP2 {
		t.Errorf("ForceAttemptHTTP2 = %v, want %v", transport.ForceAttemptHTTP2, defaultTransport.ForceAttemptHTTP2)
	}
}
