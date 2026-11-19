package ebookmeta

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

type mobiSpec struct {
	encoding uint32
	title    string
	exth     []exthRecord
	pad      int  // zero bytes after the title
	joint    bool // add a KF8 header record found through EXTH 121
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// headerRecord builds a PalmDOC + MOBI header record.
func headerRecord(s mobiSpec, exth []exthRecord, firstImage uint32) []byte {
	const headerLen = 232
	rec := make([]byte, 16+headerLen)
	copy(rec[16:], "MOBI")
	binary.BigEndian.PutUint32(rec[20:], headerLen)
	binary.BigEndian.PutUint32(rec[28:], s.encoding)
	binary.BigEndian.PutUint32(rec[108:], firstImage)
	var block bytes.Buffer
	if exth != nil {
		binary.BigEndian.PutUint32(rec[128:], exthFlag)
		length := 12
		for _, r := range exth {
			length += 8 + len(r.data)
		}
		block.WriteString("EXTH")
		block.Write(u32(uint32(length)))
		block.Write(u32(uint32(len(exth))))
		for _, r := range exth {
			block.Write(u32(r.typ))
			block.Write(u32(uint32(8 + len(r.data))))
			block.Write(r.data)
		}
		for block.Len()%4 != 0 {
			block.WriteByte(0)
		}
	}
	rec = append(rec, block.Bytes()...)
	binary.BigEndian.PutUint32(rec[84:], uint32(len(rec)))
	binary.BigEndian.PutUint32(rec[88:], uint32(len(s.title)))
	rec = append(rec, s.title...)
	return append(rec, make([]byte, s.pad)...)
}

// buildMOBI lays out: header, text, cover image, [boundary, KF8 header], EOF.
func buildMOBI(s mobiSpec) []byte {
	exth := append([]exthRecord{{exthCoverOffset, u32(0)}}, s.exth...)
	records := [][]byte{nil, []byte("text record"), jpegData}
	if s.joint {
		exth = append(exth, exthRecord{exthKF8Boundary, u32(4)})
		records = append(records, []byte("BOUNDARY"), headerRecord(s, s.exth, noImage))
	}
	records[0] = headerRecord(s, exth, 2)
	records = append(records, []byte{0xe9, 0x8e, 0x0d, 0x0a})

	n := len(records)
	out := make([]byte, pdbHeaderLen+8*n+2)
	copy(out, "Old_Name")
	copy(out[60:], "BOOKMOBI")
	binary.BigEndian.PutUint16(out[76:], uint16(n))
	off := len(out)
	for i, r := range records {
		binary.BigEndian.PutUint32(out[pdbHeaderLen+8*i:], uint32(off))
		binary.BigEndian.PutUint32(out[pdbHeaderLen+8*i+4:], uint32(2*i))
		off += len(r)
	}
	for _, r := range records {
		out = append(out, r...)
	}
	return out
}

func strRec(typ uint32, s string) exthRecord { return exthRecord{typ, []byte(s)} }

func TestReadMOBI(t *testing.T) {
	data := buildMOBI(mobiSpec{encoding: 65001, title: "Full Name", pad: 64, exth: []exthRecord{
		strRec(exthAuthor, "Ann One & Bob Two"), strRec(exthPublisher, "Pub"), strRec(exthSubject, "x; y"),
		strRec(exthISBN, "9780000000002"), strRec(exthASIN, "B000000000"), strRec(exthLanguage, "en"),
		strRec(exthPublished, "2001-01-01"), strRec(exthDescription, "About."), strRec(999, "keep"),
	}})
	p := writeTemp(t, "b.azw3", data)
	m, format, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := &Metadata{
		Title: "Full Name", Authors: []string{"Ann One", "Bob Two"}, Publisher: "Pub", Languages: []string{"en"},
		Tags: []string{"x", "y"}, Identifiers: []Identifier{{"asin", "B000000000"}, {"isbn", "9780000000002"}},
		Published: "2001-01-01", Description: "About.", Cover: "image/jpeg",
	}
	if format != FormatMOBI || !reflect.DeepEqual(m, want) {
		t.Fatalf("%s\ngot  %+v\nwant %+v", format, m, want)
	}
	cov, err := CoverFile(p)
	if err != nil || !bytes.Equal(cov.Data, jpegData) {
		t.Fatalf("cover %v %v", cov, err)
	}
}

func TestWriteMOBI(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec mobiSpec
	}{
		{"utf8 in place", mobiSpec{encoding: 65001, title: "Old", pad: 4096, exth: []exthRecord{strRec(999, "keep")}}},
		{"cp1252 grows", mobiSpec{encoding: 1252, title: "Old", exth: []exthRecord{strRec(999, "keep")}}},
		{"joint", mobiSpec{encoding: 65001, title: "Old", pad: 8, joint: true, exth: []exthRecord{strRec(999, "keep")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := buildMOBI(tc.spec)
			p := writeTemp(t, "b.mobi", orig)
			c := fullChanges()
			c.Cover = &Cover{Data: pngData}
			c.Title = new("Café Title")
			DropUnsupported(FormatMOBI, c)
			if err := WriteFile(p, c); err != nil {
				t.Fatal(err)
			}
			m, _, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			want := &Metadata{
				Title: "Café Title", Authors: []string{"Alice Ash", "Bob Birch"}, Publisher: "Other House",
				Languages: []string{"de", "en"}, Tags: []string{"One", "Two"},
				Identifiers: []Identifier{{"asin", "B000000000"}, {"isbn", "9781111111113"}},
				Published:   "2024-03-04", Description: "Line one.\nLine two.", Cover: "image/png",
			}
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("got  %+v\nwant %+v", m, want)
			}
			got, _ := os.ReadFile(p)
			b, err := openMOBI(bytes.NewReader(got), int64(len(got)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got[:10]) != "Caf_Title\x00" {
				t.Errorf("PDB name = %q", got[:10])
			}
			text, _ := b.record(1)
			if string(text) != "text record" {
				t.Errorf("text record = %q", text)
			}
			for _, h := range b.headers {
				if v := h.strings(999); len(v) != 1 || v[0] != "keep" {
					t.Errorf("header %d lost an unknown EXTH record", h.index)
				}
				if h.metadata().Title != "Café Title" {
					t.Errorf("header %d title = %q", h.index, h.metadata().Title)
				}
			}
			if tc.spec.joint && len(b.headers) != 2 {
				t.Fatalf("KF8 header not found after write")
			}
			if tc.spec.pad == 4096 {
				if len(got) != len(orig)-len(jpegData)+len(pngData) {
					t.Errorf("record 0 changed size: %d -> %d", len(orig), len(got))
				}
			}
		})
	}
}

func TestWriteMOBIRefusesDRM(t *testing.T) {
	data := buildMOBI(mobiSpec{encoding: 65001, title: "T"})
	rec0 := int(binary.BigEndian.Uint32(data[pdbHeaderLen:]))
	binary.BigEndian.PutUint16(data[rec0+12:], 2)
	p := writeTemp(t, "b.mobi", data)
	if _, _, err := ReadFile(p); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := WriteFile(p, &Changes{Title: new("X")}); err == nil {
		t.Fatal("expected ErrEncrypted")
	}
}

func FuzzReadMOBI(f *testing.F) {
	f.Add(buildMOBI(mobiSpec{encoding: 65001, title: "T", exth: []exthRecord{strRec(exthAuthor, "A")}}))
	f.Add(buildMOBI(mobiSpec{encoding: 1252, title: "T", joint: true}))
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		if _, err := (mobiHandler{}).read(r, int64(len(data))); err != nil {
			return
		}
		var out bytes.Buffer
		c := fullChanges()
		c.Cover.MediaType = "image/jpeg"
		_ = mobiHandler{}.write(r, int64(len(data)), &out, c)
	})
}
