package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/heartleo/zlib"
	"github.com/heartleo/zlib/internal/config"
)

func TestResolveLoginDomain(t *testing.T) {
	tests := []struct {
		name       string
		flagDomain string
		eapi       bool
		domainEnv  string
		want       string
		wantErr    string
	}{
		{name: "explicit HTML", flagDomain: "https://z-lib.gl/", domainEnv: "https://z-lib.sk", want: "https://z-lib.gl"},
		{name: "HTML environment", domainEnv: "https://z-lib.sk", want: "https://z-lib.sk"},
		{name: "EAPI uses same environment", eapi: true, domainEnv: "https://z-lib.gd", want: "https://z-lib.gd"},
		{name: "missing HTML environment", wantErr: zlib.EnvDomain},
		{name: "missing EAPI environment", eapi: true, wantErr: zlib.EnvDomain},
		{name: "invalid EAPI environment", eapi: true, domainEnv: "https://z-library.sk", wantErr: "EAPI requires"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(zlib.EnvDomain, tt.domainEnv)
			got, err := resolveLoginDomain(tt.flagDomain, tt.eapi)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveLoginDomain() error = %v, want mention of %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLoginDomain() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveLoginDomain() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRememberLoginEmailPreservesOtherPreferences(t *testing.T) {
	isolateConfigHome(t)
	if err := config.SaveConfig(config.Config{Theme: "dracula"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := rememberLoginEmail("reader@example.com"); err != nil {
		t.Fatalf("rememberLoginEmail() error = %v", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.LastLoginEmail != "reader@example.com" {
		t.Errorf("LastLoginEmail = %q, want %q", cfg.LastLoginEmail, "reader@example.com")
	}
	if cfg.Theme != "dracula" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "dracula")
	}
	if got := rememberedLoginEmail(); got != "reader@example.com" {
		t.Errorf("rememberedLoginEmail() = %q, want %q", got, "reader@example.com")
	}

	data, err := os.ReadFile(config.ConfigPath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "password") {
		t.Fatalf("config must not persist a password: %s", data)
	}
}

func TestRememberLoginEmailIgnoresBlankEmail(t *testing.T) {
	isolateConfigHome(t)
	if err := config.SaveConfig(config.Config{LastLoginEmail: "reader@example.com"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := rememberLoginEmail("  "); err != nil {
		t.Fatalf("rememberLoginEmail() error = %v", err)
	}
	if got := rememberedLoginEmail(); got != "reader@example.com" {
		t.Errorf("rememberedLoginEmail() = %q, want existing email", got)
	}
}
