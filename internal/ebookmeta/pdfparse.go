package ebookmeta

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// A minimal reader for the PDF object syntax (ISO 32000-1, 7.3), enough to
// locate the document information dictionary through classic or stream
// cross-reference sections and object streams.

type (
	pdfName string
	pdfRef  struct{ num, gen int }
	pdfDict struct {
		keys []pdfName
		vals map[pdfName]any
		raw  map[pdfName][]byte // source bytes of each value
	}
	pdfStream struct {
		dict      *pdfDict
		dataStart int64 // file offset of the first data byte; -1 inside an object stream
	}
)

func (d *pdfDict) get(k pdfName) any { return d.vals[k] }

// errShort means the parser ran out of buffered bytes before the end of
// the file; the caller retries with a larger window.
var errShort = errors.New("pdf: need more data")

const maxPDFDepth = 64

type pdfParser struct {
	b    []byte
	pos  int
	full bool // b ends at the end of the file
}

func isPDFSpace(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func isPDFDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func (p *pdfParser) short() error {
	if p.full {
		return fmt.Errorf("%w: unexpected end of PDF data", ErrMalformed)
	}
	return errShort
}

func (p *pdfParser) skipSpace() {
	for p.pos < len(p.b) {
		c := p.b[p.pos]
		switch {
		case isPDFSpace(c):
			p.pos++
		case c == '%':
			for p.pos < len(p.b) && p.b[p.pos] != '\n' && p.b[p.pos] != '\r' {
				p.pos++
			}
		default:
			return
		}
	}
}

// keyword reads a regular-character token such as obj, R or 123.
func (p *pdfParser) token() (string, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.b) && !isPDFSpace(p.b[p.pos]) && !isPDFDelim(p.b[p.pos]) {
		p.pos++
	}
	if p.pos == len(p.b) && !p.full {
		return "", errShort
	}
	if start == p.pos {
		if p.pos >= len(p.b) {
			return "", p.short()
		}
		return "", fmt.Errorf("%w: expected a token at %d", ErrMalformed, start)
	}
	return string(p.b[start:p.pos]), nil
}

func (p *pdfParser) expect(kw string) error {
	t, err := p.token()
	if err != nil {
		return err
	}
	if t != kw {
		return fmt.Errorf("%w: expected %q, found %q", ErrMalformed, kw, t)
	}
	return nil
}

func (p *pdfParser) int() (int64, error) {
	t, err := p.token()
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: expected an integer, found %q", ErrMalformed, t)
	}
	return v, nil
}

func (p *pdfParser) object(depth int) (any, error) {
	if depth > maxPDFDepth {
		return nil, fmt.Errorf("%w: PDF objects nested too deeply", ErrMalformed)
	}
	p.skipSpace()
	if p.pos >= len(p.b) {
		return nil, p.short()
	}
	switch c := p.b[p.pos]; {
	case c == '/':
		return p.name()
	case c == '(':
		return p.literal()
	case c == '<':
		if p.pos+1 >= len(p.b) {
			return nil, p.short()
		}
		if p.b[p.pos+1] == '<' {
			return p.dict(depth)
		}
		return p.hex()
	case c == '[':
		p.pos++
		var arr []any
		for {
			p.skipSpace()
			if p.pos >= len(p.b) {
				return nil, p.short()
			}
			if p.b[p.pos] == ']' {
				p.pos++
				return arr, nil
			}
			v, err := p.object(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
	}
	t, err := p.token()
	if err != nil {
		return nil, err
	}
	switch t {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		// Look ahead for "gen R".
		save := p.pos
		g, err := p.int()
		if errors.Is(err, errShort) {
			return nil, err
		}
		if err == nil && n >= 0 && g >= 0 && g < 65536 && n < 1<<31 {
			r, err := p.token()
			if errors.Is(err, errShort) {
				return nil, err
			}
			if err == nil && r == "R" {
				return pdfRef{int(n), int(g)}, nil
			}
		}
		p.pos = save
		return n, nil
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("%w: unexpected PDF token %q", ErrMalformed, t)
}

func (p *pdfParser) name() (pdfName, error) {
	p.pos++ // '/'
	var b []byte
	for p.pos < len(p.b) && !isPDFSpace(p.b[p.pos]) && !isPDFDelim(p.b[p.pos]) {
		c := p.b[p.pos]
		if c == '#' && p.pos+2 < len(p.b) {
			if v, err := strconv.ParseUint(string(p.b[p.pos+1:p.pos+3]), 16, 8); err == nil {
				b = append(b, byte(v))
				p.pos += 3
				continue
			}
		}
		b = append(b, c)
		p.pos++
	}
	if p.pos >= len(p.b) && !p.full {
		return "", errShort
	}
	return pdfName(b), nil
}

func (p *pdfParser) literal() ([]byte, error) {
	p.pos++ // '('
	var out []byte
	depth := 1
	for {
		if p.pos >= len(p.b) {
			return nil, p.short()
		}
		c := p.b[p.pos]
		p.pos++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return out, nil
			}
		case '\\':
			if p.pos >= len(p.b) {
				return nil, p.short()
			}
			e := p.b[p.pos]
			p.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if p.pos < len(p.b) && p.b[p.pos] == '\n' {
					p.pos++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for i := 0; i < 2 && p.pos < len(p.b) && p.b[p.pos] >= '0' && p.b[p.pos] <= '7'; i++ {
						v = v*8 + int(p.b[p.pos]-'0')
						p.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
			continue
		}
		out = append(out, c)
	}
}

func (p *pdfParser) hex() ([]byte, error) {
	p.pos++ // '<'
	var digits []byte
	for {
		if p.pos >= len(p.b) {
			return nil, p.short()
		}
		c := p.b[p.pos]
		p.pos++
		if c == '>' {
			break
		}
		if isPDFSpace(c) {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return nil, fmt.Errorf("%w: bad hex string", ErrMalformed)
		}
		digits = append(digits, c)
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	out := make([]byte, len(digits)/2)
	for i := range out {
		v, _ := strconv.ParseUint(string(digits[2*i:2*i+2]), 16, 8)
		out[i] = byte(v)
	}
	return out, nil
}

func (p *pdfParser) dict(depth int) (*pdfDict, error) {
	p.pos += 2 // "<<"
	d := &pdfDict{vals: map[pdfName]any{}, raw: map[pdfName][]byte{}}
	for {
		p.skipSpace()
		if p.pos+1 >= len(p.b) {
			return nil, p.short()
		}
		if p.b[p.pos] == '>' && p.b[p.pos+1] == '>' {
			p.pos += 2
			return d, nil
		}
		if p.b[p.pos] != '/' {
			return nil, fmt.Errorf("%w: dictionary key is not a name", ErrMalformed)
		}
		k, err := p.name()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		start := p.pos
		v, err := p.object(depth + 1)
		if err != nil {
			return nil, err
		}
		if _, dup := d.vals[k]; !dup {
			d.keys = append(d.keys, k)
		}
		d.vals[k] = v
		d.raw[k] = append([]byte(nil), p.b[start:p.pos]...)
	}
}

// pdfFile reads objects on demand through an io.ReaderAt.
type pdfFile struct {
	src      io.ReaderAt
	size     int64
	xref     map[int]xrefEntry
	trailer  *pdfDict
	start    int64 // the last startxref value
	isStream bool  // the section at start is a cross-reference stream
	objstms  map[int]*objStream
	resolves int
}

type xrefEntry struct {
	typ      byte // 0 free, 1 in file, 2 in an object stream
	off      int64
	stm, idx int
}

// parseAt runs fn on a growing window of the file starting at off.
func (f *pdfFile) parseAt(off int64, fn func(p *pdfParser) error) error {
	if off < 0 || off >= f.size {
		return fmt.Errorf("%w: PDF offset %d out of range", ErrMalformed, off)
	}
	for win := int64(64 << 10); ; win *= 4 {
		n := min(win, f.size-off)
		buf := make([]byte, n)
		if _, err := f.src.ReadAt(buf, off); err != nil && err != io.EOF {
			return err
		}
		p := &pdfParser{b: buf, full: off+n == f.size}
		err := fn(p)
		if !errors.Is(err, errShort) {
			return err
		}
		if p.full || n >= maxPartSize {
			return fmt.Errorf("%w: PDF object too large", ErrMalformed)
		}
	}
}

// indirect parses "num gen obj <object> [stream]" at off.
func (f *pdfFile) indirect(off int64) (num int, obj any, err error) {
	err = f.parseAt(off, func(p *pdfParser) error {
		n, err := p.int()
		if err != nil {
			return err
		}
		// The same bound as an "n g R" reference, so the int below cannot wrap.
		if n < 0 || n >= 1<<31 {
			return fmt.Errorf("%w: bad object number %d", ErrMalformed, n)
		}
		if _, err := p.int(); err != nil {
			return err
		}
		if err := p.expect("obj"); err != nil {
			return err
		}
		v, err := p.object(0)
		if err != nil {
			return err
		}
		num, obj = int(n), v
		d, ok := v.(*pdfDict)
		if !ok {
			return nil
		}
		save := p.pos
		if t, err := p.token(); err != nil || t != "stream" {
			if errors.Is(err, errShort) {
				return err
			}
			p.pos = save
			return nil
		}
		if p.pos < len(p.b) && p.b[p.pos] == '\r' {
			p.pos++
		}
		if p.pos < len(p.b) && p.b[p.pos] == '\n' {
			p.pos++
		}
		obj = &pdfStream{dict: d, dataStart: off + int64(p.pos)}
		return nil
	})
	return num, obj, err
}

func openPDF(src io.ReaderAt, size int64) (*pdfFile, error) {
	f := &pdfFile{src: src, size: size, xref: map[int]xrefEntry{}, objstms: map[int]*objStream{}}
	tail := min(size, 2048)
	buf := make([]byte, tail)
	if _, err := src.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return nil, err
	}
	i := bytes.LastIndex(buf, []byte("startxref"))
	if i < 0 {
		return nil, fmt.Errorf("%w: PDF has no startxref", ErrMalformed)
	}
	p := &pdfParser{b: buf[i+len("startxref"):], full: true}
	start, err := p.int()
	if err != nil {
		return nil, err
	}
	f.start = start
	seen := map[int64]bool{}
	var load func(off int64, first bool) error
	load = func(off int64, first bool) error {
		if seen[off] || len(seen) > 1000 {
			return nil
		}
		seen[off] = true
		trailer, isStream, err := f.section(off)
		if err != nil {
			return err
		}
		if first {
			f.trailer, f.isStream = trailer, isStream
		}
		if v, ok := trailer.get("XRefStm").(int64); ok {
			if err := load(v, false); err != nil {
				return err
			}
		}
		if v, ok := trailer.get("Prev").(int64); ok {
			return load(v, false)
		}
		return nil
	}
	if err := load(start, true); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *pdfFile) add(num int, e xrefEntry) {
	if _, ok := f.xref[num]; !ok {
		f.xref[num] = e
	}
}

// section loads one cross-reference section and returns its trailer.
func (f *pdfFile) section(off int64) (*pdfDict, bool, error) {
	var trailer *pdfDict
	isTable := false
	err := f.parseAt(off, func(p *pdfParser) error {
		p.skipSpace()
		if !bytes.HasPrefix(p.b[p.pos:], []byte("xref")) {
			return nil
		}
		isTable = true
		p.pos += 4
		entries := map[int]xrefEntry{}
		var order []int
		for {
			t, err := p.token()
			if err != nil {
				return err
			}
			if t == "trailer" {
				break
			}
			first, err := strconv.Atoi(t)
			if err != nil {
				return fmt.Errorf("%w: bad xref subsection", ErrMalformed)
			}
			count, err := p.int()
			if err != nil || count < 0 || count > 10_000_000 {
				return fmt.Errorf("%w: bad xref subsection", ErrMalformed)
			}
			for j := range int(count) {
				o, err := p.int()
				if err != nil {
					return err
				}
				// The generation is read only to advance past it: nothing uses
				// the value, so any integer is accepted. A value that is not an
				// integer still fails, as it did before.
				if _, err := p.int(); err != nil {
					return err
				}
				kind, err := p.token()
				if err != nil {
					return err
				}
				e := xrefEntry{off: o}
				if kind == "n" {
					e.typ = 1
				}
				if _, dup := entries[first+j]; !dup {
					order = append(order, first+j)
				}
				entries[first+j] = e
			}
		}
		d, err := p.object(0)
		if err != nil {
			return err
		}
		td, ok := d.(*pdfDict)
		if !ok {
			return fmt.Errorf("%w: trailer is not a dictionary", ErrMalformed)
		}
		for _, n := range order {
			f.add(n, entries[n])
		}
		trailer = td
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if isTable {
		return trailer, false, nil
	}
	_, obj, err := f.indirect(off)
	if err != nil {
		return nil, false, err
	}
	s, ok := obj.(*pdfStream)
	if !ok || s.dict.get("Type") != pdfName("XRef") {
		return nil, false, fmt.Errorf("%w: startxref does not point to a cross-reference section", ErrMalformed)
	}
	if err := f.loadXrefStream(s); err != nil {
		return nil, false, err
	}
	return s.dict, true, nil
}

func (f *pdfFile) loadXrefStream(s *pdfStream) error {
	data, err := f.streamData(s)
	if err != nil {
		return err
	}
	wArr, _ := s.dict.get("W").([]any)
	if len(wArr) != 3 {
		return fmt.Errorf("%w: bad /W in xref stream", ErrMalformed)
	}
	var w [3]int
	for i, v := range wArr {
		n, ok := v.(int64)
		if !ok || n < 0 || n > 8 {
			return fmt.Errorf("%w: bad /W in xref stream", ErrMalformed)
		}
		w[i] = int(n)
	}
	size, _ := s.dict.get("Size").(int64)
	index := []int64{0, size}
	if arr, ok := s.dict.get("Index").([]any); ok {
		index = index[:0]
		for _, v := range arr {
			n, ok := v.(int64)
			if !ok {
				return fmt.Errorf("%w: bad /Index in xref stream", ErrMalformed)
			}
			index = append(index, n)
		}
	}
	field := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}
	rowLen := w[0] + w[1] + w[2]
	if rowLen == 0 {
		return fmt.Errorf("%w: empty xref stream rows", ErrMalformed)
	}
	pos := 0
	for i := 0; i+1 < len(index); i += 2 {
		first, count := index[i], index[i+1]
		if first < 0 || count < 0 || count > int64(len(data)/rowLen)+1 {
			return fmt.Errorf("%w: bad /Index in xref stream", ErrMalformed)
		}
		for j := range count {
			if pos+rowLen > len(data) {
				return nil // truncated; keep what was read
			}
			row := data[pos : pos+rowLen]
			pos += rowLen
			typ := int64(1)
			if w[0] > 0 {
				typ = field(row[:w[0]])
			}
			a, b := field(row[w[0]:w[0]+w[1]]), field(row[w[0]+w[1]:])
			num := int(first + j)
			switch typ {
			case 0:
				f.add(num, xrefEntry{typ: 0})
			case 1:
				f.add(num, xrefEntry{typ: 1, off: a})
			case 2:
				f.add(num, xrefEntry{typ: 2, stm: int(a), idx: int(b)})
			}
		}
	}
	return nil
}

// resolve follows references.
func (f *pdfFile) resolve(v any) (any, error) {
	for range 32 {
		r, ok := v.(pdfRef)
		if !ok {
			return v, nil
		}
		var err error
		if v, err = f.object(r.num); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: reference chain too long", ErrMalformed)
}

func (f *pdfFile) object(num int) (any, error) {
	f.resolves++
	if f.resolves > 10000 {
		return nil, fmt.Errorf("%w: too many object lookups", ErrMalformed)
	}
	e, ok := f.xref[num]
	if !ok || e.typ == 0 {
		return nil, nil // missing objects are null
	}
	if e.typ == 1 {
		got, obj, err := f.indirect(e.off)
		if err != nil {
			return nil, err
		}
		if got != num {
			return nil, fmt.Errorf("%w: xref entry %d points at object %d", ErrMalformed, num, got)
		}
		return obj, nil
	}
	ostm, ok := f.objstms[e.stm]
	if !ok {
		v, err := f.object(e.stm)
		if err != nil {
			return nil, err
		}
		s, ok := v.(*pdfStream)
		if !ok || s.dataStart < 0 {
			return nil, fmt.Errorf("%w: object stream %d missing", ErrMalformed, e.stm)
		}
		data, err := f.streamData(s)
		if err != nil {
			return nil, err
		}
		n, _ := s.dict.get("N").(int64)
		first, _ := s.dict.get("First").(int64)
		ostm = &objStream{data: data, n: n, first: first}
		f.objstms[e.stm] = ostm
	}
	return ostm.object(num, e.idx)
}

// objStream is a decoded object stream (ISO 32000-1, 7.5.7).
type objStream struct {
	data     []byte
	n, first int64
}

func (s *objStream) object(num, idx int) (any, error) {
	data, n, first := s.data, s.n, s.first
	if n <= 0 || first < 0 || first > int64(len(data)) || idx < 0 || int64(idx) >= n {
		return nil, fmt.Errorf("%w: bad object stream header", ErrMalformed)
	}
	p := &pdfParser{b: data[:first], full: true}
	for i := int64(0); i < n; i++ {
		on, err := p.int()
		if err != nil {
			return nil, err
		}
		off, err := p.int()
		if err != nil {
			return nil, err
		}
		if i != int64(idx) {
			continue
		}
		// first is within data (checked above), so this subtraction cannot wrap,
		// unlike first+off for an off near the top of int64.
		if on != int64(num) || off < 0 || off > int64(len(data))-first {
			return nil, fmt.Errorf("%w: object stream index mismatch", ErrMalformed)
		}
		q := &pdfParser{b: data[first+off:], full: true}
		return q.object(0)
	}
	return nil, fmt.Errorf("%w: object %d not in its stream", ErrMalformed, num)
}

// streamData reads and decodes a stream. Only FlateDecode (with optional
// PNG predictors) is supported, which covers xref and object streams.
func (f *pdfFile) streamData(s *pdfStream) ([]byte, error) {
	lv, err := f.resolve(s.dict.get("Length"))
	if err != nil {
		return nil, err
	}
	length, ok := lv.(int64)
	if !ok || length < 0 || length > maxPartSize || s.dataStart+length > f.size {
		return nil, fmt.Errorf("%w: bad stream length", ErrMalformed)
	}
	raw := make([]byte, length)
	if _, err := f.src.ReadAt(raw, s.dataStart); err != nil && err != io.EOF {
		return nil, err
	}
	filter := s.dict.get("Filter")
	if arr, ok := filter.([]any); ok {
		if len(arr) > 1 {
			return nil, fmt.Errorf("%w: chained stream filters", ErrMalformed)
		}
		if len(arr) == 1 {
			filter = arr[0]
		} else {
			filter = nil
		}
	}
	switch filter {
	case nil:
		return raw, nil
	case pdfName("FlateDecode"):
	default:
		return nil, fmt.Errorf("%w: unsupported stream filter %v", ErrMalformed, filter)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	data, err := io.ReadAll(io.LimitReader(zr, maxPartSize+1))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(data) > maxPartSize {
		return nil, fmt.Errorf("%w: stream too large", ErrMalformed)
	}
	parms := s.dict.get("DecodeParms")
	if arr, ok := parms.([]any); ok && len(arr) == 1 {
		parms = arr[0]
	}
	if pd, ok := parms.(*pdfDict); ok {
		pred, _ := pd.get("Predictor").(int64)
		if pred >= 10 {
			cols, _ := pd.get("Columns").(int64)
			if cols <= 0 {
				cols = 1
			}
			return pngUnpredict(data, int(cols))
		} else if pred > 1 {
			return nil, fmt.Errorf("%w: unsupported predictor %d", ErrMalformed, pred)
		}
	}
	return data, nil
}

// pngUnpredict reverses PNG row filters for 8-bit, single-component rows.
func pngUnpredict(data []byte, cols int) ([]byte, error) {
	if cols > maxPartSize {
		return nil, fmt.Errorf("%w: bad predictor columns", ErrMalformed)
	}
	row := cols + 1
	out := make([]byte, 0, len(data))
	prev := make([]byte, cols)
	for i := 0; i+row <= len(data); i += row {
		ft, cur := data[i], append([]byte(nil), data[i+1:i+row]...)
		for j := range cur {
			var left, up, ul byte
			if j > 0 {
				left, ul = cur[j-1], prev[j-1]
			}
			up = prev[j]
			switch ft {
			case 1:
				cur[j] += left
			case 2:
				cur[j] += up
			case 3:
				cur[j] += byte((int(left) + int(up)) / 2)
			case 4:
				cur[j] += paeth(left, up, ul)
			case 0:
			default:
				return nil, fmt.Errorf("%w: bad PNG predictor %d", ErrMalformed, ft)
			}
		}
		out = append(out, cur...)
		prev = cur
	}
	return out, nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
