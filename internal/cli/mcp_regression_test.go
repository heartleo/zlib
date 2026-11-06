package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heartleo/zlib/internal/zlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func useMCPFixtureClient(t *testing.T, domain string) {
	t.Helper()
	t.Setenv(zlib.EnvProxy, "")
	previous := loadClient
	loadClient = func() (*zlib.Client, error) {
		c := zlib.NewClient(zlib.WithDomain(domain))
		c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "fixture-only"})
		return c, nil
	}
	t.Cleanup(func() { loadClient = previous })
}

func TestMCPCancelledCallsNeverLoadAccountOrSend(t *testing.T) {
	previous := loadClient
	var loads int
	loadClient = func() (*zlib.Client, error) {
		loads++
		return nil, errors.New("unexpected account load")
	}
	defer func() { loadClient = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func() error{
		"profile": func() error { _, _, err := handleMCPGetProfile(ctx, nil, mcpEmptyInput{}); return err },
		"search": func() error {
			_, _, err := handleMCPSearchBooks(ctx, nil, mcpSearchBooksInput{Query: "fixture"})
			return err
		},
		"book":        func() error { _, _, err := handleMCPGetBook(ctx, nil, mcpBookIDInput{ID: "42:abc"}); return err },
		"recommended": func() error { _, _, err := handleMCPGetRecommendedBooks(ctx, nil, mcpEmptyInput{}); return err },
		"popular":     func() error { _, _, err := handleMCPGetMostPopularBooks(ctx, nil, mcpEmptyInput{}); return err },
		"saved":       func() error { _, _, err := handleMCPGetSavedBooks(ctx, nil, mcpPageInput{}); return err },
		"history":     func() error { _, _, err := handleMCPGetDownloadHistory(ctx, nil, mcpHistoryInput{}); return err },
		"delete": func() error {
			_, _, err := handleMCPDeleteHistory(ctx, nil, mcpDeleteHistoryInput{ID: "42"})
			return err
		},
		"download": func() error {
			_, _, err := handleMCPDownloadBook(ctx, nil, mcpDownloadBookInput{ID: "42:abc", Dir: filepath.Join(t.TempDir(), "unused")})
			return err
		},
		"kindle": func() error {
			_, _, err := handleMCPSendToKindle(ctx, nil, mcpSendToKindleInput{FilePath: "fixture.epub"})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled call returned %v", err)
			}
		})
	}
	if loads != 0 {
		t.Fatalf("cancelled calls loaded account %d times", loads)
	}
}

func TestMCPCancelPropagatesToActiveHTTPRequest(t *testing.T) {
	started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	useMCPFixtureClient(t, server.URL)
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_profile", Arguments: map[string]any{}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request never started")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("MCP cancellation did not reach HTTP request")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled MCP call did not return")
	}
}

func TestMCPHistoryDoesNotHideEnrichmentFailures(t *testing.T) {
	for _, tc := range []struct {
		name, extension, detailExtension, detailSize string
		status                                       int
		wantError                                    bool
	}{
		{name: "network failure", status: http.StatusServiceUnavailable, wantError: true},
		{name: "still unknown", status: http.StatusOK, wantError: true},
		{name: "size only missing", extension: "pdf", detailExtension: "pdf", detailSize: "4 MB", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var details atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/eapi/user/book/downloaded" {
					_ = json.NewEncoder(w).Encode(map[string]any{"success": 1, "books": []any{map[string]any{"id": 42, "hash": "abc", "title": "Fixture", "extension": tc.extension}}, "pagination": map[string]any{"current": 1, "total_pages": 1}})
					return
				}
				details.Add(1)
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": 1, "book": map[string]any{"id": 42, "hash": "abc", "title": "Fixture", "extension": tc.detailExtension, "filesizeString": tc.detailSize}})
			}))
			defer server.Close()
			useMCPFixtureClient(t, server.URL)
			session := connectMCPTestSession(t, mcpServerOptions{}, nil)
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_download_history", Arguments: map[string]any{"format": "pdf"}})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tc.wantError || details.Load() != 1 {
				t.Fatalf("IsError=%v, detail calls=%d, content=%v", res.IsError, details.Load(), res.Content)
			}
			if !tc.wantError {
				var history zlib.DownloadHistoryResult
				decodeToolJSON(t, res, &history)
				if len(history.Items) != 1 || history.Items[0].Size != "4 MB" || history.Items[0].ID != "42:abc" {
					t.Fatalf("history metadata not completed: %+v", history)
				}
			}
		})
	}
}

func TestMCPIndependentWriteCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   mcpServerOptions
		hidden []string
		count  int
	}{
		{"download", mcpServerOptions{DisableDownload: true}, []string{"download_book"}, 11},
		{"delete", mcpServerOptions{DisableHistoryDelete: true}, []string{"delete_download_history"}, 11},
		{"kindle", mcpServerOptions{DisableKindle: true}, []string{"send_to_kindle"}, 11},
		{"weread", mcpServerOptions{DisableWeRead: true}, []string{"import_to_weread"}, 11},
		{"read only", mcpServerOptions{ReadOnly: true}, []string{"download_book", "delete_download_history", "send_to_kindle", "import_to_weread"}, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := connectMCPTestSession(t, tc.opts, nil)
			tools, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(tools.Tools) != tc.count {
				t.Fatalf("tool count=%d, want %d", len(tools.Tools), tc.count)
			}
			for _, name := range tc.hidden {
				if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}}); err == nil {
					t.Fatalf("disabled tool %q remained callable", name)
				}
			}
			if tc.opts.DisableKindle {
				for _, tool := range tools.Tools {
					if tool.Name != "download_book" {
						continue
					}
					raw, _ := json.Marshal(tool.InputSchema)
					if strings.Contains(string(raw), "send_to_kindle") {
						t.Fatalf("disabled Kindle delivery still advertised: %s", raw)
					}
				}
				res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "download_book", Arguments: map[string]any{"id": "42:abc", "send_to_kindle": true}})
				if err != nil || !res.IsError {
					t.Fatalf("download bypassed Kindle restriction: %v %+v", err, res)
				}
				ctx := context.WithValue(context.Background(), mcpOptionsKey{}, tc.opts)
				if _, _, err := handleMCPDownloadBook(ctx, nil, mcpDownloadBookInput{ID: "42:abc", SendToKindle: true}); err == nil {
					t.Fatal("handler did not enforce Kindle restriction independently of schema")
				}
			}
			if tc.opts.DisableWeRead {
				for _, tool := range tools.Tools {
					raw, _ := json.Marshal(tool.InputSchema)
					if tool.Name == "download_book" && strings.Contains(string(raw), "send_to_weread") {
						t.Fatalf("disabled WeRead import still advertised: %s", raw)
					}
				}
				ctx := context.WithValue(context.Background(), mcpOptionsKey{}, tc.opts)
				if _, _, err := handleMCPDownloadBook(ctx, nil, mcpDownloadBookInput{ID: "42:abc", SendToWeRead: true}); err == nil {
					t.Fatal("handler did not enforce WeRead restriction independently of schema")
				}
				if _, _, err := handleMCPImportToWeRead(ctx, nil, mcpImportToWeReadInput{FilePath: "book.epub"}); err == nil {
					t.Fatal("import handler did not enforce WeRead restriction independently of registration")
				}
			}
		})
	}
}

func TestMCPSchemaRejectsInvalidSearchInputs(t *testing.T) {
	previous := loadClient
	var loads atomic.Int32
	loadClient = func() (*zlib.Client, error) {
		loads.Add(1)
		return nil, errors.New("unexpected account load")
	}
	t.Cleanup(func() { loadClient = previous })
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	for _, args := range []map[string]any{
		{"query": "fixture", "count": 0},
		{"query": "fixture", "count": 51},
		{"query": "fixture", "page": 0},
		{"query": "fixture", "from_year": 99},
		{"query": "fixture", "to_year": 10000},
		{"query": "fixture", "order": "bogus"},
		{"query": ""},
	} {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_books", Arguments: args})
		if err != nil || !res.IsError {
			t.Fatalf("invalid arguments accepted: %v, err=%v, result=%+v", args, err, res)
		}
	}
	if loads.Load() != 0 {
		t.Fatalf("invalid arguments reached the account loader %d times", loads.Load())
	}
}

func TestMCPErrorsPreserveOriginalCause(t *testing.T) {
	if err := formatMCPError(zlib.ErrSessionExpired); !errors.Is(err, zlib.ErrSessionExpired) {
		t.Fatalf("sentinel lost: %v", err)
	}
	original := &url.Error{Op: "Get", URL: "https://fixture.example", Err: context.DeadlineExceeded}
	err := formatMCPError(original)
	var got *url.Error
	if !errors.As(err, &got) || got != original || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("typed error chain lost: %v", err)
	}
}

func TestSearchFlagsAndMCPShareNormalization(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("ext", []string{" .epub ", "EPUB"}, "")
	cmd.Flags().StringArray("format", []string{" pdf "}, "")
	cmd.Flags().StringArray("language", []string{" English ", ""}, "")
	cmd.Flags().Int("from-year", 2000, "")
	cmd.Flags().Int("to-year", 2020, "")
	cmd.Flags().Bool("exact", true, "")
	cmd.Flags().String("order", " popular ", "")
	fromFlags, err := searchOptsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	fromMCP, err := searchOptsFromMCPInput(mcpSearchBooksInput{Extensions: []string{" .epub ", "EPUB", " pdf "}, Languages: []string{" English ", ""}, FromYear: 2000, ToYear: 2020, Exact: true, Order: " popular "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromFlags, fromMCP) || len(fromMCP.Extensions) != 2 || len(fromMCP.Languages) != 1 {
		t.Fatalf("inconsistent search normalization: flags=%+v mcp=%+v", fromFlags, fromMCP)
	}
}

func TestMCPReusesProxyConnectionsWithFreshAccounts(t *testing.T) {
	t.Setenv(zlib.EnvProxy, "")
	var connections atomic.Int32
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("remix-userid")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": 1, "user": map[string]any{"id": id, "name": "Fixture account " + id}})
	}))
	proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	proxy.Start()
	defer proxy.Close()
	source := zlib.NewClient(zlib.WithProxy(proxy.URL))
	defer source.CloseIdleConnections()
	previous := loadClient
	var accounts int
	loadClient = func() (*zlib.Client, error) {
		accounts++
		c := zlib.NewClient(zlib.WithDomain("http://fixture.invalid"), zlib.WithProxy(proxy.URL))
		c.SetCookies(map[string]string{"remix_userid": fmt.Sprint(accounts), "remix_userkey": "fixture-only"})
		return c, nil
	}
	defer func() { loadClient = previous }()
	session := connectMCPTestSession(t, mcpServerOptions{transportSource: source}, nil)
	for i := 1; i <= 2; i++ {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_profile", Arguments: map[string]any{}})
		if err != nil || res.IsError {
			t.Fatalf("profile failed: %v %+v", err, res)
		}
		var profile zlib.UserProfile
		decodeToolJSON(t, res, &profile)
		if profile.ID != fmt.Sprint(i) {
			t.Fatalf("account state was cached: %+v", profile)
		}
	}
	if connections.Load() != 1 || accounts != 2 {
		t.Fatalf("connections=%d, account reloads=%d", connections.Load(), accounts)
	}
}
