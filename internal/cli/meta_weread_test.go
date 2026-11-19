package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

func TestMetaSourcesReadTheWeReadKeyAndAreOptIn(t *testing.T) {
	t.Setenv(metaSourcesEnv, "")
	t.Setenv(wereadAPIKeyEnv, "wrk-fake-key-0000")
	for _, s := range newMetaSources(nil) {
		if s.Name() == "WeRead" {
			t.Fatal("WeRead is on without being listed")
		}
	}

	t.Setenv(metaSourcesEnv, "weread")
	srcs := newMetaSources(nil)
	if len(srcs) != 1 || srcs[0].Name() != "WeRead" {
		t.Fatalf("sources = %v", srcs)
	}

	// Listed without a key it fails clearly and sends nothing.
	t.Setenv(wereadAPIKeyEnv, "")
	_, err := newMetaSources(nil)[0].Identify(context.Background(), metasource.Query{Title: "x"})
	if !errors.Is(err, metasource.ErrWeReadNoKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestMetaSourceErrorPointsAtTheWeReadKey(t *testing.T) {
	for _, err := range []error{metasource.ErrWeReadKeyRejected, metasource.ErrWeReadKeyMalformed} {
		got := metaSourceError(metasource.SourceError{Source: "WeRead", Err: err})
		if !strings.Contains(got, wereadAPIKeyEnv) {
			t.Errorf("%v: %q does not name %s", err, got, wereadAPIKeyEnv)
		}
	}
	// The Google hint is unchanged.
	if got := metaSourceError(metasource.SourceError{Source: "Google Books", Err: metasource.ErrKeyRejected}); !strings.Contains(got, "zlib config metadata") {
		t.Errorf("Google hint = %q", got)
	}
}
