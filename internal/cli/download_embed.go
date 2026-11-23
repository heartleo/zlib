package cli

import (
	"errors"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
)

// embeddedISBN reports what a download recorded in the saved file: the ISBN-13
// of the Z-Library record, as the identifier "isbn", and the record's ID.
type embeddedISBN struct {
	// Scheme is "isbn" when the ISBN-13 was written, and "" when only the ID
	// was, or the ISBN-13 could not be.
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
	// ZlibID is the Z-Library record (id:hash) written under
	// metasource.SchemeZlibID. It is reported apart from the ISBN, in the
	// download's own zlib_id field.
	ZlibID string `json:"-"`
	// Unstorable is the record's ISBN-13 when the file's format has no place for
	// an ISBN, so it was not written. A later lookup still shows it for a
	// download made in the same session.
	Unstorable string `json:"-"`
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
	case e.Scheme == "" && e.Unstorable != "":
		return "ISBN " + e.Unstorable + " was not written: this format cannot store an ISBN"
	case e.Scheme == "":
		return "Recorded the Z-Library ID in the file metadata"
	}
	return "Recorded ISBN " + e.Value + " in the file metadata"
}

// embedDownloadISBN records the ISBN-13 of a Z-Library record in the file just
// downloaded, as its own identifier "isbn", so the file carries it and a later
// `zlib meta --fetch` finds the exact edition instead of guessing from the
// title; and the record's own ID (id:hash) as the identifier
// metasource.SchemeZlibID, so the file can always be traced back to it. Only an
// ISBN-13 is written: a record without one (an ISBN-10 alone included) gives no
// ISBN. It returns nil when there was nothing to record: no ISBN-13 and no ID,
// a file that already has both, an unreadable format. A format with no place for
// an ISBN (it is reported as Unstorable) keeps only the ID.
//
// It never fails a download: callers report the error as a warning. The file
// is rewritten through a temporary file and renamed, so a failure leaves it
// as it was.
func embedDownloadISBN(path string, book zlib.Book) (*embeddedISBN, error) {
	id := bookCommandID(book)
	if !strings.Contains(id, ":") { // only a full id:hash names the record
		id = ""
	}
	isbn13 := metasource.ISBN13(book.ISBN13)
	if isbn13 == "" && id == "" {
		return nil, nil
	}
	cur, format, err := ebookmeta.ReadFile(path)
	if err != nil {
		if errors.Is(err, ebookmeta.ErrUnsupportedFormat) || errors.Is(err, ebookmeta.ErrEncrypted) {
			return nil, nil
		}
		return nil, err
	}
	changes := metasource.PlanDownloadISBN(cur, isbn13)
	if changes == nil {
		changes = &ebookmeta.Changes{}
	}
	if id != "" && cur.Identifier(metasource.SchemeZlibID) == "" {
		if changes.Identifiers == nil {
			changes.Identifiers = map[string]string{}
		}
		changes.Identifiers[metasource.SchemeZlibID] = id
	}
	out := &embeddedISBN{}
	if v := changes.Identifiers["isbn"]; v != "" && !ebookmeta.CanStore(format, "identifier:isbn") {
		out.Unstorable = v
	}
	ebookmeta.DropUnsupported(format, changes) // a format may store the ISBN but not the ID
	if changes.Empty() {
		if out.Unstorable != "" {
			return out, nil
		}
		return nil, nil
	}
	if err := ebookmeta.WriteFile(path, changes); err != nil {
		return nil, err
	}
	out.ZlibID = changes.Identifiers[metasource.SchemeZlibID]
	if v := changes.Identifiers["isbn"]; v != "" {
		out.Scheme, out.Value = "isbn", v
	}
	return out, nil
}
