package ebookmeta

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Office Open XML documents keep their metadata in the core properties
// part (ECMA-376 Part 2, 11), usually docProps/core.xml.

const (
	nsCoreProps  = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
	relCoreProps = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	ctCoreProps  = "application/vnd.openxmlformats-package.core-properties+xml"
	defaultCore  = "docProps/core.xml"
)

type docxHandler struct{}

func (docxHandler) supports(field string) bool {
	switch field {
	case "title", "authors", "tags", "description", "languages":
		return true
	}
	return false
}

func (docxHandler) cover(io.ReaderAt, int64) (*Cover, error) { return nil, ErrNoCover }

type docxBook struct {
	zr       *zip.Reader
	corePath string // "" when the package has no core properties part
	core     *xdoc
}

func openDOCX(src io.ReaderAt, size int64) (*docxBook, error) {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	b := &docxBook{zr: zr}
	if rels := zipFile(zr, "_rels/.rels"); rels != nil {
		data, err := readZipFile(rels, 4<<20)
		if err != nil {
			return nil, err
		}
		b.corePath = relationshipTarget(data, relCoreProps)
	}
	if b.corePath == "" && zipFile(zr, defaultCore) != nil {
		b.corePath = defaultCore
	}
	if b.corePath == "" {
		return b, nil
	}
	f := zipFile(zr, b.corePath)
	if f == nil {
		b.corePath = ""
		return b, nil
	}
	data, err := readZipFile(f, 4<<20)
	if err != nil {
		return nil, err
	}
	// Record the root and its children; keep text for the children only.
	rootOrChild := func(p string) bool { return strings.Count(p, "/") <= 1 }
	child := func(p string) bool { return strings.Count(p, "/") == 1 }
	if b.core, err = indexXML(data, rootOrChild, child); err != nil {
		return nil, err
	}
	return b, nil
}

func zipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// relationshipTarget returns the package-relative target of the first
// relationship of type typ.
func relationshipTarget(data []byte, typ string) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		t, ok := tok.(xml.StartElement)
		if !ok || t.Name.Local != "Relationship" || attrValue(t, "Type") != typ {
			continue
		}
		if attrValue(t, "TargetMode") == "External" {
			return ""
		}
		p, err := cleanZipPath(attrValue(t, "Target"))
		if err != nil {
			return ""
		}
		return p
	}
}

func (b *docxBook) metadata() *Metadata {
	m := &Metadata{}
	if b.core == nil {
		return m
	}
	for _, n := range b.core.nodes {
		if !strings.Contains(n.path, "/") {
			continue
		}
		switch n.local {
		case "title":
			m.Title = n.text
		case "creator":
			m.Authors = splitList(n.text, ";")
		case "keywords":
			m.Tags = splitList(strings.ReplaceAll(n.text, ";", ","), ",")
		case "description":
			m.Description = n.text
		case "language":
			m.Languages = splitList(n.text, ",")
		}
	}
	return m
}

func splitList(s, sep string) []string {
	var out []string
	for p := range strings.SplitSeq(s, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (docxHandler) read(src io.ReaderAt, size int64) (*Metadata, error) {
	b, err := openDOCX(src, size)
	if err != nil {
		return nil, err
	}
	return b.metadata(), nil
}

const emptyCoreProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"></cp:coreProperties>`

func (docxHandler) write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error {
	b, err := openDOCX(src, size)
	if err != nil {
		return err
	}
	replace := map[string][]byte{}
	if b.core == nil {
		// Add a core properties part and register it in the package.
		b.corePath = defaultCore
		if b.core, err = indexXML([]byte(emptyCoreProps), func(string) bool { return true }, func(string) bool { return true }); err != nil {
			return err
		}
		if err := b.registerCore(replace); err != nil {
			return err
		}
	}
	next := b.metadata()
	c.Apply(next)

	d := b.core
	root := d.nodes[0]
	dc, dcDecl := d.bindings[nsDC], ""
	if dc == "" {
		dc, dcDecl = "dc", ` xmlns:dc="`+nsDC+`"`
	}
	cp, cpDecl := d.bindings[nsCoreProps], ""
	if cp == "" {
		cp, cpDecl = "cp", ` xmlns:cp="`+nsCoreProps+`"`
	}
	var edits []edit
	var add []string
	set := func(changed bool, local, prefix, decl, value string) {
		if !changed {
			return
		}
		for _, n := range d.nodes[1:] {
			if n.local == local {
				edits = append(edits, d.removal(n))
			}
		}
		if value != "" {
			add = append(add, "<"+prefix+":"+local+decl+">"+escText(value)+"</"+prefix+":"+local+">")
		}
	}
	set(c.Title != nil, "title", dc, dcDecl, next.Title)
	set(c.Authors != nil, "creator", dc, dcDecl, strings.Join(next.Authors, "; "))
	set(c.Tags != nil, "keywords", cp, cpDecl, strings.Join(next.Tags, ", "))
	set(c.Description != nil, "description", dc, dcDecl, next.Description)
	set(c.Languages != nil, "language", dc, dcDecl, strings.Join(next.Languages, ","))
	if len(add) > 0 {
		if root.openEnd == root.end {
			// <cp:coreProperties .../>: expand into a start and end tag.
			tag := strings.TrimSpace(string(d.data[root.start : root.end-2]))
			name := d.prefix + root.local
			edits = append(edits, edit{start: root.start, end: root.end,
				text: tag + ">" + strings.Join(add, "") + "</" + name + ">"})
		} else {
			at := int64(bytes.LastIndex(d.data[:root.end], []byte("</")))
			edits = append(edits, edit{start: at, end: at, text: strings.Join(add, "")})
		}
	}
	out, err := applyEdits(d.data, edits)
	if err != nil {
		return err
	}
	replace[b.corePath] = out

	zw := zip.NewWriter(dst)
	if err := copyZip(zw, b.zr.File, replace, ""); err != nil {
		return err
	}
	return zw.Close()
}

// registerCore adds the relationship and content type for a new
// docProps/core.xml to replace.
func (b *docxBook) registerCore(replace map[string][]byte) error {
	insert := func(name, closing, elem string) error {
		f := zipFile(b.zr, name)
		if f == nil {
			return fmt.Errorf("%w: package has no %s", ErrMalformed, name)
		}
		data, err := readZipFile(f, 4<<20)
		if err != nil {
			return err
		}
		i := bytes.LastIndex(data, []byte(closing))
		if i < 0 {
			return fmt.Errorf("%w: %s has no %s", ErrMalformed, name, closing)
		}
		out := append(append(append([]byte{}, data[:i]...), elem...), data[i:]...)
		replace[name] = out
		return nil
	}
	if err := insert("[Content_Types].xml", "</Types>",
		`<Override PartName="/`+defaultCore+`" ContentType="`+ctCoreProps+`"/>`); err != nil {
		return err
	}
	if zipFile(b.zr, "_rels/.rels") == nil {
		replace["_rels/.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdCore" Type="` + relCoreProps + `" Target="` + defaultCore + `"/></Relationships>`)
		return nil
	}
	return insert("_rels/.rels", "</Relationships>",
		`<Relationship Id="rIdZlibCore" Type="`+relCoreProps+`" Target="`+defaultCore+`"/>`)
}
