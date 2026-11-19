package ebookmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// MOBI, AZW and AZW3 files are Palm databases. Record 0 holds a PalmDOC
// header, the MOBI header and an EXTH block with the metadata. Joint
// MOBI/KF8 files carry a second header record for the KF8 part, found
// through EXTH 121.

const (
	pdbHeaderLen = 78
	exthFlag     = 0x40

	exthAuthor      = 100
	exthPublisher   = 101
	exthDescription = 103
	exthISBN        = 104
	exthSubject     = 105
	exthPublished   = 106
	exthASIN        = 113
	exthKF8Boundary = 121
	exthCoverOffset = 201
	exthTitle       = 503
	exthLanguage    = 524

	noImage = 0xFFFFFFFF
)

type mobiHandler struct{}

func (mobiHandler) supports(field string) bool {
	switch field {
	case "title_sort", "author_sort", "series", "series_index", "rating":
		return false
	}
	return true
}

func (mobiHandler) identifierSchemes() []string { return []string{"asin", "isbn"} }

type exthRecord struct {
	typ  uint32
	data []byte
}

// mobiHeader is one parsed header record (record 0, or the KF8 one).
type mobiHeader struct {
	index      int // record index
	rec        []byte
	headerLen  int // MOBI header length, from offset 16
	encoding   uint32
	encrypted  bool
	exth       []exthRecord
	hasEXTH    bool
	exthEnd    int // end of the EXTH block, padded
	nameOff    int
	nameLen    int
	firstImage uint32
}

type mobiBook struct {
	src     io.ReaderAt
	size    int64
	offsets []int64 // record start offsets; offsets[n] == size
	headers []*mobiHeader
}

func openMOBI(src io.ReaderAt, size int64) (*mobiBook, error) {
	hdr := make([]byte, pdbHeaderLen)
	if _, err := src.ReadAt(hdr, 0); err != nil {
		return nil, fmt.Errorf("%w: short PDB header", ErrMalformed)
	}
	n := int(binary.BigEndian.Uint16(hdr[76:78]))
	if n == 0 {
		return nil, fmt.Errorf("%w: PDB has no records", ErrMalformed)
	}
	list := make([]byte, 8*n)
	if _, err := src.ReadAt(list, pdbHeaderLen); err != nil {
		return nil, fmt.Errorf("%w: short PDB record list", ErrMalformed)
	}
	b := &mobiBook{src: src, size: size, offsets: make([]int64, n+1)}
	prev := int64(pdbHeaderLen + 8*n)
	for i := range n {
		off := int64(binary.BigEndian.Uint32(list[8*i:]))
		if off < prev || off > size {
			return nil, fmt.Errorf("%w: PDB record %d has a bad offset", ErrMalformed, i)
		}
		b.offsets[i] = off
		prev = off
	}
	b.offsets[n] = size

	h, err := b.parseHeader(0)
	if err != nil {
		return nil, err
	}
	b.headers = append(b.headers, h)
	if v, ok := h.uint(exthKF8Boundary); ok && v != noImage {
		// The value names the KF8 header record; some writers point at the
		// BOUNDARY marker just before it.
		for _, idx := range []int{int(v), int(v) + 1} {
			if idx > 0 && idx < n {
				if kh, err := b.parseHeader(idx); err == nil {
					b.headers = append(b.headers, kh)
					break
				}
			}
		}
	}
	return b, nil
}

func (b *mobiBook) record(i int) ([]byte, error) {
	if i < 0 || i >= len(b.offsets)-1 {
		return nil, fmt.Errorf("%w: record %d out of range", ErrMalformed, i)
	}
	n := b.offsets[i+1] - b.offsets[i]
	if n > maxPartSize {
		return nil, fmt.Errorf("%w: record %d is too large", ErrMalformed, i)
	}
	buf := make([]byte, n)
	if _, err := b.src.ReadAt(buf, b.offsets[i]); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

func (b *mobiBook) parseHeader(i int) (*mobiHeader, error) {
	rec, err := b.record(i)
	if err != nil {
		return nil, err
	}
	if len(rec) < 132 || string(rec[16:20]) != "MOBI" {
		return nil, fmt.Errorf("%w: record %d has no MOBI header", ErrMalformed, i)
	}
	h := &mobiHeader{index: i, rec: rec}
	h.headerLen = int(binary.BigEndian.Uint32(rec[20:24]))
	if h.headerLen < 116 || 16+h.headerLen > len(rec) {
		return nil, fmt.Errorf("%w: bad MOBI header length", ErrMalformed)
	}
	h.encoding = binary.BigEndian.Uint32(rec[28:32])
	h.encrypted = binary.BigEndian.Uint16(rec[12:14]) != 0
	h.nameOff = int(binary.BigEndian.Uint32(rec[84:88]))
	h.nameLen = int(binary.BigEndian.Uint32(rec[88:92]))
	if h.nameOff > len(rec) || h.nameLen > len(rec)-h.nameOff {
		h.nameOff, h.nameLen = 0, 0
	}
	h.firstImage = binary.BigEndian.Uint32(rec[108:112])
	h.exthEnd = 16 + h.headerLen
	if binary.BigEndian.Uint32(rec[128:132])&exthFlag != 0 {
		start := 16 + h.headerLen
		if start+12 > len(rec) || string(rec[start:start+4]) != "EXTH" {
			return nil, fmt.Errorf("%w: EXTH flag set but no EXTH block", ErrMalformed)
		}
		length := int(binary.BigEndian.Uint32(rec[start+4:]))
		count := int(binary.BigEndian.Uint32(rec[start+8:]))
		if length < 12 || length > len(rec)-start {
			return nil, fmt.Errorf("%w: bad EXTH length", ErrMalformed)
		}
		p := start + 12
		for range count {
			if p+8 > start+length {
				return nil, fmt.Errorf("%w: EXTH record overruns its block", ErrMalformed)
			}
			typ := binary.BigEndian.Uint32(rec[p:])
			rl := int(binary.BigEndian.Uint32(rec[p+4:]))
			if rl < 8 || rl > start+length-p {
				return nil, fmt.Errorf("%w: bad EXTH record length", ErrMalformed)
			}
			h.exth = append(h.exth, exthRecord{typ, append([]byte(nil), rec[p+8:p+rl]...)})
			p += rl
		}
		h.hasEXTH = true
		h.exthEnd = start + (length+3)&^3
	}
	return h, nil
}

func (h *mobiHeader) uint(typ uint32) (uint32, bool) {
	for _, r := range h.exth {
		if r.typ == typ && len(r.data) == 4 {
			return binary.BigEndian.Uint32(r.data), true
		}
	}
	return 0, false
}

func (h *mobiHeader) decode(b []byte) string {
	if h.encoding == 1252 {
		return cp1252.decode(b)
	}
	return string(b)
}

func (h *mobiHeader) encode(s string) []byte {
	if h.encoding == 1252 {
		return cp1252.encode(s, func(rune) string { return "?" })
	}
	return []byte(s)
}

func (h *mobiHeader) strings(typ uint32) []string {
	var out []string
	for _, r := range h.exth {
		if r.typ == typ {
			out = append(out, h.decode(r.data))
		}
	}
	return out
}

func (h *mobiHeader) metadata() *Metadata {
	m := &Metadata{}
	firstStr := func(typ uint32) string {
		if s := h.strings(typ); len(s) > 0 {
			return s[0]
		}
		return ""
	}
	m.Title = firstStr(exthTitle)
	if m.Title == "" && h.nameLen > 0 {
		m.Title = h.decode(h.rec[h.nameOff : h.nameOff+h.nameLen])
	}
	for _, a := range h.strings(exthAuthor) {
		m.Authors = append(m.Authors, splitList(a, "&")...)
	}
	m.Publisher = firstStr(exthPublisher)
	m.Description = firstStr(exthDescription)
	for _, s := range h.strings(exthSubject) {
		m.Tags = append(m.Tags, splitList(s, ";")...)
	}
	m.Published = firstStr(exthPublished)
	m.Languages = h.strings(exthLanguage)
	if v := firstStr(exthISBN); v != "" {
		m.Identifiers = append(m.Identifiers, Identifier{"isbn", v})
	}
	if v := firstStr(exthASIN); v != "" {
		m.Identifiers = append(m.Identifiers, Identifier{"asin", v})
	}
	return m
}

// coverIndex returns the absolute record index of the cover image.
func (b *mobiBook) coverIndex() (int, bool) {
	for _, h := range b.headers {
		off, ok := h.uint(exthCoverOffset)
		if !ok || off == noImage || h.firstImage == noImage {
			continue
		}
		base := int(h.firstImage)
		if h.index > 0 {
			base += h.index // KF8 indices are relative to its header record
		}
		idx := base + int(off)
		if idx > 0 && idx < len(b.offsets)-1 {
			return idx, true
		}
	}
	return 0, false
}

func (mobiHandler) read(src io.ReaderAt, size int64) (*Metadata, error) {
	b, err := openMOBI(src, size)
	if err != nil {
		return nil, err
	}
	m := b.headers[0].metadata()
	if idx, ok := b.coverIndex(); ok {
		if rec, err := b.record(idx); err == nil {
			m.Cover = SniffImage(rec)
		}
	}
	return m, nil
}

func (mobiHandler) cover(src io.ReaderAt, size int64) (*Cover, error) {
	b, err := openMOBI(src, size)
	if err != nil {
		return nil, err
	}
	idx, ok := b.coverIndex()
	if !ok {
		return nil, ErrNoCover
	}
	rec, err := b.record(idx)
	if err != nil {
		return nil, err
	}
	mt := SniffImage(rec)
	if mt == "" {
		return nil, ErrNoCover
	}
	return &Cover{Data: rec, MediaType: mt}, nil
}

func (mobiHandler) write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error {
	b, err := openMOBI(src, size)
	if err != nil {
		return err
	}
	for _, h := range b.headers {
		if h.encrypted {
			return fmt.Errorf("%w: the book is DRM-protected", ErrEncrypted)
		}
	}
	next := b.headers[0].metadata()
	c.Apply(next)

	replaced := map[int][]byte{}
	for _, h := range b.headers {
		rec, err := h.rebuild(c, next)
		if err != nil {
			return err
		}
		replaced[h.index] = rec
	}
	if c.Cover != nil {
		idx, ok := b.coverIndex()
		if !ok {
			return fmt.Errorf("%w: this MOBI file has no cover to replace", ErrNoCover)
		}
		replaced[idx] = c.Cover.Data
	}
	name := make([]byte, 32)
	if _, err := src.ReadAt(name, 0); err != nil {
		return err
	}
	if c.Title != nil {
		name = pdbName(next.Title, name)
	}
	return b.writePDB(dst, name, replaced)
}

// rebuild returns the header record with the EXTH block and full name
// updated for c.
func (h *mobiHeader) rebuild(c *Changes, next *Metadata) ([]byte, error) {
	if h.nameOff != 0 && h.nameOff < h.exthEnd {
		return nil, fmt.Errorf("%w: unexpected record 0 layout", ErrMalformed)
	}
	tailStart := h.exthEnd
	if h.nameOff != 0 {
		tailStart = h.nameOff + h.nameLen
	}
	if tailStart > len(h.rec) {
		tailStart = len(h.rec)
	}
	for _, x := range h.rec[tailStart:] {
		if x != 0 {
			return nil, fmt.Errorf("%w: unrecognized data after the book title", ErrMalformed)
		}
	}

	drop := map[uint32]bool{}
	var add []exthRecord
	str := func(typ uint32, values ...string) {
		drop[typ] = true
		for _, v := range values {
			if v != "" {
				add = append(add, exthRecord{typ, h.encode(v)})
			}
		}
	}
	if c.Title != nil {
		str(exthTitle, next.Title)
	}
	if c.Authors != nil {
		str(exthAuthor, next.Authors...)
	}
	if c.Publisher != nil {
		str(exthPublisher, next.Publisher)
	}
	if c.Description != nil {
		str(exthDescription, next.Description)
	}
	if c.Tags != nil {
		str(exthSubject, next.Tags...)
	}
	if c.Published != nil {
		str(exthPublished, next.Published)
	}
	if c.Languages != nil {
		str(exthLanguage, next.Languages...)
	}
	if _, ok := c.Identifiers["isbn"]; ok {
		str(exthISBN, next.Identifier("isbn"))
	}
	if _, ok := c.Identifiers["asin"]; ok {
		str(exthASIN, next.Identifier("asin"))
	}
	var records []exthRecord
	for _, r := range h.exth {
		if !drop[r.typ] {
			records = append(records, r)
		}
	}
	records = append(records, add...)

	var exth bytes.Buffer
	exth.WriteString("EXTH")
	length := 12
	for _, r := range records {
		length += 8 + len(r.data)
	}
	_ = binary.Write(&exth, binary.BigEndian, uint32(length))
	_ = binary.Write(&exth, binary.BigEndian, uint32(len(records)))
	for _, r := range records {
		_ = binary.Write(&exth, binary.BigEndian, r.typ)
		_ = binary.Write(&exth, binary.BigEndian, uint32(8+len(r.data)))
		exth.Write(r.data)
	}
	for exth.Len()%4 != 0 {
		exth.WriteByte(0)
	}

	title := h.rec[h.nameOff : h.nameOff+h.nameLen]
	if c.Title != nil && next.Title != "" {
		title = h.encode(next.Title)
	}
	out := append([]byte(nil), h.rec[:16+h.headerLen]...)
	out = append(out, exth.Bytes()...)
	nameOff := len(out)
	out = append(out, title...)
	out = append(out, 0, 0)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	binary.BigEndian.PutUint32(out[84:], uint32(nameOff))
	binary.BigEndian.PutUint32(out[88:], uint32(len(title)))
	flags := binary.BigEndian.Uint32(out[128:])
	binary.BigEndian.PutUint32(out[128:], flags|exthFlag)
	// Keep the record size when the new header fits, so no later record
	// moves; readers that edit in place rely on the zero padding.
	if len(out) < len(h.rec) {
		out = append(out, make([]byte, len(h.rec)-len(out))...)
	}
	return out, nil
}

// pdbName is the 31-byte ASCII database name derived from a title.
func pdbName(title string, old []byte) []byte {
	var b []byte
	for _, r := range title {
		switch {
		case r == ' ':
			b = append(b, '_')
		case r > ' ' && r < 0x7f:
			b = append(b, byte(r))
		}
		if len(b) == 31 {
			break
		}
	}
	if len(b) == 0 {
		return old
	}
	name := make([]byte, 32)
	copy(name, b)
	return name
}

// writePDB writes the database with the records in replaced substituted
// and every record offset recomputed.
func (b *mobiBook) writePDB(dst io.Writer, name []byte, replaced map[int][]byte) error {
	n := len(b.offsets) - 1
	hdr := make([]byte, pdbHeaderLen+8*n)
	if _, err := b.src.ReadAt(hdr, 0); err != nil {
		return err
	}
	copy(hdr[:32], name)
	gap := make([]byte, b.offsets[0]-int64(len(hdr)))
	if _, err := b.src.ReadAt(gap, int64(len(hdr))); err != nil && len(gap) > 0 {
		return err
	}
	pos := int64(len(hdr) + len(gap))
	for i := range n {
		if pos > 0xFFFFFFFF {
			return fmt.Errorf("%w: file too large for a PDB", ErrMalformed)
		}
		binary.BigEndian.PutUint32(hdr[pdbHeaderLen+8*i:], uint32(pos))
		if r, ok := replaced[i]; ok {
			pos += int64(len(r))
		} else {
			pos += b.offsets[i+1] - b.offsets[i]
		}
	}
	if _, err := dst.Write(hdr); err != nil {
		return err
	}
	if _, err := dst.Write(gap); err != nil {
		return err
	}
	for i := range n {
		if r, ok := replaced[i]; ok {
			if _, err := dst.Write(r); err != nil {
				return err
			}
			continue
		}
		sec := io.NewSectionReader(b.src, b.offsets[i], b.offsets[i+1]-b.offsets[i])
		if _, err := io.Copy(dst, sec); err != nil {
			return err
		}
	}
	return nil
}
