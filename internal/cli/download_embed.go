package cli

import (
	"errors"

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
}

// embeddedISBNMessage words what a download recorded, for a screen.
func embeddedISBNMessage(e *embeddedISBN) string {
	if e.Scheme == metasource.SchemeZlibISBN {
		return "Kept ISBN " + e.Value + " in the file as a hint (title differs)"
	}
	return "Recorded ISBN " + e.Value + " in the file metadata"
}

// embedDownloadISBN records the ISBN of a Z-Library record in the file just
// downloaded, so a later `zlib meta --fetch` can find the exact edition
// instead of guessing from the title. It returns nil when there was nothing
// to record: no valid ISBN, a file that already has one, or a format that
// cannot store it.
//
// It never fails a download: callers report the error as a warning. The file
// is rewritten through a temporary file and renamed, so a failure leaves it
// as it was.
func embedDownloadISBN(path string, book zlib.Book) (*embeddedISBN, error) {
	if book.ISBN13 == "" && book.ISBN10 == "" && book.ISBN == "" {
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
		return nil, nil
	}
	ebookmeta.DropUnsupported(format, changes) // a format may store "isbn" but not the hint
	if changes.Empty() {
		return nil, nil
	}
	if err := ebookmeta.WriteFile(path, changes); err != nil {
		return nil, err
	}
	return &embeddedISBN{Scheme: scheme, Value: changes.Identifiers[scheme]}, nil
}
