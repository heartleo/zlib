package cli

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/heartleo/zlib/internal/metasource"
)

// metaSourceChoice is one catalogue on the prepare page.
type metaSourceChoice struct {
	name      string // the name in ZLIB_METADATA_SOURCES
	label     string
	enabled   bool
	available bool   // false: it cannot run now, and note says why
	note      string // shown beside the row
}

// metaSourceLookup rebuilds the lookup for the named sources.
type metaSourceLookup func(names []string) metaLookupFunc

// withMetaSources gives the page a source panel. choices says what is searched
// now and factory builds the lookup for another selection. Without it the page
// searches what its lookup was built with and has no panel.
func withMetaSources(choices []metaSourceChoice, factory metaSourceLookup) metaOption {
	return func(m *metaPageModel) {
		m.sources, m.lookupFor = choices, factory
	}
}

// newMetaSourcesFor builds the named sources; tests replace it to stay offline.
var newMetaSourcesFor = func(names []string) []metasource.Source {
	return metasource.NewSources(names, metaSourceConfig(metaTransport()))
}

// newMetaLookupFor is newDefaultMetaLookup for a chosen list of sources. The
// sources and the proxy they use are resolved now, as there: the lookup runs
// on another goroutine and must not read package state then.
func newMetaLookupFor(names []string) metaLookupFunc {
	sources := newMetaSourcesFor(names)
	limit := metaResultLimit()
	return func(ctx context.Context, q metasource.Query) (metasource.Result, error) {
		return metasource.Lookup(ctx, sources, q, metasource.Options{MaxResults: limit})
	}
}

// defaultMetaSourceChoices lists the built-in sources with what the
// environment enables. It says whether a key is set and never shows one.
func defaultMetaSourceChoices() []metaSourceChoice {
	listed := configuredMetaSources()
	on := func(name string) bool { return slices.Contains(listed, name) }
	googleNote := "shared quota"
	if googleBooksKey() != "" {
		googleNote = "key set"
	}
	wereadKey := strings.TrimSpace(os.Getenv(wereadAPIKeyEnv))
	wereadOK := metasource.ValidWeReadKey(wereadKey)
	wereadNote := "key set"
	switch {
	case wereadKey == "":
		wereadNote = "needs " + wereadAPIKeyEnv + " (not set)"
	case !wereadOK:
		wereadNote = wereadAPIKeyEnv + " is not a valid key"
	}
	return []metaSourceChoice{
		{name: metasource.NameOpenLibrary, label: "Open Library", enabled: on(metasource.NameOpenLibrary), available: true},
		{name: metasource.NameGoogle, label: "Google Books", enabled: on(metasource.NameGoogle), available: true, note: googleNote},
		{name: metasource.NameDouban, label: "Douban", enabled: on(metasource.NameDouban), available: true, note: "sends the title to Douban"},
		// Listed means the first search asked it, key or not: without a key it
		// answered with an error. The panel shows what was searched.
		{name: metasource.NameWeRead, label: "WeRead", enabled: on(metasource.NameWeRead), available: wereadOK, note: wereadNote},
	}
}

// saveMetaSources writes the sources as the default in the global .env, which
// is what the user asked for with r on the prepare page.
func saveMetaSources(names []string) (string, error) {
	store, err := newSettingsStore()
	if err != nil {
		return "", err
	}
	return saveMetaSourcesTo(store, names)
}

// saveMetaSourcesTo saves through store and returns the name of the setting
// that overrides the saved list, or "" when it takes effect. A saved list that
// takes effect is also applied to this process, so the next page opened in the
// same session starts from it; a real environment variable or a .env in the
// working directory keeps its precedence.
func saveMetaSourcesTo(store settingsStore, names []string) (string, error) {
	if err := store.save(metaSourcesEnv, strings.Join(names, ",")); err != nil {
		return "", err
	}
	values, err := store.values()
	if err != nil {
		return "", err
	}
	for _, v := range values {
		if v.Key != metaSourcesEnv {
			continue
		}
		if v.Override != "" {
			return v.Override, nil
		}
		metaSourcesFlag = "" // the choice just saved replaces --source for the pages that follow
		return "", os.Setenv(metaSourcesEnv, v.Value)
	}
	return "", nil
}

// enabledNames are the names of the sources that are on, in panel order.
func enabledMetaSources(choices []metaSourceChoice) []string {
	var names []string
	for _, c := range choices {
		if c.enabled {
			names = append(names, c.name)
		}
	}
	return names
}

// wereadOn reports whether the WeRead source is among the enabled ones: on the
// prepare page the choice being made, elsewhere the one the search used.
func (m metaPageModel) wereadOn() bool {
	choices := m.sources
	if m.stage == metaPrepare {
		choices = m.draft
	}
	for _, c := range choices {
		if c.name == metasource.NameWeRead && c.enabled {
			return true
		}
	}
	return false
}

// titleNote warns that WeRead cannot search without a title: it has no lookup
// by ISBN, so a title-less search would quietly return nothing from it.
func (m metaPageModel) titleNote() string {
	if m.wereadOn() && len(strings.TrimSpace(m.query.Title)) == 0 {
		return "WeRead needs a title: it cannot search by ISBN"
	}
	return ""
}
