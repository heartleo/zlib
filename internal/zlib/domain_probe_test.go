package zlib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKnownDomains(t *testing.T) {
	// Derived from the allowlist rather than repeated literally: the mirror
	// list churns, and a second hardcoded copy only makes every churn edit
	// fail a test that is not about the mirrors.
	suffixes := AllowedDomainSuffixes()
	want := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		want = append(want, "https://"+suffix)
	}
	if got := KnownDomains(); !reflect.DeepEqual(got, want) {
		t.Errorf("KnownDomains() = %v, want %v", got, want)
	}
}

// A fast domain is reported while a slow one is still running.
func TestProbeDomainsEachReportsInCompletionOrder(t *testing.T) {
	respond := func(delay time.Duration) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":1,"domains":[]}`))
		}))
	}
	slow, fast := respond(300*time.Millisecond), respond(0)
	defer slow.Close()
	defer fast.Close()

	var mu sync.Mutex
	var order []int
	start := time.Now()
	var fastAt time.Duration
	ProbeDomainsEach(t.Context(), []string{slow.URL, fast.URL}, DomainProbeOptions{Path: "/eapi/info/domains"}, func(i int, check DomainCheck) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, i)
		if i == 1 {
			fastAt = time.Since(start)
		}
		if check.Domain == "" {
			t.Error("empty check reported")
		}
	})
	if len(order) != 2 || order[0] != 1 || order[1] != 0 {
		t.Fatalf("completion order = %v, want fast (1) before slow (0)", order)
	}
	if fastAt > 250*time.Millisecond {
		t.Fatalf("fast result waited %v for the slow one", fastAt)
	}
}

func TestProbeDomainHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Errorf("ProbeDomain() HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusOK)
	}
}

func TestProbeDomainDetectsDiamWallRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "__diamwall=challenge; Path=/")
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusDiamWallBlocked {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusDiamWallBlocked, result)
	}
	if result.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("ProbeDomain() HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusTemporaryRedirect)
	}
}

// __diamwall is DiamWall's CLEARANCE cookie: it is issued on requests that
// pass. A mirror that answers 200 with real content while setting it is
// healthy, not blocked. Keying on the cookie alone reported every reachable
// mirror as diamwall_blocked.
func TestProbeDomainHealthyDespiteDiamWallClearanceCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "__diamwall=cleared; Path=/")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><title>Z-Library</title><body>books</body></html>`)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
}

// A challenge has its own status; EAPI cannot consume it.
func TestProbeDomainReportsChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<html><title>Checking your browser ...</title><body><script>`+
			`var a=['6f1e2d3c4b5a69788796a5b4c3d2e1f009182736','c_token=','array'];</script></body></html>`)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusChallenged {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusChallenged, result)
	}
}

func TestProbeDomainEAPIRequiresJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		fmt.Fprint(w, `<html>login required</html>`)
	}))
	defer server.Close()

	result := ProbeDomainWithOptions(context.Background(), server.URL, DomainProbeOptions{Path: "/eapi/info/domains"})
	if result.Status != DomainStatusHTTPError || result.Detail != "EAPI returned non-JSON content" {
		t.Fatalf("probe = %+v", result)
	}
}

// The real block terminates in 513 "Verifying your browser | DiamWall".
func TestProbeDomainDetectsDiamWallStatus513(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(513)
		fmt.Fprint(w, `<html><title>Verifying your browser | DiamWall</title></html>`)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusDiamWallBlocked {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusDiamWallBlocked, result)
	}
}

// Ordinary content mentioning the vendor is not a block page.
func TestProbeDomainHealthyWhenBodyMentionsDiamWall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>Search</title><body>Defeating DiamWall, 2nd ed.</body></html>`)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
}

func TestProbeDomainDetectsRedirectLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusRedirectLoop {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusRedirectLoop, result)
	}
}

func TestProbeDomainFollowsRedirectToHealthyDomain(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	result := ProbeDomain(context.Background(), redirector.URL)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
	if result.FinalURL != target.URL {
		t.Errorf("ProbeDomain() FinalURL = %q, want %q", result.FinalURL, target.URL)
	}
}

func TestProbeDomainReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	result := ProbeDomain(context.Background(), server.URL)
	if result.Status != DomainStatusHTTPError {
		t.Fatalf("ProbeDomain() status = %q, want %q (%+v)", result.Status, DomainStatusHTTPError, result)
	}
	if result.HTTPStatus != http.StatusServiceUnavailable {
		t.Errorf("ProbeDomain() HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusServiceUnavailable)
	}
}

func TestProbeDomainWithOptionsUsesExplicitProxy(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Host != "unreachable.invalid" {
			t.Errorf("proxy request host = %q, want unreachable.invalid", r.URL.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	result := ProbeDomainWithOptions(
		context.Background(),
		"http://unreachable.invalid",
		DomainProbeOptions{ProxyURL: proxy.URL},
	)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomainWithOptions() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
	if requests.Load() != 1 {
		t.Errorf("proxy received %d requests, want 1", requests.Load())
	}
}

func TestProbeDomainWithOptionsUsesEndpointPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/info/domains" {
			t.Errorf("request path = %q, want /eapi/info/domains", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":1}`)
	}))
	defer server.Close()

	result := ProbeDomainWithOptions(
		context.Background(),
		server.URL,
		DomainProbeOptions{Path: "/eapi/info/domains"},
	)
	if result.Status != DomainStatusHealthy {
		t.Fatalf("ProbeDomainWithOptions() status = %q, want %q (%+v)", result.Status, DomainStatusHealthy, result)
	}
	if result.FinalURL != server.URL+"/eapi/info/domains" {
		t.Errorf("FinalURL = %q, want EAPI endpoint", result.FinalURL)
	}
}

func TestProbeDomainWithOptionsRejectsAbsoluteEndpointURL(t *testing.T) {
	result := ProbeDomainWithOptions(
		context.Background(),
		"https://example.com",
		DomainProbeOptions{Path: "https://other.example/eapi/info/domains"},
	)
	if result.Status != DomainStatusInvalidDomain {
		t.Fatalf("status = %q, want %q (%+v)", result.Status, DomainStatusInvalidDomain, result)
	}
}
