package zlib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProfileMapsAccountWithoutCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":1,"user":{"id":42,"name":"Reader","email":"reader@example.com","kindle_email":"reader@kindle.com","remix_userkey":"private-key","isPremium":1,"confirmed":0,"donations_active":1,"donations_expire":"2026-12-31","personalDomains":["reader.example"],"downloads_today":0,"dailyDownloadsCount":5,"downloads_limit":10}}`)
	}))
	defer server.Close()
	profile, err := newLoggedInEAPITestClient(server.URL).GetProfile()
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if profile.ID != "42" || profile.Name != "Reader" || profile.Email != "reader@example.com" || profile.KindleEmail != "reader@kindle.com" {
		t.Fatalf("account fields = %+v", profile)
	}
	if profile.Premium == nil || !*profile.Premium || profile.Confirmed == nil || *profile.Confirmed || profile.DonationsActive == nil || !*profile.DonationsActive {
		t.Fatalf("account flags = %+v", profile)
	}
	if !profile.QuotaKnown || profile.DailyAmount != 0 || profile.DailyAllowed != 10 || profile.DailyRemaining != 10 {
		t.Fatalf("quota = %+v", profile)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("remix_userkey")) {
		t.Fatal("serialized profile contains credentials")
	}
}

func TestGetProfileWithoutQuotaDoesNotImplyZeroLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":1,"user":{"name":"Reader"}}`)
	}))
	defer server.Close()
	profile, err := newLoggedInEAPITestClient(server.URL).GetProfile()
	if err != nil || profile.QuotaKnown {
		t.Fatalf("profile = %+v, error = %v", profile, err)
	}
}

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

func TestProfileJSONOmitsResetTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":1,"user":{"downloads_today":3,"downloads_limit":10}}`)
	}))
	defer server.Close()

	profile, err := newLoggedInEAPITestClient(server.URL).GetProfile()
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if bytes.Contains(data, []byte("daily_reset")) {
		t.Errorf("profile JSON advertises a reset time EAPI does not provide: %s", data)
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
	if item.ID != "42" || item.Hash != "abc123" || item.Extension != "epub" || item.Date != "2026-08-23" || len(item.Authors) != 1 || item.Authors[0] != "Reader" {
		t.Errorf("history item = %+v", item)
	}
	if item.DownloadURL != server.URL+"/eapi/book/42/abc123/file" {
		t.Errorf("DownloadURL = %q", item.DownloadURL)
	}
}

// The list response already names a book's ISBN; the history item keeps it, as
// a book from any other list does, so the card shows it before the details load.
func TestDownloadHistoryKeepsTheISBN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":1,"books":[
{"id":1,"hash":"a","title":"Both","isbn10":"7115390576","isbn13":"9787115390578"},
{"id":2,"hash":"b","title":"Identifier only","identifier":"978-7-5366-9293-0"},
{"id":3,"hash":"c","title":"None"}],"pagination":{"current":1,"total_pages":1}}`)
	}))
	defer server.Close()

	result, err := newLoggedInEAPITestClient(server.URL).DownloadHistory(1)
	if err != nil || len(result.Items) != 3 {
		t.Fatalf("DownloadHistory() = %+v, %v", result, err)
	}
	both, derived, none := result.Items[0], result.Items[1], result.Items[2]
	if both.ISBN10 != "7115390576" || both.ISBN13 != "9787115390578" {
		t.Errorf("both = %+v", both)
	}
	// ISBN 10 and 13 supply each other's equivalent, as they do for other lists.
	if derived.ISBN13 != "9787536692930" || derived.ISBN10 != "7536692935" || derived.ISBN == "" {
		t.Errorf("derived = %+v", derived)
	}
	if none.ISBN != "" || none.ISBN10 != "" || none.ISBN13 != "" {
		t.Errorf("none = %+v", none)
	}
}
