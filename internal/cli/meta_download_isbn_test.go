package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
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

// The whole path: a download writes the record's ISBN-13, and the results of a
// lookup on that file show it, on a wide terminal (the panels) and a narrow one.
func TestTheISBNADownloadWritesIsShownByTheMatchResults(t *testing.T) {
	path := writeTestEPUB(t)
	got, err := embedDownloadISBN(path, zlib.Book{Name: "Whatever", ISBN13: embedTestISBN})
	if err != nil || got == nil || got.Scheme != "isbn" {
		t.Fatalf("embedded %+v, %v", got, err)
	}
	m, err := newMetaPageModel(path, answerWith(twoCandidates(), nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	m = searchFromPrepare(t, m)
	// embedTestISBN is not any candidate's ISBN, so it is on screen only because
	// the file holds it.
	for _, size := range [][2]int{{120, 30}, {80, 24}} {
		screen := metaScreen(sized(t, m, size[0], size[1]))
		if !strings.Contains(screen, embedTestISBN) {
			t.Errorf("%dx%d: the results do not show the ISBN the download wrote:\n%s", size[0], size[1], screen)
		}
	}
	narrow := metaScreen(sized(t, m, 80, 24))
	if !strings.Contains(narrow, "File ISBN") {
		t.Errorf("the narrow details have no File ISBN row:\n%s", narrow)
	}
}

func TestNoISBN13MeansNoISBNRowAnywhere(t *testing.T) {
	path := writeTestEPUB(t)
	if got, err := embedDownloadISBN(path, zlib.Book{Name: "Whatever", ISBN10: "0441013597"}); err != nil || got != nil {
		t.Fatalf("an ISBN-10 alone was recorded: %+v, %v", got, err)
	}
	m, _ := newMetaPageModel(path, answerWith(twoCandidates(), nil, nil), nil)
	m = searchFromPrepare(t, m)
	for _, w := range []int{120, 80} {
		if screen := metaScreen(sized(t, m, w, 30)); strings.Contains(screen, "File ISBN") || strings.Contains(screen, "ISBN (download)") {
			t.Errorf("%d columns: an ISBN row for a book without one:\n%s", w, screen)
		}
	}
}
