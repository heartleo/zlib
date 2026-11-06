package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connectMCPTestSession(t *testing.T, opts mcpServerOptions, clientOpts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	if opts.transportSource == nil {
		opts.transportSource = zlib.NewClient()
	}
	t.Cleanup(opts.transportSource.CloseIdleConnections)
	server := newMCPServer(opts)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, clientOpts)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func withDemoMCPFixtures(t *testing.T) {
	t.Helper()
	t.Setenv("ZLIB_DEMO_FAST", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(zlib.EnvDomain, "")
	t.Setenv(zlib.EnvProxy, "")

	server := newDemoServer()
	t.Cleanup(server.Close)

	prevLoad := loadClient
	loadClient = func() (*zlib.Client, error) {
		c := zlib.NewClient(zlib.WithDomain(server.URL))
		c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "demo"})
		return c, nil
	}
	t.Cleanup(func() { loadClient = prevLoad })

	prevProbe := probeAvailableDomains
	probeAvailableDomains = func(_ context.Context, _ []string, _ string) []zlib.DomainCheck {
		return []zlib.DomainCheck{{
			Domain:     "https://z-lib.gd",
			Status:     zlib.DomainStatusHealthy,
			HTTPStatus: http.StatusOK,
			FinalURL:   "https://z-lib.gd/eapi/info/domains",
		}}
	}
	t.Cleanup(func() { probeAvailableDomains = prevProbe })
}

func TestMCPListToolsAndSchemas(t *testing.T) {
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	wantAll := []string{
		"search_books",
		"get_book",
		"get_recommended_books",
		"get_most_popular_books",
		"get_saved_books",
		"get_download_history",
		"get_profile",
		"check_domains",
		"download_book",
		"delete_download_history",
		"send_to_kindle",
		"import_to_weread",
	}
	var gotNames []string
	for _, tool := range res.Tools {
		gotNames = append(gotNames, tool.Name)
		if tool.Title == "" || tool.Description == "" {
			t.Errorf("tool %s missing title or description", tool.Name)
		}
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Errorf("tool %s missing openWorldHint annotation", tool.Name)
		}
		rawIn, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema for %s: %v", tool.Name, err)
		}
		if strings.Contains(string(rawIn), `"null"`) {
			t.Errorf("tool %s input schema contains nullable union type: %s", tool.Name, rawIn)
		}
		var inObj map[string]any
		if err := json.Unmarshal(rawIn, &inObj); err != nil {
			t.Fatalf("unmarshal input schema for %s: %v", tool.Name, err)
		}
		if inObj["type"] != "object" {
			t.Errorf("tool %s input schema type = %v, want object", tool.Name, inObj["type"])
		}
		if _, ok := inObj["properties"]; !ok {
			t.Errorf("tool %s input schema missing properties object: %s", tool.Name, rawIn)
		}
		if tool.OutputSchema == nil {
			t.Errorf("tool %s missing output schema", tool.Name)
		}
	}
	for _, want := range wantAll {
		if !slices.Contains(gotNames, want) {
			t.Errorf("ListTools missing %q in %v", want, gotNames)
		}
	}

	roSession := connectMCPTestSession(t, mcpServerOptions{ReadOnly: true}, nil)
	roRes, err := roSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools(ReadOnly): %v", err)
	}
	for _, tool := range roRes.Tools {
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only server exposed non-read-only tool %q", tool.Name)
		}
		if slices.Contains([]string{"download_book", "delete_download_history", "send_to_kindle", "import_to_weread"}, tool.Name) {
			t.Errorf("read-only server exposed write tool %q", tool.Name)
		}
	}
	if len(roRes.Tools) != 8 {
		t.Errorf("read-only tool count = %d, want 8", len(roRes.Tools))
	}
}

func TestMCPReadTools(t *testing.T) {
	withDemoMCPFixtures(t)
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)

	// search_books
	searchRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "search_books",
		Arguments: map[string]any{
			"query":      "Dune",
			"extensions": []string{"epub"},
			"languages":  []string{"english"},
			"from_year":  1960,
			"to_year":    2025,
			"order":      "popular",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(search_books): %v", err)
	}
	if searchRes.IsError {
		t.Fatalf("search_books returned error: %+v", searchRes.Content)
	}
	var sr zlib.SearchResult
	decodeToolJSON(t, searchRes, &sr)
	if len(sr.Books) == 0 {
		t.Fatal("search_books returned 0 books")
	}
	if sr.Books[0].ID != "115066162:e9f13b" {
		t.Errorf("search_books first ID = %q, want 115066162:e9f13b", sr.Books[0].ID)
	}
	for _, b := range sr.Books {
		if !strings.EqualFold(b.Extension, "epub") {
			t.Errorf("search_books extension filter leaked %q", b.Extension)
		}
	}

	// get_book
	bookRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_book",
		Arguments: map[string]any{"id": "115066162:e9f13b"},
	})
	if err != nil || bookRes.IsError {
		t.Fatalf("get_book failed: err=%v res=%+v", err, bookRes)
	}
	var gotBook zlib.Book
	decodeToolJSON(t, bookRes, &gotBook)
	if gotBook.ID != "115066162:e9f13b" || gotBook.Name != "Dune" {
		t.Errorf("get_book = %+v", gotBook)
	}

	// get_recommended_books & get_most_popular_books
	for _, toolName := range []string{"get_recommended_books", "get_most_popular_books"} {
		feedRes, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      toolName,
			Arguments: map[string]any{},
		})
		if err != nil || feedRes.IsError {
			t.Fatalf("%s failed: err=%v res=%+v", toolName, err, feedRes)
		}
		var list mcpBookListResult
		decodeToolJSON(t, feedRes, &list)
		if len(list.Books) == 0 || !strings.Contains(list.Books[0].ID, ":") {
			t.Errorf("%s books = %+v", toolName, list.Books)
		}
	}

	// get_saved_books
	savedRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_saved_books",
		Arguments: map[string]any{"page": 1},
	})
	if err != nil || savedRes.IsError {
		t.Fatalf("get_saved_books failed: err=%v res=%+v", err, savedRes)
	}
	var saved zlib.SearchResult
	decodeToolJSON(t, savedRes, &saved)
	if len(saved.Books) != 4 || saved.Books[0].ID != "115066162:e9f13b" {
		t.Errorf("get_saved_books = %+v", saved)
	}

	// get_download_history with format filter
	histRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_download_history",
		Arguments: map[string]any{"page": 1, "format": "pdf"},
	})
	if err != nil || histRes.IsError {
		t.Fatalf("get_download_history failed: err=%v res=%+v", err, histRes)
	}
	var hist zlib.DownloadHistoryResult
	decodeToolJSON(t, histRes, &hist)
	if len(hist.Items) != 1 || hist.Items[0].ID != "38104421:b81e22" {
		t.Errorf("get_download_history(pdf) = %+v", hist)
	}

	// get_profile
	profRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_profile",
		Arguments: map[string]any{},
	})
	if err != nil || profRes.IsError {
		t.Fatalf("get_profile failed: err=%v res=%+v", err, profRes)
	}
	var prof zlib.UserProfile
	decodeToolJSON(t, profRes, &prof)
	if !prof.QuotaKnown || prof.DailyRemaining != 7 || prof.Email != "reader@example.com" {
		t.Errorf("get_profile = %+v", prof)
	}

	// check_domains
	domRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_domains",
		Arguments: map[string]any{},
	})
	if err != nil || domRes.IsError {
		t.Fatalf("check_domains failed: err=%v res=%+v", err, domRes)
	}
	var doms mcpCheckDomainsResult
	decodeToolJSON(t, domRes, &doms)
	if len(doms.Domains) != 1 || doms.Domains[0].Domain != "https://z-lib.gd" {
		t.Errorf("check_domains = %+v", doms)
	}
}

func TestMCPWriteToolsAndProgress(t *testing.T) {
	withDemoMCPFixtures(t)
	ctx := context.Background()

	var mu sync.Mutex
	var progressCount int
	session := connectMCPTestSession(t, mcpServerOptions{}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			progressCount++
			mu.Unlock()
		},
	})

	destDir := filepath.Join(t.TempDir(), "nested", "books")
	dlRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "download_book",
		Meta: mcp.Meta{"progressToken": "dl-1"},
		Arguments: map[string]any{
			"id":  "115066162:e9f13b",
			"dir": destDir,
		},
	})
	if err != nil || dlRes.IsError {
		t.Fatalf("download_book failed: err=%v res=%+v", err, dlRes)
	}
	var gotDL downloadResult
	decodeToolJSON(t, dlRes, &gotDL)
	if gotDL.ID != "115066162:e9f13b" || gotDL.Name != "Dune" || gotDL.Size <= 0 {
		t.Errorf("download_book result = %+v", gotDL)
	}
	if _, err := os.Stat(gotDL.Path); err != nil {
		t.Errorf("downloaded file missing at %q: %v", gotDL.Path, err)
	}
	mu.Lock()
	gotProgress := progressCount
	mu.Unlock()
	if gotProgress == 0 {
		t.Error("expected download_book to emit MCP progress notifications when progressToken is set")
	}

	// download_book with send_to_kindle when Kindle config is missing still reports
	// the saved file path while flagging IsError=true.
	dlKindleRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "download_book",
		Arguments: map[string]any{
			"id":             "27462851:4c0a7d",
			"dir":            destDir,
			"send_to_kindle": true,
		},
	})
	if err != nil {
		t.Fatalf("download_book(send_to_kindle): %v", err)
	}
	if !dlKindleRes.IsError {
		t.Fatal("expected IsError=true when send_to_kindle fails after saving")
	}
	var partialDL downloadResult
	decodeToolJSON(t, dlKindleRes, &partialDL)
	if partialDL.Path == "" || partialDL.Kindle == nil || partialDL.Kindle.Sent || !strings.Contains(partialDL.Kindle.Error, "kindle config not found") {
		t.Errorf("partial download result = %+v", partialDL)
	}

	// delete_download_history
	delRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delete_download_history",
		Arguments: map[string]any{"id": "115066162:e9f13b"},
	})
	if err != nil || delRes.IsError {
		t.Fatalf("delete_download_history failed: err=%v res=%+v", err, delRes)
	}
	var delOut mcpDeleteHistoryResult
	decodeToolJSON(t, delRes, &delOut)
	if !delOut.Success || delOut.ID != "115066162:e9f13b" {
		t.Errorf("delete_download_history = %+v", delOut)
	}
}

func TestMCPErrorHandlingAndSanitization(t *testing.T) {
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)

	// Invalid search year range and order
	withDemoMCPFixtures(t)
	badYearRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_books",
		Arguments: map[string]any{"query": "Dune", "from_year": 2025, "to_year": 2020},
	})
	if err != nil || !badYearRes.IsError || !strings.Contains(toolText(t, badYearRes), "invalid year range") {
		t.Fatalf("expected invalid year range error, got err=%v res=%+v", err, badYearRes)
	}

	badOrderRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_books",
		Arguments: map[string]any{"query": "Dune", "order": "bogus"},
	})
	if err != nil || !badOrderRes.IsError || !strings.Contains(toolText(t, badOrderRes), "order") {
		t.Fatalf("expected invalid search order error, got err=%v res=%+v", err, badOrderRes)
	}

	// Not logged in
	prevLoad := loadClient
	loadClient = func() (*zlib.Client, error) {
		return nil, errNotLoggedIn
	}
	defer func() { loadClient = prevLoad }()

	authRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_profile",
		Arguments: map[string]any{},
	})
	if err != nil || !authRes.IsError || !strings.Contains(toolText(t, authRes), "zlib login") {
		t.Fatalf("expected zlib login remedy, got err=%v res=%+v", err, authRes)
	}

	// Control characters stripped from formatMCPErrorText
	got := formatMCPErrorText(errors.New("mirror said \x1b[31mboom\x1b[0m"))
	if strings.Contains(got, "\x1b") {
		t.Errorf("formatMCPErrorText leaked escape sequence: %q", got)
	}
	if !strings.Contains(formatMCPErrorText(zlib.ErrBotProtection), "check_domains") {
		t.Errorf("ErrBotProtection message missing check_domains hint: %q", formatMCPErrorText(zlib.ErrBotProtection))
	}
}

func TestMCPReadOnlyRejectsWriteTools(t *testing.T) {
	withDemoMCPFixtures(t)
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{ReadOnly: true}, nil)

	for _, name := range []string{"download_book", "delete_download_history", "send_to_kindle", "import_to_weread"} {
		if _, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{},
		}); err == nil {
			t.Errorf("read-only server accepted a call to %q", name)
		}
	}

	// Read tools still work on the read-only server.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_profile",
		Arguments: map[string]any{},
	})
	if err != nil || res.IsError {
		t.Fatalf("read-only get_profile failed: err=%v res=%+v", err, res)
	}
}

func TestMCPHistoryFillsMissingMetadataBeforeFiltering(t *testing.T) {
	withDemoMCPFixtures(t)
	ctx := context.Background()

	// The fixture history omits extension/size; without the detail fetch the
	// pdf filter would drop every row.
	prevHistory := demoHistoryBookJSON
	demoHistoryBookJSON = func(b demoBook) map[string]any {
		m := b.json()
		delete(m, "extension")
		delete(m, "filesizeString")
		return m
	}
	defer func() { demoHistoryBookJSON = prevHistory }()

	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_download_history",
		Arguments: map[string]any{"page": 1, "format": "pdf"},
	})
	if err != nil || res.IsError {
		t.Fatalf("get_download_history failed: err=%v res=%+v", err, res)
	}
	var hist zlib.DownloadHistoryResult
	decodeToolJSON(t, res, &hist)
	if len(hist.Items) != 1 || hist.Items[0].ID != "38104421:b81e22" {
		t.Fatalf("filled history (pdf) = %+v", hist)
	}
	if hist.Items[0].Extension != "pdf" || hist.Items[0].Size == "" {
		t.Errorf("history item metadata not filled: %+v", hist.Items[0])
	}
}

func TestMCPDownloadBookDoesNotCreateDirForUnknownID(t *testing.T) {
	withDemoMCPFixtures(t)
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)

	dir := filepath.Join(t.TempDir(), "never", "created")
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "download_book",
		Arguments: map[string]any{"id": "999999:deadbe", "dir": dir},
	})
	if err != nil {
		t.Fatalf("download_book: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for an unknown book ID")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("download dir %q was created before the book resolved", dir)
	}
}

func TestMCPDownloadBookAnnotationIsDestructive(t *testing.T) {
	ctx := context.Background()
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "download_book" {
			continue
		}
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Fatalf("download_book must be marked destructive: it renames over an existing file")
		}
		return
	}
	t.Fatal("download_book tool not listed")
}

func TestMCPStdioSubprocess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestDemo$")
	cmd.Env = append(os.Environ(),
		"ZLIB_DEMO=1",
		"ZLIB_DEMO_FAST=1",
		"ZLIB_DEMO_HOME="+t.TempDir(),
		"ZLIB_DEMO_DIR="+t.TempDir(),
		"ZLIB_DEMO_ARGS=mcp",
	)
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("client.Connect over stdio subprocess: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_books",
		Arguments: map[string]any{"query": "Dune", "extensions": []string{"epub"}},
	})
	if err != nil || res.IsError {
		t.Fatalf("stdio search_books failed: err=%v res=%+v", err, res)
	}
	var sr zlib.SearchResult
	decodeToolJSON(t, res, &sr)
	if len(sr.Books) == 0 || sr.Books[0].ID != "115066162:e9f13b" {
		t.Errorf("stdio search_books result = %+v", sr)
	}
}

func decodeToolJSON(t *testing.T, res *mcp.CallToolResult, dst any) {
	t.Helper()
	text := toolText(t, res)
	if err := json.Unmarshal([]byte(text), dst); err != nil {
		t.Fatalf("unmarshal tool JSON %q: %v", text, err)
	}
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("tool content[0] = %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}
