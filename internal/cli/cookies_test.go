package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heartleo/zlib/internal/config"
)

func writeCookieFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestLoadNetscapeCookiesFiltersForTargetDomain(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.example.com	TRUE	/	TRUE	1800000000	remix_userid	123
#HttpOnly_.example.com	TRUE	/	TRUE	0	remix_userkey	secret
.example.com	TRUE	/	FALSE	1600000000	expired	old
.other.example	TRUE	/	FALSE	1800000000	other	value
.example.com	TRUE	/private	FALSE	1800000000	private	value
`)

	cookies, err := loadNetscapeCookies(path, "https://sub.example.com", now)
	if err != nil {
		t.Fatalf("loadNetscapeCookies() error = %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("loadNetscapeCookies() = %v, want 2 matching cookies", cookies)
	}
	if cookies["remix_userid"] != "123" || cookies["remix_userkey"] != "secret" {
		t.Errorf("loadNetscapeCookies() = %v, want imported login cookies", cookies)
	}
}

func TestLoadNetscapeCookiesSkipsSecureCookieForHTTPDomain(t *testing.T) {
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.example.com	TRUE	/	TRUE	0	secure_cookie	secret
.example.com	TRUE	/	FALSE	0	plain_cookie	value
`)

	cookies, err := loadNetscapeCookies(path, "http://example.com", time.Now())
	if err != nil {
		t.Fatalf("loadNetscapeCookies() error = %v", err)
	}
	if _, ok := cookies["secure_cookie"]; ok {
		t.Errorf("secure cookie must not be imported for HTTP target")
	}
	if cookies["plain_cookie"] != "value" {
		t.Errorf("plain_cookie = %q, want value", cookies["plain_cookie"])
	}
}

func TestLoadNetscapeCookiesRejectsInvalidFile(t *testing.T) {
	path := writeCookieFile(t, "not a Netscape cookie file\n")
	if _, err := loadNetscapeCookies(path, "https://example.com", time.Now()); err == nil {
		t.Fatal("loadNetscapeCookies() expected an error")
	}
}

func TestImportCookieSessionPersistsCookiesAndDomain(t *testing.T) {
	isolateConfigHome(t)
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.z-lib.gd	TRUE	/	TRUE	0	remix_userid	123
.z-lib.gd	TRUE	/	TRUE	0	remix_userkey	secret
`)

	count, err := importCookieSession(path, "https://z-lib.gd", time.Now())
	if err != nil {
		t.Fatalf("importCookieSession() error = %v", err)
	}
	if count != 2 {
		t.Errorf("importCookieSession() count = %d, want 2", count)
	}

	session, err := config.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if session.Domain != "https://z-lib.gd" {
		t.Errorf("session.Domain = %q, want https://z-lib.gd", session.Domain)
	}
	if session.Cookies["remix_userkey"] != "secret" {
		t.Errorf("session cookies = %v, want imported cookie", session.Cookies)
	}
}

// A cookie file can match the domain and still authenticate nothing. Saving it
// makes every later command fail with an opaque "session expired" instead.
func TestImportCookieSessionRejectsFileWithoutAuthCookies(t *testing.T) {
	isolateConfigHome(t)
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.z-lib.gd	TRUE	/	TRUE	0	siteLanguage	en
`)

	_, err := importCookieSession(path, "https://z-lib.gd", time.Now())
	if err == nil {
		t.Fatal("importCookieSession() error = nil, want a missing-auth-cookie error")
	}
	if !strings.Contains(err.Error(), "remix_userid") {
		t.Errorf("importCookieSession() error = %v, want it to name the missing cookie", err)
	}
}

func TestImportCookieSessionRejectsFileMissingUserKey(t *testing.T) {
	isolateConfigHome(t)
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.z-lib.gd	TRUE	/	TRUE	0	remix_userid	123
`)

	_, err := importCookieSession(path, "https://z-lib.gd", time.Now())
	if err == nil || !strings.Contains(err.Error(), "remix_userkey") {
		t.Fatalf("importCookieSession() error = %v, want it to name remix_userkey", err)
	}
}

func TestImportCookieSessionPersistsEAPIMode(t *testing.T) {
	isolateConfigHome(t)
	path := writeCookieFile(t, `# Netscape HTTP Cookie File
.z-lib.gd	TRUE	/	TRUE	0	remix_userid	123
.z-lib.gd	TRUE	/	TRUE	0	remix_userkey	secret
`)

	if _, err := importCookieSession(path, "https://z-lib.gd", time.Now(), "eapi"); err != nil {
		t.Fatalf("importCookieSession() error = %v", err)
	}
	session, err := config.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if session.Mode != "eapi" {
		t.Errorf("session.Mode = %q, want eapi", session.Mode)
	}
}
