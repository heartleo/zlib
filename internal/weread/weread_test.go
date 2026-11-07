package weread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWeRead is one httptest server standing in for the WeRead API, WeChat
// open platform, WeChat long poll and Tencent COS.
type fakeWeRead struct {
	t   *testing.T
	srv *httptest.Server
	mux *http.ServeMux

	mu       sync.Mutex
	notifies []url.Values // query of each notify request
	uploaded []byte
	cosAuth  string
	cosToken string
	notified url.Values
	tokens   []string // accessToken header of each authenticated call
}

func init() {
	// Poll without waiting: the fakes answer immediately.
	notifyInterval = time.Millisecond
}

func newFake(t *testing.T) *fakeWeRead {
	t.Helper()
	f := &fakeWeRead{t: t, mux: http.NewServeMux()}
	f.srv = httptest.NewServer(f.mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWeRead) client(opts ...Option) *Client {
	c := NewClient(opts...)
	c.apiBase = f.srv.URL
	c.openBase = f.srv.URL
	c.longBase = f.srv.URL
	c.cosURL = func(bucket, key string) string { return f.srv.URL + "/cos-put/" + bucket + "/" + key }
	return c
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func testCreds() Credentials {
	return Credentials{VID: "1001", AccessToken: "token-old", RefreshToken: "refresh-old", DeviceID: newDeviceID()}
}

// handleImport installs credential, COS and notify handlers that succeed.
func (f *fakeWeRead) handleImport() {
	f.mux.HandleFunc("GET /cos/getcredential", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.URL.Query().Get("name") != "Dune" {
			f.t.Errorf("credential name = %q, want extension stripped", r.URL.Query().Get("name"))
		}
		writeJSON(w, map[string]any{
			"bucket":     "weread-1258476243",
			"ObjectName": "/import/1001/Dune.epub",
			"Response": map[string]any{
				"Credentials": map[string]string{"TmpSecretId": "id-placeholder", "TmpSecretKey": "key-placeholder", "Token": "sts-placeholder"},
				"ExpiredTime": time.Now().Add(time.Hour).Unix(),
			},
		})
	})
	f.mux.HandleFunc("PUT /cos-put/{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.uploaded = body
		f.cosAuth = r.Header.Get("Authorization")
		f.cosToken = r.Header.Get("x-cos-security-token")
		f.mu.Unlock()
		if r.PathValue("key") != "import/1001/Dune.epub" {
			f.t.Errorf("COS key = %q", r.PathValue("key"))
		}
	})
	f.mux.HandleFunc("POST /cos/notify", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.mu.Lock()
		f.notified = r.URL.Query()
		f.notifies = append(f.notifies, r.URL.Query())
		f.mu.Unlock()
		writeJSON(w, map[string]any{"status": 0, "bookId": "CB_abc123"})
	})
}

func (f *fakeWeRead) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, r.Header.Get("accessToken"))
	if r.Header.Get("vid") != "1001" {
		f.t.Errorf("%s vid header = %q", r.URL.Path, r.Header.Get("vid"))
	}
	if r.Header.Get("User-Agent") != userAgent || r.Header.Get("appver") != appVersion {
		f.t.Errorf("%s missing device headers", r.URL.Path)
	}
}

func writeBook(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportBookUploadsAndNotifies(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	path := writeBook(t, "Dune.epub", "epub-bytes")

	var lastSent, lastTotal int64
	var stages []ImportStage
	got, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, func(p ImportProgress) {
		if p.Stage == StageUpload {
			lastSent, lastTotal = p.Sent, p.Total
		}
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatalf("ImportBook: %v", err)
	}
	if got.BookID != "CB_abc123" || got.Name != "Dune.epub" {
		t.Fatalf("result = %+v", got)
	}
	if string(f.uploaded) != "epub-bytes" {
		t.Fatalf("uploaded %q", f.uploaded)
	}
	if lastSent != 10 || lastTotal != 10 {
		t.Fatalf("progress = %d/%d", lastSent, lastTotal)
	}
	if want := []ImportStage{StagePrepare, StageUpload, StageProcess}; !slices.Equal(stages, want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	if f.cosToken != "sts-placeholder" {
		t.Fatalf("security token = %q", f.cosToken)
	}
	for _, part := range []string{"q-sign-algorithm=sha1", "q-ak=id-placeholder", "q-header-list=content-length;host", "q-signature="} {
		if !strings.Contains(f.cosAuth, part) {
			t.Fatalf("Authorization %q lacks %q", f.cosAuth, part)
		}
	}
	if f.notified.Get("name") != "Dune.epub" || f.notified.Get("path") != "/import/1001/Dune.epub" || f.notified.Get("cancel") != "0" {
		t.Fatalf("notify query = %v", f.notified)
	}
}

func TestCallRefreshesExpiredTokenOnce(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	calls := 0
	f.mux.HandleFunc("GET /expiring", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		calls++
		if r.Header.Get("accessToken") == "token-old" {
			writeJSON(w, map[string]any{"errCode": -2012, "errMsg": "expired"})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	f.mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refreshToken"] != "refresh-old" {
			t.Errorf("refresh body = %v", body)
		}
		ts := int64(body["timestamp"].(float64))
		random := int64(body["random"].(float64))
		if body["signature"] != loginSignature(ts, body["deviceId"].(string), random) {
			t.Errorf("refresh signature mismatch")
		}
		writeJSON(w, map[string]any{"vid": 1001, "accessToken": "token-new", "refreshToken": "refresh-new"})
	})

	var saved Credentials
	c := f.client(WithCredentials(testCreds()), WithCredentialsSaver(func(creds Credentials) error {
		saved = creds
		return nil
	}))
	if err := c.call(context.Background(), http.MethodGet, "/expiring", nil, nil, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if saved.AccessToken != "token-new" || saved.RefreshToken != "refresh-new" || saved.VID != "1001" {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestCallReportsExpiredSessionWhenRefreshIsRejected(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /expiring", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errCode": -2012})
	})
	f.mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"errCode": -2013, "errMsg": "bad refresh token"})
	})
	err := f.client(WithCredentials(testCreds())).call(context.Background(), http.MethodGet, "/expiring", nil, nil, nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
}

func TestCallMapsVerificationChallenge(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /cos/getcredential", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errCode": -2041, "errMsg": "verify"})
	})
	path := writeBook(t, "Dune.epub", "x")
	_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("err = %v, want ErrVerificationRequired", err)
	}
}

func TestImportBookLostNotifyReplyIsUnconfirmed(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	// Route notify to a handler that drops the connection.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cos/notify", func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	mux.Handle("/", f.mux)
	f.srv.Config.Handler = mux

	path := writeBook(t, "Dune.epub", "epub-bytes")
	_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
	if !errors.Is(err, ErrImportUnconfirmed) {
		t.Fatalf("err = %v, want ErrImportUnconfirmed", err)
	}
}

func TestImportBookRejectsNotifyFailure(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cos/notify", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errCode": -2003, "errMsg": "quota\x1b[31m reached"})
	})
	mux.Handle("/", f.mux)
	f.srv.Config.Handler = mux

	path := writeBook(t, "Dune.epub", "epub-bytes")
	_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || errors.Is(err, ErrImportUnconfirmed) {
		t.Fatalf("err = %v, want a definite APIError", err)
	}
	if strings.ContainsRune(err.Error(), 0x1b) {
		t.Fatalf("server text reached the error unescaped: %q", err)
	}
}

func TestImportBookRejectsForeignBucket(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /cos/getcredential", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"bucket": "evil.example.com/x", "ObjectName": "/a.epub",
			"Response": map[string]any{"Credentials": map[string]string{"TmpSecretId": "a", "TmpSecretKey": "b", "Token": "c"}},
		})
	})
	path := writeBook(t, "Dune.epub", "x")
	_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("err = %v, want the credential rejected", err)
	}
}

func TestImportBookValidatesFileBeforeNetwork(t *testing.T) {
	c := NewClient(WithCredentials(testCreds()))
	c.apiBase = "http://127.0.0.1:1" // any request would fail loudly
	tests := []struct {
		name string
		path string
		want error
	}{
		{"unsupported", writeBook(t, "notes.docx", "x"), ErrUnsupportedFormat},
		{"empty", writeBook(t, "empty.epub", ""), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.ImportBook(context.Background(), tt.path, nil)
			if err == nil {
				t.Fatal("want an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "127.0.0.1") {
				t.Fatalf("validation reached the network: %v", err)
			}
		})
	}
}

func TestImportBookRequiresLogin(t *testing.T) {
	path := writeBook(t, "Dune.epub", "x")
	_, err := NewClient().ImportBook(context.Background(), path, nil)
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestQRLoginFlow(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /wxticket", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("appver") != appVersion {
			t.Errorf("wxticket lacks device headers")
		}
		writeJSON(w, map[string]any{"signature": "sig-placeholder", "timeStamp": 1700000000})
	})
	f.mux.HandleFunc("GET /connect/sdk/qrconnect", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("appid") != wechatAppID || q.Get("signature") != "sig-placeholder" || q.Get("timestamp") != "1700000000" {
			t.Errorf("qrconnect query = %v", q)
		}
		writeJSON(w, map[string]any{"errcode": 0, "uuid": "uuid-1"})
	})
	states := []map[string]any{{"wx_errcode": 408}, {"wx_errcode": 404}, {"wx_errcode": 405, "wx_code": "wx-code"}}
	var polls []string
	f.mux.HandleFunc("GET /connect/l/qrconnect", func(w http.ResponseWriter, r *http.Request) {
		polls = append(polls, r.URL.Query().Get("last"))
		writeJSON(w, states[0])
		states = states[1:]
	})
	var exchanged map[string]any
	f.mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&exchanged)
		writeJSON(w, map[string]any{"vid": 1001, "accessToken": "access", "refreshToken": "refresh"})
	})
	defer func(d time.Duration) { pollDelay = d }(pollDelay)
	pollDelay = time.Millisecond

	c := f.client()
	qr, err := c.RequestQRCode(context.Background())
	if err != nil {
		t.Fatalf("RequestQRCode: %v", err)
	}
	if qr.UUID != "uuid-1" || qr.URL != f.srv.URL+"/connect/confirm?uuid=uuid-1" {
		t.Fatalf("qr = %+v", qr)
	}
	scanned := 0
	code, err := c.WaitForConfirmation(context.Background(), qr.UUID, func() { scanned++ })
	if err != nil || code != "wx-code" {
		t.Fatalf("WaitForConfirmation = %q, %v", code, err)
	}
	if scanned != 1 || fmt.Sprint(polls) != "[ 408 404]" {
		t.Fatalf("scanned = %d, polls = %v", scanned, polls)
	}
	creds, err := c.Exchange(context.Background(), code)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if creds.VID != "1001" || creds.AccessToken != "access" || creds.RefreshToken != "refresh" ||
		!strings.HasPrefix(creds.DeviceID, deviceIDPrefix) || len(creds.DeviceID) != len(deviceIDPrefix)+19 {
		t.Fatalf("creds = %+v", creds)
	}
	if exchanged["code"] != "wx-code" || exchanged["isFromQrcode"] != float64(1) || exchanged["deviceId"] != creds.DeviceID {
		t.Fatalf("exchange body = %v", exchanged)
	}
	if c.Credentials() != creds {
		t.Fatal("client did not adopt the new credentials")
	}
}

func TestPingUsesTransportAndRefusesRedirects(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /wxticket", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"signature": "sig", "timeStamp": 1})
	})
	used := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return http.DefaultTransport.RoundTrip(r)
	})
	c := f.client(WithTransport(transport))
	if err := c.Ping(context.Background()); err != nil || !used {
		t.Fatalf("Ping = %v, transport used = %v", err, used)
	}

	f.mux.HandleFunc("GET /moved/wxticket", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	})
	c.apiBase = f.srv.URL + "/moved"
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping followed a redirect")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWaitForConfirmationTerminalStates(t *testing.T) {
	for code, want := range map[int]error{402: ErrQRExpired, 403: ErrQRDeclined} {
		f := newFake(t)
		f.mux.HandleFunc("GET /connect/l/qrconnect", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"wx_errcode": code})
		})
		_, err := f.client().WaitForConfirmation(context.Background(), "uuid", nil)
		if !errors.Is(err, want) {
			t.Fatalf("wx_errcode %d: err = %v, want %v", code, err, want)
		}
	}
}

func TestCOSAuthorizationFormat(t *testing.T) {
	got := cosAuthorization("AKIDexample", "secret", http.MethodPut, "/a b.epub",
		map[string]string{"host": "b-1.cos.accelerate.myqcloud.com", "content-length": "13"}, 100, 200)
	httpString := "put\n/a b.epub\n\ncontent-length=13&host=b-1.cos.accelerate.myqcloud.com\n"
	stringToSign := "sha1\n100;200\n" + sha1Hex(httpString) + "\n"
	want := "q-sign-algorithm=sha1&q-ak=AKIDexample&q-sign-time=100;200&q-key-time=100;200" +
		"&q-header-list=content-length;host&q-url-param-list=&q-signature=" +
		hmacSHA1Hex(hmacSHA1Hex("secret", "100;200"), stringToSign)
	if got != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
	if cosEscape("a b+c/") != "a%20b%2Bc%2F" {
		t.Fatalf("cosEscape = %q", cosEscape("a b+c/"))
	}
}

func TestCleanText(t *testing.T) {
	// A lone 0x9b byte (C1 CSI) is invalid UTF-8 and becomes U+FFFD.
	if got := cleanText(" ok\x1b[31m‮\x9b\u009b done "); got != "ok[31m� done" {
		t.Fatalf("cleanText = %q", got)
	}
}

// notifyReplies replaces the notify handler with one that answers each
// request with the next reply, repeating the last one.
func (f *fakeWeRead) notifyReplies(replies ...map[string]any) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cos/notify", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.notifies = append(f.notifies, r.URL.Query())
		reply := replies[min(len(f.notifies), len(replies))-1]
		f.mu.Unlock()
		writeJSON(w, reply)
	})
	mux.Handle("/", f.mux)
	f.srv.Config.Handler = mux
}

func TestImportBookPollsUntilWeReadFinishes(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	f.notifyReplies(map[string]any{"status": 1}, map[string]any{}, map[string]any{"status": 0, "bookId": "CB_done"})
	path := writeBook(t, "Dune.epub", "epub-bytes")

	var attempts []int
	got, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, func(p ImportProgress) {
		if p.Stage == StageProcess {
			attempts = append(attempts, p.Attempt)
			if p.Attempts != notifyAttempts {
				t.Errorf("Attempts = %d", p.Attempts)
			}
		}
	})
	if err != nil {
		t.Fatalf("ImportBook: %v", err)
	}
	if got.BookID != "CB_done" || got.AlreadyExisted {
		t.Fatalf("result = %+v", got)
	}
	if !slices.Equal(attempts, []int{1, 2, 3}) {
		t.Fatalf("attempts = %v", attempts)
	}
	for _, q := range f.notifies {
		if q.Get("cancel") != "0" {
			t.Fatalf("notify query = %v", q)
		}
	}
}

func TestImportBookReportsExistingAndParseFailure(t *testing.T) {
	tests := []struct {
		name  string
		reply map[string]any
		want  error
	}{
		{"exists", map[string]any{"status": 3, "bookId": "CB_old"}, nil},
		{"parse error", map[string]any{"status": 2}, ErrImportParseFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.handleImport()
			f.notifyReplies(tt.reply)
			path := writeBook(t, "Dune.epub", "epub-bytes")
			got, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				return
			}
			if err != nil || !got.AlreadyExisted || got.BookID != "CB_old" {
				t.Fatalf("result = %+v, %v", got, err)
			}
		})
	}
}

func TestImportBookStillProcessingIsUnconfirmed(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	f.notifyReplies(map[string]any{"status": 1})
	old := notifyAttempts
	notifyAttempts = 3
	t.Cleanup(func() { notifyAttempts = old })

	path := writeBook(t, "Dune.epub", "epub-bytes")
	_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
	if !errors.Is(err, ErrImportUnconfirmed) || len(f.notifies) != 3 {
		t.Fatalf("err = %v after %d checks, want ErrImportUnconfirmed after 3", err, len(f.notifies))
	}
}

func TestImportBookCancelWhileProcessingAbandonsImport(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	f.notifyReplies(map[string]any{"status": 1})
	path := writeBook(t, "Dune.epub", "epub-bytes")

	ctx, cancel := context.WithCancel(context.Background())
	_, err := f.client(WithCredentials(testCreds())).ImportBook(ctx, path, func(p ImportProgress) {
		if p.Stage == StageProcess && p.Attempt == 2 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrImportUnconfirmed) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	last := f.notifies[len(f.notifies)-1]
	if last.Get("cancel") != "1" || last.Get("path") != "/import/1001/Dune.epub" {
		t.Fatalf("last notify = %v, want cancel=1 for the object", last)
	}
}

func TestImportBookFailedCancelIsUnconfirmed(t *testing.T) {
	f := newFake(t)
	f.handleImport()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cos/notify", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cancel") == "1" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"status": 1})
	})
	mux.Handle("/", f.mux)
	f.srv.Config.Handler = mux
	path := writeBook(t, "Dune.epub", "epub-bytes")

	ctx, cancel := context.WithCancel(context.Background())
	_, err := f.client(WithCredentials(testCreds())).ImportBook(ctx, path, func(p ImportProgress) {
		if p.Stage == StageProcess {
			cancel()
		}
	})
	if !errors.Is(err, ErrImportUnconfirmed) {
		t.Fatalf("err = %v, want ErrImportUnconfirmed", err)
	}
}

func TestImportLimitErrorsReadAsSentinels(t *testing.T) {
	tests := []struct {
		code int
		want error
	}{
		{-2999, ErrImportLimit},
		{-2014, ErrRateLimited},
	}
	for _, tt := range tests {
		f := newFake(t)
		f.mux.HandleFunc("GET /cos/getcredential", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"errCode": tt.code, "errMsg": "server reason"})
		})
		path := writeBook(t, "Dune.epub", "x")
		_, err := f.client(WithCredentials(testCreds())).ImportBook(context.Background(), path, nil)
		if !errors.Is(err, tt.want) {
			t.Fatalf("code %d: err = %v, want %v", tt.code, err, tt.want)
		}
		if got, want := err.Error(), tt.want.Error()+": server reason"; got != want {
			t.Fatalf("code %d: message = %q, want %q", tt.code, got, want)
		}
	}
}

func TestCheckImportFileAppliesPerFormatLimits(t *testing.T) {
	big := filepath.Join(t.TempDir(), "notes.txt")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTXTSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := CheckImportFile(big); !errors.Is(err, ErrFileTooLarge) || !strings.Contains(err.Error(), "20.0 MiB") {
		t.Fatalf("TXT over 20 MiB: err = %v", err)
	}
	epub := filepath.Join(t.TempDir(), "big.epub")
	if err := os.Rename(big, epub); err != nil {
		t.Fatal(err)
	}
	if size, err := CheckImportFile(epub); err != nil || size != maxTXTSize+1 {
		t.Fatalf("EPUB under 300 MiB: size %d, err %v", size, err)
	}
	if _, err := CheckImportFile(writeBook(t, "notes.docx", "x")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("docx: err = %v", err)
	}
}

func TestProcessingThenRejectionIsUnconfirmed(t *testing.T) {
	for _, code := range []int{-2014, -2041, -2999} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := newFake(t)
			f.handleImport()
			f.notifyReplies(map[string]any{"status": 1}, map[string]any{"errCode": code})
			_, err := f.client(WithCredentials(testCreds())).ImportBook(t.Context(), writeBook(t, "Dune.epub", "fixture"), nil)
			if !errors.Is(err, ErrImportUnconfirmed) {
				t.Fatalf("missing uncertain outcome: %v", err)
			}
		})
	}
	t.Run("unknown status", func(t *testing.T) {
		f := newFake(t)
		f.handleImport()
		f.notifyReplies(map[string]any{"status": 1}, map[string]any{"status": 7})
		_, err := f.client(WithCredentials(testCreds())).ImportBook(t.Context(), writeBook(t, "Dune.epub", "fixture"), nil)
		if !errors.Is(err, ErrImportUnconfirmed) {
			t.Fatalf("missing uncertain outcome: %v", err)
		}
	})
}
