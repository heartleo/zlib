package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

const downloadedISBN = "9780441013593"

// resultsFor opens the results page of a standard search over a book that was
// written with changes, as a download would have left it.
func resultsFor(t *testing.T, changes *ebookmeta.Changes, opts ...metaOption) metaPageModel {
	t.Helper()
	path := writeTestEPUB(t)
	if changes != nil {
		if err := ebookmeta.WriteFile(path, changes); err != nil {
			t.Fatal(err)
		}
	}
	m, err := newMetaPageModel(path, answerWith(twoCandidates(), nil, nil), nil, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return sized(t, searchFromPrepare(t, m), 120, 30)
}

func TestResultsShowTheISBNAHintFromTheDownloadHolds(t *testing.T) {
	m := resultsFor(t, &ebookmeta.Changes{Identifiers: map[string]string{metasource.SchemeZlibISBN: downloadedISBN}})
	screen := metaScreen(m)
	if !strings.Contains(screen, "ISBN (download)") || !strings.Contains(screen, downloadedISBN) {
		t.Fatalf("the comparison does not show the ISBN the download recorded:\n%s", screen)
	}
}

func TestResultsShowTheISBNOfTheDownloadsRecordWhenNothingWasWritten(t *testing.T) {
	// A format that stores no identifier, or a write that failed: the record's
	// ISBN is only in the hint the page was opened with.
	m := resultsFor(t, nil, withMetaHint(metaHint{Title: "Dune", ISBNs: []string{downloadedISBN}}))
	screen := metaScreen(m)
	if !strings.Contains(screen, "ISBN (download)") || !strings.Contains(screen, downloadedISBN) {
		t.Fatalf("the comparison does not show the record's ISBN:\n%s", screen)
	}
}

func TestResultsDoNotRepeatTheDownloadISBNWhenTheFileHoldsItAsItsOwn(t *testing.T) {
	m := resultsFor(t, &ebookmeta.Changes{Identifiers: map[string]string{"isbn": downloadedISBN, metasource.SchemeZlibISBN: downloadedISBN}})
	screen := metaScreen(m)
	if strings.Contains(screen, "ISBN (download)") {
		t.Fatalf("the hint row is shown next to the ISBN it duplicates:\n%s", screen)
	}
	if !strings.Contains(screen, downloadedISBN) {
		t.Fatalf("the file's own ISBN is not shown:\n%s", screen)
	}
}

func TestResultsShowNoDownloadISBNRowForABookWithNone(t *testing.T) {
	if screen := metaScreen(resultsFor(t, nil)); strings.Contains(screen, "ISBN (download)") {
		t.Fatalf("a row for an ISBN that does not exist:\n%s", screen)
	}
}

func TestTheFieldsPageListsTheDownloadISBNToo(t *testing.T) {
	m := resultsFor(t, &ebookmeta.Changes{Identifiers: map[string]string{metasource.SchemeZlibISBN: downloadedISBN}})
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaFields || !strings.Contains(metaScreen(m), downloadedISBN) {
		t.Fatalf("the fields page lacks the download ISBN:\n%s", metaScreen(m))
	}
}

func TestTheCompletionPageSaysWhenTheISBNCouldNotBeRecorded(t *testing.T) {
	m := newDownloadModel("1:abc", t.TempDir(), nil)
	m.stayOnComplete = true
	next, _ := m.Update(downloadDoneMsg{path: "x.epub", embedErr: errors.New("disk is full")})
	got := next.(downloadModel)
	if !strings.Contains(got.completeMessage, "Could not record the ISBN") || !strings.Contains(got.completeMessage, "disk is full") || got.completeTone != noticeError {
		t.Fatalf("message=%q tone=%v", got.completeMessage, got.completeTone)
	}
}
