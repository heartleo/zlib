package metasource

import (
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func TestDownloadISBNIsTheFilesOwnWhenTheRecordIsNamedLikeAFile(t *testing.T) {
	const isbn = "9780441013593"
	for _, tc := range []struct {
		record, file, want string
	}{
		// Z-Library's "Author - Series NN - Title" shape: the title is the last part.
		{"Herbert, Frank - Dune 01 - Dune", "Dune", "isbn"},
		{"Frank Herbert - Dune Chronicles 02 - Dune Messiah", "Dune Messiah", "isbn"},
		// A two-part name can be either order: compared whole only, so a hint.
		{"Herbert, Frank Patrick Junior - Dune", "Dune", SchemeZlibISBN},
		// Another book is still another book, whatever its shape.
		{"Herbert, Frank - Dune 02 - Dune Messiah", "Dune", SchemeZlibISBN},
		{"Herbert, Frank - Dune 01 - Dune", "Foundation", SchemeZlibISBN},
		// Plain agreement is unchanged.
		{"The Great Gatsby (Penguin Classics)", "The Great Gatsby", "isbn"},
	} {
		got, ch := PlanDownloadISBN(&ebookmeta.Metadata{Title: tc.file}, tc.record, isbn)
		if got != tc.want || ch == nil || ch.Identifiers[tc.want] != isbn {
			t.Errorf("record %q, file %q: scheme %q (changes %v), want %q", tc.record, tc.file, got, ch, tc.want)
		}
	}
}

func TestDownloadISBNIsNeverWrittenOverAnISBNTheFileHas(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "Dune", Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9780441172719"}}}
	if scheme, ch := PlanDownloadISBN(cur, "Herbert, Frank - Dune 01 - Dune", "9780441013593"); scheme != "" || ch != nil {
		t.Fatalf("an existing ISBN was replaced: %q %v", scheme, ch)
	}
}
