package zlib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestKnownDomains(t *testing.T) {
	want := []string{
		"https://z-library.gs",
		"https://1lib.sk",
		"https://z-lib.fm",
		"https://z-lib.gd",
		"https://z-lib.gl",
		"https://zliba.ru",
		"https://z-lib.sk",
		"https://z-library.ec",
	}
	if got := KnownDomains(); !reflect.DeepEqual(got, want) {
		t.Errorf("KnownDomains() = %v, want %v", got, want)
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
