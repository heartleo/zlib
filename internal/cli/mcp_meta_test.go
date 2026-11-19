package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callMetaTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// toolErrorText is the text of a failed call. The SDK reports a handler error as
// an error result, or as a protocol error, depending on the schema step.
func toolErrorText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s %v did not fail: %v", name, args, res.Content)
	}
	var out []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return strings.Join(out, " ")
}

// read_metadata shows what a local file holds and what a lookup would search
// for, with the name rename would give it, and never leaves the allowed folder.
func TestMCPReadMetadata(t *testing.T) {
	p := richEPUB(t)
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	var out mcpReadMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "read_metadata", map[string]any{"file_path": p}), &out)
	if out.Format != "epub" || out.File != p || out.Metadata.Title != "Dune" || len(out.Metadata.Authors) != 2 || out.Metadata.Publisher != "Ace Books" {
		t.Fatalf("result = %+v", out)
	}
	if out.Query.Title != "Whatever" || out.Query.TitleFrom != "file name" || out.SuggestedFileName != "Whatever - Nobody.epub" {
		t.Errorf("query %+v, suggested %q", out.Query, out.SuggestedFileName)
	}
	if !strings.HasPrefix(out.Metadata.Description, "A desert planet.") {
		t.Errorf("description = %q", out.Metadata.Description)
	}

	dir := t.TempDir()
	confined := connectMCPTestSession(t, mcpServerOptions{AllowedDir: realPath(t, dir)}, nil)
	if msg := toolErrorText(t, confined, "read_metadata", map[string]any{"file_path": p}); !strings.Contains(msg, "outside the allowed directory") {
		t.Errorf("a file outside --allowed-dir was read: %q", msg)
	}
	if msg := toolErrorText(t, session, "read_metadata", map[string]any{"file_path": filepath.Join(dir, "missing.epub")}); msg == "" {
		t.Error("a missing file was not refused")
	}
}

func realPath(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// lookup_metadata lists ranked matches; with a file each match says what it would
// change and which rows are safe, and nothing is written.
func TestMCPLookupMetadata(t *testing.T) {
	match := dune()
	match.Title = "Dune"
	match.Series, match.SeriesIndex = "Dune Chronicles", 1
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{match}})
	p := namedEPUB(t, "Whatever (Nobody).epub")
	before, _ := os.ReadFile(p)
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)

	var out mcpLookupMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "lookup_metadata", map[string]any{"file_path": p, "title": "Dune", "authors": []string{"Frank Herbert"}}), &out)
	if out.Query.Title != "Dune" || out.Query.TitleFrom != "edited" || out.Current == nil || out.Format != "epub" || len(out.Candidates) == 0 {
		t.Fatalf("result = %+v", out)
	}
	c := out.Candidates[0]
	if c.Rank != 1 || c.Title != "Dune" || len(c.Sources) == 0 || c.SuggestedFileName != "Dune - Frank Herbert.epub" {
		t.Errorf("candidate = %+v", c)
	}
	var title, series *mcpPlannedChange
	for i := range c.Changes {
		switch c.Changes[i].Field {
		case "title":
			title = &c.Changes[i]
		case "series":
			series = &c.Changes[i]
		}
	}
	if title == nil || title.Old != out.Current.Title || title.New != "Dune" {
		t.Errorf("the title change is missing or wrong: %+v", c.Changes)
	}
	// A series from a catalogue is listed on the match and as a change.
	if c.Series != "Dune Chronicles" || c.SeriesIndex != 1 {
		t.Errorf("the match's series = %q #%v", c.Series, c.SeriesIndex)
	}
	if series == nil || series.Old != "" || series.New != "Dune Chronicles #1" || !series.Fill {
		t.Errorf("the series change is missing or wrong: %+v", c.Changes)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("a lookup changed the file")
	}

	// Without a file there is nothing to compare with.
	var bare mcpLookupMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "lookup_metadata", map[string]any{"title": "Dune"}), &bare)
	if len(bare.Candidates) == 0 || len(bare.Candidates[0].Changes) != 0 || bare.Current != nil {
		t.Errorf("a file-less lookup = %+v", bare)
	}

	for args, want := range map[string]string{
		"":                                       "give file_path",
		`"isbn":"123"`:                           "not a valid ISBN",
		`"title":"x","sources":["librarything"]`: "unknown source",
	} {
		in := map[string]any{}
		switch args {
		case "":
		case `"isbn":"123"`:
			in["isbn"] = "123"
		default:
			in["title"], in["sources"] = "x", []string{"librarything"}
		}
		if msg := toolErrorText(t, session, "lookup_metadata", in); !strings.Contains(msg, want) {
			t.Errorf("%v: %q, want %q", in, msg, want)
		}
	}
}

// edit_metadata checks every value like the editor, previews with dry_run,
// writes what was asked, renames without replacing, and is held by the policy.
func TestMCPEditMetadata(t *testing.T) {
	p := namedEPUB(t, "Whatever (Nobody).epub")
	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	edit := map[string]any{"file_path": p, "title": "Dune", "authors": []string{"Frank Herbert"}, "isbn": "9780441013593", "rating": 4.5,
		"series": "Dune Chronicles", "series_index": 1, "tags": []string{"Fiction", "Classics"}}

	// A dry run reports the change and touches nothing.
	dry := map[string]any{"dry_run": true}
	for k, v := range edit {
		dry[k] = v
	}
	var preview mcpEditMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "edit_metadata", dry), &preview)
	if preview.Written || !preview.DryRun || preview.Metadata.Title != "Dune" || len(preview.Changes) < 6 {
		t.Fatalf("preview = %+v", preview)
	}
	if m, _, _ := ebookmeta.ReadFile(p); m.Title == "Dune" {
		t.Fatal("a dry run wrote the file")
	}

	// The real call writes, and renames in the user's format.
	real := map[string]any{"rename": true}
	for k, v := range edit {
		real[k] = v
	}
	var done mcpEditMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "edit_metadata", real), &done)
	want := filepath.Join(filepath.Dir(p), "Dune - Frank Herbert.epub")
	if !done.Written || done.File != want || done.RenamedFrom != p {
		t.Fatalf("done = %+v, want file %q", done, want)
	}
	m, _, err := ebookmeta.ReadFile(want)
	if err != nil || m.Title != "Dune" || m.Series != "Dune Chronicles" || m.SeriesIndex != 1 || m.Rating != 4.5 || m.Identifier("isbn") != "9780441013593" || len(m.Tags) != 2 {
		t.Fatalf("the file holds %+v, %v", m, err)
	}

	// Clearing, and a rename that cannot happen, leave the file alone.
	var cleared mcpEditMetadataResult
	decodeToolJSON(t, callMetaTool(t, session, "edit_metadata", map[string]any{"file_path": want, "clear": []string{"tags", "rating", "series"}}), &cleared)
	if m, _, _ := ebookmeta.ReadFile(want); len(m.Tags) != 0 || m.Rating != 0 || m.Series != "" || len(cleared.Changes) != 3 {
		t.Errorf("clear: %+v, %+v", m, cleared.Changes)
	}
	other := writeNamed(t, filepath.Dir(want), "Taken.epub", "other")
	if msg := toolErrorText(t, session, "edit_metadata", map[string]any{"file_path": want, "title": "Changed", "rename_to": "Taken"}); !strings.Contains(msg, "already exists") {
		t.Errorf("a clash: %q", msg)
	}
	if m, _, _ := ebookmeta.ReadFile(want); m.Title != "Dune" {
		t.Errorf("a refused rename still wrote the title: %q", m.Title)
	}
	if data, _ := os.ReadFile(other); string(data) != "other" {
		t.Error("another file was replaced")
	}

	// Bad values are refused with the reason, and nothing is written.
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"rating":       {map[string]any{"rating": 7}, "0 to 5"},
		"isbn":         {map[string]any{"isbn": "12345"}, "not a valid ISBN"},
		"date":         {map[string]any{"published": "last spring"}, "must look like"},
		"language":     {map[string]any{"languages": []string{"klingon!"}}, "language code"},
		"author &":     {map[string]any{"authors": []string{"A & B"}}, "separates entries"},
		"tag comma":    {map[string]any{"tags": []string{"a, b"}}, "separates entries"},
		"clear title":  {map[string]any{"clear": []string{"title"}}, "cannot be cleared"},
		"uuid":         {map[string]any{"identifiers": map[string]string{"uuid": "x"}}, "cannot be written"},
		"cover host":   {map[string]any{"cover_url": "https://evil.example/c.jpg"}, "cover host"},
		"both renames": {map[string]any{"rename": true, "rename_to": "x"}, "not both"},
		"nothing":      {map[string]any{}, "nothing to change"},
		"already so":   {map[string]any{"title": "Dune"}, "nothing to change"},
		"series index": {map[string]any{"series_index": 2}, "no series"},
	} {
		args := map[string]any{"file_path": want}
		for k, v := range tc.args {
			args[k] = v
		}
		if msg := toolErrorText(t, session, "edit_metadata", args); !strings.Contains(strings.ToLower(msg), strings.ToLower(tc.want)) {
			t.Errorf("%s: %q, want %q", name, msg, tc.want)
		}
	}
	if m, _, _ := ebookmeta.ReadFile(want); m.Title != "Dune" || m.Rating != 0 {
		t.Errorf("a refused edit wrote the file: %+v", m)
	}

	// The policy hides the tool, and a path outside --allowed-dir is refused.
	for _, opts := range []mcpServerOptions{{ReadOnly: true}, {DisableMetadataEdit: true}} {
		ro := connectMCPTestSession(t, opts, nil)
		if _, err := ro.CallTool(context.Background(), &mcp.CallToolParams{Name: "edit_metadata", Arguments: map[string]any{"file_path": want, "title": "X"}}); err == nil {
			t.Errorf("edit_metadata is callable with %+v", opts)
		}
	}
	confined := connectMCPTestSession(t, mcpServerOptions{AllowedDir: realPath(t, t.TempDir())}, nil)
	if msg := toolErrorText(t, confined, "edit_metadata", map[string]any{"file_path": want, "title": "X"}); !strings.Contains(msg, "outside the allowed directory") {
		t.Errorf("confinement: %q", msg)
	}
}
