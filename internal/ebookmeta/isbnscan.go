package ebookmeta

import (
	"archive/zip"
	"html"
	"regexp"
	"slices"
	"strings"
)

const (
	// isbnScanFront and isbnScanBack are how many content documents of an
	// EPUB are read from the start and from the end: copyright pages sit in
	// the front matter, and some books print them last.
	isbnScanFront = 16
	isbnScanBack  = 4
	// isbnScanFileLimit caps one document; isbnScanBudget caps the lot.
	isbnScanFileLimit = 1 << 20
	isbnScanBudget    = 6 << 20
)

var (
	isbnLabelRe = regexp.MustCompile(`(?i)ISBN(?:[-\s]?1[03])?\s*[:：#]?\s*([0-9０-９Xx\-－‐–\s\x{a0}]{10,30})`)
	// A bare hyphenated ISBN-13 with no label, as a copyright page may print
	// it after a book-number prefix.
	isbnBareRe   = regexp.MustCompile(`\b(97[89]-\d{1,5}-\d{1,7}-\d{1,7}-\d)\b`)
	blockEndRe   = regexp.MustCompile(`(?i)<\s*(?:br|/p|/div|/li|/h[1-6]|/tr|/td)\b[^>]*>`)
	anyTagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
	contentDocRe = regexp.MustCompile(`(?i)\.(?:x?html?)$`)
)

// ISBNSnippets returns the text that follows an "ISBN" label in the front and
// back matter of a book, in reading order, for the caller to check. A snippet
// is only a candidate: it may hold a price or a phone number, so it has to
// pass an ISBN check digit before it counts. Formats the scan cannot read
// (MOBI, AZW3 and PDF text is compressed) return nothing and no error.
//
// This reads the book's own text, which a metadata field such as
// dc:identifier often lacks for books made from a publisher's typeset file.
func ISBNSnippets(path string) ([]string, error) {
	f, size, _, format, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if format != FormatEPUB && format != FormatDOCX {
		return nil, nil
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return nil, err
	}
	var docs []*zip.File
	for _, zf := range zr.File {
		switch {
		case format == FormatDOCX && zf.Name == "word/document.xml":
			docs = append(docs, zf)
		case format == FormatEPUB && contentDocRe.MatchString(zf.Name) && !strings.HasPrefix(zf.Name, "META-INF/"):
			docs = append(docs, zf)
		}
	}
	if len(docs) > isbnScanFront+isbnScanBack {
		docs = append(docs[:isbnScanFront:isbnScanFront], docs[len(docs)-isbnScanBack:]...)
	}
	var out []string
	budget := int64(isbnScanBudget)
	for _, zf := range docs {
		if budget <= 0 {
			break
		}
		limit := min(int64(isbnScanFileLimit), budget)
		data, err := readZipFile(zf, limit)
		if err != nil {
			continue // an oversized or damaged part: look at the others
		}
		budget -= int64(len(data))
		out = append(out, snippetsIn(string(data))...)
	}
	return out, nil
}

// snippetsIn extracts the ISBN candidates of one markup document.
func snippetsIn(markup string) []string {
	text := blockEndRe.ReplaceAllString(markup, "\n")
	text = anyTagRe.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	var out []string
	for _, m := range isbnLabelRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	labeled := len(out)
	for _, m := range isbnBareRe.FindAllStringSubmatch(text, -1) {
		// A labeled ISBN also matches the bare form; keep it once.
		if !slices.ContainsFunc(out[:labeled], func(s string) bool { return strings.Contains(s, m[1]) }) {
			out = append(out, m[1])
		}
	}
	return out
}
