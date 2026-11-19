package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
)

type fakeMetaSource struct {
	name  string
	cands []metasource.Candidate
	err   error
	got   *metasource.Query
}

func (f *fakeMetaSource) Name() string { return f.name }
func (f *fakeMetaSource) Identify(_ context.Context, q metasource.Query) ([]metasource.Candidate, error) {
	if f.got != nil {
		*f.got = q
	}
	return f.cands, f.err
}

func useMetaSources(t *testing.T, sources ...metasource.Source) {
	t.Helper()
	old, oldFor := newMetaSources, newMetaSourcesFor
	newMetaSources = func(http.RoundTripper) []metasource.Source { return sources }
	// The source panel builds its lookups through newMetaSourcesFor: keep it
	// offline too, whatever sources are asked for.
	newMetaSourcesFor = func([]string) []metasource.Source { return sources }
	t.Cleanup(func() { newMetaSources, newMetaSourcesFor = old, oldFor })
}

func dune() metasource.Candidate {
	return metasource.Candidate{
		Title: "Before", Authors: []string{"Frank Herbert"}, Publisher: "Ace", Published: "2005-08-02",
		Description: "A desert planet.\nSecond paragraph.", EditionLevel: true,
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9780441013593"}},
	}
}

func TestMetaFetchListsCandidatesAndWritesNothing(t *testing.T) {
	var got metasource.Query
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}, got: &got})
	p := writeTestEPUB(t)
	before, _ := os.ReadFile(p)

	out, stderr, err := runMetaWithStderr(t, p, "--fetch")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Before" {
		t.Errorf("query = %+v", got)
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"1. Before", "title match", "Fake", "Authors       Frank Herbert", "Identifiers   isbn:9780441013593", "Description   A desert planet."} {
		if !strings.Contains(plain, want) {
			t.Errorf("output missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Second paragraph") {
		t.Errorf("description not cut to its first line:\n%s", plain)
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Fatal("--fetch changed the file")
	}
}

func TestMetaFetchFlagsOverrideTheQuery(t *testing.T) {
	var got metasource.Query
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}, got: &got})
	p := writeTestEPUB(t)
	if _, _, err := runMetaWithStderr(t, p, "--fetch", "-t", "Dune", "-a", "Frank Herbert & Someone", "--isbn", "0-441-01359-7"); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Dune" || len(got.Authors) != 2 || len(got.ISBNs) != 1 || got.ISBNs[0] != "9780441013593" {
		t.Fatalf("query = %+v", got)
	}
}

func TestMetaFetchJSON(t *testing.T) {
	useMetaSources(t,
		&fakeMetaSource{name: "Good", cands: []metasource.Candidate{dune()}},
		&fakeMetaSource{name: "Limited", err: metasource.ErrRateLimited})
	out, stderr, err := runMetaWithStderr(t, writeTestEPUB(t), "--fetch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Query      map[string]any   `json:"query"`
		Candidates []map[string]any `json:"candidates"`
		Errors     []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if doc.Query["title"] != "Before" || len(doc.Candidates) != 1 || doc.Candidates[0]["title"] != "Before" ||
		doc.Candidates[0]["match"] != "title" || len(doc.Errors) != 1 || doc.Errors[0]["source"] != "Limited" {
		t.Fatalf("doc = %+v", doc)
	}
	if !strings.Contains(stderr, "Limited: rate limited") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestMetaFetchExitCodes(t *testing.T) {
	p := writeTestEPUB(t)
	cases := []struct {
		name    string
		sources []metasource.Source
		args    []string
		want    int
	}{
		{"nothing found", []metasource.Source{&fakeMetaSource{name: "A"}}, []string{"--fetch"}, exitError},
		{"every source failed", []metasource.Source{&fakeMetaSource{name: "A", err: errors.New("boom")}, &fakeMetaSource{name: "B", err: metasource.ErrRateLimited}}, []string{"--fetch"}, exitNetwork},
		{"write flag with fetch", nil, []string{"--fetch", "--cover", "x.png"}, exitUsage},
		{"invalid isbn", nil, []string{"--fetch", "--isbn", "123"}, exitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useMetaSources(t, tc.sources...)
			_, _, err := runMetaWithStderr(t, append([]string{p}, tc.args...)...)
			if err == nil || exitCode(err) != tc.want {
				t.Fatalf("err = %v, exit = %d, want %d", err, exitCode(err), tc.want)
			}
		})
	}
}

func TestMetaFetchEmptyQueryIsAUsageError(t *testing.T) {
	useMetaSources(t, &fakeMetaSource{name: "A"})
	p := writeTestEPUB(t)
	if err := ebookmeta.WriteFile(p, &ebookmeta.Changes{Title: new("")}); err != nil {
		t.Fatal(err)
	}
	_, _, err := runMetaWithStderr(t, p, "--fetch")
	if exitCode(err) != exitUsage || !strings.Contains(err.Error(), "pass -t, -a or --isbn") {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaTransportFollowsZlibProxy(t *testing.T) {
	t.Setenv("ZLIB_PROXY", "")
	if metaTransport() != nil {
		t.Error("an unset proxy should use the default transport")
	}
	t.Setenv("ZLIB_PROXY", "http://127.0.0.1:9")
	rt := metaTransport()
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if u, err := rt.(*http.Transport).Proxy(req); err != nil || u.Host != "127.0.0.1:9" {
		t.Fatalf("proxy = %v, %v", u, err)
	}
	t.Setenv("ZLIB_PROXY", "ftp://nope")
	if _, err := metaTransport().(*http.Transport).Proxy(req); err == nil || !strings.Contains(err.Error(), "invalid ZLIB_PROXY") {
		t.Fatalf("err = %v", err)
	}
}
