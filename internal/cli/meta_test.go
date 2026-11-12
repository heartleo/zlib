package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	cmd := &cobra.Command{Use: "meta", Args: metaCmd.Args, RunE: runMeta, SilenceUsage: true, SilenceErrors: true}
	addMetaFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if errOut.Len() > 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	return out.String(), err
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
