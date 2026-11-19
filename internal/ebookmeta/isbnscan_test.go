package ebookmeta

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeZip writes name/content pairs, in order, to a new file and returns it.
func writeZip(t *testing.T, name string, files [][2]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, f := range files {
		w, err := zw.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(f[1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	out.Close()
	return path
}

func page(body string) string {
	return `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><body>` + body + `</body></html>`
}

func TestISBNSnippetsReadsEPUBFrontAndBackMatter(t *testing.T) {
	files := [][2]string{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container/>`},
		{"OEBPS/cover.xhtml", page(`<img src="c.jpg"/>`)},
		// Split across inline tags and entities, as typeset copyright pages are.
		{"OEBPS/copyright.xhtml", page(`<p>书号：ISBN&#160;<span>978</span>-7-5366-9293-0</p><p>定价：38.00元</p>`)},
	}
	// 30 chapters follow the cover and copyright page. The scan reads the
	// first 16 documents and the last 4, so chapters 14 to 26 are skipped.
	for i := range 30 {
		body := `<p>text</p>`
		if i >= 14 && i <= 26 {
			body = `<p>ISBN 111-1-1111-1111-1 in the middle</p>`
		}
		files = append(files, [2]string{"OEBPS/ch" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".xhtml", page(body)})
	}
	files = append(files, [2]string{"OEBPS/colophon.xhtml", page(`<p>ISBN 7-5366-9293-5</p>`)})
	path := writeZip(t, "book.epub", files)

	got, err := ISBNSnippets(path)
	if err != nil {
		t.Fatal(err)
	}
	var joined []string
	for _, s := range got {
		joined = append(joined, strings.TrimSpace(s))
	}
	text := strings.Join(joined, "|")
	if !strings.Contains(text, "978-7-5366-9293-0") || !strings.Contains(text, "7-5366-9293-5") {
		t.Errorf("snippets = %q", joined)
	}
	if strings.Contains(text, "111-1-1111") {
		t.Errorf("scanned the middle of the book: %q", joined)
	}
}

func TestISBNSnippetsFindsABareHyphenatedISBN(t *testing.T) {
	path := writeZip(t, "book.epub", [][2]string{
		{"mimetype", "application/epub+zip"},
		{"OEBPS/c.xhtml", page(`<p>书号 978-7-5366-9293-0</p>`)},
	})
	got, err := ISBNSnippets(path)
	if err != nil || !reflect.DeepEqual(got, []string{"978-7-5366-9293-0"}) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestISBNSnippetsReadsDOCXBody(t *testing.T) {
	path := writeZip(t, "book.docx", [][2]string{
		{"[Content_Types].xml", `<Types/>`},
		{"word/document.xml", `<w:document><w:p><w:r><w:t>ISBN：978-7-5366-9293-0</w:t></w:r></w:p></w:document>`},
	})
	got, err := ISBNSnippets(path)
	if err != nil || len(got) != 1 || !strings.HasPrefix(got[0], "978-7-5366-9293-0") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestISBNSnippetsLeavesOtherFormatsAlone(t *testing.T) {
	fb2 := filepath.Join(t.TempDir(), "book.fb2")
	if err := os.WriteFile(fb2, []byte(`<?xml version="1.0"?><FictionBook><body>ISBN 978-7-5366-9293-0</body></FictionBook>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ISBNSnippets(fb2); err != nil || got != nil {
		t.Errorf("FB2: got %q, %v", got, err)
	}
	txt := filepath.Join(t.TempDir(), "book.txt")
	os.WriteFile(txt, []byte("ISBN 978-7-5366-9293-0"), 0o644)
	if _, err := ISBNSnippets(txt); err == nil {
		t.Error("an unknown format should report ErrUnsupportedFormat")
	}
}
