package zlib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	domainProbeTimeout      = 10 * time.Second
	maxDomainProbeRedirects = 3
	maxDomainProbeBodyBytes = 64 << 10
)

// DomainStatus is the health classification returned by ProbeDomain.
type DomainStatus string

const (
	DomainStatusHealthy DomainStatus = "healthy"
	// DomainStatusChallenged means the domain answered with Z-Library's own JS
	// proof-of-work interstitial. It is a USABLE domain: Client.get() solves the
	// challenge transparently. The interstitial arrives with HTTP 503, so
	// without this status the most usable mirrors were reported as http_error.
	DomainStatusChallenged      DomainStatus = "challenged"
	DomainStatusDiamWallBlocked DomainStatus = "diamwall_blocked"
	DomainStatusRedirectLoop    DomainStatus = "redirect_loop"
	DomainStatusHTTPError       DomainStatus = "http_error"
	DomainStatusNetworkError    DomainStatus = "network_error"
	DomainStatusInvalidDomain   DomainStatus = "invalid_domain"
)

// DomainCheck contains the result of a read-only unauthenticated domain probe.
type DomainCheck struct {
	Domain     string       `json:"domain"`
	Status     DomainStatus `json:"status"`
	HTTPStatus int          `json:"http_status,omitempty"`
	FinalURL   string       `json:"final_url,omitempty"`
	Detail     string       `json:"detail,omitempty"`
}

// DomainProbeOptions controls how doctor probes reach candidate domains.
type DomainProbeOptions struct {
	// ProxyURL overrides ZLIB_PROXY for this probe when non-empty.
	ProxyURL string
	// Path selects an endpoint below each candidate origin. Empty probes root.
	Path string
}

// KnownDomains returns the domains checked by the CLI doctor command.
func KnownDomains() []string {
	suffixes := AllowedDomainSuffixes()
	domains := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		domains = append(domains, "https://"+suffix)
	}
	return domains
}

// ProbeDomains checks domains concurrently. It makes only unauthenticated GET
// requests and never changes the client's configured default domain.
func ProbeDomains(ctx context.Context, domains []string) []DomainCheck {
	return ProbeDomainsWithOptions(ctx, domains, DomainProbeOptions{})
}

// ProbeDomainsWithOptions checks domains concurrently using opts.
func ProbeDomainsWithOptions(ctx context.Context, domains []string, opts DomainProbeOptions) []DomainCheck {
	results := make([]DomainCheck, len(domains))
	var wg sync.WaitGroup
	for i, domain := range domains {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = ProbeDomainWithOptions(ctx, domain, opts)
		}()
	}
	wg.Wait()
	return results
}

// ProbeDomain checks whether an unauthenticated GET to a domain reaches a
// successful response. Redirects are followed manually so self-redirects and
// DiamWall's __diamwall cookie can be reported precisely.
func ProbeDomain(ctx context.Context, domain string) DomainCheck {
	return ProbeDomainWithOptions(ctx, domain, DomainProbeOptions{})
}

// ProbeDomainWithOptions checks a single domain using opts.
func ProbeDomainWithOptions(ctx context.Context, domain string, opts DomainProbeOptions) DomainCheck {
	domain = normalizeDomain(domain)
	result := DomainCheck{Domain: domain}
	current, err := url.ParseRequestURI(domain)
	if err != nil || current.Scheme == "" || current.Host == "" {
		result.Status = DomainStatusInvalidDomain
		result.Detail = "domain must be an absolute HTTP URL"
		return result
	}
	if opts.Path != "" {
		probePath, pathErr := url.Parse(opts.Path)
		if pathErr != nil || !strings.HasPrefix(opts.Path, "/") || probePath.IsAbs() || probePath.Host != "" {
			result.Status = DomainStatusInvalidDomain
			result.Detail = "probe path must be an absolute URL path"
			return result
		}
		current = current.ResolveReference(probePath)
	}

	ctx, cancel := context.WithTimeout(ctx, domainProbeTimeout)
	defer cancel()

	clientOptions := []ClientOption{WithDomain(domain)}
	if strings.TrimSpace(opts.ProxyURL) != "" {
		clientOptions = append(clientOptions, WithProxy(opts.ProxyURL))
	}
	client := NewClient(clientOptions...).httpClient
	client.Timeout = domainProbeTimeout
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// A __diamwall cookie seen anywhere in the chain only means the request was
	// blocked if the chain never resolves to a real response; see
	// isDiamWallResponse.
	sawDiamWallCookie := false
	blockedOrLoop := func(detail string) DomainCheck {
		if sawDiamWallCookie {
			result.Status = DomainStatusDiamWallBlocked
			result.Detail = "DiamWall anti-bot redirect that never resolves"
			return result
		}
		result.Status = DomainStatusRedirectLoop
		result.Detail = detail
		return result
	}

	seen := make(map[string]struct{})
	for redirects := 0; ; redirects++ {
		currentURL := current.String()
		result.FinalURL = currentURL
		if _, ok := seen[currentURL]; ok {
			return blockedOrLoop("redirected to a previously visited URL")
		}
		seen[currentURL] = struct{}{}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			result.Status = DomainStatusInvalidDomain
			result.Detail = err.Error()
			return result
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := client.Do(req)
		if err != nil {
			result.Status = DomainStatusNetworkError
			result.Detail = err.Error()
			return result
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxDomainProbeBodyBytes))
		resp.Body.Close()
		if readErr != nil {
			result.Status = DomainStatusNetworkError
			result.Detail = readErr.Error()
			return result
		}

		result.HTTPStatus = resp.StatusCode
		result.FinalURL = resp.Request.URL.String()
		if hasDiamWallCookie(resp) {
			sawDiamWallCookie = true
		}
		if isDiamWallResponse(resp, body) {
			result.Status = DomainStatusDiamWallBlocked
			result.Detail = "DiamWall anti-bot response"
			return result
		}
		// Z-Library's own challenge arrives with HTTP 503, so it must be
		// recognised before the status checks below call it an http_error. The
		// domain is usable: the client clears this automatically.
		if isChallengePage(string(body)) {
			result.Status = DomainStatusChallenged
			result.Detail = "JS challenge; the client solves this automatically"
			return result
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			result.Status = DomainStatusHealthy
			return result
		}
		if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusMultipleChoices+100 {
			result.Status = DomainStatusHTTPError
			result.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
			return result
		}

		location, err := resp.Location()
		if err != nil {
			result.Status = DomainStatusHTTPError
			result.Detail = fmt.Sprintf("HTTP %d without a valid Location header", resp.StatusCode)
			return result
		}
		if redirects >= maxDomainProbeRedirects {
			return blockedOrLoop(fmt.Sprintf("exceeded %d redirects", maxDomainProbeRedirects))
		}
		current = resp.Request.URL.ResolveReference(location)
	}
}

// isDiamWallResponse reports whether a single response is DiamWall refusing the
// request outright.
//
// It deliberately does NOT consider the __diamwall Set-Cookie. That is
// DiamWall's clearance cookie, issued on requests that PASS, so keying on it
// reported every reachable mirror as blocked. The cookie is only evidence of a
// block when the probe never reaches a successful response at all, which
// hasDiamWallCookie feeds into the redirect-loop exits below.
func isDiamWallResponse(resp *http.Response, body []byte) bool {
	return isBotProtectionResponse(resp.StatusCode, resp.Header, body)
}

// hasDiamWallCookie reports whether a response issues DiamWall's cookie.
func hasDiamWallCookie(resp *http.Response) bool {
	for _, value := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(strings.ToLower(value), "__diamwall") {
			return true
		}
	}
	return false
}
