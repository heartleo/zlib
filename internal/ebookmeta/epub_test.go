package ebookmeta

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const opf2 = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>Old Title</dc:title>
    <dc:creator opf:role="aut" opf:file-as="Doe, Jane">Jane Doe</dc:creator>
    <dc:creator opf:role="edt">Ed Itor</dc:creator>
    <dc:language>en</dc:language>
    <dc:identifier id="bookid" opf:scheme="UUID">0f1e2d3c-0000-4000-8000-000000000001</dc:identifier>
    <dc:identifier opf:scheme="ISBN">9780000000002</dc:identifier>
    <dc:date opf:event="modification">2020-01-01</dc:date>
    <dc:date opf:event="publication">1999-05-01</dc:date>
    <dc:subject>Fiction</dc:subject>
    <dc:publisher>Example Press</dc:publisher>
    <dc:description>A story.</dc:description>
    <meta name="calibre:series" content="Saga"/>
    <meta name="calibre:series_index" content="2"/>
    <meta name="calibre:rating" content="8"/>
    <meta name="custom:keep" content="me"/>
  </metadata>
  <manifest>
    <item id="ch1" href="Text/ch%201.xhtml" media-type="application/xhtml+xml"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>
`

const opf3 = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:uuid:0f1e2d3c-0000-4000-8000-000000000003</dc:identifier>
    <dc:identifier id="isbn">urn:isbn:9780000000019</dc:identifier>
    <dc:title id="t1">Main Title</dc:title>
    <meta refines="#t1" property="title-type">main</meta>
    <meta refines="#t1" property="file-as">Main Title, The</meta>
    <dc:title id="t2">A Subtitle</dc:title>
    <meta refines="#t2" property="title-type">subtitle</meta>
    <dc:creator id="c1">Ann Author</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c1" property="file-as">Author, Ann</meta>
    <dc:creator id="c2">Ill Ustrator</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">ill</meta>
    <dc:language>fr</dc:language>
    <dc:date>2011-02-03</dc:date>
    <meta property="dcterms:modified">2020-01-01T00:00:00Z</meta>
    <meta property="belongs-to-collection" id="coll">Cycle</meta>
    <meta refines="#coll" property="collection-type">series</meta>
    <meta refines="#coll" property="group-position">3.5</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="img" href="images/front.png" media-type="image/png" properties="cover-image"/>
  </manifest>
  <spine><itemref idref="nav"/></spine>
</package>
`

const container = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

var (
	pngData  = []byte("\x89PNG\r\n\x1a\nfake-png-body")
	jpegData = []byte("\xff\xd8\xff\xe0fake-jpeg-body")
)

// buildEPUB writes a minimal EPUB. files maps entry names to contents and
// is written after the required entries, in sorted order.
func buildEPUB(t testing.TB, opf string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeMimetype(zw); err != nil {
		t.Fatal(err)
	}
	add := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add("META-INF/container.xml", []byte(container))
	add("OEBPS/content.opf", []byte(opf))
	add("OEBPS/Text/ch 1.xhtml", []byte("<html><body><p>Hello</p></body></html>"))
	for _, name := range sortedKeys(files) {
		add(name, files[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sortedKeys(m map[string][]byte) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func zipEntries(t *testing.T, data []byte) map[string]*zip.File {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*zip.File{}
	for _, f := range zr.File {
		out[f.Name] = f
	}
	return out
}

func rawEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	r, err := f.OpenRaw()
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func entryText(t *testing.T, f *zip.File) string {
	t.Helper()
	b, err := readZipFile(f, maxPartSize)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadEPUB2(t *testing.T) {
	p := writeTemp(t, "b.epub", buildEPUB(t, opf2, nil))
	m, format, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatEPUB {
		t.Fatalf("format = %q", format)
	}
	want := &Metadata{
		Title:       "Old Title",
		Authors:     []string{"Jane Doe"},
		AuthorSort:  "Doe, Jane",
		Publisher:   "Example Press",
		Languages:   []string{"en"},
		Tags:        []string{"Fiction"},
		Series:      "Saga",
		SeriesIndex: 2,
		Identifiers: []Identifier{{"isbn", "9780000000002"}, {"uuid", "0f1e2d3c-0000-4000-8000-000000000001"}},
		Published:   "1999-05-01",
		Description: "A story.",
		Rating:      4,
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %+v\nwant %+v", m, want)
	}
}

func TestReadEPUB3(t *testing.T) {
	p := writeTemp(t, "b.epub", buildEPUB(t, opf3, map[string][]byte{"OEBPS/images/front.png": pngData}))
	m, _, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := &Metadata{
		Title:       "Main Title",
		TitleSort:   "Main Title, The",
		Authors:     []string{"Ann Author"},
		AuthorSort:  "Author, Ann",
		Languages:   []string{"fr"},
		Series:      "Cycle",
		SeriesIndex: 3.5,
		Identifiers: []Identifier{{"isbn", "9780000000019"}, {"uuid", "0f1e2d3c-0000-4000-8000-000000000003"}},
		Published:   "2011-02-03",
		Cover:       "image/png",
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %+v\nwant %+v", m, want)
	}
	cov, err := CoverFile(p)
	if err != nil || !bytes.Equal(cov.Data, pngData) || cov.MediaType != "image/png" {
		t.Fatalf("cover = %+v, %v", cov, err)
	}
}

func fullChanges() *Changes {
	return &Changes{
		Title:       new("New <Title> & More"),
		TitleSort:   new("New Title"),
		Authors:     new([]string{"Alice Ash", "Bob Birch"}),
		AuthorSort:  new("Ash, Alice"),
		Publisher:   new("Other House"),
		Languages:   new([]string{"de", "en"}),
		Tags:        new([]string{"One", "Two"}),
		Series:      new("New Saga"),
		SeriesIndex: new(7.0),
		Identifiers: map[string]string{"isbn": "9781111111113", "asin": "B000000000"},
		Published:   new("2024-03-04"),
		Description: new("Line one.\nLine two."),
		Rating:      new(3.5),
		Cover:       &Cover{Data: jpegData},
	}
}

func TestWriteEPUBRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opf   string
		files map[string][]byte
		uuid  string
	}{
		{"epub2", opf2, nil, "0f1e2d3c-0000-4000-8000-000000000001"},
		{"epub3", opf3, map[string][]byte{"OEBPS/images/front.png": pngData}, "0f1e2d3c-0000-4000-8000-000000000003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := buildEPUB(t, tc.opf, tc.files)
			p := writeTemp(t, "b.epub", orig)
			if err := WriteFile(p, fullChanges()); err != nil {
				t.Fatal(err)
			}
			m, _, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			want := &Metadata{
				Title:       "New <Title> & More",
				TitleSort:   "New Title",
				Authors:     []string{"Alice Ash", "Bob Birch"},
				AuthorSort:  "Ash, Alice",
				Publisher:   "Other House",
				Languages:   []string{"de", "en"},
				Tags:        []string{"One", "Two"},
				Series:      "New Saga",
				SeriesIndex: 7,
				Identifiers: []Identifier{{"asin", "B000000000"}, {"isbn", "9781111111113"}, {"uuid", tc.uuid}},
				Published:   "2024-03-04",
				Description: "Line one.\nLine two.",
				Rating:      3.5,
				Cover:       "image/jpeg",
			}
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("got  %+v\nwant %+v", m, want)
			}
			cov, err := CoverFile(p)
			if err != nil || !bytes.Equal(cov.Data, jpegData) {
				t.Fatalf("cover = %v, %v", cov, err)
			}

			got, _ := os.ReadFile(p)
			assertMimetypeFirst(t, got)
			before, after := zipEntries(t, orig), zipEntries(t, got)
			for _, name := range []string{"META-INF/container.xml", "OEBPS/Text/ch 1.xhtml"} {
				if !bytes.Equal(rawEntry(t, before[name]), rawEntry(t, after[name])) {
					t.Errorf("%s changed", name)
				}
			}
			newOPF := entryText(t, after["OEBPS/content.opf"])
			if err := checkWellFormed(newOPF); err != nil {
				t.Fatalf("OPF not well-formed: %v\n%s", err, newOPF)
			}
			oldOPF := tc.opf
			// The manifest and spine are untouched apart from a new cover item.
			if spine := oldOPF[strings.Index(oldOPF, "<spine"):]; !strings.Contains(newOPF, spine) {
				t.Errorf("spine changed:\n%s", newOPF)
			}
			if !strings.Contains(newOPF, "<dc:title") || strings.Contains(newOPF, "xmlns:dc=\"http://purl.org/dc/elements/1.1/\" ") && strings.Count(newOPF, "xmlns:dc=") > 1 {
				t.Errorf("dc prefix not reused:\n%s", newOPF)
			}
			if tc.name == "epub2" {
				for _, keep := range []string{`<meta name="custom:keep" content="me"/>`, `opf:event="modification"`, `opf:role="edt"`} {
					if !strings.Contains(newOPF, keep) {
						t.Errorf("lost %s:\n%s", keep, newOPF)
					}
				}
			} else {
				for _, keep := range []string{"Ill Ustrator", "dcterms:modified", `href="images/front.png" media-type="image/jpeg"`} {
					if !strings.Contains(newOPF, keep) {
						t.Errorf("lost %s:\n%s", keep, newOPF)
					}
				}
				if strings.Contains(newOPF, "A Subtitle") || strings.Contains(newOPF, `refines="#t2"`) {
					t.Errorf("old titles or their refinements survived:\n%s", newOPF)
				}
			}
		})
	}
}

func checkWellFormed(s string) error {
	_, err := parseOPF([]byte(s))
	return err
}

// assertMimetypeFirst checks the OCF rule that the archive starts with an
// uncompressed "mimetype" entry with no extra field.
func assertMimetypeFirst(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 58 || string(data[30:38]) != "mimetype" || string(data[38:58]) != epubMimetype {
		t.Fatalf("mimetype is not the first stored entry: %q", data[:min(len(data), 64)])
	}
	if extra := int(data[28]) | int(data[29])<<8; extra != 0 {
		t.Fatalf("mimetype has a %d-byte extra field", extra)
	}
}

func TestWriteEPUBClearsAndKeeps(t *testing.T) {
	p := writeTemp(t, "b.epub", buildEPUB(t, opf2, nil))
	err := WriteFile(p, &Changes{
		Series:      new(""),
		Tags:        new([]string(nil)),
		Rating:      new(0.0),
		Identifiers: map[string]string{"isbn": "", "uuid": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Series != "" || m.SeriesIndex != 0 || m.Tags != nil || m.Rating != 0 || m.Identifier("isbn") != "" {
		t.Fatalf("fields not cleared: %+v", m)
	}
	// The unique identifier cannot be removed.
	if m.Identifier("uuid") == "" || m.Title != "Old Title" || m.Publisher != "Example Press" {
		t.Fatalf("untouched fields changed: %+v", m)
	}
}

func TestWriteEPUBAddsCoverToBookWithout(t *testing.T) {
	p := writeTemp(t, "b.epub", buildEPUB(t, opf2, nil))
	if err := WriteFile(p, &Changes{Cover: &Cover{Data: pngData}}); err != nil {
		t.Fatal(err)
	}
	cov, err := CoverFile(p)
	if err != nil || !bytes.Equal(cov.Data, pngData) || cov.MediaType != "image/png" {
		t.Fatalf("cover = %v, %v", cov, err)
	}
	got, _ := os.ReadFile(p)
	opf := entryText(t, zipEntries(t, got)["OEBPS/content.opf"])
	if !strings.Contains(opf, `<item id="cover-image" href="cover-image.png" media-type="image/png"/>`) ||
		!strings.Contains(opf, `<meta name="cover" content="cover-image"/>`) {
		t.Fatalf("cover not registered:\n%s", opf)
	}
}

func TestWriteEPUBRejectsBadCover(t *testing.T) {
	orig := buildEPUB(t, opf2, nil)
	p := writeTemp(t, "b.epub", orig)
	if err := WriteFile(p, &Changes{Cover: &Cover{Data: []byte("not an image")}}); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Fatal("file changed after a failed write")
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".zlib-meta-*")); len(matches) > 0 {
		t.Fatalf("temp files left: %v", matches)
	}
}

func TestWriteEPUBRefusesEncryptedCover(t *testing.T) {
	enc := `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
<enc:EncryptedData><enc:CipherData><enc:CipherReference URI="OEBPS/images/front.png"/></enc:CipherData></enc:EncryptedData></encryption>`
	p := writeTemp(t, "b.epub", buildEPUB(t, opf3, map[string][]byte{
		"OEBPS/images/front.png":  pngData,
		"META-INF/encryption.xml": []byte(enc),
	}))
	if err := WriteFile(p, &Changes{Cover: &Cover{Data: jpegData}}); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("err = %v", err)
	}
	// Text metadata can still be changed.
	if err := WriteFile(p, &Changes{Title: new("Fine")}); err != nil {
		t.Fatal(err)
	}
}

func TestReadCleansControlCharacters(t *testing.T) {
	opf := strings.Replace(opf2, "Old Title", "Evil\u009b31m‮Title", 1)
	p := writeTemp(t, "b.epub", buildEPUB(t, opf, nil))
	m, _, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Evil31mTitle" {
		t.Fatalf("title = %q", m.Title)
	}
}

func TestOPFSidecarRoundTrip(t *testing.T) {
	m := &Metadata{}
	fullChanges().Apply(m)
	data, err := MarshalOPF(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalOPF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("got  %+v\nwant %+v\n%s", got, m, data)
	}
}

func TestDetectRejectsUnknown(t *testing.T) {
	p := writeTemp(t, "x.epub", []byte("just text"))
	if _, _, err := ReadFile(p); err != ErrUnsupportedFormat {
		t.Fatalf("err = %v", err)
	}
}

func FuzzParseOPF(f *testing.F) {
	f.Add([]byte(opf2))
	f.Add([]byte(opf3))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := parseOPF(data)
		if err != nil {
			return
		}
		d.metadata()
		_, _ = d.rewrite(fullChanges(), nil)
	})
}
