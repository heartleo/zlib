package zlib

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newLoggedInEAPITestClient(serverURL string) *Client {
	c := NewClient(WithDomain(serverURL))
	c.SetMode(ClientModeEAPI)
	c.SetCookies(map[string]string{"remix_userid": "123", "remix_userkey": "key123"})
	return c
}

func TestGetLimitsEAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/user/profile" {
			t.Fatalf("path = %q, want profile endpoint", r.URL.Path)
		}
		if r.Header.Get("remix-userid") != "123" {
			t.Error("profile request missing EAPI credentials")
		}
		fmt.Fprint(w, `{"success":1,"user":{"downloads_today":3,"downloads_limit":10}}`)
	}))
	defer server.Close()

	limits, err := newLoggedInEAPITestClient(server.URL).GetLimits()
	if err != nil {
		t.Fatalf("GetLimits() error = %v", err)
	}
	if limits.DailyAmount != 3 || limits.DailyAllowed != 10 || limits.DailyRemaining != 7 {
		t.Errorf("GetLimits() = %+v", limits)
	}
}

func TestDownloadHistoryEAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/user/book/downloaded" || r.URL.Query().Get("page") != "2" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"success":1,"books":[{"id":42,"hash":"abc123","title":"History Book","author":"Reader","extension":"epub","filesizeString":"1.5 MB","href":"/book/42/history","date":"2026-08-23"}],"pagination":{"current":2,"total_pages":4}}`)
	}))
	defer server.Close()

	result, err := newLoggedInEAPITestClient(server.URL).DownloadHistory(2)
	if err != nil {
		t.Fatalf("DownloadHistory() error = %v", err)
	}
	if result.Page != 2 || result.TotalPages != 4 || len(result.Items) != 1 {
		t.Fatalf("DownloadHistory() = %+v", result)
	}
	item := result.Items[0]
	if item.ID != "42" || item.Hash != "abc123" || item.Extension != "epub" || item.Date != "2026-08-23" {
		t.Errorf("history item = %+v", item)
	}
	if item.DownloadURL != server.URL+"/eapi/book/42/abc123/file" {
		t.Errorf("DownloadURL = %q", item.DownloadURL)
	}
}
