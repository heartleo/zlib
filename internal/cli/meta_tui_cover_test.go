package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/heartleo/zlib/internal/zlib"
)

var tinyPNG = []byte("\x89PNG\r\n\x1a\nbody")

func withCover() metasource.Result {
	res := twoCandidates()
	res.Candidates[0].CoverURL = "https://covers.openlibrary.org/b/id/1-L.jpg"
	return res
}

// runWrite runs the commands apply returned and returns the write result.
func runWrite(t *testing.T, cmd tea.Cmd) metaWriteDone {
	t.Helper()
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaWriteDone); ok {
			return done
		}
	}
	t.Fatal("no write command ran")
	return metaWriteDone{}
}

func okCover(context.Context, string) (*ebookmeta.Cover, error) {
	return &ebookmeta.Cover{Data: tinyPNG, MediaType: "image/png"}, nil
}

func TestMetaPageCoverOnlySelectionIsWritten(t *testing.T) {
	var wrote *ebookmeta.Changes
	write := func(_ string, c *ebookmeta.Changes) error { wrote = c; return nil }
	m := metaPageAfterLookup(t, answerWith(withCover(), nil, nil), write, withMetaCover(okCover))
	m, _ = metaStep(t, m, metaKey("enter"))
	for i, ch := range m.plan {
		m.selected[i] = ch.Field == metasource.FieldCover
	}
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	if m.stage != metaWriting {
		t.Fatalf("stage = %v: %s", m.stage, m.message)
	}
	if done := runWrite(t, cmd); done.err != nil || wrote == nil || wrote.Cover == nil || wrote.Title != nil || wrote.Authors != nil {
		t.Fatalf("wrote %+v, err %v", wrote, done.err)
	}
}

func TestMetaPageCoverFailureWritesNothing(t *testing.T) {
	cover := func(context.Context, string) (*ebookmeta.Cover, error) {
		return nil, errors.New("the image is too small (30x40)")
	}
	written := false
	write := func(string, *ebookmeta.Changes) error { written = true; return nil }
	m := metaPageAfterLookup(t, answerWith(withCover(), nil, nil), write, withMetaCover(cover))
	m, _ = metaStep(t, m, metaKey("enter"))
	m, cmd := metaStep(t, m, metaKey("ctrl+s"))
	m, _ = metaStep(t, m, runWrite(t, cmd))
	if written || m.stage != metaFields || len(m.applied) != 0 {
		t.Fatalf("written = %v, stage = %v", written, m.stage)
	}
	if !strings.Contains(metaScreen(m), "Cover download failed, nothing was written: the image is too small (30x40)") {
		t.Errorf("message missing:\n%s", metaScreen(m))
	}
}

func TestMetaPageHintFillsGapsAndAddsISBNs(t *testing.T) {
	p := writeTestEPUB(t) // title "Before", no authors
	m, err := newMetaPageModel(p, nil, nil, withMetaHint(metaHint{Title: "Other", Authors: []string{"Frank Herbert"}, ISBNs: []string{"0-441-01359-7", "9780441013593", "", "bad"}}))
	if err != nil {
		t.Fatal(err)
	}
	if m.query.Title != "Before" || len(m.query.Authors) != 1 || len(m.query.ISBNs) != 1 || m.query.ISBNs[0] != "9780441013593" {
		t.Fatalf("query = %+v", m.query)
	}
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("")}); err != nil {
		t.Fatal(err)
	}
	m, err = newMetaPageModel(p, nil, nil, withMetaHint(metaHint{Title: "Dune"}))
	if err != nil || m.query.Title != "Dune" {
		t.Fatalf("a hint should make an empty file searchable: %+v, %v", m.query, err)
	}
}

func TestDownloadCompletionOffersFixMetadataForReadableFormats(t *testing.T) {
	d := newDownloadModel("1:abc", "", nil)
	for path, want := range map[string]bool{"book.epub": true, "book.PDF": true, "book.azw3": true, "book.txt": false, "book.djvu": false} {
		d.savedPath = path
		if got := slices.Contains(d.completeActions(), actionFixMetadata); got != want {
			t.Errorf("%s: Fix metadata offered = %v, want %v", path, got, want)
		}
	}
}

func TestShellDownloadFixMetadataActionSeedsFromTheRecordAndReturns(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake"})
	path := writeTestEPUB(t)
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "Book"}}
	m.open("list", list)
	m.previous = m.page
	download := newDownloadModel("1:abc", filepath.Dir(path), nil)
	download.state, download.savedPath, download.bookName = stateComplete, path, "Book"
	download.book = zlib.Book{Name: "Dune", Authors: []string{"Frank Herbert"}, ISBN13: "9780441013593"}
	m.open("download", download)

	m, _ = shellStep(m, key("left")) // wraps from Back to the last action
	m, cmd := shellStep(m, key("enter"))
	if cmd == nil {
		t.Fatal("the action did not leave the completion page")
	}
	m, _ = shellStep(m, cmd())
	page, ok := m.page.(metaPageModel)
	if !ok || m.kind != "meta" || m.deliveryReturn == nil {
		t.Fatalf("kind = %q, page = %T", m.kind, m.page)
	}
	if page.query.Title != "Before" || len(page.query.ISBNs) != 1 || page.query.ISBNs[0] != "9780441013593" {
		t.Fatalf("the Z-Library ISBN was not searched: %+v", page.query)
	}

	page.applied = []string{"authors", "identifier:isbn"}
	m.metaDone(page)
	if m.kind != "download" || m.deliveryReturn != nil || !strings.Contains(ansi.Strip(m.View().Content), "Updated 2 fields in") {
		t.Fatalf("kind = %q:\n%s", m.kind, ansi.Strip(m.View().Content))
	}

	// Cancelling leaves the saved-file page without a message.
	back := m.page.(downloadModel)
	m.deliveryReturn = &back
	page.cancelled = true
	m.metaDone(page)
	if m.kind != "download" || m.deliveryReturn != nil || strings.Contains(ansi.Strip(m.View().Content), "Updated") {
		t.Fatalf("kind = %q", m.kind)
	}
}
