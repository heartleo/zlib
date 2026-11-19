package ebookmeta

import (
	"archive/zip"
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

const docxCore = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>Report</dc:title><dc:creator>Ann One; Bob Two</dc:creator><cp:keywords>alpha, beta</cp:keywords><cp:lastModifiedBy>Someone</cp:lastModifiedBy><dcterms:created xsi:type="dcterms:W3CDTF">2020-01-01T00:00:00Z</dcterms:created></cp:coreProperties>`

func buildDOCX(t *testing.T, core string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	types := `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`
	rels := `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`
	if core != "" {
		rels += `<Relationship Id="rId2" Type="` + relCoreProps + `" Target="docProps/core.xml"/>`
	}
	add("[Content_Types].xml", types)
	add("_rels/.rels", rels+`</Relationships>`)
	add("word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`)
	if core != "" {
		add("docProps/core.xml", core)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDOCXReadWrite(t *testing.T) {
	orig := buildDOCX(t, docxCore)
	p := writeTemp(t, "r.docx", orig)
	m, format, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := (&Metadata{Title: "Report", Authors: []string{"Ann One", "Bob Two"}, Tags: []string{"alpha", "beta"}}); format != FormatDOCX || !reflect.DeepEqual(m, want) {
		t.Fatalf("%s %+v", format, m)
	}
	c := fullChanges()
	skipped := DropUnsupported(FormatDOCX, c)
	if len(skipped) == 0 || c.Series != nil || c.Cover != nil || c.Identifiers != nil && len(c.Identifiers) > 0 {
		t.Fatalf("skipped = %v, changes = %+v", skipped, c)
	}
	if err := WriteFile(p, c); err != nil {
		t.Fatal(err)
	}
	m, _, err = ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := &Metadata{
		Title:       "New <Title> & More",
		Authors:     []string{"Alice Ash", "Bob Birch"},
		Languages:   []string{"de", "en"},
		Tags:        []string{"One", "Two"},
		Description: "Line one.\nLine two.",
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %+v\nwant %+v", m, want)
	}
	got, _ := os.ReadFile(p)
	core := entryText(t, zipEntries(t, got)["docProps/core.xml"])
	if !strings.Contains(core, `<cp:lastModifiedBy>Someone</cp:lastModifiedBy>`) || !strings.Contains(core, `xsi:type="dcterms:W3CDTF"`) {
		t.Fatalf("untouched properties lost:\n%s", core)
	}
	before, after := zipEntries(t, orig), zipEntries(t, got)
	if !bytes.Equal(rawEntry(t, before["word/document.xml"]), rawEntry(t, after["word/document.xml"])) {
		t.Fatal("document.xml changed")
	}
}

func TestDOCXAddsCoreProperties(t *testing.T) {
	p := writeTemp(t, "r.docx", buildDOCX(t, ""))
	if err := WriteFile(p, &Changes{Title: new("Fresh")}); err != nil {
		t.Fatal(err)
	}
	m, _, err := ReadFile(p)
	if err != nil || m.Title != "Fresh" {
		t.Fatalf("m = %+v, %v", m, err)
	}
	got, _ := os.ReadFile(p)
	e := zipEntries(t, got)
	if !strings.Contains(entryText(t, e["[Content_Types].xml"]), `PartName="/docProps/core.xml"`) ||
		!strings.Contains(entryText(t, e["_rels/.rels"]), relCoreProps) {
		t.Fatal("core properties part not registered")
	}
}

func TestDOCXSelfClosingCore(t *testing.T) {
	core := `<?xml version="1.0"?><cp:coreProperties xmlns:cp="` + nsCoreProps + `" xmlns:dc="` + nsDC + `"/>`
	p := writeTemp(t, "r.docx", buildDOCX(t, core))
	if err := WriteFile(p, &Changes{Authors: new([]string{"Solo"})}); err != nil {
		t.Fatal(err)
	}
	if m, _, err := ReadFile(p); err != nil || !reflect.DeepEqual(m.Authors, []string{"Solo"}) {
		t.Fatalf("m = %+v, %v", m, err)
	}
}
