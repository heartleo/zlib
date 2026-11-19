package metasource

import "testing"

// A merged result in a narrow list is named by two-letter source names, so
// every built-in source needs one, and a name it does not know stays as it is.
func TestEveryBuiltInSourceHasAShortName(t *testing.T) {
	seen := map[string]string{}
	for _, s := range NewSources(KnownSourceNames, SourceConfig{}) {
		short := ShortSourceName(s.Name())
		if short == s.Name() || len(short) != 2 {
			t.Errorf("%s has no two-letter short name: %q", s.Name(), short)
		}
		if other, dup := seen[short]; dup {
			t.Errorf("%s and %s share the short name %q", s.Name(), other, short)
		}
		seen[short] = s.Name()
	}
	if got := ShortSourceName("Some Catalogue"); got != "Some Catalogue" {
		t.Errorf("an unknown name changed: %q", got)
	}
}
