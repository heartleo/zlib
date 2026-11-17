package metasource

import (
	"slices"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	// SchemeZlibISBN holds an ISBN taken from the Z-Library record of a
	// download that nothing has confirmed. A lookup searches it, and the
	// matcher still checks the title, but it is never read as the book's own
	// ISBN: a wrong value here must not turn into an "exact" match later.
	SchemeZlibISBN = "zlib-isbn"

	// embedMinTitleSimilarity is how close the record's title must be to the
	// file's before the record's ISBN is written as the file's own ISBN. It
	// is stricter than the matcher's threshold, since a write is permanent.
	embedMinTitleSimilarity = 0.8
)

// SchemeZlibID is the identifier a download writes for the Z-Library record a
// file came from, as id:hash, so it can be traced back to that record.
const SchemeZlibID = "zlib-id"

// PlanDownloadISBN decides how to record the ISBN of a Z-Library record in the
// file it was downloaded as. isbns are tried in order and the first valid one
// is used. It returns the identifier scheme written and the change, or "" and
// nil when there is nothing to write:
//
//   - the record has no valid ISBN;
//   - the file already has an ISBN: it is never replaced here, and the record
//     is not allowed to contradict it quietly;
//   - the file already carries this ISBN under SchemeZlibISBN.
//
// The scheme is "isbn" only when the file has a title, the record's title
// agrees with it, and the record names one edition: an identifier can list the
// ISBNs of several editions, and a title cannot say which one the file is.
// Otherwise it is SchemeZlibISBN.
func PlanDownloadISBN(cur *ebookmeta.Metadata, recordTitle string, isbns ...string) (string, *ebookmeta.Changes) {
	var isbn string
	editions := map[string]bool{}
	for _, raw := range isbns {
		for _, value := range splitIdentifiers(raw) {
			if v := NormalizeISBN(value); v != "" {
				editions[v] = true // an ISBN-10 counts as its ISBN-13
				if isbn == "" {
					isbn = v
				}
			}
		}
	}
	if isbn == "" || strings.TrimSpace(cur.Identifier("isbn")) != "" || NormalizeISBN(cur.Identifier(SchemeZlibISBN)) == isbn {
		return "", nil
	}
	scheme := SchemeZlibISBN
	if cur.Title != "" && recordTitle != "" && len(editions) == 1 && titleSimilarity(recordTitle, cur.Title) >= embedMinTitleSimilarity {
		scheme = "isbn"
	}
	return scheme, &ebookmeta.Changes{Identifiers: map[string]string{scheme: isbn}}
}

// splitIdentifiers splits an identifier field that lists several values, as
// Z-Library's does ("9787229042066,7229042062").
func splitIdentifiers(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '|' || r == '\n' })
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
