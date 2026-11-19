package ebookmeta

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	nsOPF = "http://www.idpf.org/2007/opf"
	nsDC  = "http://purl.org/dc/elements/1.1/"
)

// opfElem is one element under <metadata> or <manifest>, with its byte
// range in the document so it can be removed without reserializing the rest.
type opfElem struct {
	dc         bool // in the Dublin Core namespace
	local      string
	attr       map[string]string // keyed by local name; opf:role is "role"
	text       string
	start, end int64
}

// opfDoc is a parsed package document. Edits are applied as byte splices:
// encoding/xml cannot round-trip namespace prefixes, and readers depend on
// them, so the parts of the document this package does not change are
// never reserialized.
type opfDoc struct {
	data     []byte
	version  int // 2 or 3
	uniqueID string

	meta         []*opfElem
	metaOpenEnd  int64 // just after <metadata ...>
	elemPrefix   string
	dcPrefix     string // "" when the DC namespace is not bound
	opfAttrPrefx string // "" when the OPF namespace has no prefix

	items       []*opfElem
	manifestEnd int64 // offset of </manifest>, or -1
	ids         map[string]bool
}

func parseOPF(data []byte) (*opfDoc, error) {
	data, err := xmlToUTF8(data)
	if err != nil {
		return nil, err
	}
	d := &opfDoc{data: data, version: 2, manifestEnd: -1, ids: map[string]bool{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = charsetReader

	var (
		stack      []string
		inMetadata = -1 // depth of <metadata>, or -1
		inManifest = -1
		cur        *opfElem
		curDepth   int
		text       strings.Builder
		bindings   = map[string]string{} // uri -> prefix on package/metadata
		sawPackage bool
	)
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: OPF: %v", ErrMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth := len(stack)
			stack = append(stack, t.Name.Local)
			for _, a := range t.Attr {
				if a.Name.Local == "id" && a.Name.Space == "" {
					d.ids[a.Value] = true
				}
			}
			switch {
			case depth == 0 && t.Name.Local == "package":
				sawPackage = true
				if v := attrValue(t, "version"); strings.HasPrefix(strings.TrimSpace(v), "3") {
					d.version = 3
				}
				d.uniqueID = attrValue(t, "unique-identifier")
				collectBindings(t, bindings)
			case depth == 1 && t.Name.Local == "metadata" && inMetadata < 0:
				inMetadata = depth
				d.metaOpenEnd = dec.InputOffset()
				d.elemPrefix = rawPrefix(data[off:])
				collectBindings(t, bindings)
			case depth == 1 && t.Name.Local == "manifest" && inManifest < 0:
				inManifest = depth
			case cur == nil && inMetadata >= 0 && (depth == inMetadata+1 ||
				depth == inMetadata+2 && isLegacyMetaGroup(stack[inMetadata+1])):
				if isLegacyMetaGroup(t.Name.Local) {
					continue
				}
				cur, curDepth = newElem(t, off), depth
				text.Reset()
			case cur == nil && inManifest >= 0 && depth == inManifest+1 && t.Name.Local == "item":
				cur, curDepth = newElem(t, off), depth
				text.Reset()
			}
		case xml.CharData:
			if cur != nil {
				text.Write(t)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
			depth := len(stack)
			switch {
			case cur != nil && depth == curDepth:
				cur.end = dec.InputOffset()
				cur.text = strings.TrimSpace(text.String())
				if inManifest >= 0 && depth == inManifest+1 {
					d.items = append(d.items, cur)
				} else {
					d.meta = append(d.meta, cur)
				}
				cur = nil
			case depth == inMetadata:
				inMetadata = -2 // done; ignore any later <metadata>
			case depth == inManifest:
				d.manifestEnd = off
				inManifest = -2
			}
		}
	}
	if !sawPackage || d.metaOpenEnd == 0 {
		return nil, fmt.Errorf("%w: OPF has no package metadata", ErrMalformed)
	}
	d.dcPrefix = bindings[nsDC]
	d.opfAttrPrefx = bindings[nsOPF]
	return d, nil
}

func isLegacyMetaGroup(local string) bool {
	return local == "dc-metadata" || local == "x-metadata"
}

func newElem(t xml.StartElement, off int64) *opfElem {
	e := &opfElem{dc: t.Name.Space == nsDC, local: t.Name.Local, attr: map[string]string{}, start: off}
	for _, a := range t.Attr {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
			continue
		}
		// Unprefixed attributes win over opf:-prefixed ones of the same name.
		if _, ok := e.attr[a.Name.Local]; ok && a.Name.Space != "" {
			continue
		}
		e.attr[a.Name.Local] = a.Value
	}
	return e
}

func attrValue(t xml.StartElement, local string) string {
	for _, a := range t.Attr {
		if a.Name.Local == local && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func collectBindings(t xml.StartElement, into map[string]string) {
	for _, a := range t.Attr {
		if a.Name.Space == "xmlns" {
			into[a.Value] = a.Name.Local
		}
	}
}

// rawPrefix returns "p:" when the start tag at b is written <p:name.
func rawPrefix(b []byte) string {
	end := bytes.IndexAny(b, " \t\r\n/>")
	if end < 0 || len(b) == 0 || b[0] != '<' {
		return ""
	}
	name := b[1:end]
	if i := bytes.IndexByte(name, ':'); i >= 0 {
		return string(name[:i+1])
	}
	return ""
}

// refinement is an EPUB 3 <meta refines="#id" property="..."> value.
type refinement struct {
	property, scheme, value string
	elem                    *opfElem
}

func (d *opfDoc) refinements() map[string][]refinement {
	out := map[string][]refinement{}
	for _, e := range d.meta {
		if e.dc || e.local != "meta" {
			continue
		}
		ref := strings.TrimPrefix(e.attr["refines"], "#")
		if ref == "" || e.attr["property"] == "" {
			continue
		}
		out[ref] = append(out[ref], refinement{e.attr["property"], e.attr["scheme"], e.text, e})
	}
	return out
}

func refValue(refs []refinement, property string) string {
	for _, r := range refs {
		if r.property == property {
			return r.value
		}
	}
	return ""
}

// legacyMeta returns the content of <meta name="name" content="...">.
func (d *opfDoc) legacyMeta(name string) string {
	for _, e := range d.meta {
		if !e.dc && e.local == "meta" && e.attr["name"] == name {
			return e.attr["content"]
		}
	}
	return ""
}

func (d *opfDoc) isAuthor(e *opfElem, refs map[string][]refinement) bool {
	if !e.dc || e.local != "creator" {
		return false
	}
	role := e.attr["role"]
	if id := e.attr["id"]; id != "" {
		if r := refValue(refs[id], "role"); r != "" {
			role = r
		}
	}
	role = strings.ToLower(strings.TrimSpace(role))
	return role == "" || role == "aut"
}

func isPublicationDate(e *opfElem) bool {
	if !e.dc || e.local != "date" {
		return false
	}
	ev := strings.ToLower(e.attr["event"])
	return ev == "" || ev == "publication" || ev == "original-publication"
}

// schemePrefixRe reads an EPUB 3 identifier written "scheme:value", the form
// Calibre uses and this package writes for any scheme without a URN namespace
// (see opfWriter.identifier). The list is closed, so a URL or a value that only
// contains a colon is not split: the common catalogue schemes, and the ones zlib
// records itself (a download's Z-Library record and ISBN hint, and the lookup
// sources' own IDs).
var schemePrefixRe = regexp.MustCompile(`^(?i)(isbn|doi|asin|mobi-asin|amazon|google|goodreads|uuid|calibre|issn|oclc|lccn|openlibrary|douban|weread|zlib-id|zlib-isbn):\s*(\S.*)$`)

// identifierOf derives (scheme, value) from a dc:identifier element.
func identifierOf(e *opfElem, refs map[string][]refinement) (string, string) {
	value := strings.TrimSpace(e.text)
	scheme := e.attr["scheme"]
	if id := e.attr["id"]; id != "" {
		if t := refValue(refs[id], "identifier-type"); t != "" {
			scheme = t
		}
	}
	if scheme == "" {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "urn:"):
			rest := value[4:]
			if i := strings.IndexByte(rest, ':'); i > 0 {
				scheme, value = rest[:i], rest[i+1:]
			}
		case schemePrefixRe.MatchString(value):
			m := schemePrefixRe.FindStringSubmatch(value)
			scheme, value = m[1], m[2]
		case looksLikeISBN(value):
			scheme = "isbn"
		}
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "isbn" {
		value = strings.TrimPrefix(strings.TrimPrefix(value, "urn:isbn:"), "isbn:")
	}
	return scheme, strings.TrimSpace(value)
}

func looksLikeISBN(s string) bool {
	digits := 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' || r == ' ':
		case (r == 'X' || r == 'x') && i == len(s)-1:
			digits++
		default:
			return false
		}
	}
	return digits == 10 || digits == 13
}

// metadata extracts the format-neutral metadata.
func (d *opfDoc) metadata() *Metadata {
	m := &Metadata{}
	refs := d.refinements()
	for _, e := range d.meta {
		if e.dc && e.local == "identifier" && d.uniqueID != "" && e.attr["id"] == d.uniqueID {
			m.UniqueScheme, _ = identifierOf(e, refs)
		}
	}
	var titles []*opfElem
	for _, e := range d.meta {
		if !e.dc {
			continue
		}
		switch e.local {
		case "title":
			titles = append(titles, e)
		case "creator":
			if d.isAuthor(e, refs) && e.text != "" {
				m.Authors = append(m.Authors, e.text)
				if m.AuthorSort == "" {
					m.AuthorSort = e.attr["file-as"]
					if v := refValue(refs[e.attr["id"]], "file-as"); v != "" && e.attr["id"] != "" {
						m.AuthorSort = v
					}
				}
			}
		case "publisher":
			if m.Publisher == "" {
				m.Publisher = e.text
			}
		case "language":
			m.Languages = append(m.Languages, e.text)
		case "subject":
			m.Tags = append(m.Tags, e.text)
		case "description":
			if m.Description == "" {
				m.Description = e.text
			}
		case "date":
			if m.Published == "" && isPublicationDate(e) {
				m.Published = e.text
			}
		case "identifier":
			if s, v := identifierOf(e, refs); s != "" && v != "" && m.Identifier(s) == "" {
				m.Identifiers = append(m.Identifiers, Identifier{s, v})
			}
		}
	}
	if len(titles) > 0 {
		main := titles[0]
		for _, t := range titles {
			if id := t.attr["id"]; id != "" && refValue(refs[id], "title-type") == "main" {
				main = t
				break
			}
		}
		m.Title = main.text
		m.TitleSort = main.attr["file-as"]
		if id := main.attr["id"]; id != "" {
			if v := refValue(refs[id], "file-as"); v != "" {
				m.TitleSort = v
			}
		}
	}
	if v := d.legacyMeta("calibre:title_sort"); v != "" {
		m.TitleSort = v
	}
	for _, e := range d.meta {
		if !e.dc && e.local == "meta" && e.attr["property"] == "belongs-to-collection" && e.attr["refines"] == "" {
			ctype := refValue(refs[e.attr["id"]], "collection-type")
			if e.attr["id"] != "" && ctype != "" && ctype != "series" {
				continue
			}
			m.Series = e.text
			if e.attr["id"] != "" {
				m.SeriesIndex = parseIndex(refValue(refs[e.attr["id"]], "group-position"))
			}
			break
		}
	}
	if m.Series == "" {
		m.Series = d.legacyMeta("calibre:series")
		m.SeriesIndex = parseIndex(d.legacyMeta("calibre:series_index"))
	}
	if r, err := strconv.ParseFloat(d.legacyMeta("calibre:rating"), 64); err == nil {
		m.Rating = r / 2
	}
	if it := d.coverItem(); it != nil {
		m.Cover = it.attr["media-type"]
	}
	return m
}

// uniqueIDValue returns the text of the package's unique identifier.
func (d *opfDoc) uniqueIDValue() string {
	for _, e := range d.meta {
		if e.dc && e.local == "identifier" && e.attr["id"] == d.uniqueID {
			return e.text
		}
	}
	return ""
}

// coverItem returns the manifest item holding the cover image, if any.
func (d *opfDoc) coverItem() *opfElem {
	isImage := func(e *opfElem) bool { return strings.HasPrefix(e.attr["media-type"], "image/") }
	for _, it := range d.items {
		if isImage(it) && hasToken(it.attr["properties"], "cover-image") {
			return it
		}
	}
	if id := d.legacyMeta("cover"); id != "" {
		for _, it := range d.items {
			if it.attr["id"] == id && isImage(it) {
				return it
			}
		}
	}
	for _, it := range d.items {
		if isImage(it) && (strings.Contains(strings.ToLower(it.attr["id"]), "cover") ||
			strings.Contains(strings.ToLower(it.attr["href"]), "cover")) {
			return it
		}
	}
	return nil
}

func hasToken(list, tok string) bool {
	for _, f := range strings.Fields(list) {
		if f == tok {
			return true
		}
	}
	return false
}

// edit replaces data[start:end] with text; start == end inserts.
type edit struct {
	start, end int64
	text       string
}

func applyEdits(data []byte, edits []edit) ([]byte, error) {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		// Insertions go before a removal starting at the same offset.
		return edits[i].end-edits[i].start < edits[j].end-edits[j].start
	})
	var out bytes.Buffer
	var pos int64
	for _, e := range edits {
		if e.start < pos || e.end > int64(len(data)) {
			return nil, fmt.Errorf("%w: overlapping OPF edits", ErrMalformed)
		}
		out.Write(data[pos:e.start])
		out.WriteString(e.text)
		pos = e.end
	}
	out.Write(data[pos:])
	return out.Bytes(), nil
}

// removal deletes e together with the indentation in front of it, so no
// blank line is left behind.
func (d *opfDoc) removal(e *opfElem) edit {
	s := e.start
	for s > 0 && (d.data[s-1] == ' ' || d.data[s-1] == '\t') {
		s--
	}
	if s > 0 && d.data[s-1] == '\n' {
		s--
		if s > 0 && d.data[s-1] == '\r' {
			s--
		}
	}
	return edit{start: s, end: e.end}
}

// indentBefore returns the newline and indentation preceding offset, or a
// default.
func (d *opfDoc) indentBefore(off int64, def string) string {
	i := off
	for i > 0 && (d.data[i-1] == ' ' || d.data[i-1] == '\t') {
		i--
	}
	if i > 0 && d.data[i-1] == '\n' {
		nl := i - 1
		if nl > 0 && d.data[nl-1] == '\r' {
			nl--
		}
		return string(d.data[nl:off])
	}
	return def
}

func (d *opfDoc) newID(base string) string {
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			id = base + "-" + strconv.Itoa(n)
		}
		if !d.ids[id] {
			d.ids[id] = true
			return id
		}
	}
}

// opfWriter builds new metadata elements with the document's prefixes.
type opfWriter struct {
	d   *opfDoc
	out []string
}

func (w *opfWriter) dcName(local string) (string, string) {
	if w.d.dcPrefix == "" {
		return "dc:" + local, ` xmlns:dc="` + nsDC + `"`
	}
	return w.d.dcPrefix + ":" + local, ""
}

func (w *opfWriter) opfAttr(local string) (string, string) {
	if w.d.opfAttrPrefx == "" {
		return "opf:" + local, ` xmlns:opf="` + nsOPF + `"`
	}
	return w.d.opfAttrPrefx + ":" + local, ""
}

// dc appends <dc:local attrs>text</dc:local>. opfAttrs are written with the
// OPF namespace prefix (EPUB 2 only).
func (w *opfWriter) dc(local, text string, attrs [][2]string, opfAttrs [][2]string) {
	name, decl := w.dcName(local)
	var b strings.Builder
	b.WriteString("<" + name + decl)
	for _, a := range attrs {
		b.WriteString(" " + a[0] + `="` + escAttr(a[1]) + `"`)
	}
	for _, a := range opfAttrs {
		n, decl := w.opfAttr(a[0])
		b.WriteString(decl + " " + n + `="` + escAttr(a[1]) + `"`)
	}
	b.WriteString(">" + escText(text) + "</" + name + ">")
	w.out = append(w.out, b.String())
}

func (w *opfWriter) legacyMeta(name, content string) {
	w.out = append(w.out, "<"+w.d.elemPrefix+`meta name="`+escAttr(name)+`" content="`+escAttr(content)+`"/>`)
}

func (w *opfWriter) refine(id, property, value, scheme string) {
	s := ""
	if scheme != "" {
		s = ` scheme="` + escAttr(scheme) + `"`
	}
	n := w.d.elemPrefix + "meta"
	w.out = append(w.out, "<"+n+` refines="#`+escAttr(id)+`" property="`+escAttr(property)+`"`+s+">"+escText(value)+"</"+n+">")
}

func (w *opfWriter) property(id, property, value string) {
	n := w.d.elemPrefix + "meta"
	idAttr := ""
	if id != "" {
		idAttr = ` id="` + escAttr(id) + `"`
	}
	w.out = append(w.out, "<"+n+` property="`+escAttr(property)+`"`+idAttr+">"+escText(value)+"</"+n+">")
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
		"\r", "&#xD;", "\n", "&#xA;", "\t", "&#x9;")
)

// escText escapes character data; unlike xml.EscapeText it keeps newlines
// readable.
func escText(s string) string { return textEscaper.Replace(s) }

func escAttr(s string) string { return attrEscaper.Replace(s) }

// modifiedNow is the time a write records as the package's last modification;
// tests replace it.
var modifiedNow = time.Now

// rewrite returns the document with c applied to its metadata. extra edits
// (manifest changes for a cover) are applied in the same pass.
func (d *opfDoc) rewrite(c *Changes, extra []edit) ([]byte, error) {
	cur := d.metadata()
	next := *cur
	c.Apply(&next)
	refs := d.refinements()

	remove := map[*opfElem]bool{}
	w := &opfWriter{d: d}
	v3 := d.version >= 3

	if c.Title != nil || c.TitleSort != nil {
		for _, e := range d.meta {
			if e.dc && e.local == "title" || !e.dc && e.local == "meta" && e.attr["name"] == "calibre:title_sort" {
				remove[e] = true
			}
		}
		if next.Title != "" {
			switch {
			case v3 && next.TitleSort != "":
				id := d.newID("title")
				w.dc("title", next.Title, [][2]string{{"id", id}}, nil)
				w.refine(id, "file-as", next.TitleSort, "")
			case next.TitleSort != "":
				w.dc("title", next.Title, nil, [][2]string{{"file-as", next.TitleSort}})
			default:
				w.dc("title", next.Title, nil, nil)
			}
		}
	}
	if c.Authors != nil || c.AuthorSort != nil {
		for _, e := range d.meta {
			if d.isAuthor(e, refs) {
				remove[e] = true
			}
		}
		for i, a := range next.Authors {
			sortAs := ""
			if i == 0 {
				sortAs = next.AuthorSort
			}
			if v3 {
				id := d.newID("creator")
				w.dc("creator", a, [][2]string{{"id", id}}, nil)
				w.refine(id, "role", "aut", "marc:relators")
				if sortAs != "" {
					w.refine(id, "file-as", sortAs, "")
				}
			} else {
				attrs := [][2]string{{"role", "aut"}}
				if sortAs != "" {
					attrs = append(attrs, [2]string{"file-as", sortAs})
				}
				w.dc("creator", a, nil, attrs)
			}
		}
	}
	simple := func(set bool, local string, values ...string) {
		if !set {
			return
		}
		for _, e := range d.meta {
			if e.dc && e.local == local {
				remove[e] = true
			}
		}
		for _, v := range values {
			if v != "" {
				w.dc(local, v, nil, nil)
			}
		}
	}
	simple(c.Publisher != nil, "publisher", next.Publisher)
	simple(c.Languages != nil, "language", next.Languages...)
	simple(c.Tags != nil, "subject", next.Tags...)
	simple(c.Description != nil, "description", next.Description)
	if c.Published != nil {
		for _, e := range d.meta {
			if isPublicationDate(e) {
				remove[e] = true
			}
		}
		if next.Published != "" {
			if v3 {
				w.dc("date", next.Published, nil, nil)
			} else {
				w.dc("date", next.Published, nil, [][2]string{{"event", "publication"}})
			}
		}
	}
	if c.Series != nil || c.SeriesIndex != nil {
		for _, e := range d.meta {
			if e.dc || e.local != "meta" {
				continue
			}
			n := e.attr["name"]
			if n == "calibre:series" || n == "calibre:series_index" ||
				e.attr["property"] == "belongs-to-collection" && e.attr["refines"] == "" {
				remove[e] = true
			}
		}
		if next.Series != "" {
			if v3 {
				id := d.newID("series")
				w.property(id, "belongs-to-collection", next.Series)
				w.refine(id, "collection-type", "series", "")
				if next.SeriesIndex > 0 {
					w.refine(id, "group-position", formatIndex(next.SeriesIndex), "")
				}
			}
			w.legacyMeta("calibre:series", next.Series)
			if next.SeriesIndex > 0 {
				w.legacyMeta("calibre:series_index", formatIndex(next.SeriesIndex))
			}
		}
	}
	if c.Rating != nil {
		for _, e := range d.meta {
			if !e.dc && e.local == "meta" && e.attr["name"] == "calibre:rating" {
				remove[e] = true
			}
		}
		if next.Rating > 0 {
			w.legacyMeta("calibre:rating", formatIndex(next.Rating*2))
		}
	}
	if len(c.Identifiers) > 0 {
		changed := map[string]bool{}
		for s := range c.Identifiers {
			changed[strings.ToLower(strings.TrimSpace(s))] = true
		}
		for _, e := range d.meta {
			if !e.dc || e.local != "identifier" {
				continue
			}
			s, _ := identifierOf(e, refs)
			if !changed[s] {
				continue
			}
			if e.attr["id"] != "" && e.attr["id"] == d.uniqueID {
				// The package's unique identifier must stay; rewrite its
				// value in place when one is given.
				if v := next.Identifier(s); v != "" {
					remove[e] = true
					w.identifier(s, v, e.attr["id"])
				}
				delete(changed, s)
				continue
			}
			remove[e] = true
		}
		var schemes []string
		for s := range changed {
			schemes = append(schemes, s)
		}
		sort.Strings(schemes)
		for _, s := range schemes {
			if v := next.Identifier(s); v != "" {
				w.identifier(s, v, "")
			}
		}
	}

	// EPUB 3.3 §5.5.5: a package holds exactly one dcterms:modified (the one
	// that refines nothing), in UTC as YYYY-MM-DDThh:mm:ssZ, and it should be
	// updated whenever the publication changes. A cover alone changes only an
	// image, but it is a change all the same.
	if v3 && (!c.Empty() || len(extra) > 0) {
		for _, e := range d.meta {
			if !e.dc && e.local == "meta" && e.attr["property"] == "dcterms:modified" && e.attr["refines"] == "" {
				remove[e] = true
			}
		}
		w.property("", "dcterms:modified", modifiedNow().UTC().Format("2006-01-02T15:04:05Z"))
	}

	// Drop EPUB 3 refinements of removed elements.
	for _, e := range d.meta {
		if remove[e] && e.attr["id"] != "" {
			for _, r := range refs[e.attr["id"]] {
				remove[r.elem] = true
			}
		}
	}

	edits := append([]edit(nil), extra...)
	var lastKept *opfElem
	for _, e := range d.meta {
		if remove[e] {
			edits = append(edits, d.removal(e))
		} else {
			lastKept = e
		}
	}
	if len(w.out) > 0 {
		at := d.metaOpenEnd
		indent := "\n    "
		if len(d.meta) > 0 {
			indent = d.indentBefore(d.meta[0].start, indent)
		}
		if lastKept != nil {
			at = lastKept.end
			indent = d.indentBefore(lastKept.start, indent)
		}
		edits = append(edits, edit{start: at, end: at, text: indent + strings.Join(w.out, indent)})
	}
	return applyEdits(d.data, edits)
}

func (w *opfWriter) identifier(scheme, value, id string) {
	var attrs [][2]string
	if id == "" {
		id = w.d.newID("id-" + strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '-'
		}, scheme))
	}
	attrs = append(attrs, [2]string{"id", id})
	if w.d.version >= 3 {
		switch scheme {
		case "isbn", "uuid", "issn":
			w.dc("identifier", "urn:"+scheme+":"+value, attrs, nil)
		default:
			w.dc("identifier", scheme+":"+value, attrs, nil)
		}
		return
	}
	w.dc("identifier", value, attrs, [][2]string{{"scheme", strings.ToUpper(scheme)}})
}

// manifestItem builds an <item> start tag for the manifest.
func (d *opfDoc) manifestItem(id, href, mediaType, properties string) string {
	props := ""
	if properties != "" && d.version >= 3 {
		props = ` properties="` + escAttr(properties) + `"`
	}
	return "<" + d.elemPrefix + `item id="` + escAttr(id) + `" href="` + escAttr(href) +
		`" media-type="` + escAttr(mediaType) + `"` + props + "/>"
}

var mediaTypeAttrRe = regexp.MustCompile(`media-type\s*=\s*("[^"]*"|'[^']*')`)

// retype changes the media-type attribute of item in place.
func (d *opfDoc) retype(item *opfElem, mediaType string) (edit, bool) {
	tag := d.data[item.start:item.end]
	loc := mediaTypeAttrRe.FindIndex(tag)
	if loc == nil {
		return edit{}, false
	}
	return edit{
		start: item.start + int64(loc[0]),
		end:   item.start + int64(loc[1]),
		text:  `media-type="` + escAttr(mediaType) + `"`,
	}, true
}

// MarshalOPF renders m as a standalone OPF 2.0 package document, the
// sidecar format ebook managers exchange.
func MarshalOPF(m *Metadata) ([]byte, error) {
	const tmpl = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
  </metadata>
  <guide/>
</package>
`
	d, err := parseOPF([]byte(tmpl))
	if err != nil {
		return nil, err
	}
	return d.rewrite(ChangesFrom(m), nil)
}

// UnmarshalOPF reads metadata from a standalone OPF document.
func UnmarshalOPF(data []byte) (*Metadata, error) {
	d, err := parseOPF(data)
	if err != nil {
		return nil, err
	}
	m := d.metadata()
	m.Cover = ""
	m.clean()
	return m, nil
}

// ChangesFrom returns Changes that set every non-empty field of m. Cover is
// not included.
func ChangesFrom(m *Metadata) *Changes {
	c := &Changes{}
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	list := func(l []string) *[]string {
		if len(l) == 0 {
			return nil
		}
		return &l
	}
	c.Title, c.TitleSort = str(m.Title), str(m.TitleSort)
	c.Authors, c.AuthorSort = list(m.Authors), str(m.AuthorSort)
	c.Publisher = str(m.Publisher)
	c.Languages, c.Tags = list(m.Languages), list(m.Tags)
	c.Series = str(m.Series)
	if m.SeriesIndex > 0 {
		v := m.SeriesIndex
		c.SeriesIndex = &v
	}
	for _, id := range m.Identifiers {
		if c.Identifiers == nil {
			c.Identifiers = map[string]string{}
		}
		c.Identifiers[id.Scheme] = id.Value
	}
	c.Published, c.Description = str(m.Published), str(m.Description)
	if m.Rating > 0 {
		v := m.Rating
		c.Rating = &v
	}
	return c
}
