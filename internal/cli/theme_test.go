package cli

import (
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/config"
)

func TestAutoThemeName(t *testing.T) {
	if got := autoThemeName(true); got != "mocha" {
		t.Errorf("autoThemeName(dark) = %q, want mocha", got)
	}
	if got := autoThemeName(false); got != "latte" {
		t.Errorf("autoThemeName(light) = %q, want latte", got)
	}
}

func TestAutoThemeNamesResolveToRealThemes(t *testing.T) {
	for _, dark := range []bool{true, false} {
		name := autoThemeName(dark)
		if _, ok := themes[name]; !ok {
			t.Errorf("autoThemeName(%v) = %q is not a built-in theme", dark, name)
		}
	}
}

func TestIsSelectableTheme(t *testing.T) {
	if !isSelectableTheme(themeAuto) {
		t.Errorf("auto should be selectable")
	}
	if !isSelectableTheme("mocha") {
		t.Errorf("mocha should be selectable")
	}
	if isSelectableTheme("nope") {
		t.Errorf("unknown theme should not be selectable")
	}
}

// isolateConfigHome points config-file lookups (os.UserHomeDir) at a temp dir
// so tests don't read or clobber the real ~/.config/zlib. It covers both the
// Unix ($HOME) and Windows (%USERPROFILE%) home-resolution paths.
func isolateConfigHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestConfiguredThemeNamePrefersEnvOverConfig(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(themeEnvVar, "dracula")
	if err := config.SaveConfig(config.Config{Theme: "latte"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if got := configuredThemeName(); got != "dracula" {
		t.Errorf("configuredThemeName() = %q, want env theme dracula", got)
	}
}

func TestConfiguredThemeNameDefaultsToAuto(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(themeEnvVar, "")

	if got := configuredThemeName(); got != themeAuto {
		t.Errorf("configuredThemeName() = %q, want auto", got)
	}
}

func TestThemeLongHelpFormatsAvailableOnOwnLine(t *testing.T) {
	if !strings.Contains(themeCmd.Long, "\n\nAvailable: ") {
		t.Errorf("theme help should put Available on its own line: %q", themeCmd.Long)
	}
	if !strings.HasSuffix(themeCmd.Long, "\n") {
		t.Errorf("theme help should end with newline: %q", themeCmd.Long)
	}
}
