package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// namedEPUB copies the test EPUB (title "Before", no authors) to a file with
// the given name, as Z-Library would have saved it.
func namedEPUB(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(writeTestEPUB(t))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The command line builds its query the way the page does, and its --json says
// what the file name and the file offered and where each part of the query came
// from.
func TestMetaFetchJSONSaysWhereTheQueryCameFrom(t *testing.T) {
	var got metasource.Query
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}, got: &got})
	p := namedEPUB(t, "Before (Frank Herbert).epub")
	out, _, err := runMetaWithStderr(t, p, "--fetch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Query      metasource.Query `json:"query"`
		Extraction struct {
			FileName struct {
				Title   string   `json:"Title"`
				Authors []string `json:"Authors"`
			} `json:"file_name"`
			Embedded struct {
				Title string `json:"Title"`
			} `json:"embedded"`
			TitleFrom   string `json:"title_from"`
			AuthorsFrom string `json:"authors_from"`
		} `json:"extraction"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Extraction.FileName.Title != "Before" || len(doc.Extraction.FileName.Authors) != 1 || doc.Extraction.Embedded.Title != "Before" {
		t.Errorf("extraction = %+v", doc.Extraction)
	}
	if doc.Extraction.TitleFrom != "file name" || doc.Extraction.AuthorsFrom != "file name" || doc.Query.Title != "Before" || doc.Query.TitleGuessed {
		t.Errorf("query = %+v from %q / %q", doc.Query, doc.Extraction.TitleFrom, doc.Extraction.AuthorsFrom)
	}
	if got.Title != "Before" || len(got.Authors) != 1 || got.Authors[0] != "Frank Herbert" {
		t.Errorf("the sources were asked %+v", got)
	}
}

// -t, -a and --isbn are the user's own words: a title they give is no guess and
// an ISBN they give is confirmed.
func TestMetaFetchOverridesAreMarkedAsTyped(t *testing.T) {
	var got metasource.Query
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}, got: &got})
	p := namedEPUB(t, "Whatever (Nobody).epub")
	out, _, err := runMetaWithStderr(t, p, "--fetch", "--json", "-t", "Before", "-a", "Frank Herbert", "--isbn", "9780441013593")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Extraction struct {
			TitleFrom   string   `json:"title_from"`
			AuthorsFrom string   `json:"authors_from"`
			ISBNFrom    []string `json:"isbn_from"`
		} `json:"extraction"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Extraction.TitleFrom != "edited" || doc.Extraction.AuthorsFrom != "edited" || len(doc.Extraction.ISBNFrom) != 1 || doc.Extraction.ISBNFrom[0] != "typed" {
		t.Errorf("extraction = %+v", doc.Extraction)
	}
	if got.TitleGuessed || len(got.Unconfirmed) != 0 || got.ISBNs[0] != "9780441013593" {
		t.Errorf("the typed words were treated as guesses: %+v", got)
	}
}

// An ISBN match below 1 is "not confirmed", not "title differs": the title may
// be identical and simply have nothing independent to vouch for a hint ISBN.
func TestMetaMatchLabelDoesNotClaimTheTitleDiffers(t *testing.T) {
	c := metasource.Candidate{Match: metasource.MatchISBN, Score: 0.5}
	if got := metaMatchLabel(c); got != "ISBN match, not confirmed" {
		t.Errorf("label = %q", got)
	}
	c.Score = 1
	if got := metaMatchLabel(c); got != "ISBN match" {
		t.Errorf("label = %q", got)
	}
}

// A file whose name is only an ISBN still gets a query: the number is a hint.
func TestMetaFetchUsesAnISBNInTheFileName(t *testing.T) {
	var got metasource.Query
	useMetaSources(t, &fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}, got: &got})
	p := namedEPUB(t, "9780441013593.epub")
	if _, _, err := runMetaWithStderr(t, p, "--fetch", "--json"); err != nil {
		t.Fatal(err)
	}
	if len(got.ISBNs) != 1 || got.ISBNs[0] != "9780441013593" || len(got.Unconfirmed) != 1 {
		t.Errorf("query = %+v", got)
	}
}
