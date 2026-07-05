package cli

import "testing"

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
