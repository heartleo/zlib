package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/zlib"
)

// demoBook is one fixture result. The first books match the hero picker on
// the site (site/index.html), so the README GIF and the page agree.
type demoBook struct {
	id, hash, title, author, ext, size, rating, year string
}

var demoCatalogue = []demoBook{
	{"115066162", "e9f13b", "Dune", "Frank Herbert", "epub", "2.8 MB", "5.0", "1965"},
	{"27462851", "4c0a7d", "Dune Messiah", "Frank Herbert", "epub", "1.4 MB", "4.6", "1969"},
	{"38104421", "b81e22", "Children of Dune", "Frank Herbert", "pdf", "3.1 MB", "4.4", "1976"},
	{"19288013", "07d9fa", "God Emperor of Dune", "Frank Herbert", "epub", "1.9 MB", "4.2", "1981"},
	{"55519037", "a3c615", "Heretics of Dune", "Frank Herbert", "mobi", "2.2 MB", "4.1", "1984"},
	{"88214470", "5f2e90", "Chapterhouse: Dune", "Frank Herbert", "epub", "2.0 MB", "4.0", "1985"},
	{"41003377", "c47b08", "The Dune Encyclopedia", "W. E. McNelly", "pdf", "24.6 MB", "4.7", "1984"},
	{"76620518", "19ad53", "Dune: The Graphic Novel", "Brian Herbert", "pdf", "61.3 MB", "4.3", "2020"},
	{"90135520", "e2b4c1", "The Road to Dune", "Frank Herbert", "epub", "1.6 MB", "4.0", "2005"},
	{"62884051", "7a90de", "Sisterhood of Dune", "Brian Herbert", "epub", "2.4 MB", "3.9", "2012"},
	{"11390745", "d05f37", "Mentats of Dune", "Brian Herbert", "epub", "2.3 MB", "3.8", "2014"},
	{"47712806", "3be812", "Navigators of Dune", "Brian Herbert", "epub", "2.5 MB", "3.8", "2016"},
	{"30948117", "f61c4a", "Dune: House Atreides", "Brian Herbert", "pdf", "4.8 MB", "4.1", "1999"},
	{"88230948", "8e27b5", "Dune: House Harkonnen", "Brian Herbert", "pdf", "4.6 MB", "4.0", "2000"},
	{"71204458", "2d93f0", "Dune: House Corrino", "Brian Herbert", "epub", "2.7 MB", "4.0", "2001"},
	{"55190322", "c1e86b", "Paul of Dune", "Brian Herbert", "epub", "2.6 MB", "3.9", "2008"},
	{"20417736", "9b3e51", "Dune: The Butlerian Jihad", "Brian Herbert", "epub", "2.9 MB", "4.0", "2002"},
	{"20417802", "a61c07", "Dune: The Machine Crusade", "Brian Herbert", "epub", "3.0 MB", "3.9", "2003"},
	{"20417915", "3fd284", "Dune: The Battle of Corrin", "Brian Herbert", "epub", "2.8 MB", "3.9", "2004"},
	{"31166048", "c08e9a", "Hunters of Dune", "Brian Herbert", "epub", "2.2 MB", "3.8", "2006"},
	{"31166112", "5e7d3b", "Sandworms of Dune", "Brian Herbert", "epub", "2.4 MB", "3.8", "2007"},
	{"40281975", "e45a10", "The Winds of Dune", "Brian Herbert", "epub", "2.3 MB", "3.7", "2009"},
	{"68830421", "7c19fe", "Dune: The Duke of Caladan", "Brian Herbert", "epub", "1.8 MB", "3.9", "2020"},
	{"68830559", "b2d846", "Dune: The Lady of Caladan", "Brian Herbert", "epub", "1.9 MB", "3.9", "2021"},
}

// demoPageSize keeps two pages of results, like the site's picker.
const demoPageSize = 15

// demoSearch filters the catalogue by the requested formats (none means
// any) and returns one page of it.
func demoSearch(formats []string, page int) ([]demoBook, int) {
	var matches []demoBook
	for _, b := range demoCatalogue {
		if len(formats) == 0 || slices.Contains(formats, b.ext) {
			matches = append(matches, b)
		}
	}
	pages := max(1, (len(matches)+demoPageSize-1)/demoPageSize)
	page = min(max(page, 1), pages)
	from := (page - 1) * demoPageSize
	return matches[from:min(len(matches), from+demoPageSize)], pages
}

func (b demoBook) json() map[string]any {
	return map[string]any{
		"id": b.id, "hash": b.hash, "title": b.title, "author": b.author,
		"extension": b.ext, "filesizeString": b.size, "interestScore": b.rating,
		"year": b.year, "language": "English", "publisher": "Ace",
	}
}

func demoFind(id string) (demoBook, bool) {
	for _, b := range demoCatalogue {
		if b.id == id {
			return b, true
		}
	}
	return demoBook{}, false
}

// demoHistoryBookJSON renders a history entry. It is a variable so tests can
// drop the extension and size fields the EAPI history sometimes omits.
var demoHistoryBookJSON = func(b demoBook) map[string]any { return b.json() }

// demoBookList renders books for a saved/history response; history uses the
// replaceable renderer.
func demoBookList(books []demoBook, path string) []map[string]any {
	render := demoHistoryBookJSON
	if path != "/eapi/user/book/downloaded" {
		render = func(b demoBook) map[string]any { return b.json() }
	}
	out := make([]map[string]any, 0, len(books))
	for _, b := range books {
		out = append(out, render(b))
	}
	return out
}

// newDemoServer answers the EAPI calls the CLI makes with fixture data. It
// holds no real account, session or book, and never contacts Z-Library.
func newDemoServer() *httptest.Server {
	var server *httptest.Server
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	list := func(books []demoBook) []map[string]any {
		out := make([]map[string]any, 0, len(books))
		for _, b := range books {
			out = append(out, b.json())
		}
		return out
	}
	fast := os.Getenv("ZLIB_DEMO_FAST") == "1"
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/eapi/book/search":
			if !fast {
				// Make the request visibly take a moment, as a real mirror does.
				time.Sleep(700 * time.Millisecond)
			}
			r.ParseForm()
			page, _ := strconv.Atoi(r.FormValue("page"))
			page = max(page, 1)
			books, pages := demoSearch(r.Form["extensions[]"], page)
			reply(w, map[string]any{"success": 1, "books": list(books),
				"pagination": map[string]any{"current": min(page, pages), "total_pages": pages}})
		case path == "/eapi/user/profile":
			reply(w, map[string]any{"success": 1, "user": map[string]any{
				"id": 1, "name": "Reader", "email": "reader@example.com",
				"downloads_today": 3, "downloads_limit": 10}})
		case path == "/eapi/user/book/downloaded", path == "/eapi/user/book/saved":
			reply(w, map[string]any{"success": 1, "books": demoBookList(demoCatalogue[:4], path),
				"pagination": map[string]any{"current": 1, "total_pages": 1}})
		case strings.HasPrefix(path, "/eapi/user/book/") && strings.HasSuffix(path, "/delete"):
			reply(w, map[string]any{"success": 1})
		case path == "/eapi/user/book/recommended", path == "/eapi/book/most-popular":
			reply(w, map[string]any{"success": 1, "books": list(demoCatalogue[8:16])})
		case strings.HasPrefix(path, "/eapi/book/") && strings.HasSuffix(path, "/file"):
			id := strings.Split(strings.TrimPrefix(path, "/eapi/book/"), "/")[0]
			reply(w, map[string]any{"success": 1, "file": map[string]any{"downloadLink": server.URL + "/files/" + id}})
		case strings.HasPrefix(path, "/eapi/book/"):
			id := strings.Split(strings.TrimPrefix(path, "/eapi/book/"), "/")[0]
			b, ok := demoFind(id)
			if !ok {
				reply(w, map[string]any{"success": 0, "error": "Requested book not found"})
				return
			}
			book := b.json()
			book["description"] = "Fixture data for the zlib demo."
			reply(w, map[string]any{"success": 1, "book": book})
		case strings.HasPrefix(path, "/files/"):
			b, ok := demoFind(strings.TrimPrefix(path, "/files/"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			serveDemoFile(w, b)
		default:
			reply(w, map[string]any{"success": 0, "error": "not in the demo"})
		}
	}))
	return server
}

// serveDemoFile streams a file of the advertised size over about two
// seconds, so the progress bar has something to show.
func serveDemoFile(w http.ResponseWriter, b demoBook) {
	mb, _ := strconv.ParseFloat(strings.Fields(b.size)[0], 64)
	size := int(mb * (1 << 20))
	name := strings.ReplaceAll(b.title, ":", "") + " (" + b.author + ")." + b.ext
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Content-Length", strconv.Itoa(size))
	fast := os.Getenv("ZLIB_DEMO_FAST") == "1"
	const steps = 40
	chunk := make([]byte, size/steps+1)
	for written := 0; written < size; written += len(chunk) {
		n := min(len(chunk), size-written)
		if _, err := w.Write(chunk[:n]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		if !fast {
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestDemo runs the real CLI against fixture data, for recording the README
// GIFs with VHS (docs/*.tape). Nothing in it is a real account, session,
// book list or path; see docs/search.tape for how it is driven.
//
//	go test -c ./internal/cli -o zlib-demo
//	ZLIB_DEMO=1 ZLIB_DEMO_HOME=/home/you ZLIB_DEMO_DIR=/home/books ZLIB_DEMO_ARGS="tui" ./zlib-demo -test.run '^TestDemo$'
func TestDemo(t *testing.T) {
	if os.Getenv("ZLIB_DEMO") != "1" {
		t.Skip("demo recorder; set ZLIB_DEMO=1")
	}
	home := os.Getenv("ZLIB_DEMO_HOME")
	if home == "" {
		home = t.TempDir()
	}
	// ZLIB_DEMO_DIR picks the download folder shown in the recording.
	books := os.Getenv("ZLIB_DEMO_DIR")
	if books == "" {
		books = filepath.Join(home, "books")
	}
	if err := os.MkdirAll(books, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(downloadDirEnvVar, books)
	t.Setenv(zlib.EnvDomain, "")
	t.Setenv(zlib.EnvProxy, "")
	cookies := map[string]string{"remix_userid": "1", "remix_userkey": "demo"}
	if err := config.SaveSession(config.Session{Cookies: cookies, Domain: "https://z-lib.gd", Mode: string(zlib.ClientModeEAPI)}); err != nil {
		t.Fatal(err)
	}

	server := newDemoServer()
	defer server.Close()
	previous := loadClient
	loadClient = func() (*zlib.Client, error) {
		c := zlib.NewClient(zlib.WithDomain(server.URL))
		c.SetCookies(cookies)
		return c, nil
	}
	defer func() { loadClient = previous }()
	prevProbe := probeAvailableDomains
	probeAvailableDomains = func(_ context.Context, _ []string, _ string) []zlib.DomainCheck {
		return []zlib.DomainCheck{{
			Domain:     "https://z-lib.gd",
			Status:     zlib.DomainStatusHealthy,
			HTTPStatus: http.StatusOK,
			FinalURL:   "https://z-lib.gd/eapi/info/domains",
		}}
	}
	defer func() { probeAvailableDomains = prevProbe }()

	resolveTheme()
	markUsageErrors(rootCmd)
	rootCmd.SetArgs(strings.Fields(os.Getenv("ZLIB_DEMO_ARGS")))
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, formatCLIError(err))
	}
	server.Close()
	// Exit like the real binary: returning would print the test runner's
	// "PASS" into the recording.
	os.Exit(exitCode(err))
}
