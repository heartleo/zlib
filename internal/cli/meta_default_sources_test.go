package cli

import (
	"reflect"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// Without a chosen list, a Chinese book is asked of Douban and another book of
// Open Library, plus Google Books when a key is set; a chosen list wins.
func TestDefaultSourcesFollowTheBookLanguageAndTheGoogleKey(t *testing.T) {
	chinese := namedEPUB(t, "三体 (刘慈欣).epub")
	english := namedEPUB(t, "Dune (Frank Herbert).epub")
	t.Setenv(metaSourcesEnv, "")
	t.Setenv(googleBooksKeyEnv, "")
	t.Setenv(googleBooksKeyAliasEnv, "")
	metaSourcesFlag = ""

	for _, tc := range []struct {
		name string
		path string
		key  string
		want []string
	}{
		{"chinese", chinese, "", []string{"douban"}},
		{"chinese with a key", chinese, secretKey, []string{"douban"}},
		{"english", english, "", []string{"openlibrary"}},
		{"english with a key", english, secretKey, []string{"openlibrary", "google"}},
	} {
		t.Setenv(googleBooksKeyEnv, tc.key)
		if got := metaSourcesForFile(tc.path, metaHint{}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
		// The panel on the prepare page shows exactly those sources on.
		if got := enabledMetaSources(metaSourceChoices(tc.want)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: panel %v, want %v", tc.name, got, tc.want)
		}
	}

	// A download's record can say what the file does not.
	t.Setenv(googleBooksKeyEnv, "")
	hint := metaHint{Title: "三体", Authors: []string{"刘慈欣"}}
	if got := metaSourcesForFile(english, hint); !reflect.DeepEqual(got, []string{"douban"}) {
		t.Errorf("hint: %v", got)
	}

	// The user's list beats the language, from the environment and from --source.
	t.Setenv(metaSourcesEnv, "openlibrary,weread")
	if got := metaSourcesForFile(chinese, metaHint{}); !reflect.DeepEqual(got, []string{"openlibrary", "weread"}) {
		t.Errorf("env list: %v", got)
	}
	metaSourcesFlag = "google"
	t.Cleanup(func() { metaSourcesFlag = "" })
	if got := metaSourcesForFile(english, metaHint{}); !reflect.DeepEqual(got, []string{"google"}) {
		t.Errorf("--source: %v", got)
	}
}

// The command line asks the same sources for a book as the page does.
func TestFetchAsksTheDefaultSourcesOfTheBooksLanguage(t *testing.T) {
	t.Setenv(metaSourcesEnv, "")
	t.Setenv(googleBooksKeyEnv, secretKey)
	t.Setenv(googleBooksKeyAliasEnv, "")
	metaSourcesFlag = ""
	var asked [][]string
	old := newMetaSourcesFor
	newMetaSourcesFor = func(names []string) []metasource.Source {
		asked = append(asked, names)
		return []metasource.Source{&fakeMetaSource{name: "Fake", cands: []metasource.Candidate{dune()}}}
	}
	t.Cleanup(func() { newMetaSourcesFor = old })

	for _, name := range []string{"三体 (刘慈欣).epub", "Dune (Frank Herbert).epub"} {
		// Whether the fake source matches the book does not matter here.
		_, _, _ = runMetaWithStderr(t, namedEPUB(t, name), "--fetch", "--json")
	}
	want := [][]string{{"douban"}, {"openlibrary", "google"}}
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("sources asked = %v, want %v", asked, want)
	}
}
