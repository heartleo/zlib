package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func writeTestEPUB(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`, zip.Deflate)
	add("content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>Before</dc:title>
    <dc:identifier id="id" opf:scheme="UUID">0f1e2d3c-0000-4000-8000-000000000001</dc:identifier>
    <dc:language>en</dc:language>
  </metadata>
  <manifest><item id="t" href="t.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="t"/></spine>
</package>`, zip.Deflate)
	add("t.xhtml", "<html/>", zip.Deflate)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runMetaForTest runs the meta command on a fresh cobra.Command, so flag
// state does not leak between tests and rootCmd is left alone.
func runMetaForTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, _, err := runMetaWithStderr(t, args...)
	return out, err
}

func runMetaWithStderr(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "meta", Args: metaCmd.Args, RunE: runMeta, SilenceUsage: true, SilenceErrors: true}
	addMetaFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestMetaCommandSkipsUnsupportedFields(t *testing.T) {
	pdf := "%PDF-1.4\n1 0 obj\n<</Type/Catalog>>\nendobj\n"
	xref := len(pdf)
	pdf += "xref\n0 2\n0000000000 65535 f\r\n0000000009 00000 n\r\ntrailer\n<</Size 2/Root 1 0 R>>\nstartxref\n" +
		strconv.Itoa(xref) + "\n%%EOF\n"
	p := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(p, []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := runMetaWithStderr(t, p, "-t", "Kept", "--series", "Lost", "--isbn", "9780000000002")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "PDF files cannot store series, identifier:isbn; skipped.") {
		t.Fatalf("stderr = %q", stderr)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "Title    Kept") || strings.Contains(plain, "Lost") {
		t.Fatalf("output:\n%s", plain)
	}
}

func TestMetaCommandWritesAndPrints(t *testing.T) {
	p := writeTestEPUB(t)
	cover := filepath.Join(t.TempDir(), "c.png")
	if err := os.WriteFile(cover, []byte("\x89PNG\r\n\x1a\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	opfOut := filepath.Join(t.TempDir(), "out.opf")
	out, err := runMetaForTest(t, p, "-t", "After", "-a", "Jane Doe & John Roe",
		"--series", "Saga", "-i", "2", "--isbn", "9780000000002", "--identifier", "asin:B000000000",
		"--tags", "a, b", "-r", "4", "--cover", cover, "--to-opf", opfOut)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"Title         After", "Authors       Jane Doe & John Roe", "Series        Saga #2",
		"asin:B000000000, isbn:9780000000002", "Rating        4/5", "Cover         image/png"} {
		if !strings.Contains(plain, want) {
			t.Errorf("output missing %q:\n%s", want, plain)
		}
	}
	if data, err := os.ReadFile(opfOut); err != nil || !strings.Contains(string(data), "<dc:title>After</dc:title>") {
		t.Fatalf("OPF sidecar: %v\n%s", err, data)
	}
}

func TestMetaCommandJSONAndClear(t *testing.T) {
	p := writeTestEPUB(t)
	out, err := runMetaForTest(t, p, "--language", "", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if doc["format"] != "epub" || doc["title"] != "Before" || doc["languages"] != nil {
		t.Fatalf("doc = %v", doc)
	}
}

// The flags check an ISBN the way the terminal editor and edit_metadata do:
// a bad one is refused and nothing is written, a valid one is stored as an
// ISBN-13.
func TestMetaCommandChecksISBN(t *testing.T) {
	for _, args := range [][]string{
		{"--isbn", "banana"},
		{"--isbn", "9780000000001"}, // wrong check digit
		{"--identifier", "isbn:banana"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p := writeTestEPUB(t)
			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runMetaForTest(t, append([]string{p}, args...)...)
			var usage *usageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), "not a valid ISBN") {
				t.Fatalf("err = %v, want a usage error about the ISBN", err)
			}
			if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
				t.Fatal("the file was rewritten")
			}
		})
	}

	for _, args := range [][]string{{"--isbn", "978-0-00-000000-2"}, {"--identifier", "ISBN:9780000000002"}} {
		p := writeTestEPUB(t)
		out, err := runMetaForTest(t, append([]string{p}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if plain := ansi.Strip(out); !strings.Contains(plain, "isbn:9780000000002") {
			t.Errorf("%v: output:\n%s", args, plain)
		}
	}
}

// --index needs a series to number. With none, the command says so and leaves
// the file alone instead of rewriting it and exiting 0.
func TestMetaCommandIndexNeedsSeries(t *testing.T) {
	p := writeTestEPUB(t)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--index", "2"}, {"--series", "", "--index", "2"}} {
		_, err := runMetaForTest(t, append([]string{p}, args...)...)
		var usage *usageError
		if !errors.As(err, &usage) || !strings.Contains(err.Error(), "no series") {
			t.Fatalf("%v: err = %v, want a usage error about the series", args, err)
		}
		if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
			t.Fatalf("%v: the file was rewritten", args)
		}
	}

	// Naming a series in the same command, or numbering one the file has, works.
	if _, err := runMetaForTest(t, p, "--series", "Saga", "--index", "2"); err != nil {
		t.Fatal(err)
	}
	out, err := runMetaForTest(t, p, "--index", "3")
	if err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "Saga #3") {
		t.Errorf("output:\n%s", plain)
	}
}

func TestMetaCommandUsageErrors(t *testing.T) {
	p := writeTestEPUB(t)
	for _, args := range [][]string{
		{},
		{p, "--rating", "9"},
		{p, "--index", "-1"},
		{p, "--identifier", "novalue"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := runMetaForTest(t, args...)
			var usage *usageError
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v, want usage error", err)
			}
		})
	}
}
