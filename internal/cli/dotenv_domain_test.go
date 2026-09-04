package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceDotEnvDomain(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "replaces in place and keeps the other keys",
			content: "ZLIB_SMTP_PWD=secret\n" +
				"ZLIB_DOMAIN=https://z-lib.sk\n" +
				"ZLIB_PROXY=http://127.0.0.1:7890\n",
			want: "ZLIB_SMTP_PWD=secret\n" +
				"ZLIB_DOMAIN=https://z-lib.gd\n" +
				"ZLIB_PROXY=http://127.0.0.1:7890\n",
		},
		{
			name:    "appends when the key is absent",
			content: "ZLIB_SMTP_PWD=secret\n",
			want:    "ZLIB_SMTP_PWD=secret\nZLIB_DOMAIN=https://z-lib.gd\n",
		},
		{
			name:    "appends to an empty file without a leading blank line",
			content: "",
			want:    "ZLIB_DOMAIN=https://z-lib.gd\n",
		},
		{
			name:    "appends when the file has no trailing newline",
			content: "ZLIB_PROXY=http://127.0.0.1:7890",
			want:    "ZLIB_PROXY=http://127.0.0.1:7890\nZLIB_DOMAIN=https://z-lib.gd\n",
		},
		{
			name:    "replaces the export form",
			content: "export ZLIB_DOMAIN=https://z-lib.sk\n",
			want:    "ZLIB_DOMAIN=https://z-lib.gd\n",
		},
		{
			// godotenv lets the last assignment win, so a leftover duplicate
			// would shadow the line just written.
			name:    "drops a later duplicate",
			content: "ZLIB_DOMAIN=https://z-lib.sk\nZLIB_PROXY=p\nZLIB_DOMAIN=https://1lib.sk\n",
			want:    "ZLIB_DOMAIN=https://z-lib.gd\nZLIB_PROXY=p\n",
		},
		{
			name:    "leaves a commented assignment alone",
			content: "# ZLIB_DOMAIN=https://z-lib.sk\n",
			want:    "# ZLIB_DOMAIN=https://z-lib.sk\nZLIB_DOMAIN=https://z-lib.gd\n",
		},
		{
			name:    "does not match a key that merely ends in ZLIB_DOMAIN",
			content: "MY_ZLIB_DOMAIN=https://z-lib.sk\n",
			want:    "MY_ZLIB_DOMAIN=https://z-lib.sk\nZLIB_DOMAIN=https://z-lib.gd\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replaceDotEnvDomain(tt.content, "https://z-lib.gd"); got != tt.want {
				t.Errorf("replaceDotEnvDomain() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPersistDotEnvDomainCreatesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path, changed, err := persistDotEnvDomain("https://z-lib.gd")
	if err != nil {
		t.Fatalf("persistDotEnvDomain() error = %v", err)
	}
	if !changed {
		t.Error("persistDotEnvDomain() reported no change, want the file written")
	}
	if want := filepath.Join(home, ".config", "zlib", ".env"); path != want {
		t.Errorf("persistDotEnvDomain() path = %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "ZLIB_DOMAIN=https://z-lib.gd\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// Writing an unchanged value must report no change, so a repeated login does
// not claim to have edited anything.
func TestPersistDotEnvDomainIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if _, _, err := persistDotEnvDomain("https://z-lib.gd"); err != nil {
		t.Fatalf("persistDotEnvDomain() error = %v", err)
	}
	_, changed, err := persistDotEnvDomain("https://z-lib.gd")
	if err != nil {
		t.Fatalf("persistDotEnvDomain() error = %v", err)
	}
	if changed {
		t.Error("persistDotEnvDomain() reported a change for an unchanged value")
	}
}

func TestPersistDotEnvDomainPreservesOtherKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".config", "zlib")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existing := "# mirrors change often\nZLIB_SMTP_PWD=app-password\nZLIB_DOMAIN=https://z-lib.sk\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(existing), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	path, changed, err := persistDotEnvDomain("https://z-lib.gd")
	if err != nil {
		t.Fatalf("persistDotEnvDomain() error = %v", err)
	}
	if !changed {
		t.Fatal("persistDotEnvDomain() reported no change, want the domain rewritten")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := "# mirrors change often\nZLIB_SMTP_PWD=app-password\nZLIB_DOMAIN=https://z-lib.gd\n"
	if got := string(data); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}
