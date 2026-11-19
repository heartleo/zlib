package ebookmeta

import (
	"bytes"
	"encoding/base64"
	"os"
	"reflect"
	"strings"
	"testing"
)

const fb2Body = `<body><section><p>Once upon a time.</p></section></body>`

func fb2Doc(encoding, titleInfoExtra, binaries string) string {
	return `<?xml version="1.0" encoding="` + encoding + `"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
  <description>
    <title-info>
      <genre>sf_fantasy</genre>
      <author><first-name>Ivan</first-name><middle-name>I.</middle-name><last-name>Petrov</last-name></author>
      <author><nickname>Anon</nickname></author>
      <book-title>Старая книга</book-title>
      <annotation><p>First.</p><p>Second.</p></annotation>
      <date value="2001-02-03">2001</date>` + titleInfoExtra + `
      <lang>ru</lang>
      <sequence name="Cycle" number="4"/>
    </title-info>
    <document-info><id>doc-1</id></document-info>
    <publish-info><publisher>Izdat</publisher><year>2002</year><isbn>978-0-00-000000-2</isbn></publish-info>
  </description>
  ` + fb2Body + binaries + `
</FictionBook>
`
}

func TestReadFB2(t *testing.T) {
	cover := `
      <coverpage><image l:href="#c.png"/></coverpage>`
	bin := `
  <binary id="c.png" content-type="image/png">` + base64.StdEncoding.EncodeToString(pngData) + `</binary>`
	for _, enc := range []string{"utf-8", "windows-1251"} {
		t.Run(enc, func(t *testing.T) {
			doc := []byte(fb2Doc(enc, cover, bin))
			if enc != "utf-8" {
				doc = cp1251.encode(string(doc), func(rune) string { t.Fatal("unencodable"); return "" })
			}
			p := writeTemp(t, "b.fb2", doc)
			m, format, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			want := &Metadata{
				Title:       "Старая книга",
				Authors:     []string{"Ivan I. Petrov", "Anon"},
				Publisher:   "Izdat",
				Languages:   []string{"ru"},
				Tags:        []string{"sf_fantasy"},
				Series:      "Cycle",
				SeriesIndex: 4,
				Identifiers: []Identifier{{"isbn", "978-0-00-000000-2"}},
				Published:   "2001-02-03",
				Description: "First.\nSecond.",
				Cover:       "image/png",
			}
			if format != FormatFB2 || !reflect.DeepEqual(m, want) {
				t.Fatalf("format %s\ngot  %+v\nwant %+v", format, m, want)
			}
			cov, err := CoverFile(p)
			if err != nil || !bytes.Equal(cov.Data, pngData) {
				t.Fatalf("cover = %v, %v", cov, err)
			}
		})
	}
}

func TestWriteFB2RoundTrip(t *testing.T) {
	for _, enc := range []string{"utf-8", "windows-1251"} {
		t.Run(enc, func(t *testing.T) {
			doc := []byte(fb2Doc(enc, "", ""))
			if enc != "utf-8" {
				doc = cp1251.encode(string(doc), xmlCharRef)
			}
			p := writeTemp(t, "b.fb2", doc)
			c := fullChanges()
			skipped := DropUnsupported(FormatFB2, c)
			if want := []string{"title_sort", "author_sort", "rating", "identifier:asin"}; !reflect.DeepEqual(skipped, want) {
				t.Fatalf("skipped = %v, want %v", skipped, want)
			}
			if err := WriteFile(p, c); err != nil {
				t.Fatal(err)
			}
			m, _, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			want := &Metadata{
				Title:       "New <Title> & More",
				Authors:     []string{"Alice Ash", "Bob Birch"},
				Publisher:   "Other House",
				Languages:   []string{"de"},
				Tags:        []string{"One", "Two"},
				Series:      "New Saga",
				SeriesIndex: 7,
				Identifiers: []Identifier{{"isbn", "9781111111113"}},
				Published:   "2024-03-04",
				Description: "Line one.\nLine two.",
				Cover:       "image/jpeg",
			}
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("got  %+v\nwant %+v", m, want)
			}
			cov, err := CoverFile(p)
			if err != nil || !bytes.Equal(cov.Data, jpegData) {
				t.Fatalf("cover = %v, %v", cov, err)
			}
			out, _ := os.ReadFile(p)
			s := string(out)
			if !strings.Contains(s, fb2Body) || !strings.Contains(s, "<document-info><id>doc-1</id></document-info>") {
				t.Fatalf("untouched parts changed:\n%s", s)
			}
			if enc != "utf-8" && !strings.Contains(s, `encoding="utf-8"`) {
				t.Fatalf("declaration not converted:\n%s", s[:80])
			}
			// Schema order inside title-info is kept.
			order := []string{"<genre>", "<author>", "<book-title>", "<annotation>", "<date ", "<coverpage>", "<lang>", "<sequence "}
			last := -1
			for _, tag := range order {
				i := strings.Index(s, tag)
				if i < last {
					t.Fatalf("%s out of order:\n%s", tag, s)
				}
				last = i
			}
		})
	}
}

func TestWriteFB2ReplacesCoverAndCreatesPublishInfo(t *testing.T) {
	doc := strings.Replace(fb2Doc("utf-8", `
      <coverpage><image l:href="#c.png"/></coverpage>`, `
  <binary id="c.png" content-type="image/png">`+base64.StdEncoding.EncodeToString(pngData)+`</binary>`),
		"<publish-info><publisher>Izdat</publisher><year>2002</year><isbn>978-0-00-000000-2</isbn></publish-info>", "", 1)
	p := writeTemp(t, "b.fb2", []byte(doc))
	if err := WriteFile(p, &Changes{Cover: &Cover{Data: jpegData}, Publisher: new("New Pub")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if n := strings.Count(string(out), "<binary "); n != 1 {
		t.Fatalf("%d binaries:\n%s", n, out)
	}
	m, _, err := ReadFile(p)
	if err != nil || m.Cover != "image/jpeg" || m.Publisher != "New Pub" {
		t.Fatalf("m = %+v, %v\n%s", m, err, out)
	}
}

func FuzzReadFB2(f *testing.F) {
	f.Add([]byte(fb2Doc("utf-8", "", "")))
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		b, err := openFB2(r, int64(len(data)))
		if err != nil {
			return
		}
		b.metadata()
		var out bytes.Buffer
		_ = fb2Handler{}.write(r, int64(len(data)), &out, fullChanges())
	})
}
