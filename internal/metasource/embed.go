package metasource

import (
	"slices"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	// SchemeZlibISBN is the identifier older downloads used for an ISBN taken
	// from the Z-Library record that nothing had confirmed. A download no longer
	// writes it (the record's ISBN-13 is written as the file's own ISBN), but
	// files that carry it are still read: a lookup searches it, and the matcher
	// still checks the title before it counts it as exact.
	SchemeZlibISBN = "zlib-isbn"

	// embedMinTitleSimilarity is how close a title must be to the file's before
	// a hint ISBN is confirmed by it (see hintTitleSimilarity).
	embedMinTitleSimilarity = 0.8
)

// SchemeZlibID is the identifier a download writes for the Z-Library record a
// file came from, as id:hash, so it can be traced back to that record.
const SchemeZlibID = "zlib-id"

// ISBN13 returns raw as an ISBN-13 when it is one, and "" otherwise: hyphens
// and spaces are ignored, the check digit must be right, and an ISBN-10 is not
// converted. A download writes only an ISBN-13 the record gives as one.
func ISBN13(raw string) string {
	digits := strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(raw))
	if len(digits) != 13 {
		return ""
	}
	return NormalizeISBN(digits)
}

// PlanDownloadISBN decides what to write for the Z-Library record's ISBN-13 in
// the file it was downloaded as: the identifier "isbn", always, when the record
// has an ISBN-13. It returns nil when there is nothing to write:
//
//   - the record has no valid ISBN-13 (an ISBN-10 alone is not enough);
//   - the file already has an ISBN of its own: it is never replaced here.
//
// There is no title test: the file was downloaded from that record, so its ISBN
// is the file's. An older hint under SchemeZlibISBN does not stop the write.
func PlanDownloadISBN(cur *ebookmeta.Metadata, isbn13 string) *ebookmeta.Changes {
	v := ISBN13(isbn13)
	if v == "" || strings.TrimSpace(cur.Identifier("isbn")) != "" {
		return nil
	}
	return &ebookmeta.Changes{Identifiers: map[string]string{"isbn": v}}
}

// ISBNsFromSnippets reads ISBNs from the text that follows an "ISBN" label in
// a book, as ebookmeta.ISBNSnippets returns it. Each snippet yields at most
// one ISBN, kept only when its check digit is right, so a price or a phone
// number next to the label is not mistaken for one. The result is unique and
// in order of appearance.
func ISBNsFromSnippets(snippets []string) []string {
	var out []string
	for _, s := range snippets {
		if isbn := leadingISBN(s); isbn != "" && !slices.Contains(out, isbn) {
			out = append(out, isbn)
		}
	}
	return out
}

// leadingISBN finds the ISBN a snippet starts with: 13 digits when it begins
// 978 or 979, else 10 (the last may be X). Hyphens and spaces between digits
// are skipped; any other character ends the number.
func leadingISBN(s string) string {
	var digits []byte
scan:
	for _, r := range s {
		switch {
		case r >= 0xFF10 && r <= 0xFF19, r >= 0xFF21 && r <= 0xFF3A, r >= 0xFF41 && r <= 0xFF5A:
			r -= 0xFEE0 // full-width digit or letter
		}
		switch {
		case r >= '0' && r <= '9':
			digits = append(digits, byte(r))
		case r == 'x' || r == 'X':
			digits = append(digits, 'X')
			break scan // X only ends an ISBN-10
		case r == '-' || r == ' ' || r == ' ' || r == '‐' || r == '–' || r == '－':
		default:
			break scan
		}
		if len(digits) == 13 {
			break
		}
	}
	d := string(digits)
	if len(d) >= 13 && (strings.HasPrefix(d, "978") || strings.HasPrefix(d, "979")) {
		if isbn := NormalizeISBN(d[:13]); isbn != "" {
			return isbn
		}
	}
	if len(d) >= 10 {
		return NormalizeISBN(d[:10])
	}
	return ""
}
