package ebookmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// PDF metadata lives in the document information dictionary (ISO 32000-1,
// 14.3.3). Writes append an incremental update: the original bytes are
// kept, and a new Info object, cross-reference section and trailer follow.

type pdfHandler struct{}

func (pdfHandler) supports(field string) bool {
	switch field {
	case "title", "authors", "tags", "description":
		return true
	}
	return false
}

func (pdfHandler) cover(io.ReaderAt, int64) (*Cover, error) { return nil, ErrNoCover }

func openPDFInfo(src io.ReaderAt, size int64) (*pdfFile, *pdfDict, error) {
	f, err := openPDF(src, size)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := f.trailer.vals["Encrypt"]; ok {
		return nil, nil, fmt.Errorf("%w: the PDF is encrypted", ErrEncrypted)
	}
	v, err := f.resolve(f.trailer.get("Info"))
	if err != nil {
		return nil, nil, err
	}
	info, _ := v.(*pdfDict)
	return f, info, nil
}

func (pdfHandler) read(src io.ReaderAt, size int64) (*Metadata, error) {
	f, info, err := openPDFInfo(src, size)
	if err != nil {
		return nil, err
	}
	m := &Metadata{}
	if info == nil {
		return m, nil
	}
	text := func(k pdfName) string {
		v, err := f.resolve(info.get(k))
		if err != nil {
			return ""
		}
		if b, ok := v.([]byte); ok {
			return decodePDFText(b)
		}
		return ""
	}
	m.Title = text("Title")
	m.Authors = splitList(strings.ReplaceAll(text("Author"), ";", "&"), "&")
	m.Description = text("Subject")
	m.Tags = splitList(strings.ReplaceAll(text("Keywords"), ";", ","), ",")
	return m, nil
}

func (pdfHandler) write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error {
	f, info, err := openPDFInfo(src, size)
	if err != nil {
		return err
	}
	next, err := pdfHandler{}.read(src, size)
	if err != nil {
		return err
	}
	c.Apply(next)

	// The new Info object replaces the old one under the same number, or
	// takes the next free number.
	sizeVal, _ := f.trailer.get("Size").(int64)
	if sizeVal <= 0 || sizeVal > 1<<23 {
		return fmt.Errorf("%w: bad trailer /Size", ErrMalformed)
	}
	newSize := sizeVal
	infoRef, ok := f.trailer.get("Info").(pdfRef)
	if !ok {
		infoRef = pdfRef{num: int(sizeVal)}
		newSize++
	}

	var body bytes.Buffer
	body.WriteString("<<")
	changed := map[pdfName]string{}
	if c.Title != nil {
		changed["Title"] = next.Title
	}
	if c.Authors != nil {
		changed["Author"] = strings.Join(next.Authors, " & ")
	}
	if c.Description != nil {
		changed["Subject"] = next.Description
	}
	if c.Tags != nil {
		changed["Keywords"] = strings.Join(next.Tags, ", ")
	}
	if info != nil {
		for _, k := range info.keys {
			if _, ok := changed[k]; ok {
				continue
			}
			// Unchanged entries keep their source bytes, references included.
			body.WriteString("/" + pdfNameText(k) + " " + string(info.raw[k]))
		}
	}
	for _, k := range []pdfName{"Title", "Author", "Subject", "Keywords"} {
		if v, ok := changed[k]; ok && v != "" {
			body.WriteString("/" + string(k) + " " + encodePDFText(v))
		}
	}
	body.WriteString(">>")

	if _, err := io.Copy(dst, io.NewSectionReader(src, 0, size)); err != nil {
		return err
	}
	pos := size
	w := func(s string) error {
		n, err := io.WriteString(dst, s)
		pos += int64(n)
		return err
	}
	last := make([]byte, 1)
	if _, err := src.ReadAt(last, size-1); err == nil && last[0] != '\n' && last[0] != '\r' {
		if err := w("\n"); err != nil {
			return err
		}
	}
	infoOff := pos
	if err := w(fmt.Sprintf("%d %d obj\n%s\nendobj\n", infoRef.num, infoRef.gen, body.String())); err != nil {
		return err
	}

	// Trailer entries carried over from the newest trailer.
	var keep strings.Builder
	skip := map[pdfName]bool{"Size": true, "Prev": true, "Info": true, "XRefStm": true,
		"Type": true, "W": true, "Index": true, "Length": true, "Filter": true, "DecodeParms": true}
	for _, k := range f.trailer.keys {
		if !skip[k] {
			keep.WriteString("/" + pdfNameText(k) + " " + string(f.trailer.raw[k]))
		}
	}
	infoEntry := fmt.Sprintf("/Info %d %d R", infoRef.num, infoRef.gen)

	if !f.isStream {
		xrefOff := pos
		s := fmt.Sprintf("xref\n%d 1\n%010d %05d n\r\ntrailer\n<</Size %d%s%s/Prev %d>>\nstartxref\n%d\n%%%%EOF\n",
			infoRef.num, infoOff, infoRef.gen, newSize, keep.String(), infoEntry, f.start, xrefOff)
		return w(s)
	}

	// The file uses cross-reference streams; continue with one.
	xrefNum := int(newSize)
	newSize++
	xrefOff := pos
	offW := 4
	if xrefOff > 0xFFFFFFFF {
		offW = 8
	}
	row := func(off int64, gen int) []byte {
		b := []byte{1}
		var o [8]byte
		binary.BigEndian.PutUint64(o[:], uint64(off))
		b = append(b, o[8-offW:]...)
		return append(b, byte(gen>>8), byte(gen))
	}
	data := append(row(infoOff, infoRef.gen), row(xrefOff, 0)...)
	s := fmt.Sprintf("%d 0 obj\n<</Type/XRef/Size %d/W[1 %d 2]/Index[%d 1 %d 1]%s%s/Prev %d/Length %d>>\nstream\n",
		xrefNum, newSize, offW, infoRef.num, xrefNum, keep.String(), infoEntry, f.start, len(data))
	if err := w(s); err != nil {
		return err
	}
	if _, err := dst.Write(data); err != nil {
		return err
	}
	pos += int64(len(data))
	return w(fmt.Sprintf("\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xrefOff))
}

// pdfNameText writes a name with #xx escapes for delimiters and
// non-printing bytes.
func pdfNameText(n pdfName) string {
	var b strings.Builder
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c <= ' ' || c >= 0x7f || c == '#' || isPDFDelim(c) {
			fmt.Fprintf(&b, "#%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// pdfDocEncoding maps the bytes of PDFDocEncoding that differ from
// Latin-1 (ISO 32000-1, Annex D.2).
var pdfDocEncoding = map[byte]rune{
	0x18: 0x02D8, 0x19: 0x02C7, 0x1A: 0x02C6, 0x1B: 0x02D9, 0x1C: 0x02DD, 0x1D: 0x02DB, 0x1E: 0x02DA, 0x1F: 0x02DC,
	0x80: 0x2022, 0x81: 0x2020, 0x82: 0x2021, 0x83: 0x2026, 0x84: 0x2014, 0x85: 0x2013, 0x86: 0x0192, 0x87: 0x2044,
	0x88: 0x2039, 0x89: 0x203A, 0x8A: 0x2212, 0x8B: 0x2030, 0x8C: 0x201E, 0x8D: 0x201C, 0x8E: 0x201D, 0x8F: 0x2018,
	0x90: 0x2019, 0x91: 0x201A, 0x92: 0x2122, 0x93: 0xFB01, 0x94: 0xFB02, 0x95: 0x0141, 0x96: 0x0152, 0x97: 0x0160,
	0x98: 0x0178, 0x99: 0x017D, 0x9A: 0x0131, 0x9B: 0x0142, 0x9C: 0x0153, 0x9D: 0x0161, 0x9E: 0x017E, 0x9F: 0xFFFD,
	0xA0: 0x20AC,
}

// decodePDFText decodes a PDF text string: UTF-16BE or UTF-8 with a byte
// order mark, otherwise PDFDocEncoding.
func decodePDFText(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, binary.BigEndian.Uint16(b[i:]))
		}
		return string(utf16.Decode(u))
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return strings.ToValidUTF8(string(b[3:]), "�")
	}
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(pdfDocRunes[c])
	}
	return sb.String()
}

// pdfDocRunes maps every byte of a PDFDocEncoding string to its rune: the
// bytes in pdfDocEncoding, and Latin-1 for the rest.
var pdfDocRunes = func() (t [256]rune) {
	for i := range t {
		t[i] = rune(i)
		if r, ok := pdfDocEncoding[byte(i)]; ok {
			t[i] = r
		}
	}
	return t
}()

// encodePDFText writes s as a literal string when it is printable ASCII,
// and as UTF-16BE hex with a byte order mark otherwise.
func encodePDFText(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
		return "(" + r.Replace(s) + ")"
	}
	var b strings.Builder
	b.WriteString("<FEFF")
	buf := make([]byte, 0, 4)
	for _, r := range s {
		if !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		for _, u := range utf16.Encode([]rune{r}) {
			buf = strconv.AppendUint(buf[:0], uint64(u)|0x10000, 16)
			b.Write(bytes.ToUpper(buf[1:]))
		}
	}
	b.WriteString(">")
	return b.String()
}
