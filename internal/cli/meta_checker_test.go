package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

// busyCandidate has a row for every field, plus the title-only warning, so the
// fields page is as tall as it gets.
func busyCandidate() metasource.Candidate {
	c := editCandidate()
	c.Languages = []string{"fr"}
	c.CoverURL = "https://covers.openlibrary.org/b/id/1-L.jpg"
	c.Identifiers = append(c.Identifiers,
		ebookmeta.Identifier{Scheme: "google", Value: "g1"}, ebookmeta.Identifier{Scheme: "openlibrary", Value: "OL1"},
		ebookmeta.Identifier{Scheme: "douban", Value: "5"}, ebookmeta.Identifier{Scheme: "weread", Value: "7"})
	c.Match, c.Score = metasource.MatchTitle, 0.8
	return c
}

func busyFieldsPage(t *testing.T, w, h int) metaPageModel {
	t.Helper()
	cover := func(context.Context, string) (*ebookmeta.Cover, error) { return nil, nil }
	m := metaPageAfterLookup(t, answerWith(metasource.Result{Candidates: []metasource.Candidate{busyCandidate()}}, nil, nil), nil, withMetaCover(cover))
	m, _ = metaStep(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = metaStep(t, m, metaKey("enter"))
	if m.stage != metaFields || len(m.plan) < 9 {
		t.Fatalf("stage = %v with %d rows", m.stage, len(m.plan))
	}
	return m
}

// Escaping a repeat search goes back to the results it replaced; only the
// first search, which has nothing to go back to, closes the page.
func TestMetaPageEscDuringARepeatSearchKeepsThePreviousResults(t *testing.T) {
	var queries []metasource.Query
	m := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil)
	before := m.query
	m, _ = metaStep(t, m, key("e"))
	m = clearInput(t, m)
	m = typeIntoEditor(t, m, "Dune")
	m, cmd := metaStep(t, m, key("enter"))
	if m.stage != metaLooking {
		t.Fatalf("stage = %v", m.stage)
	}
	m, quit := metaStep(t, m, key("esc"))
	if m.cancelled || m.stage != metaCandidates || len(m.res.Candidates) != 2 || m.query.Title != before.Title {
		t.Fatalf("esc: cancelled=%v stage=%v results=%d query=%q", m.cancelled, m.stage, len(m.res.Candidates), m.query.Title)
	}
	if quit != nil {
		t.Error("esc during a repeat search tried to quit")
	}
	// The abandoned search's answer arrives late and must be ignored.
	for _, c := range batchCmds(cmd) {
		if done, ok := c().(metaLookupDone); ok {
			m, _ = metaStep(t, m, done)
		}
	}
	if m.stage != metaCandidates || m.query.Title != before.Title || len(m.res.Candidates) != 2 {
		t.Errorf("a late answer replaced the results: stage=%v query=%q", m.stage, m.query.Title)
	}
	// With nothing to go back to, esc still cancels the page.
	first, err := newMetaPageModel(writeTestEPUB(t), func(ctx context.Context, q metasource.Query) (metasource.Result, error) {
		<-ctx.Done()
		return metasource.Result{}, ctx.Err()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.stage = metaLooking
	first, _ = metaStep(t, first, key("esc"))
	if !first.cancelled {
		t.Error("esc on the first search did not cancel the page")
	}
}

// What the panel shows is what the first search used: a source listed in the
// environment is on even if it cannot run, and fails with a message.
func TestDefaultMetaSourceChoicesMatchTheFirstSearch(t *testing.T) {
	t.Setenv(metaSourcesEnv, "openlibrary,weread")
	t.Setenv(wereadAPIKeyEnv, "")
	var weread metaSourceChoice
	for _, c := range defaultMetaSourceChoices() {
		if c.name == "weread" {
			weread = c
		}
	}
	if !weread.enabled || weread.available {
		t.Fatalf("a listed source without a key = %+v, want on but unavailable", weread)
	}
	m := metaPageModel{sources: defaultMetaSourceChoices(), query: metasource.Query{ISBNs: []string{"9780441013593"}}}
	if !m.wereadOn() || !strings.Contains(m.titleNote(), "WeRead needs a title") {
		t.Errorf("wereadOn=%v note=%q", m.wereadOn(), m.titleNote())
	}
	// In the panel it can be turned off but not back on without a key.
	var queries []metasource.Query
	page := metaPageAfterLookup(t, recordingLookup(twoCandidates(), &queries), nil,
		withMetaSources(defaultMetaSourceChoices(), func([]string) metaLookupFunc { return recordingLookup(twoCandidates(), &queries) }))
	page, _ = metaStep(t, page, key("s"))
	page, _ = metaStep(t, page, metaKey("up")) // WeRead
	page, _ = metaStep(t, page, metaKey("space"))
	if !strings.Contains(metaScreen(page), "[ ] WeRead") {
		t.Fatalf("it could not be turned off:\n%s", metaScreen(page))
	}
	page, _ = metaStep(t, page, metaKey("space"))
	if !strings.Contains(metaScreen(page), "[ ] WeRead") || !strings.Contains(page.message, wereadAPIKeyEnv) {
		t.Errorf("it was turned on without a key: %q\n%s", page.message, metaScreen(page))
	}
}

// help drops trailing hints, so every page keeps Esc by choosing a shorter
// wording, not only the candidates and no-match pages.
func TestMetaPageFootersKeepEscOnNarrowTerminals(t *testing.T) {
	var asked [][]string
	var queries []metasource.Query
	for _, w := range []int{57, 50, 44, 40} {
		build := map[string]func() metaPageModel{
			"fields": func() metaPageModel { return editPage(t, nil) },
			"value editor": func() metaPageModel {
				m := editPage(t, nil)
				m, _ = metaStep(t, m, key("e"))
				return m
			},
			"search editor": func() metaPageModel {
				m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
				m, _ = metaStep(t, m, key("e"))
				return m
			},
			"source panel": func() metaPageModel {
				m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
				m, _ = metaStep(t, m, key("s"))
				return m
			},
		}
		for name, mk := range build {
			m, _ := metaStep(t, mk(), tea.WindowSizeMsg{Width: w, Height: 24})
			got := footer(ansi.Strip(m.View().Content))
			if !strings.Contains(got, "esc") || strings.Contains(got, "…") || ansi.StringWidth(got) > w {
				t.Errorf("%s at %d columns: footer %q", name, w, got)
			}
		}
	}
}

// `zlib meta book --fetch --publisher X` is a usage error, on a terminal too:
// the page it would open cannot write that field.
func TestMetaFetchWithAWriteOptionIsRefusedBeforeOpeningThePage(t *testing.T) {
	interactiveMeta(t, true)
	p := writeTestEPUB(t)
	_, _, err := runMetaWithStderr(t, p, "--fetch", "--publisher", "X")
	if !isUsage(err) || !strings.Contains(err.Error(), "--publisher") {
		t.Fatalf("err = %v, want a usage error naming --publisher", err)
	}
}

// Choosing sources writes nothing anywhere: neither the working directory's
// .env nor the one in the configuration directory.
func TestMetaPageSourcePanelWritesNoEnvFileAnywhere(t *testing.T) {
	home := isolateConfigHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	var asked [][]string
	var queries []metasource.Query
	m := sourcesPage(t, fakeSourceChoices(), &asked, &queries)
	m, _ = metaStep(t, m, key("s"))
	m, _ = metaStep(t, m, metaKey("up"))
	m, _ = metaStep(t, m, metaKey("up"))
	m, _ = metaStep(t, m, metaKey("space"))
	metaStep(t, m, key("enter"))
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".env") {
			t.Errorf("a .env file was written: %s", path)
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		t.Error("a .env file was written in the working directory")
	}
}

// Without a terminal `runStandaloneShellCommand` must not tell a `zlib meta`
// user to use a Kindle command.
func TestStandaloneErrorOffATerminalNamesNoUnrelatedCommand(t *testing.T) {
	if isInteractive() {
		t.Skip("needs a process without a terminal")
	}
	err := runStandaloneShellCommand("/meta")
	if err == nil || !strings.Contains(err.Error(), "requires a terminal") || strings.Contains(strings.ToLower(err.Error()), "kindle") {
		t.Fatalf("err = %v", err)
	}
	if err := runStandaloneShellCommand("/kindle send x"); err == nil || !strings.Contains(err.Error(), "zlib kindle send") {
		t.Errorf("the Kindle hint was lost: %v", err)
	}
}
