package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

func writeNamed(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenameBookMovesOnlyWithinTheDirectoryAndNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	src := writeNamed(t, dir, "a.epub", "one")
	other := writeNamed(t, dir, "taken.epub", "two")

	got, err := renameBook(src, "Dune - Frank Herbert.epub")
	if err != nil || got != filepath.Join(dir, "Dune - Frank Herbert.epub") {
		t.Fatalf("renamed to %q, %v", got, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the old name is still there: %v", err)
	}
	if data, _ := os.ReadFile(got); string(data) != "one" {
		t.Errorf("the content changed: %q", data)
	}

	// Another file with the wanted name is never replaced.
	if _, err := renameBook(got, "taken.epub"); err != errRenameTaken {
		t.Errorf("a clash gave %v", err)
	}
	if data, _ := os.ReadFile(other); string(data) != "two" {
		t.Errorf("the other file was replaced: %q", data)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("a refused rename moved the file: %v", err)
	}

	for _, name := range []string{"", "../x.epub", "sub/x.epub", `sub\x.epub`, ".", "..", "x.mobi", "x"} {
		if _, err := renameBook(got, name); err == nil {
			t.Errorf("renameBook accepted %q", name)
		}
	}
	// The same name is no change.
	if p, err := renameBook(got, filepath.Base(got)); err != nil || p != got {
		t.Errorf("the same name: %q, %v", p, err)
	}
	// A name with another case for the extension is the same extension.
	if p, err := renameBook(got, "Dune.EPUB"); err != nil || filepath.Base(p) != "Dune.EPUB" {
		t.Errorf("an upper-case extension: %q, %v", p, err)
	}
}

// A name another name for the same file (a hard link) is refused: rename would
// do nothing and leave two names.
func TestRenameBookRefusesAHardLinkToItself(t *testing.T) {
	dir := t.TempDir()
	src := writeNamed(t, dir, "a.epub", "one")
	if err := os.Link(src, filepath.Join(dir, "b.epub")); err != nil {
		t.Skip("hard links are not available here")
	}
	if _, err := renameBook(src, "b.epub"); err != errRenameTaken {
		t.Errorf("a hard link gave %v", err)
	}
}

// A rename that only changes case works where the file system ignores case,
// and is a plain rename where it does not.
func TestRenameBookAllowsACaseOnlyChange(t *testing.T) {
	dir := t.TempDir()
	src := writeNamed(t, dir, "dune.epub", "x")
	got, err := renameBook(src, "Dune.epub")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "Dune.epub" || filepath.Base(got) != "Dune.epub" {
		t.Errorf("entries = %v, got %q", entries, got)
	}
}

// renameFixture is a book with a title and an author in its metadata, named the
// way a download names it, and a page on it that records where it writes.
func renameFixture(t *testing.T) (metaPageModel, *[]string, string) {
	t.Helper()
	p := namedEPUB(t, "Whatever (Nobody).epub")
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("Dune"), Authors: &[]string{"Frank Herbert"}}); err != nil {
		t.Fatal(err)
	}
	var wrote []string
	write := func(path string, c *ebookmeta.Changes) error { wrote = append(wrote, path); return nil }
	var queries []metasource.Query
	m, err := newMetaPageModel(p, recordingLookup(twoCandidates(), &queries), write)
	if err != nil {
		t.Fatal(err)
	}
	return m, &wrote, p
}

// chooseRename moves the rename page's cursor to the option with this label.
func chooseRename(t *testing.T, m metaPageModel, label string) metaPageModel {
	t.Helper()
	for i, o := range m.renameOpts {
		if o.label != label {
			continue
		}
		for m.renameCursor != i {
			m, _ = metaStep(t, m, metaKey("down"))
		}
		return m
	}
	t.Fatalf("no %q option in %+v", label, m.renameOpts)
	return m
}

func TestMetaPageRenamesFromTheFilesTitleBeforeAnySearch(t *testing.T) {
	m, _, p := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	if m.stage != metaRename {
		t.Fatalf("n did not open the rename page: stage %v", m.stage)
	}
	screen := metaScreen(m)
	for _, want := range []string{"Rename the file", "Whatever (Nobody).epub", "Search terms", "Whatever - Nobody.epub", "The file's own title", "Dune - Frank Herbert.epub", "Type a name"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the rename page lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "Selected match") {
		t.Errorf("a match is offered before any search:\n%s", screen)
	}
	// The search terms, which start from the file name, come first and are
	// the default: an embedded title often carries a marketing line.
	if m.renameOpts[m.renameCursor].label != "Search terms" {
		t.Errorf("the page starts on %q", m.renameOpts[m.renameCursor].label)
	}
	m = chooseRename(t, m, "The file's own title")
	m, _ = metaStep(t, m, key("enter"))
	want := filepath.Join(filepath.Dir(p), "Dune - Frank Herbert.epub")
	if m.path != want || m.stage != metaPrepare || !strings.Contains(m.message, "Renamed to Dune - Frank Herbert.epub") {
		t.Fatalf("path %q stage %v message %q", m.path, m.stage, m.message)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the new file is missing: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("the old file is still there: %v", err)
	}
	if !strings.Contains(metaScreen(m), "Dune - Frank Herbert.epub") {
		t.Errorf("the page still shows the old name:\n%s", metaScreen(m))
	}
	if got := m.summary(); got != "Renamed to Dune - Frank Herbert.epub" {
		t.Errorf("summary = %q", got)
	}
}

func TestMetaPageOffersTheSelectedMatchAndWritesToTheNewPath(t *testing.T) {
	m, wrote, p := renameFixture(t)
	m = searchFromPrepare(t, m)
	m, _ = metaStep(t, m, key("n"))
	screen := metaScreen(m)
	cand := m.res.Candidates[m.cursor]
	wantName := metasource.FileNameWith(m.nameLayout, cand.Title, cand.Authors, ".epub")
	if wantName == "" || !strings.Contains(screen, "Selected match") {
		t.Fatalf("no match option (%q):\n%s", wantName, screen)
	}
	// Move to the match row and rename.
	for i, o := range m.renameOpts {
		if o.label == "Selected match" {
			for m.renameCursor != i {
				m, _ = metaStep(t, m, metaKey("down"))
			}
		}
	}
	m, _ = metaStep(t, m, key("enter"))
	if filepath.Base(m.path) != wantName || m.stage != metaCandidates {
		t.Fatalf("path %q stage %v message %q", m.path, m.stage, m.message)
	}
	// The fields are written to the file under its new name.
	m, _ = metaStep(t, m, key("enter")) // review fields
	m = applyAndWait(t, m)
	if len(*wrote) != 1 || (*wrote)[0] != m.path || m.path == p {
		t.Errorf("wrote %v, page path %q, old path %q", *wrote, m.path, p)
	}
	if got := m.summary(); !strings.Contains(got, "and renamed to "+wantName) {
		t.Errorf("summary = %q", got)
	}
}

func TestMetaPageRenameRefusesAClashAndKeepsTheFile(t *testing.T) {
	m, _, p := renameFixture(t)
	taken := writeNamed(t, filepath.Dir(p), "Whatever - Nobody.epub", "other")
	m, _ = metaStep(t, m, key("n"))
	// The name that is taken says so on its row, and the page starts past it.
	if !strings.Contains(metaScreen(m), "already exists") || m.renameOpts[m.renameCursor].label == "Search terms" {
		t.Errorf("the clash is not shown, or the page starts on it:\n%s", metaScreen(m))
	}
	m = chooseRename(t, m, "Search terms")
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaRename || m.path != p || !strings.Contains(m.message, "already exists") {
		t.Fatalf("stage %v path %q message %q", m.stage, m.path, m.message)
	}
	if data, _ := os.ReadFile(taken); string(data) != "other" {
		t.Errorf("the other file was replaced: %q", data)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the file moved: %v", err)
	}
	if m.renamedFrom != "" {
		t.Errorf("a refused rename was recorded: %q", m.renamedFrom)
	}
}

func TestMetaPageRenameMarksTheCurrentNameAndEscGoesBack(t *testing.T) {
	m, _, p := renameFixture(t)
	good := filepath.Join(filepath.Dir(p), "Dune - Frank Herbert.epub")
	if err := os.Rename(p, good); err != nil {
		t.Fatal(err)
	}
	m.path = good
	m, _ = metaStep(t, m, key("n"))
	if !strings.Contains(metaScreen(m), "(current)") {
		t.Errorf("the current name is not marked:\n%s", metaScreen(m))
	}
	// The page starts on the first name that would change something; go back to
	// the marked one.
	for m.renameChoiceFor(m.renameOpts[m.renameCursor]).note == "" {
		m, _ = metaStep(t, m, metaKey("up"))
	}
	m, _ = metaStep(t, m, key("enter"))
	if m.stage != metaRename || !strings.Contains(m.message, "already the file") || m.path != good {
		t.Errorf("stage %v message %q path %q", m.stage, m.message, m.path)
	}
	m, _ = metaStep(t, m, key("esc"))
	if m.stage != metaPrepare || m.cancelled || m.renamedFrom != "" {
		t.Errorf("esc: stage %v cancelled %v renamedFrom %q", m.stage, m.cancelled, m.renamedFrom)
	}
}

func TestMetaPageRenamesToATypedName(t *testing.T) {
	m, _, p := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	m, _ = metaStep(t, m, key("e")) // edit the highlighted name
	if !m.editing || m.editKind != editName {
		t.Fatalf("e did not open the name editor: editing=%v kind=%v", m.editing, m.editKind)
	}
	if got := m.editor.values()[0]; got != "Whatever - Nobody" {
		t.Errorf("the editor holds %q, want the proposal without its extension", got)
	}
	// An unusable name is refused in place.
	for range len("Whatever - Nobody") {
		m, _ = metaStep(t, m, key("backspace"))
	}
	m, _ = metaStep(t, m, key("enter"))
	if !m.editing || !strings.Contains(m.editor.err, "Enter a name") {
		t.Fatalf("an empty name: editing=%v err=%q", m.editing, m.editor.err)
	}
	m = typeIntoEditor(t, m, `Dune: Messiah / part 1?`)
	m, _ = metaStep(t, m, key("enter"))
	want := filepath.Join(filepath.Dir(p), "Dune  Messiah - part 1.epub")
	if m.editing || m.path != want {
		t.Fatalf("editing=%v path %q, want %q (message %q)", m.editing, m.path, want, m.message)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the new file is missing: %v", err)
	}
}

func TestMetaPageTypedNameIsRefusedWhenTaken(t *testing.T) {
	m, _, p := renameFixture(t)
	writeNamed(t, filepath.Dir(p), "Taken.epub", "other")
	m, _ = metaStep(t, m, key("n"))
	m, _ = metaStep(t, m, key("e"))
	for range len("Whatever - Nobody") {
		m, _ = metaStep(t, m, key("backspace"))
	}
	m = typeIntoEditor(t, m, "Taken")
	m, _ = metaStep(t, m, key("enter"))
	if !m.editing || !strings.Contains(m.editor.err, "already exists") || m.path != p {
		t.Errorf("editing=%v err=%q path=%q", m.editing, m.editor.err, m.path)
	}
}

// The saved-file page of a download sends its path to Kindle and WeRead, so a
// rename must reach it, even when the page is left with Esc afterwards.
func TestShellMetaDoneGivesTheDownloadPageTheNewPath(t *testing.T) {
	m, _, p := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	m, _ = metaStep(t, m, key("enter"))
	m, _ = metaStep(t, m, key("esc")) // leaves the page
	if !m.cancelled {
		t.Fatalf("esc did not leave the page")
	}
	shell := newShellModel()
	shell.deliveryReturn = &downloadModel{savedPath: p}
	shell.metaDone(m)
	page, ok := shell.page.(downloadModel)
	if !ok || page.savedPath != m.path || page.savedPath == p {
		t.Fatalf("page = %T %+v, want the renamed path %q", shell.page, shell.page, m.path)
	}
	if !strings.Contains(page.completeMessage, "Renamed to Whatever - Nobody.epub") {
		t.Errorf("message = %q", page.completeMessage)
	}
}

func TestMetaPageRenameFitsCompactTerminals(t *testing.T) {
	m, _, _ := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 12}, {24, 8}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := next.(metaPageModel).View().Content
		lines := strings.Split(ansi.Strip(view), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line wider than the terminal: %q", size[0], size[1], l)
				break
			}
		}
		if got := footer(ansi.Strip(view)); !strings.Contains(got, "esc") {
			t.Errorf("%dx%d: footer %q lost esc", size[0], size[1], got)
		}
		// The highlighted name can be read in full, wrapped if need be.
		squashed := strings.Join(strings.Fields(ansi.Strip(view)), "")
		if size[1] >= 12 && !strings.Contains(squashed, "Whatever-Nobody.epub") {
			t.Errorf("%dx%d: the highlighted name is not on screen:\n%s", size[0], size[1], ansi.Strip(view))
		}
	}
}

// The note that a name is the file's current one is on the row, not cut off
// after a long name.
func TestMetaPageRenameKeepsTheCurrentNameNoteOnNarrowTerminals(t *testing.T) {
	m, _, p := renameFixture(t)
	good := filepath.Join(filepath.Dir(p), "Dune - Frank Herbert.epub")
	if err := os.Rename(p, good); err != nil {
		t.Fatal(err)
	}
	m.path = good
	m, _ = metaStep(t, m, key("n"))
	for _, width := range []int{60, 50, 40} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		if view := ansi.Strip(next.(metaPageModel).View().Content); !strings.Contains(view, "(current)") {
			t.Errorf("%d columns: the note is cut off:\n%s", width, view)
		}
	}
}

func TestMetaCommandRenameNamesTheFileFromItsTitleAndAuthors(t *testing.T) {
	p := namedEPUB(t, "Whatever (Nobody).epub")
	out, errOut, err := runMetaWithStderr(t, p, "-t", "Dune: Messiah", "-a", "Frank Herbert", "--rename", "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(p), "Dune  Messiah - Frank Herbert.epub")
	var doc struct {
		File        string `json:"file"`
		RenamedFrom string `json:"renamed_from"`
		Title       string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.File != want || doc.RenamedFrom != p || doc.Title != "Dune: Messiah" {
		t.Errorf("doc = %+v, want file %q", doc, want)
	}
	if !strings.Contains(errOut, "Renamed to Dune  Messiah - Frank Herbert.epub") {
		t.Errorf("stderr = %q", errOut)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the new file is missing: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("the old name is still there: %v", err)
	}
	// The title in the file keeps its colon: only the name changed.
	if m, _, err := ebookmeta.ReadFile(want); err != nil || m.Title != "Dune: Messiah" {
		t.Errorf("the file's title = %q, %v", m.Title, err)
	}
	// Run again: the name is already right, and nothing is reported.
	_, errOut, err = runMetaWithStderr(t, want, "--rename")
	if err != nil || strings.Contains(errOut, "Renamed") {
		t.Errorf("a second run: %v, stderr %q", err, errOut)
	}
}

func TestMetaCommandRenameToTakesATypedName(t *testing.T) {
	p := namedEPUB(t, "Whatever (Nobody).epub")
	if _, _, err := runMetaWithStderr(t, p, "--rename-to", `My: "Book".epub`); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(p), "My  'Book'.epub")
	if _, err := os.Stat(want); err != nil {
		entries, _ := os.ReadDir(filepath.Dir(p))
		t.Fatalf("missing %q: %v (have %v)", want, err, entries)
	}
}

// noTitleEPUB is a book with no title in it and a name that gives none.
func noTitleEPUB(t *testing.T) string {
	t.Helper()
	p := emptyMetadataEPUB(t)
	q := filepath.Join(filepath.Dir(p), "9780441013593.epub")
	if err := os.Rename(p, q); err != nil {
		t.Fatal(err)
	}
	return q
}

// A name is built from the title and authors the lookup would search for, not
// from an embedded title that carries a marketing line.
func TestMetaCommandRenameIgnoresAMarketingLineInTheEmbeddedTitle(t *testing.T) {
	p := namedEPUB(t, "Dune (Frank Herbert).epub")
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("Dune: the classic masterpiece, now a major film, soon in cinemas"), Authors: &[]string{"Frank Herbert"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runMetaWithStderr(t, p, "--rename"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), "Dune - Frank Herbert.epub")); err != nil {
		entries, _ := os.ReadDir(filepath.Dir(p))
		t.Errorf("the name was not built from the clean title: %v", entries)
	}
}

func TestMetaCommandRenameRefusesWithoutHarm(t *testing.T) {
	interactiveMeta(t, false)
	p := namedEPUB(t, "Whatever (Nobody).epub")
	taken := writeNamed(t, filepath.Dir(p), "Taken.epub", "other")
	for _, tc := range []struct {
		args  []string
		usage bool
		want  string
	}{
		{[]string{p, "--rename", "--rename-to", "x"}, true, "not both"},
		{[]string{p, "--rename-to", "  "}, true, "needs a name"},
		{[]string{p, "--fetch", "--rename"}, true, "cannot be combined with --rename"},
		{[]string{p, "--fetch", "--rename-to", "x"}, true, "cannot be combined with --rename-to"},
		{[]string{p, "--rename-to", "Taken"}, false, "already exists"},
		{[]string{noTitleEPUB(t), "--rename"}, false, "no title"},
	} {
		_, _, err := runMetaWithStderr(t, tc.args...)
		if err == nil || isUsage(err) != tc.usage || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v (usage %v), want %q", tc.args, err, isUsage(err), tc.want)
		}
	}
	if data, _ := os.ReadFile(taken); string(data) != "other" {
		t.Errorf("the other file changed: %q", data)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("a refused rename moved the file: %v", err)
	}
}

// --fetch only reads: it says what the name would be and renames nothing.
func TestMetaFetchJSONSuggestsAFileNamePerCandidate(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}})
	p := namedEPUB(t, "Before (Frank Herbert).epub")
	out, _, err := runMetaWithStderr(t, p, "--fetch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Candidates []struct {
			Title             string `json:"title"`
			SuggestedFileName string `json:"suggested_file_name"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Candidates) != 1 {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := doc.Candidates[0].SuggestedFileName; got != "Before - Frank Herbert.epub" {
		t.Errorf("suggested name = %q", got)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("--fetch moved the file: %v", err)
	}
}

// A refusal that is known before anything is written writes nothing: the
// metadata options of the same command are not applied.
func TestMetaCommandRenameRefusesBeforeWritingAnyField(t *testing.T) {
	interactiveMeta(t, false)
	p := namedEPUB(t, "Whatever (Nobody).epub")
	writeNamed(t, filepath.Dir(p), "Taken.epub", "other")
	for _, args := range [][]string{
		{p, "-t", "Changed", "--rename", "--rename-to", "y"},
		{p, "-t", "Changed", "--rename-to", "  "},
		{p, "-t", "Changed", "--rename-to", "Taken"},
	} {
		if _, _, err := runMetaWithStderr(t, args...); err == nil {
			t.Errorf("%v: no error", args)
		}
		if m, _, err := ebookmeta.ReadFile(p); err != nil || m.Title != "Before" {
			t.Errorf("%v: the title was written anyway: %q, %v", args, m.Title, err)
		}
	}
	// --rename on a file that gives no title fails before its other options run.
	q := noTitleEPUB(t)
	if _, _, err := runMetaWithStderr(t, q, "--tags", "fiction", "--rename"); err == nil || !strings.Contains(err.Error(), "no title") {
		t.Fatalf("err = %v", err)
	}
	if m, _, _ := ebookmeta.ReadFile(q); len(m.Tags) != 0 {
		t.Errorf("the tags were written anyway: %v", m.Tags)
	}
}

// The format is detected from the contents, so a book may have no extension, and
// a dot in its name is part of the name, not an extension.
func TestMetaCommandRenamesAFileWithNoExtension(t *testing.T) {
	src := namedEPUB(t, "tmp.epub")
	p := filepath.Join(filepath.Dir(src), "J.R.R. Tolkien - The Hobbit")
	if err := os.Rename(src, p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runMetaWithStderr(t, p, "--rename-to", "The Hobbit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), "The Hobbit")); err != nil {
		entries, _ := os.ReadDir(filepath.Dir(p))
		t.Fatalf("the file was not renamed: %v (have %v)", err, entries)
	}
	if bookExt("J.R.R. Tolkien - The Hobbit") != "" || bookExt("A.EPUB") != ".epub" {
		t.Errorf("bookExt = %q, %q", bookExt("J.R.R. Tolkien - The Hobbit"), bookExt("A.EPUB"))
	}
}

// The format switch recomputes every name, F2 opens the page like n, and the
// chosen format is what the rename uses.
func TestMetaPageRenameSwitchesTheFormat(t *testing.T) {
	m, _, p := renameFixture(t)
	m, _ = metaStep(t, m, key("f2"))
	if m.stage != metaRename {
		t.Fatalf("F2 did not open the rename page: stage %v", m.stage)
	}
	m = chooseRename(t, m, "The file's own title")
	for _, tc := range []struct{ press, want string }{
		{"right", "Frank Herbert - Dune.epub"},
		{"3", "Dune (Frank Herbert).epub"},
		{"left", "Frank Herbert - Dune.epub"},
		{"1", "Dune - Frank Herbert.epub"},
	} {
		m, _ = metaStep(t, m, key(tc.press))
		if screen := metaScreen(m); !strings.Contains(screen, "New  "+tc.want) || !strings.Contains(screen, "ready") {
			t.Fatalf("after %s the new name is not %q:\n%s", tc.press, tc.want, screen)
		}
	}
	m, _ = metaStep(t, m, key("3"))
	m, _ = metaStep(t, m, key("enter"))
	if want := filepath.Join(filepath.Dir(p), "Dune (Frank Herbert).epub"); m.path != want {
		t.Errorf("path %q, want %q", m.path, want)
	}
}

// Ctrl+D saves the format as the default through the page's saver, and says
// when it cannot.
func TestMetaPageRenameSavesTheFormat(t *testing.T) {
	m, _, p := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	m, _ = metaStep(t, m, key("ctrl+d"))
	if !strings.Contains(m.message, "cannot be saved") {
		t.Errorf("no saver: %q", m.message)
	}
	var saved metasource.NameLayout
	m, err := newMetaPageModel(p, recordingLookup(twoCandidates(), new([]metasource.Query)), nil,
		withMetaLayoutSaver(func(l metasource.NameLayout) (string, error) { saved = l; return "", nil }))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = metaStep(t, m, key("n"))
	m, _ = metaStep(t, m, key("2"))
	m, _ = metaStep(t, m, key("ctrl+d"))
	if saved != metasource.LayoutAuthorTitle || !strings.Contains(m.message, "Author - Title") {
		t.Errorf("saved %q, message %q", saved, m.message)
	}
}

// The saved format reaches the settings file and this process, and a real
// environment variable keeps its precedence.
func TestSaveNameLayoutWritesTheSetting(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(nameLayoutEnv, "")
	store := testSettingsStore(t)
	if overridden, err := saveNameLayoutTo(store, metasource.LayoutTitleParen); err != nil || overridden != "" {
		t.Fatalf("overridden %q, err %v", overridden, err)
	}
	if saved, _ := readSettingsEnv(store.global); saved[nameLayoutEnv] != "title-paren" || metaNameLayout() != metasource.LayoutTitleParen {
		t.Errorf("saved %v, layout %q", saved, metaNameLayout())
	}
	store.env = map[string]string{nameLayoutEnv: "author-title"}
	if overridden, err := saveNameLayoutTo(store, metasource.LayoutTitleAuthor); err != nil || overridden == "" {
		t.Errorf("an environment value is not reported as overriding: %q, %v", overridden, err)
	}
	// A hand-edited bad value falls back to the default.
	t.Setenv(nameLayoutEnv, "sideways")
	if metaNameLayout() != metasource.DefaultNameLayout {
		t.Errorf("a bad value gave %q", metaNameLayout())
	}
}

// The prepare and fields pages say what the file is called and what its title
// and authors would call it. The key that renames is in the footer, not
// repeated on the line.
func TestMetaPagesShowTheFileNameAndTheNameItWouldGet(t *testing.T) {
	m, _, _ := renameFixture(t)
	m = sized(t, m, 100, 30)
	screen := metaScreen(m)
	if !strings.Contains(screen, "File  Whatever (Nobody).epub → Whatever - Nobody.epub") || strings.Contains(screen, "n renames") {
		t.Errorf("the prepare page lacks the file line, or repeats the key:\n%s", screen)
	}
	if !strings.Contains(footer(screen), "n rename") {
		t.Errorf("the footer does not offer the rename key:\n%s", footer(screen))
	}
	m, _ = metaStep(t, m, key("m")) // edit the file's own metadata
	if screen := metaScreen(m); !strings.Contains(screen, "File  Whatever (Nobody).epub → Dune - Frank Herbert.epub") {
		t.Errorf("the fields page names the file from the title and authors it holds:\n%s", screen)
	}
	// A short terminal gives the line up rather than a source.
	if screen := metaScreen(sized(t, m, 60, 12)); strings.Contains(screen, "File  ") {
		t.Errorf("the file line is on a short terminal:\n%s", screen)
	}
}

// A configured custom pattern is the page's starting format and a fourth choice
// beside the presets.
func TestMetaPageRenameOffersTheCustomPattern(t *testing.T) {
	t.Setenv(nameLayoutEnv, "{title} [{authors}]")
	m, _, _ := renameFixture(t)
	m, _ = metaStep(t, m, key("n"))
	m = chooseRename(t, m, "The file's own title")
	if screen := metaScreen(m); !strings.Contains(screen, "‹ {title} [{authors}] › 4/4") || !strings.Contains(screen, "New  Dune [Frank Herbert].epub") {
		t.Fatalf("the custom pattern is not the starting format:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("1"))
	m, _ = metaStep(t, m, key("4"))
	if !strings.Contains(metaScreen(m), "New  Dune [Frank Herbert].epub") {
		t.Errorf("4 did not pick the custom pattern:\n%s", metaScreen(m))
	}
	m, _ = metaStep(t, m, key("right")) // wraps to the first preset
	if !strings.Contains(metaScreen(m), "New  Dune - Frank Herbert.epub") {
		t.Errorf("→ did not wrap:\n%s", metaScreen(m))
	}
}
