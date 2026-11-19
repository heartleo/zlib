package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/metasource"
)

// GOOGLE_BOOKS_API_KEY, the name an existing .env often uses, feeds the lookups
// when ZLIB_GOOGLE_BOOKS_KEY holds nothing, and the ZLIB_ name wins over it.
func TestLookupsReadTheGoogleKeyAlias(t *testing.T) {
	const other = "AIzaOtherKeyNotReal0123456789abcdefgh"
	t.Setenv(googleBooksKeyEnv, "")
	t.Setenv(googleBooksKeyAliasEnv, secretKey)
	rt := &captureRT{}
	if _, err := googleSource(t, rt).Identify(context.Background(), metasource.Query{Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.reqs) != 1 || rt.reqs[0].Header.Get("X-Goog-Api-Key") != secretKey {
		t.Fatalf("the alias key was not sent: %+v", rt.reqs)
	}
	for _, c := range defaultMetaSourceChoices() {
		if c.name == metasource.NameGoogle && c.note != "key set" {
			t.Errorf("note = %q", c.note)
		}
	}

	t.Setenv(googleBooksKeyEnv, other)
	if got := googleBooksKey(); got != other {
		t.Errorf("the ZLIB_ name does not win: %q", got)
	}
}

// config shows a key under the alias as set, masked, and says where it is; a
// key saved in config takes over, and removing it brings the alias back.
func TestConfigShowsAndOutranksTheGoogleKeyAlias(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(googleBooksKeyEnv, "")
	t.Setenv(googleBooksKeyAliasEnv, "")
	dir := t.TempDir()
	global := filepath.Join(dir, "g.env")
	if err := os.WriteFile(global, []byte(googleBooksKeyAliasEnv+"="+secretKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testSettingsStore(t)
	store.global, store.local = global, filepath.Join(dir, "none.env")
	m, _ := newSettingsModel(t.Context(), store, "metadata")

	v := m.value(googleBooksKeyEnv)
	if !v.alias || v.Source != "Global configuration ("+googleBooksKeyAliasEnv+")" || v.Override != "" {
		t.Fatalf("value = %+v", v)
	}
	if got := displaySetting(v.Key, v.Value); got != "Configured (hidden)" || strings.Contains(got, "AIza") {
		t.Fatalf("display = %q", got)
	}

	const saved = "AIzaSavedKeyNotReal0123456789abcdefghi"
	if err := store.save(googleBooksKeyEnv, saved); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if v := m.value(googleBooksKeyEnv); v.alias || v.Value != saved || v.Source != "Global configuration" {
		t.Fatalf("the saved key does not take over: %+v", v)
	}

	if err := store.save(googleBooksKeyEnv, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if v := m.value(googleBooksKeyEnv); !v.alias || v.Value != secretKey {
		t.Fatalf("removing the saved key lost the alias: %+v", v)
	}
	if data, _ := os.ReadFile(global); !strings.Contains(string(data), googleBooksKeyAliasEnv+"="+secretKey) {
		t.Errorf("the alias line was rewritten: %q", data)
	}
}
