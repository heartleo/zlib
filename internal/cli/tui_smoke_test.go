package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// Opt-in terminal walkthrough. Fixtures never contact Z-Library or SMTP.
// go test -c ./internal/cli -o tui-smoke.exe
// ZLIB_TUI_SMOKE=1 ./tui-smoke.exe -test.run TestTUIWalkthrough -test.v
func TestTUIWalkthrough(t *testing.T) {
	if os.Getenv("ZLIB_TUI_SMOKE") != "1" {
		t.Skip("manual terminal walkthrough")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/demo.epub" {
			w.Header().Set("Content-Disposition", `attachment; filename="demo.epub"`)
			w.Header().Set("Content-Length", "1048576")
			for range 32 {
				if _, err := w.Write([]byte(strings.Repeat("x", 32768))); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				time.Sleep(60 * time.Millisecond)
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delete") {
			fmt.Fprint(w, `{"success":1}`)
			return
		}
		fmt.Fprint(w, `{"success":1,"books":[{"id":42,"hash":"abcdef","title":"Fixture book","author":"Reader","extension":"epub","filesizeString":"1 MB","date":"2026-10-14"}],"pagination":{"current":1,"total_pages":1}}`)
	}))
	defer server.Close()
	c := zlib.NewClient(zlib.WithDomain(server.URL))
	c.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "fixture"})
	books := feedTestModel().books
	load := func() ([]zlib.Book, error) { return books, nil }
	saved := newSavedBooksSelectModel(func(page int) (zlib.SearchResult, error) {
		return zlib.SearchResult{Books: books, Page: page, TotalPages: 3}, nil
	})
	search := newSearchSelectModel("fixture", c, nil, false)
	settings, _ := newSettingsModel(t.Context(), testSettingsStore(t), "domain")
	settings.domains = []string{"https://z-lib.gd", "https://z-lib.gl"}
	settings.checks = map[string]zlib.DomainCheck{
		"https://z-lib.gd": {Domain: "https://z-lib.gd", Status: zlib.DomainStatusHealthy, FinalURL: "https://z-lib.gd/eapi/info/domains"},
		"https://z-lib.gl": {Domain: "https://z-lib.gl", Status: zlib.DomainStatusNetworkError},
	}
	settings.message = "Fixture domains; no probe performed"
	dir := t.TempDir()
	os.WriteFile(dir+"/demo.epub", []byte("fixture"), 0600)
	models := []struct {
		name  string
		model tea.Model
	}{
		{"recommended", newBookFeedSelectModel("Recommended", load)},
		{"most-popular", newBookFeedSelectModel("Most popular", load)},
		{"saved", saved}, {"search", search}, {"history", newHistorySelectModel(c)},
		{"domains", settingsStart{settingsModel: settings}}, {"download-directory", newDownloadDirectoryModel(dir)},
		{"kindle-file", newKindleFileModel(dir)},
		{"weread-file", newWeReadFileModel(dir)},
		{"download", newDirectDownloadModel("Fixture book", server.URL+"/demo.epub", dir, c)},
	}
	for _, scene := range models {
		t.Log("Scene:", scene.name)
		result, err := tea.NewProgram(scene.model).Run()
		if err != nil {
			t.Fatal(err)
		}
		if dm, ok := result.(downloadModel); ok {
			if dm.err != nil {
				t.Fatal(dm.err)
			}
			t.Logf("download state=%d bytes=%d", dm.state, dm.savedSize)
		}
	}
}
