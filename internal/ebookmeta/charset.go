package ebookmeta

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// charmap maps the bytes 0x80-0xFF of a single-byte encoding to runes;
// 0xFFFD marks an unassigned byte. Bytes below 0x80 are ASCII.
type charmap [128]rune

var cp1251 = func() *charmap {
	var m charmap
	copy(m[:64], []rune{
		0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021, 0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
		0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0xFFFD, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
		0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7, 0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
		0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7, 0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
	})
	for i := 0; i < 64; i++ {
		m[64+i] = rune(0x0410 + i) // А..я
	}
	return &m
}()

var cp1252 = func() *charmap {
	var m charmap
	copy(m[:32], []rune{
		0x20AC, 0xFFFD, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0xFFFD, 0x017D, 0xFFFD,
		0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0xFFFD, 0x017E, 0x0178,
	})
	for i := 32; i < 128; i++ {
		m[i] = rune(0x80 + i)
	}
	return &m
}()

var latin1 = func() *charmap {
	var m charmap
	for i := range m {
		m[i] = rune(0x80 + i)
	}
	return &m
}()

// koi8r covers the Cyrillic letters; the box-drawing block 0x80-0xBF is
// left unassigned apart from Ё/ё, which metadata does not need.
var koi8r = func() *charmap {
	var m charmap
	for i := 0; i < 64; i++ {
		m[i] = 0xFFFD
	}
	m[0xA3-0x80], m[0xB3-0x80] = 'ё', 'Ё'
	m[0xA0-0x80] = 0x2550
	lower := []rune("юабцдефгхийклмнопярстужвьызшэщчъ")
	upper := []rune("ЮАБЦДЕФГХИЙКЛМНОПЯРСТУЖВЬЫЗШЭЩЧЪ")
	copy(m[64:96], lower)
	copy(m[96:128], upper)
	return &m
}()

func lookupCharmap(label string) (*charmap, bool) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "windows-1251", "cp1251", "x-cp1251":
		return cp1251, true
	case "windows-1252", "cp1252":
		return cp1252, true
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1", "l1":
		return latin1, true
	case "koi8-r", "koi8r":
		return koi8r, true
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return nil, true
	}
	return nil, false
}

func (m *charmap) decode(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else {
			sb.WriteRune(m[c-0x80])
		}
	}
	return sb.String()
}

// encode converts s to the single-byte encoding. Runes it cannot hold are
// written with fallback, for example as an XML character reference.
func (m *charmap) encode(s string, fallback func(rune) string) []byte {
	var out []byte
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
			continue
		}
		found := false
		for i, mr := range m {
			if mr == r && r != 0xFFFD {
				out = append(out, byte(0x80+i))
				found = true
				break
			}
		}
		if !found {
			out = append(out, fallback(r)...)
		}
	}
	return out
}

// charsetReader is an xml.Decoder CharsetReader for the encodings old FB2
// and OPF files use, so the package needs no golang.org/x/text.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	m, ok := lookupCharmap(label)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported character encoding %q", ErrMalformed, label)
	}
	if m == nil {
		return input, nil
	}
	return &charmapReader{m: m, r: bufio.NewReader(input)}, nil
}

type charmapReader struct {
	m       *charmap
	r       *bufio.Reader
	pending []byte
}

func (c *charmapReader) Read(p []byte) (int, error) {
	for len(c.pending) == 0 {
		b, err := c.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b < 0x80 {
			c.pending = append(c.pending, b)
		} else {
			c.pending = utf8.AppendRune(c.pending, c.m[b-0x80])
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

var xmlEncodingRe = regexp.MustCompile(`^(?:\xef\xbb\xbf)?\s*<\?xml[^>]*?encoding\s*=\s*["']([A-Za-z0-9._-]+)["']`)

// xmlToUTF8 converts an XML document in a single-byte encoding to UTF-8
// and rewrites its declaration to match. Edits are byte splices at decoder
// offsets, and those offsets count decoded bytes, so a document must be
// UTF-8 before it is indexed. UTF-8 documents are returned unchanged.
func xmlToUTF8(data []byte) ([]byte, error) {
	loc := xmlEncodingRe.FindSubmatchIndex(data[:min(len(data), 512)])
	if loc == nil {
		return data, nil
	}
	label := string(data[loc[2]:loc[3]])
	cm, ok := lookupCharmap(label)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported character encoding %q", ErrMalformed, label)
	}
	if cm == nil {
		return data, nil
	}
	// The declaration is ASCII, so its offsets survive decoding.
	out := []byte(cm.decode(data[loc[3]:]))
	return append(append(append([]byte{}, data[:loc[2]]...), "utf-8"...), out...), nil
}

// xmlCharRef is an encode fallback that writes r as an XML reference.
func xmlCharRef(r rune) string { return "&#" + strconv.Itoa(int(r)) + ";" }
