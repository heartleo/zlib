package ebookmeta

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

const epubMimetype = "application/epub+zip"

type epubHandler struct{}

func (epubHandler) supports(string) bool { return true }

type epubBook struct {
	zr      *zip.Reader
	opfPath string
	opfFile *zip.File
	opf     *opfDoc
}

func openEPUB(src io.ReaderAt, size int64) (*epubBook, error) {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	b := &epubBook{zr: zr}
	container := b.file("META-INF/container.xml")
	if container == nil {
		return nil, fmt.Errorf("%w: EPUB has no META-INF/container.xml", ErrMalformed)
	}
	data, err := readZipFile(container, 1<<20)
	if err != nil {
		return nil, err
	}
	b.opfPath, err = containerRootfile(data)
	if err != nil {
		return nil, err
	}
	b.opfFile = b.file(b.opfPath)
	if b.opfFile == nil {
		return nil, fmt.Errorf("%w: EPUB package %q is missing", ErrMalformed, b.opfPath)
	}
	if b.encrypted(b.opfPath) {
		return nil, fmt.Errorf("%w: the package document is encrypted", ErrEncrypted)
	}
	data, err = readZipFile(b.opfFile, maxPartSize)
	if err != nil {
		return nil, err
	}
	if b.opf, err = parseOPF(data); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *epubBook) file(name string) *zip.File {
	for _, f := range b.zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// encrypted reports whether name is listed in META-INF/encryption.xml.
func (b *epubBook) encrypted(name string) bool {
	f := b.file("META-INF/encryption.xml")
	if f == nil {
		return false
	}
	data, err := readZipFile(f, 4<<20)
	if err != nil {
		return true // fail closed
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return err != io.EOF
		}
		if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "CipherReference" {
			if uri, err := url.PathUnescape(attrValue(t, "URI")); err == nil && path.Clean(uri) == name {
				return true
			}
		}
	}
}

func containerRootfile(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader
	first := ""
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: container.xml: %v", ErrMalformed, err)
		}
		t, ok := tok.(xml.StartElement)
		if !ok || t.Name.Local != "rootfile" {
			continue
		}
		p := attrValue(t, "full-path")
		if p == "" {
			continue
		}
		if attrValue(t, "media-type") == "application/oebps-package+xml" {
			return cleanZipPath(p)
		}
		if first == "" {
			first = p
		}
	}
	if first == "" {
		return "", fmt.Errorf("%w: container.xml names no package document", ErrMalformed)
	}
	return cleanZipPath(first)
}

func cleanZipPath(p string) (string, error) {
	p = path.Clean(strings.TrimPrefix(p, "/"))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("%w: path %q leaves the archive", ErrMalformed, p)
	}
	return p, nil
}

// resolve turns a manifest href into a zip entry name.
func (b *epubBook) resolve(href string) (string, error) {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	u, err := url.PathUnescape(href)
	if err != nil {
		return "", fmt.Errorf("%w: bad href %q", ErrMalformed, href)
	}
	return cleanZipPath(path.Join(path.Dir(b.opfPath), u))
}

func (epubHandler) read(src io.ReaderAt, size int64) (*Metadata, error) {
	b, err := openEPUB(src, size)
	if err != nil {
		return nil, err
	}
	return b.opf.metadata(), nil
}

func (epubHandler) cover(src io.ReaderAt, size int64) (*Cover, error) {
	b, err := openEPUB(src, size)
	if err != nil {
		return nil, err
	}
	it := b.opf.coverItem()
	if it == nil {
		return nil, ErrNoCover
	}
	name, err := b.resolve(it.attr["href"])
	if err != nil {
		return nil, err
	}
	f := b.file(name)
	if f == nil {
		return nil, ErrNoCover
	}
	if b.encrypted(name) {
		return nil, fmt.Errorf("%w: the cover image is encrypted", ErrEncrypted)
	}
	data, err := readZipFile(f, maxPartSize)
	if err != nil {
		return nil, err
	}
	mt := SniffImage(data)
	if mt == "" {
		mt = it.attr["media-type"]
	}
	return &Cover{Data: data, MediaType: mt}, nil
}

func (epubHandler) write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error {
	b, err := openEPUB(src, size)
	if err != nil {
		return err
	}
	var extra []edit
	replace := map[string][]byte{} // zip entry -> new content
	if c.Cover != nil {
		if extra, err = b.planCover(c.Cover, replace); err != nil {
			return err
		}
	}
	opfData, err := b.opf.rewrite(c, extra)
	if err != nil {
		return err
	}
	if b.file("META-INF/encryption.xml") != nil {
		// Font obfuscation keys derive from the unique identifier, so a new
		// value would make obfuscated fonts unreadable.
		next, err := parseOPF(opfData)
		if err != nil {
			return err
		}
		if next.uniqueIDValue() != b.opf.uniqueIDValue() {
			return fmt.Errorf("%w: the book has encrypted or obfuscated files keyed to its unique identifier, which cannot change", ErrEncrypted)
		}
	}
	replace[b.opfPath] = opfData

	zw := zip.NewWriter(dst)
	if err := writeMimetype(zw, b.file("mimetype")); err != nil {
		return err
	}
	if err := copyZip(zw, b.zr.File, replace, "mimetype"); err != nil {
		return err
	}
	return zw.Close()
}

// copyZip copies files to zw, substituting the contents in replace and
// leaving out the entry named skip. Entries in replace that do not exist
// yet are appended in name order.
func copyZip(zw *zip.Writer, files []*zip.File, replace map[string][]byte, skip string) error {
	written := map[string]bool{}
	put := func(name string, data []byte, modified time.Time) error {
		method := zip.Deflate
		if SniffImage(data) != "" {
			method = zip.Store // already compressed
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: modified})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	for _, f := range files {
		if f.Name == skip {
			continue
		}
		data, ok := replace[f.Name]
		if !ok && strings.HasSuffix(f.Name, "/") {
			// Some writers deflate directory entries into a two-byte empty
			// stream, which zip.Writer refuses to copy. A directory has no
			// data, so write the header alone.
			fh := f.FileHeader
			fh.Method, fh.CRC32, fh.CompressedSize64, fh.UncompressedSize64 = zip.Store, 0, 0, 0
			fh.Flags &^= 0x8 // no data descriptor
			if _, err := zw.CreateRaw(&fh); err != nil {
				return err
			}
			continue
		}
		if !ok {
			// Copy the compressed bytes as they are: nothing is recompressed
			// and encrypted entries stay intact.
			if err := zw.Copy(f); err != nil {
				return err
			}
			continue
		}
		if written[f.Name] {
			continue // a duplicate name; the first copy wins
		}
		written[f.Name] = true
		if err := put(f.Name, data, f.Modified); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(replace))
	for name := range replace {
		if !written[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if err := put(name, replace[name], time.Time{}); err != nil {
			return err
		}
	}
	return nil
}

// writeMimetype writes the EPUB signature entry first, stored and with no
// extra field, so the string sits at a fixed offset (OCF 3.0 4.3). The
// MS-DOS timestamp of orig is kept: setting Modified would add an extra
// field.
func writeMimetype(zw *zip.Writer, orig *zip.File) error {
	data := []byte(epubMimetype)
	fh := &zip.FileHeader{
		Name:               "mimetype",
		Method:             zip.Store,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(data)),
		UncompressedSize64: uint64(len(data)),
	}
	if orig != nil {
		fh.ModifiedTime, fh.ModifiedDate = orig.ModifiedTime, orig.ModifiedDate
	}
	w, err := zw.CreateRaw(fh)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// planCover decides how to embed cov: replace the existing cover image's
// bytes, or add a new manifest item. It fills replace with entry contents
// and returns the OPF edits.
func (b *epubBook) planCover(cov *Cover, replace map[string][]byte) ([]edit, error) {
	if it := b.opf.coverItem(); it != nil {
		name, err := b.resolve(it.attr["href"])
		if err == nil && b.file(name) != nil {
			if b.encrypted(name) {
				return nil, fmt.Errorf("%w: the cover image is encrypted", ErrEncrypted)
			}
			replace[name] = cov.Data
			var edits []edit
			if it.attr["media-type"] != cov.MediaType {
				if e, ok := b.opf.retype(it, cov.MediaType); ok {
					edits = append(edits, e)
				}
			}
			return edits, nil
		}
	}
	if b.opf.manifestEnd < 0 {
		return nil, fmt.Errorf("%w: OPF has no manifest", ErrMalformed)
	}
	id := b.opf.newID("cover-image")
	href := "cover-image" + coverExt(cov.MediaType)
	name, err := b.resolve(href)
	if err != nil {
		return nil, err
	}
	for n := 2; b.file(name) != nil; n++ {
		href = fmt.Sprintf("cover-image-%d%s", n, coverExt(cov.MediaType))
		if name, err = b.resolve(href); err != nil {
			return nil, err
		}
	}
	replace[name] = cov.Data
	d := b.opf
	indent := "\n    "
	if len(d.items) > 0 {
		indent = d.indentBefore(d.items[len(d.items)-1].start, indent)
	}
	at := d.manifestEnd
	if len(d.items) > 0 {
		at = d.items[len(d.items)-1].end
	}
	edits := []edit{{start: at, end: at, text: indent + d.manifestItem(id, href, cov.MediaType, "cover-image")}}
	// EPUB 2 readers find the cover through <meta name="cover">. Insert it
	// right after <metadata>, which no removal can overlap.
	for _, e := range d.meta {
		if !e.dc && e.local == "meta" && e.attr["name"] == "cover" {
			edits = append(edits, d.removal(e))
		}
	}
	mi := "\n    "
	if len(d.meta) > 0 {
		mi = d.indentBefore(d.meta[0].start, mi)
	}
	edits = append(edits, edit{start: d.metaOpenEnd, end: d.metaOpenEnd,
		text: mi + "<" + d.elemPrefix + `meta name="cover" content="` + escAttr(id) + `"/>`})
	return edits, nil
}
