package ebookmeta

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// FictionBook 2 stores metadata in <description><title-info> and
// <publish-info>; the cover is a <binary> referenced from <coverpage>.

const (
	fb2Root      = "FictionBook"
	fb2Desc      = fb2Root + "/description"
	fb2TitleInfo = fb2Desc + "/title-info"
	fb2Publish   = fb2Desc + "/publish-info"
	fb2DocInfo   = fb2Desc + "/document-info"
	fb2Binary    = fb2Root + "/binary"
	nsXLink      = "http://www.w3.org/1999/xlink"
	maxFB2Size   = 256 << 20
)

var (
	fb2TitleInfoOrder = []string{"genre", "author", "book-title", "annotation", "keywords", "date",
		"coverpage", "lang", "src-lang", "translator", "sequence"}
	fb2PublishOrder = []string{"book-name", "publisher", "city", "year", "isbn", "sequence"}
)

type fb2Handler struct{}

func (fb2Handler) supports(field string) bool {
	switch field {
	case "title_sort", "author_sort", "rating":
		return false
	}
	return true
}

func (fb2Handler) identifierSchemes() []string { return []string{"isbn"} }

type fb2Book struct {
	doc *xdoc
}

func openFB2(src io.ReaderAt, size int64) (*fb2Book, error) {
	if size > maxFB2Size {
		return nil, fmt.Errorf("%w: FB2 file is larger than %d MiB", ErrMalformed, maxFB2Size>>20)
	}
	data := make([]byte, size)
	if _, err := src.ReadAt(data, 0); err != nil && err != io.EOF {
		return nil, err
	}
	b := &fb2Book{}
	record := func(p string) bool {
		return p == fb2Root || p == fb2Desc || strings.HasPrefix(p, fb2Desc+"/") || p == fb2Binary
	}
	keep := func(p string) bool { return p != fb2Root && p != fb2Desc }
	doc, err := indexXML(data, record, keep)
	if err != nil {
		return nil, err
	}
	if doc.first(fb2Root) == nil {
		return nil, fmt.Errorf("%w: not a FictionBook document", ErrMalformed)
	}
	b.doc = doc
	return b, nil
}

func (b *fb2Book) metadata() *Metadata {
	d := b.doc
	m := &Metadata{}
	ti := d.first(fb2TitleInfo)
	if ti == nil {
		return m
	}
	if n := d.within(ti, "book-title"); len(n) > 0 {
		m.Title = n[0].text
	}
	for _, a := range d.within(ti, "author") {
		if name := fb2AuthorName(d, a); name != "" {
			m.Authors = append(m.Authors, name)
		}
	}
	for _, g := range d.within(ti, "genre") {
		m.Tags = append(m.Tags, g.text)
	}
	if n := d.within(ti, "annotation"); len(n) > 0 {
		m.Description = fb2Paragraphs(d, n[0])
	}
	if n := d.within(ti, "date"); len(n) > 0 {
		m.Published = n[0].attr["value"]
		if m.Published == "" {
			m.Published = n[0].text
		}
	}
	for _, n := range d.within(ti, "lang") {
		m.Languages = append(m.Languages, n.text)
	}
	if n := d.within(ti, "sequence"); len(n) > 0 {
		m.Series = n[0].attr["name"]
		m.SeriesIndex = parseIndex(n[0].attr["number"])
	}
	if pi := d.first(fb2Publish); pi != nil {
		if n := d.within(pi, "publisher"); len(n) > 0 {
			m.Publisher = n[0].text
		}
		if n := d.within(pi, "isbn"); len(n) > 0 && n[0].text != "" {
			m.Identifiers = append(m.Identifiers, Identifier{"isbn", n[0].text})
		}
		if n := d.within(pi, "year"); len(n) > 0 && m.Published == "" {
			m.Published = n[0].text
		}
		if n := d.within(pi, "sequence"); len(n) > 0 && m.Series == "" {
			m.Series = n[0].attr["name"]
			m.SeriesIndex = parseIndex(n[0].attr["number"])
		}
	}
	if bin := b.coverBinary(); bin != nil {
		m.Cover = bin.attr["content-type"]
	}
	return m
}

func fb2AuthorName(d *xdoc, a *xnode) string {
	var parts []string
	for _, f := range []string{"first-name", "middle-name", "last-name"} {
		if n := d.within(a, f); len(n) > 0 && n[0].text != "" {
			parts = append(parts, n[0].text)
		}
	}
	if len(parts) == 0 {
		if n := d.within(a, "nickname"); len(n) > 0 {
			return n[0].text
		}
	}
	return strings.Join(parts, " ")
}

// fb2Paragraphs joins the <p> children of an annotation with newlines.
func fb2Paragraphs(d *xdoc, n *xnode) string {
	ps := d.within(n, "p")
	if len(ps) == 0 {
		return n.text
	}
	var lines []string
	for _, p := range ps {
		lines = append(lines, p.text)
	}
	return strings.Join(lines, "\n")
}

// coverImage returns the <image> in <coverpage> and the binary id it names.
func (b *fb2Book) coverImage() (*xnode, string) {
	ti := b.doc.first(fb2TitleInfo)
	if ti == nil {
		return nil, ""
	}
	cp := b.doc.within(ti, "coverpage")
	if len(cp) == 0 {
		return nil, ""
	}
	imgs := b.doc.within(cp[0], "image")
	if len(imgs) == 0 {
		return nil, ""
	}
	return imgs[0], strings.TrimPrefix(imgs[0].attr["href"], "#")
}

func (b *fb2Book) coverBinary() *xnode {
	_, id := b.coverImage()
	if id == "" {
		return nil
	}
	for _, bin := range b.doc.all(fb2Binary) {
		if bin.attr["id"] == id {
			return bin
		}
	}
	return nil
}

func (fb2Handler) read(src io.ReaderAt, size int64) (*Metadata, error) {
	b, err := openFB2(src, size)
	if err != nil {
		return nil, err
	}
	return b.metadata(), nil
}

func (fb2Handler) cover(src io.ReaderAt, size int64) (*Cover, error) {
	b, err := openFB2(src, size)
	if err != nil {
		return nil, err
	}
	bin := b.coverBinary()
	if bin == nil {
		return nil, ErrNoCover
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(bin.text), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: cover is not valid base64", ErrMalformed)
	}
	mt := SniffImage(data)
	if mt == "" {
		mt = bin.attr["content-type"]
	}
	return &Cover{Data: data, MediaType: mt}, nil
}

func (fb2Handler) write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error {
	b, err := openFB2(src, size)
	if err != nil {
		return err
	}
	d := b.doc
	ti := d.first(fb2TitleInfo)
	if ti == nil {
		return fmt.Errorf("%w: FB2 has no <title-info>", ErrMalformed)
	}
	next := b.metadata()
	c.Apply(next)
	p := d.prefix
	el := func(name, text string) string {
		return "<" + p + name + ">" + escText(text) + "</" + p + name + ">"
	}

	removed := map[*xnode]bool{}
	tslot := &schemaSlot{d: d, parent: ti, order: fb2TitleInfoOrder, removed: removed}
	if c.Tags != nil {
		var out []string
		for _, t := range next.Tags {
			out = append(out, el("genre", t))
		}
		tslot.replace("genre", out...)
	}
	if c.Authors != nil {
		var out []string
		for _, a := range next.Authors {
			out = append(out, fb2Author(p, a))
		}
		tslot.replace("author", out...)
	}
	if c.Title != nil {
		tslot.replace("book-title", el("book-title", next.Title))
	}
	if c.Description != nil {
		var out []string
		if next.Description != "" {
			var ps strings.Builder
			for line := range strings.SplitSeq(next.Description, "\n") {
				ps.WriteString(el("p", line))
			}
			out = append(out, "<"+p+"annotation>"+ps.String()+"</"+p+"annotation>")
		}
		tslot.replace("annotation", out...)
	}
	if c.Published != nil {
		var out []string
		if next.Published != "" {
			out = append(out, "<"+p+`date value="`+escAttr(next.Published)+`">`+escText(next.Published)+"</"+p+"date>")
		}
		tslot.replace("date", out...)
	}
	var binEdits []edit
	if c.Cover != nil {
		img, coverPage, binEdit := b.planCover(c.Cover)
		if img != "" {
			tslot.replace("coverpage", coverPage)
		}
		binEdits = append(binEdits, binEdit)
	}
	if c.Languages != nil {
		var out []string
		// FB2 allows one book language.
		if len(next.Languages) > 0 {
			out = append(out, el("lang", next.Languages[0]))
		}
		tslot.replace("lang", out...)
	}
	if c.Series != nil || c.SeriesIndex != nil {
		var out []string
		if next.Series != "" {
			num := ""
			if next.SeriesIndex > 0 {
				num = ` number="` + escAttr(formatIndex(next.SeriesIndex)) + `"`
			}
			out = append(out, "<"+p+`sequence name="`+escAttr(next.Series)+`"`+num+"/>")
		}
		tslot.replace("sequence", out...)
	}
	edits := append(tslot.edits, binEdits...)

	_, isbnSet := c.Identifiers["isbn"]
	if c.Publisher != nil || isbnSet {
		pi := d.first(fb2Publish)
		if pi == nil {
			var parts []string
			if next.Publisher != "" {
				parts = append(parts, el("publisher", next.Publisher))
			}
			if v := next.Identifier("isbn"); v != "" {
				parts = append(parts, el("isbn", v))
			}
			if len(parts) > 0 {
				anchor := d.first(fb2DocInfo)
				if anchor == nil {
					anchor = ti
				}
				indent := d.indentOf(anchor, "\n  ")
				edits = append(edits, edit{start: anchor.end, end: anchor.end,
					text: indent + "<" + p + "publish-info>" + strings.Join(parts, "") + "</" + p + "publish-info>"})
			}
		} else {
			pslot := &schemaSlot{d: d, parent: pi, order: fb2PublishOrder, removed: removed}
			if c.Publisher != nil {
				var out []string
				if next.Publisher != "" {
					out = append(out, el("publisher", next.Publisher))
				}
				pslot.replace("publisher", out...)
			}
			if isbnSet {
				var out []string
				if v := next.Identifier("isbn"); v != "" {
					out = append(out, el("isbn", v))
				}
				pslot.replace("isbn", out...)
			}
			edits = append(edits, pslot.edits...)
		}
	}
	out, err := applyEdits(d.data, edits)
	if err != nil {
		return err
	}
	_, err = dst.Write(out)
	return err
}

// fb2Author splits a display name into FB2 name parts: the last word is
// the last name. A single word becomes a nickname.
func fb2Author(p, name string) string {
	el := func(n, text string) string { return "<" + p + n + ">" + escText(text) + "</" + p + n + ">" }
	words := strings.Fields(name)
	inner := el("nickname", name)
	if len(words) > 1 {
		inner = el("first-name", strings.Join(words[:len(words)-1], " ")) + el("last-name", words[len(words)-1])
	}
	return "<" + p + "author>" + inner + "</" + p + "author>"
}

// planCover returns a new <coverpage> (or "" to keep the current one) and
// the edit that replaces or adds the <binary> holding cov.
func (b *fb2Book) planCover(cov *Cover) (string, string, edit) {
	d := b.doc
	p := d.prefix
	enc := base64.StdEncoding.EncodeToString(cov.Data)
	binary := func(id string) string {
		return "<" + p + `binary id="` + escAttr(id) + `" content-type="` + escAttr(cov.MediaType) + `">` + enc + "</" + p + "binary>"
	}
	if bin := b.coverBinary(); bin != nil {
		return "", "", edit{start: bin.start, end: bin.end, text: binary(bin.attr["id"])}
	}
	ids := map[string]bool{}
	for _, bin := range d.all(fb2Binary) {
		ids[bin.attr["id"]] = true
	}
	id := "cover" + coverExt(cov.MediaType)
	for n := 2; ids[id]; n++ {
		id = fmt.Sprintf("cover-%d%s", n, coverExt(cov.MediaType))
	}
	xl, decl := d.bindings[nsXLink], ""
	if xl == "" {
		xl, decl = "l", ` xmlns:l="`+nsXLink+`"`
	}
	img := "<" + p + "image" + decl + " " + xl + `:href="#` + escAttr(id) + `"/>`
	coverPage := "<" + p + "coverpage>" + img + "</" + p + "coverpage>"
	root := d.first(fb2Root)
	at := int64(bytes.LastIndex(d.data[:root.end], []byte("</")))
	return img, coverPage, edit{start: at, end: at, text: binary(id) + "\n"}
}
