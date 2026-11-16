package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
)

const embedTestISBN = "9787536692930"

// epubBytes builds a small EPUB 2 titled title, with an optional extra
// content page.
func epubBytes(t *testing.T, title, copyrightPage string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`, zip.Deflate)
	add("content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>`+title+`</dc:title>
    <dc:identifier id="id" opf:scheme="UUID">0f1e2d3c-0000-4000-8000-000000000001</dc:identifier>
    <dc:language>zh</dc:language>
  </metadata>
  <manifest><item id="t" href="t.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="t"/></spine>
</package>`, zip.Deflate)
	add("t.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body>`+copyrightPage+`</body></html>`, zip.Deflate)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeEPUB(t *testing.T, title, page string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(p, epubBytes(t, title, page), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEmbedDownloadISBN(t *testing.T) {
	record := zlib.Book{Name: "三体", ISBN13: embedTestISBN}

	t.Run("agreeing title is written as the ISBN, once", func(t *testing.T) {
		p := writeEPUB(t, "三体", "<p>x</p>")
		got, err := embedDownloadISBN(p, record)
		if err != nil || got == nil || got.Scheme != "isbn" || got.Value != embedTestISBN {
			t.Fatalf("got %+v, %v", got, err)
		}
		m, _, err := ebookmeta.ReadFile(p)
		if err != nil || m.Identifier("isbn") != embedTestISBN || m.Title != "三体" {
			t.Fatalf("file = %+v, %v", m, err)
		}
		// The file's own UUID survives, and a second download changes nothing.
		if m.Identifier("uuid") == "" {
			t.Error("the unique identifier was lost")
		}
		if again, err := embedDownloadISBN(p, record); err != nil || again != nil {
			t.Errorf("second run = %+v, %v", again, err)
		}
	})

	t.Run("disagreeing title is kept as a hint only", func(t *testing.T) {
		p := writeEPUB(t, "红楼梦", "<p>x</p>")
		got, err := embedDownloadISBN(p, record)
		if err != nil || got == nil || got.Scheme != metasource.SchemeZlibISBN {
			t.Fatalf("got %+v, %v", got, err)
		}
		m, _, _ := ebookmeta.ReadFile(p)
		if m.Identifier("isbn") != "" || m.Identifier(metasource.SchemeZlibISBN) != embedTestISBN {
			t.Fatalf("identifiers = %+v", m.Identifiers)
		}
		// A later lookup searches the hint without trusting it.
		if q := metasource.QueryFromMetadata(m); len(q.ISBNs) != 1 || q.ISBNs[0] != embedTestISBN {
			t.Errorf("query = %+v", q)
		}
	})

	t.Run("a record listing several editions only gives a hint", func(t *testing.T) {
		p := writeEPUB(t, "三体", "<p>x</p>")
		book := zlib.Book{Name: "三体", ISBN13: embedTestISBN, ISBN10: "7536692935", ISBN: embedTestISBN + ",7536692935,9780140328721"}
		got, err := embedDownloadISBN(p, book)
		if err != nil || got == nil || got.Scheme != metasource.SchemeZlibISBN {
			t.Fatalf("got %+v, %v", got, err)
		}
		if m, _, _ := ebookmeta.ReadFile(p); m.Identifier("isbn") != "" {
			t.Errorf("an ambiguous ISBN became the file's own: %+v", m.Identifiers)
		}
	})

	t.Run("nothing to record", func(t *testing.T) {
		p := writeEPUB(t, "三体", "<p>x</p>")
		before, _ := os.ReadFile(p)
		for name, book := range map[string]zlib.Book{
			"no ISBN":      {Name: "三体"},
			"invalid ISBN": {Name: "三体", ISBN13: "9787536692931", ISBN10: "123"},
		} {
			if got, err := embedDownloadISBN(p, book); err != nil || got != nil {
				t.Errorf("%s: got %+v, %v", name, got, err)
			}
		}
		if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
			t.Error("the file changed although there was nothing to record")
		}
	})

	t.Run("a format that cannot be read is left alone", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "book.txt")
		os.WriteFile(p, []byte("plain text"), 0o644)
		if got, err := embedDownloadISBN(p, record); err != nil || got != nil {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

// fakeEAPI serves one book's record and file, as the plain download flow
// requests them.
func fakeEAPI(t *testing.T, recordTitle string, file []byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eapi/book/42/abc":
			fmt.Fprintf(w, `{"success":1,"book":{"id":42,"hash":"abc","title":%q,"author":"刘慈欣","extension":"epub","isbn13":%q}}`, recordTitle, embedTestISBN)
		case "/eapi/book/42/abc/file":
			fmt.Fprintf(w, `{"success":1,"file":{"downloadLink":%q}}`, srv.URL+"/files/book")
		case "/files/book":
			w.Header().Set("Content-Disposition", `attachment; filename="book.epub"`)
			w.Header().Set("Content-Length", fmt.Sprint(len(file)))
			w.Write(file)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runEmbedDownload(t *testing.T, srv *httptest.Server, noEmbed bool) (downloadResult, string, string) {
	t.Helper()
	isolateConfigHome(t)
	client := zlib.NewClient(zlib.WithDomain(srv.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})
	dir := t.TempDir()
	cmd := downloadTestCommand(t, true)
	addDeliveryFlags(cmd)
	if noEmbed {
		if err := cmd.Flags().Set("no-embed", "true"); err != nil {
			t.Fatal(err)
		}
	}
	m := newDownloadModel("42:abc", dir, client)
	var runErr error
	stdout, stderr := captureOutput(t, func() { runErr = runDownload(m, deliveries{}, cmd) })
	if runErr != nil {
		t.Fatalf("runDownload: %v\n%s", runErr, stderr)
	}
	var result downloadResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return result, stderr, dir
}

func TestPlainDownloadRecordsTheISBN(t *testing.T) {
	srv := fakeEAPI(t, "三体", epubBytes(t, "三体", "<p>x</p>"))
	result, stderr, _ := runEmbedDownload(t, srv, false)
	if result.ISBN == nil || result.ISBN.Scheme != "isbn" || result.ISBN.Value != embedTestISBN {
		t.Fatalf("result.ISBN = %+v", result.ISBN)
	}
	if !strings.Contains(stderr, "Recorded ISBN "+embedTestISBN) {
		t.Errorf("stderr = %q", stderr)
	}
	m, _, err := ebookmeta.ReadFile(result.Path)
	if err != nil || m.Identifier("isbn") != embedTestISBN {
		t.Fatalf("saved file = %+v, %v", m, err)
	}
}

func TestPlainDownloadNoEmbedLeavesTheFileAlone(t *testing.T) {
	file := epubBytes(t, "三体", "<p>x</p>")
	srv := fakeEAPI(t, "三体", file)
	result, stderr, _ := runEmbedDownload(t, srv, true)
	if result.ISBN != nil || strings.Contains(stderr, "Recorded ISBN") {
		t.Fatalf("recorded despite --no-embed: %+v, %q", result.ISBN, stderr)
	}
	if saved, _ := os.ReadFile(result.Path); !bytes.Equal(saved, file) {
		t.Error("the saved file differs from the download")
	}
}

func TestPlainDownloadMismatchedTitleRecordsAHint(t *testing.T) {
	srv := fakeEAPI(t, "三体", epubBytes(t, "红楼梦", "<p>x</p>"))
	result, _, _ := runEmbedDownload(t, srv, false)
	if result.ISBN == nil || result.ISBN.Scheme != metasource.SchemeZlibISBN {
		t.Fatalf("result.ISBN = %+v", result.ISBN)
	}
}

func TestScannedISBNsFindsTheCopyrightPageISBN(t *testing.T) {
	p := writeEPUB(t, "三体", "<p>书号：ISBN 978-7-5366-9293-0</p><p>定价：38.00元</p>")
	cur, _, err := ebookmeta.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := scannedISBNs(p, cur); len(got) != 1 || got[0] != embedTestISBN {
		t.Fatalf("scanned = %v", got)
	}
	// A file that carries a valid ISBN of its own is not scanned.
	cur.SetIdentifier("isbn", "9780140328721")
	if got := scannedISBNs(p, cur); got != nil {
		t.Errorf("a file with its own ISBN was scanned: %v", got)
	}
	// A stray number that fails the check digit is not an ISBN.
	bad := writeEPUB(t, "三体", "<p>ISBN 978-7-5366-9293-1</p>")
	cur, _, _ = ebookmeta.ReadFile(bad)
	if got := scannedISBNs(bad, cur); len(got) != 0 {
		t.Errorf("scanned = %v", got)
	}
	// Through the extraction the scanned ISBN is a hint, labelled by where it was.
	cur, _, _ = ebookmeta.ReadFile(p)
	x := metasource.Extract("book.epub", cur, scannedISBNs(p, cur), metasource.Hint{})
	if len(x.Query.ISBNs) != 1 || x.ISBNFrom[0] != "copyright page" || len(x.Query.Unconfirmed) != 1 {
		t.Errorf("extraction = %+v from %v", x.Query, x.ISBNFrom)
	}
}
