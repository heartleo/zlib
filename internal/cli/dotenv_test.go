package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		var err error
		if had {
			err = os.Setenv(key, old)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Fatalf("failed to restore %s: %v", key, err)
		}
	})
}

func TestLoadDotEnvFromLoadsMissingVariables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ZLIB_DOMAIN=https://example.invalid\nZLIB_SMTP_PWD=secret\n"), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	unsetEnvForTest(t, "ZLIB_DOMAIN")
	unsetEnvForTest(t, "ZLIB_SMTP_PWD")

	if err := loadDotEnvFrom(dir); err != nil {
		t.Fatalf("expected .env to load: %v", err)
	}

	if got := os.Getenv("ZLIB_DOMAIN"); got != "https://example.invalid" {
		t.Fatalf("expected ZLIB_DOMAIN from .env, got %q", got)
	}
	if got := os.Getenv("ZLIB_SMTP_PWD"); got != "secret" {
		t.Fatalf("expected ZLIB_SMTP_PWD from .env, got %q", got)
	}
}

func TestLoadDotEnvFromDoesNotOverrideExistingVariables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ZLIB_DOMAIN=https://example.invalid\n"), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	t.Setenv("ZLIB_DOMAIN", "https://already-set.invalid")

	if err := loadDotEnvFrom(dir); err != nil {
		t.Fatalf("expected .env to load: %v", err)
	}

	if got := os.Getenv("ZLIB_DOMAIN"); got != "https://already-set.invalid" {
		t.Fatalf("expected existing env var to win, got %q", got)
	}
}

func TestLoadDotEnvFromIgnoresMissingFile(t *testing.T) {
	if err := loadDotEnvFrom(t.TempDir()); err != nil {
		t.Fatalf("expected missing .env to be ignored: %v", err)
	}
}
