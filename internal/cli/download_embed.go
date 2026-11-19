package cli

import (
	"errors"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
)

// embeddedISBN reports the ISBN a download recorded in the saved file.
// Scheme is "isbn" when the record's title agreed with the file's, and
// "zlib-isbn" when it did not and the value is only a hint for a later lookup.
type embeddedISBN struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
	// ZlibID is the Z-Library record (id:hash) written under
	// metasource.SchemeZlibID. It is reported apart from the ISBN, in the
	// download's own zlib_id field. Scheme is "" when only the ID was written.
	ZlibID string `json:"-"`
}

// isbnPart is e when it records an ISBN, or nil when it only holds the ID.
func (e *embeddedISBN) isbnPart() *embeddedISBN {
	if e == nil || e.Scheme == "" {
		return nil
	}
	return e
}

// embeddedISBNMessage words what a download recorded, for a screen.
func embeddedISBNMessage(e *embeddedISBN) string {
	switch {
	case e.Scheme == "":
		return "Recorded the Z-Library ID in the file metadata"
	case e.Scheme == metasource.SchemeZlibISBN:
		return "Kept ISBN " + e.Value + " in the file as a hint (title differs)"
	}
	return "Recorded ISBN " + e.Value + " in the file metadata"
}

// embedDownloadISBN records the ISBN of a Z-Library record in the file just
// downloaded, so a later `zlib meta --fetch` can find the exact edition
// instead of guessing from the title, and the record's own ID (id:hash) as the
// identifier metasource.SchemeZlibID, so the file can always be traced back to
// it. It returns nil when there was nothing to record: no valid ISBN and no ID,
// a file that already has both, or a format that cannot store them (only EPUB
// keeps an identifier of its own kind).
//
// It never fails a download: callers report the error as a warning. The file
// is rewritten through a temporary file and renamed, so a failure leaves it
// as it was.
func embedDownloadISBN(path string, book zlib.Book) (*embeddedISBN, error) {
	id := bookCommandID(book)
	if !strings.Contains(id, ":") { // only a full id:hash names the record
		id = ""
	}
	if book.ISBN13 == "" && book.ISBN10 == "" && book.ISBN == "" && id == "" {
		return nil, nil
	}
	cur, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		if errors.Is(err, ebookmeta.ErrUnsupportedFormat) || errors.Is(err, ebookmeta.ErrEncrypted) {
			return nil, nil
		}
		return nil, err
	}
	scheme, changes := metasource.PlanDownloadISBN(cur, book.Name, book.ISBN13, book.ISBN10, book.ISBN)
	if changes == nil {
		changes = &ebookmeta.Changes{}
	}
	if id != "" && cur.Identifier(metasource.SchemeZlibID) == "" {
		if changes.Identifiers == nil {
			changes.Identifiers = map[string]string{}
		}
		changes.Identifiers[metasource.SchemeZlibID] = id
	}
	ebookmeta.DropUnsupported(format, changes) // a format may store "isbn" but not the hint or the ID
	if changes.Empty() {
		return nil, nil
	}
	if err := ebookmeta.WriteFile(path, changes); err != nil {
		return nil, err
	}
	out := &embeddedISBN{ZlibID: changes.Identifiers[metasource.SchemeZlibID]}
	if v := changes.Identifiers[scheme]; scheme != "" && v != "" {
		out.Scheme, out.Value = scheme, v
	}
	return out, nil
}
