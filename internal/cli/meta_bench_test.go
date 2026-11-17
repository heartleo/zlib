package cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
)

// benchPage is the workbench of the first match against a book whose own
// metadata fills most fields, with every write recorded.
func benchPage(t *testing.T, opts ...metaOption) (metaPageModel, *[]*ebookmeta.Changes, *[]metasource.Query) {
	t.Helper()
	var writes []*ebookmeta.Changes
	var queries []metasource.Query
	write := func(_ string, c *ebookmeta.Changes) error { writes = append(writes, c); return nil }
	m, err := newMetaPageModel(richEPUB(t), recordingLookup(twoCandidates(), &queries), write, opts...)
	if err != nil {
		t.Fatal(err)
	}
	m = sized(t, m, 120, 32)
	return m, &writes, &queries
}

func focusBench(t *testing.T, m metaPageModel, f metasource.Field) metaPageModel {
	t.Helper()
	for i, r := range m.benchRows() {
		if r.field == f {
			m.focus = i
			return m
		}
	}
	t.Fatalf("no %s row", f)
	return m
}

// Each field is decided on its own: the match's value only where it is taken,
// a typed value, a cleared field, and nothing for the fields left alone.
func TestBenchWritesOnlyWhatEachFieldDecides(t *testing.T) {
	m, writes, _ := benchPage(t)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields {
		t.Fatalf("stage %v", m.stage)
	}
	m = focusBench(t, m, metasource.FieldAuthors)
	m, _ = metaStep(t, m, metaKey("right")) // take the match's authors
	m = focusBench(t, m, metasource.FieldTitle)
	m, _ = metaStep(t, m, metaKey("right"))
	m, _ = metaStep(t, m, metaKey("left")) // and change our mind
	m = focusBench(t, m, metasource.FieldSeries)
	m, _ = metaStep(t, m, key("x"))
	m = focusBench(t, m, metasource.FieldPublisher)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Ace Books Ltd")
	m, _ = metaStep(t, m, key("enter"))
	m = applyAndWait(t, m)
	if len(*writes) != 1 {
		t.Fatalf("%d writes", len(*writes))
	}
	c := (*writes)[0]
	if c.Title != nil || c.Description != nil || c.Published != nil {
		t.Errorf("a field left alone was written: %+v", c)
	}
	if c.Authors == nil || strings.Join(*c.Authors, "&") != "Frank Herbert" {
		t.Errorf("authors = %v", c.Authors)
	}
	if c.Series == nil || *c.Series != "" {
		t.Errorf("series was not cleared: %v", c.Series)
	}
	if c.Publisher == nil || *c.Publisher != "Ace Books Ltd" {
		t.Errorf("publisher = %v", c.Publisher)
	}
	if c.Identifiers["isbn"] != "9780441013593" { // an empty field the confirmed match fills
		t.Errorf("identifiers = %v", c.Identifiers)
	}
}

// m on the prepare page edits the file's own metadata with no lookup, the
// description in a text area of several lines.
func TestBenchEditsWithoutALookup(t *testing.T) {
	m, writes, queries := benchPage(t)
	m, _ = metaStep(t, m, key("m"))
	if m.stage != metaFields || !m.manual {
		t.Fatalf("m did not open the editor: stage %v", m.stage)
	}
	if s := ansi.Strip(m.View().Content); !strings.Contains(s, "This file") || strings.Contains(s, "Match ·") || !strings.Contains(s, "Edit metadata") {
		t.Errorf("the editor without a lookup:\n%s", s)
	}
	m = focusBench(t, m, metasource.FieldTitle)
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Dune Messiah")
	m, _ = metaStep(t, m, key("enter"))
	m = focusBench(t, m, metasource.FieldDescription)
	m, _ = metaStep(t, m, key("e"))
	if !m.editor.multiline() {
		t.Fatal("the description is not edited in a text area")
	}
	m, _ = metaStep(t, m, key("enter")) // a new line, not the end of the edit
	m = typeIntoEditor(t, m, "Third.")
	if !m.editing {
		t.Fatal("enter closed the text area")
	}
	m, _ = metaStep(t, m, metaKey("ctrl+s"))
	m = applyAndWait(t, m)
	if len(*queries) != 0 {
		t.Errorf("editing looked the book up: %d queries", len(*queries))
	}
	if len(*writes) != 1 {
		t.Fatalf("%d writes", len(*writes))
	}
	c := (*writes)[0]
	if c.Title == nil || *c.Title != "Dune Messiah" || c.Description == nil || !strings.HasSuffix(*c.Description, "\nThird.") {
		t.Errorf("wrote %+v", c)
	}
}

// Esc asks first only about the user's own changes, not what the match
// preselected; Enter keeps editing; nothing is written either way.
func TestBenchAsksBeforeDiscarding(t *testing.T) {
	m, writes, _ := benchPage(t)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	look, _ := metaStep(t, m, key("esc"))
	if look.confirmDiscard || look.stage != metaCandidates {
		t.Fatalf("only looking at a match asked: confirm %v stage %v", look.confirmDiscard, look.stage)
	}
	m = focusBench(t, m, metasource.FieldAuthors)
	m, _ = metaStep(t, m, metaKey("right"))
	m, _ = metaStep(t, m, key("esc"))
	if !m.confirmDiscard {
		t.Fatal("leaving with a value taken did not ask")
	}
	m, _ = metaStep(t, m, key("enter"))
	if m.confirmDiscard || m.stage != metaFields {
		t.Fatalf("enter: confirm %v stage %v, want to keep editing", m.confirmDiscard, m.stage)
	}
	m, _ = metaStep(t, m, key("esc"))
	m, _ = metaStep(t, m, key("y"))
	if m.stage != metaCandidates || len(*writes) != 0 {
		t.Errorf("y: stage %v writes %d", m.stage, len(*writes))
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 60, 90))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A match's cover already downloaded for the preview is the one written.
func TestBenchWritesTheMatchCoverItShowed(t *testing.T) {
	fetch := func(context.Context, string) (*ebookmeta.Cover, error) { return nil, errors.New("fetched again") }
	m, writes, _ := benchPage(t, withMetaCover(fetch))
	m = searchFromPrepare(t, m)
	m.res.Candidates[0].CoverURL = "https://covers.example.test/1.png"
	data := pngBytes(t)
	m.coverCache["https://covers.example.test/1.png"] = &metaCoverImage{cover: &ebookmeta.Cover{Data: data}}
	m, _ = metaStep(t, m, key("enter"))
	m = focusBench(t, m, metasource.FieldCover)
	m, _ = metaStep(t, m, metaKey("right"))
	m = applyAndWait(t, m)
	if len(*writes) != 1 || (*writes)[0].Cover == nil || !bytes.Equal((*writes)[0].Cover.Data, data) {
		t.Fatalf("the shown cover was not written (message %q)", m.message)
	}
}

// Both layouts fit and keep the keys to save and to leave.
func TestBenchLayoutsKeepSaveAndEsc(t *testing.T) {
	m, _, _ := benchPage(t)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	for _, size := range [][2]int{{120, 32}, {100, 26}, {80, 24}, {60, 16}, {40, 12}} {
		p := sized(t, m, size[0], size[1])
		screen := screenOf(p)
		lines := strings.Split(screen, "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%dx%d: line wider than the terminal: %q", size[0], size[1], l)
				break
			}
		}
		f := footer(screen)
		if !strings.Contains(f, "ctrl+s") || !strings.Contains(f, "esc") {
			t.Errorf("%dx%d: footer %q", size[0], size[1], f)
		}
		if size[0] >= 100 && (!strings.Contains(screen, "╭─ This file") || !strings.Contains(screen, "╭─ Match")) {
			t.Errorf("%dx%d: no side-by-side panels:\n%s", size[0], size[1], screen)
		}
	}
}

// Two covers are drawn in one write and erased together.
func TestCoverOutputDrawsAndErasesEveryImage(t *testing.T) {
	var out bytes.Buffer
	w := &coverOutput{target: &out}
	second := &coverFrame{protocol: "Kitty", data: "<second>", x: 40, y: 3, cols: 16, rows: 9, id: coverGraphicsID + 1}
	first := &coverFrame{protocol: "Kitty", data: "<first>", x: 2, y: 3, cols: 16, rows: 9, id: coverGraphicsID, more: []*coverFrame{second}}
	w.set(first)
	if _, err := w.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "<first>") || !strings.Contains(s, "<second>") {
		t.Fatalf("drawn: %q", s)
	}
	out.Reset()
	w.set(nil)
	if _, err := w.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "i=5917697") || !strings.Contains(s, "i=5917698") {
		t.Errorf("erased: %q", s)
	}
}

// A download records its Z-Library record in an EPUB, once.
func TestDownloadRecordsTheZlibIDInAnEPUB(t *testing.T) {
	p := writeTestEPUB(t)
	book := zlib.Book{ID: "123", Hash: "abc", Name: "Before"}
	got, err := embedDownloadISBN(p, book)
	if err != nil || got == nil || got.ZlibID != "123:abc" {
		t.Fatalf("embedded %+v, %v", got, err)
	}
	m, _, err := ebookmeta.ReadFile(p)
	if err != nil || m.Identifier(metasource.SchemeZlibID) != "123:abc" {
		t.Fatalf("read back %q, %v", m.Identifier(metasource.SchemeZlibID), err)
	}
	before, _ := os.ReadFile(p)
	if again, err := embedDownloadISBN(p, book); err != nil || again != nil {
		t.Errorf("a second download rewrote the file: %+v, %v", again, err)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Error("the file changed")
	}
}

// The results put the file beside the highlighted match before it is chosen,
// and the workbench keeps its panels on a 23-row shell page.
func TestResultsAndWorkbenchCompareSideBySide(t *testing.T) {
	m, _, _ := benchPage(t)
	m = searchFromPrepare(t, m)
	for _, h := range []int{32, 24, 23} {
		s := screenOf(sized(t, m, 120, h))
		if !strings.Contains(s, "╭─ This file") || !strings.Contains(s, "╭─ Match") || !strings.Contains(s, "Ace Books") {
			t.Errorf("results at 120x%d do not compare:\n%s", h, s)
		}
		if len(strings.Split(s, "\n")) > h {
			t.Errorf("results at 120x%d overflow", h)
		}
		f, _ := metaStep(t, sized(t, m, 120, h), key("enter"))
		if fs := screenOf(f); !strings.Contains(fs, "╭─ This file") || !strings.Contains(footer(fs), "ctrl+s") {
			t.Errorf("workbench at 120x%d:\n%s", h, fs)
		}
	}
}

// Review fixes: a taken title keeps the file's title sort; typing a field back
// to the file's value drops a taken match; a date with a time is kept as it is;
// Esc asks only when something would be written.
func TestBenchKeepsWhatTheUserDidNotChange(t *testing.T) {
	m, writes, _ := benchPage(t)
	m.current.TitleSort = "Dune, The"
	m.current.Published = "1990-09-01T00:00:00+00:00"
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("enter"))
	m, _ = metaStep(t, m, key("a"))
	m, _ = metaStep(t, m, key("a"))
	m, _ = metaStep(t, m, key("u"))
	if q, _ := metaStep(t, m, key("esc")); q.confirmDiscard {
		t.Error("Esc asked although nothing would be written")
	}
	m = focusBench(t, m, metasource.FieldTitle)
	m, _ = metaStep(t, m, metaKey("right"))
	m = focusBench(t, m, metasource.FieldAuthors)
	m, _ = metaStep(t, m, metaKey("right"))
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Frank Herbert & Brian Herbert") // the file's own authors
	m, _ = metaStep(t, m, key("enter"))
	m = focusBench(t, m, metasource.FieldPublished)
	m, _ = metaStep(t, m, key("e"))
	m, _ = metaStep(t, m, key("enter")) // kept as the file has it
	if m.editing {
		t.Fatalf("a date with a time could not be kept: %q", m.editor.err)
	}
	m = applyAndWait(t, m)
	if len(*writes) != 1 {
		t.Fatalf("%d writes", len(*writes))
	}
	c := (*writes)[0]
	if c.Title == nil || *c.Title != "Before" || c.TitleSort == nil || *c.TitleSort != "Dune, The" {
		t.Errorf("title %v sort %v", c.Title, c.TitleSort)
	}
	if c.Authors != nil || c.Published != nil {
		t.Errorf("fields kept as the file has them were written: authors %v published %v", c.Authors, c.Published)
	}
}

// m from a prepare page reopened from the results returns to that page.
func TestBenchManualReturnsToTheReopenedPreparePage(t *testing.T) {
	var queries []metasource.Query
	m, _, _ := benchPage(t, withMetaSources(fakeSourceChoices(), func([]string) metaLookupFunc {
		return recordingLookup(twoCandidates(), &queries)
	}))
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("s"))
	if m.stage != metaPrepare {
		t.Fatalf("s: stage %v", m.stage)
	}
	m, _ = metaStep(t, m, key("m"))
	if m.stage != metaFields || !m.manual {
		t.Fatalf("m: stage %v", m.stage)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaPrepare {
		t.Fatalf("stage %v", m.stage)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaCandidates {
		t.Errorf("a second esc: stage %v", m.stage)
	}
}
