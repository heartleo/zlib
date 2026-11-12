package ebookmeta

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

const pdfInfo = `<</Title (Old \(PDF\) Title)/Author <FEFF004A00F6006E0020004100200026002000420065006E>/Producer (Example Writer 1.0)/Keywords (a; b)>>`

// classicPDF builds a file with a cross-reference table.
func classicPDF(info string, extraTrailer string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	objs := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[]/Count 0>>",
	}
	if info != "" {
		objs = append(objs, info)
	}
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", o)
	}
	infoRef := ""
	if info != "" {
		infoRef = "/Info 3 0 R"
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 1 0 R%s/ID[<0102><0102>]%s>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, infoRef, extraTrailer, xref)
	return b.Bytes()
}

func deflate(data []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write(data)
	w.Close()
	return b.Bytes()
}

// streamPDF builds a PDF 1.5 file whose Info dictionary sits in an object
// stream, indexed by a cross-reference stream with a PNG Up predictor.
func streamPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n")
	off1 := b.Len()
	b.WriteString("1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n")
	off2 := b.Len()
	b.WriteString("2 0 obj\n<</Type/Pages/Kids[]/Count 0>>\nendobj\n")
	// Object stream 3 holds object 4 (Info).
	header := "4 0 "
	content := header + pdfInfo
	data := deflate([]byte(content))
	off3 := b.Len()
	fmt.Fprintf(&b, "3 0 obj\n<</Type/ObjStm/N 1/First %d/Filter/FlateDecode/Length %d>>\nstream\n", len(header), len(data))
	b.Write(data)
	b.WriteString("\nendstream\nendobj\n")
	off5 := b.Len()
	rows := [][]byte{
		{0, 0, 0, 0xff},
		{1, byte(off1 >> 8), byte(off1), 0},
		{1, byte(off2 >> 8), byte(off2), 0},
		{1, byte(off3 >> 8), byte(off3), 0},
		{2, 0, 3, 0},
		{1, byte(off5 >> 8), byte(off5), 0},
	}
	var pred []byte
	prev := make([]byte, 4)
	for _, r := range rows {
		pred = append(pred, 2)
		for i := range r {
			pred = append(pred, r[i]-prev[i])
		}
		prev = r
	}
	xs := deflate(pred)
	fmt.Fprintf(&b, "5 0 obj\n<</Type/XRef/Size 6/W[1 2 1]/Root 1 0 R/Info 4 0 R/ID[<0a><0a>]/Filter/FlateDecode/DecodeParms<</Predictor 12/Columns 4>>/Length %d>>\nstream\n", len(xs))
	b.Write(xs)
	fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF", off5)
	return b.Bytes()
}

func TestReadPDF(t *testing.T) {
	want := &Metadata{Title: "Old (PDF) Title", Authors: []string{"Jön A", "Ben"}, Tags: []string{"a", "b"}}
	for name, data := range map[string][]byte{"classic": classicPDF(pdfInfo, ""), "stream": streamPDF()} {
		t.Run(name, func(t *testing.T) {
			m, format, err := ReadFile(writeTemp(t, "d.pdf", data))
			if err != nil {
				t.Fatal(err)
			}
			if format != FormatPDF || !reflect.DeepEqual(m, want) {
				t.Fatalf("%s got %+v", format, m)
			}
		})
	}
}

func TestWritePDFIncremental(t *testing.T) {
	for name, orig := range map[string][]byte{
		"classic": classicPDF(pdfInfo, ""),
		"stream":  streamPDF(),
		"no info": classicPDF("", ""),
	} {
		t.Run(name, func(t *testing.T) {
			p := writeTemp(t, "d.pdf", orig)
			c := fullChanges()
			skipped := DropUnsupported(FormatPDF, c)
			if len(skipped) == 0 || c.Series != nil {
				t.Fatalf("skipped = %v", skipped)
			}
			c.Title = new("Ünïcode (Title) \\ back")
			if err := WriteFile(p, c); err != nil {
				t.Fatal(err)
			}
			// A second update chains onto the first.
			if err := WriteFile(p, &Changes{Description: new("Second pass")}); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			if !bytes.HasPrefix(got, orig) {
				t.Fatal("original bytes changed")
			}
			m, _, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			want := &Metadata{
				Title:       "Ünïcode (Title) \\ back",
				Authors:     []string{"Alice Ash", "Bob Birch"},
				Tags:        []string{"One", "Two"},
				Description: "Second pass",
			}
			if !reflect.DeepEqual(m, want) {
				t.Fatalf("got  %+v\nwant %+v\n%s", m, want, got[len(orig):])
			}
			tail := string(got[len(orig):])
			if name != "no info" && !strings.Contains(tail, "/Producer (Example Writer 1.0)") {
				t.Errorf("Producer lost:\n%s", tail)
			}
			if !strings.Contains(tail, "/Root 1 0 R") || !strings.Contains(tail, "/ID[") && !strings.Contains(tail, "/ID [") {
				t.Errorf("trailer lost Root or ID:\n%s", tail)
			}
			// The new sections are of the same kind as the original's.
			if name == "stream" && strings.Contains(tail, "\ntrailer") || name != "stream" && strings.Contains(tail, "/Type/XRef") {
				t.Errorf("wrong cross-reference kind:\n%s", tail)
			}
		})
	}
}

func TestPDFRefusesEncrypted(t *testing.T) {
	p := writeTemp(t, "d.pdf", classicPDF(pdfInfo, "/Encrypt<</Filter/Standard>>"))
	if _, _, err := ReadFile(p); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("err = %v", err)
	}
}

func TestPDFTextCodec(t *testing.T) {
	for _, s := range []string{"plain", "Ünïcode", "emoji 📚", `a\b(c)`} {
		enc := encodePDFText(s)
		p := &pdfParser{b: []byte(enc), full: true}
		v, err := p.object(0)
		if err != nil {
			t.Fatal(err)
		}
		if got := decodePDFText(v.([]byte)); got != s {
			t.Errorf("%q -> %s -> %q", s, enc, got)
		}
	}
	if got := decodePDFText([]byte{0x93, 'x', 0xA0}); got != "ﬁx€" {
		t.Errorf("PDFDocEncoding = %q", got)
	}
}

func FuzzReadPDF(f *testing.F) {
	f.Add(classicPDF(pdfInfo, ""))
	f.Add(streamPDF())
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		if _, err := (pdfHandler{}).read(r, int64(len(data))); err != nil {
			return
		}
		var out bytes.Buffer
		_ = pdfHandler{}.write(r, int64(len(data)), &out, &Changes{Title: new("T")})
	})
}
