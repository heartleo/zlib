package cli

import (
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// The result limit is a whole number from 1 to 50, ten when unset, and a
// hand-edited bad value in .env falls back to ten instead of failing lookups.
func TestMetaResultLimitSettingAndFallback(t *testing.T) {
	for in, want := range map[string]string{"": "", "  25 ": "25", "1": "1", "50": "50"} {
		got, err := validateSetting(metaResultsEnv, in)
		if err != nil || got != want {
			t.Errorf("validateSetting(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "51", "-1", "many", "2.5"} {
		if _, err := validateSetting(metaResultsEnv, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}

	t.Setenv(metaResultsEnv, "")
	if got := metaResultLimit(); got != metasource.DefaultMaxResults {
		t.Errorf("unset limit = %d", got)
	}
	t.Setenv(metaResultsEnv, "25")
	if got := metaResultLimit(); got != 25 {
		t.Errorf("limit = %d", got)
	}
	if got := metaSourceConfig(nil).MaxResults; got != 25 {
		t.Errorf("the sources are not told the limit: %d", got)
	}
	t.Setenv(metaResultsEnv, "lots")
	if got := metaResultLimit(); got != metasource.DefaultMaxResults {
		t.Errorf("a bad value gave %d", got)
	}
}
