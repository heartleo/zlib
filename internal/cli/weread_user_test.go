package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
)

// fakeWeReadAPI sends every WeRead request to handler instead of the
// network.
func fakeWeReadAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	prev := newWeReadClient
	t.Cleanup(func() { newWeReadClient = prev })
	newWeReadClient = func(opts ...weread.Option) *weread.Client {
		rewrite := roundTripper(func(r *http.Request) (*http.Response, error) {
			r = r.Clone(r.Context())
			r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
			return http.DefaultTransport.RoundTrip(r)
		})
		return weread.NewClient(append(opts, weread.WithTransport(rewrite))...)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWeReadAccountLabel(t *testing.T) {
	if got := wereadAccountLabel(weread.Credentials{VID: "7"}); got != "7" {
		t.Fatalf("label = %q", got)
	}
	if got := wereadAccountLabel(weread.Credentials{VID: "7", Name: "Reader\x1b[2J"}); got != "Reader[2J (7)" {
		t.Fatalf("label = %q", got)
	}
}

func TestWeReadWhoamiSavesNicknameAndListsKeysOnly(t *testing.T) {
	isolateConfigHome(t)
	fakeWeReadAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("vid") != "42" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"userVid": 42, "name": "Reader", "avatar": "https://example.com/a.png"})
	})
	if err := config.SaveWeRead(weread.Credentials{VID: "42", AccessToken: "a", RefreshToken: "r", DeviceID: "d"}); err != nil {
		t.Fatal(err)
	}

	out, err := executeRootForTest(t, "weread", "whoami", "--keys")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "avatar\nname\nuserVid" || strings.Contains(out, "Reader") {
		t.Fatalf("--keys printed %q; want field names only", out)
	}
	resetFlag(t, "weread", "whoami", "keys")

	out, err = executeRootForTest(t, "weread", "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nickname   Reader") {
		t.Fatalf("whoami printed %q", out)
	}
	if creds, _ := config.LoadWeRead(); creds.Name != "Reader" || creds.RefreshToken != "r" {
		t.Fatalf("saved login = %+v", creds)
	}
}

func TestWeReadLoginSavesNickname(t *testing.T) {
	isolateConfigHome(t)
	userCalls := 0
	fakeWeReadAPI(t, func(w http.ResponseWriter, r *http.Request) {
		reply := map[string]any{}
		switch r.URL.Path {
		case "/wxticket":
			reply = map[string]any{"signature": "sig", "timeStamp": 1}
		case "/connect/sdk/qrconnect":
			reply = map[string]any{"errcode": 0, "uuid": "uuid"}
		case "/connect/l/qrconnect":
			reply = map[string]any{"wx_errcode": 405, "wx_code": "code"}
		case "/login":
			reply = map[string]any{"vid": 42, "accessToken": "a", "refreshToken": "r"}
		case "/user":
			userCalls++
			reply = map[string]any{"name": "Reader"}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	var out, status strings.Builder
	if err := runWeReadLoginPlain(t.Context(), &out, &status); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nickname   Reader") || userCalls != 1 {
		t.Fatalf("output %q, /user calls %d", out.String(), userCalls)
	}
	if creds, err := config.LoadWeRead(); err != nil || creds.Name != "Reader" {
		t.Fatalf("saved login = %+v, %v", creds, err)
	}
}

func resetFlag(t *testing.T, path ...string) {
	t.Helper()
	cmd, _, err := rootCmd.Find(path[:len(path)-1])
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup(path[len(path)-1])
	_ = flag.Value.Set(flag.DefValue)
	flag.Changed = false
}
