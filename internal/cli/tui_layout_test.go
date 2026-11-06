package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// Only the theme picker used to paint the theme's background, so a light
// theme on a dark terminal (or the reverse) left titles near 1:1 contrast on
// every other screen.
func TestScreensUseTheThemeBackground(t *testing.T) {
	previous := currentTheme
	t.Cleanup(func() { currentTheme = previous })
	for _, name := range []string{"catppuccin-latte", "dracula"} {
		currentTheme = themes[name]
		if currentTheme.Background == nil {
			t.Fatalf("%s has no background", name)
		}
		shell := newShellModel()
		shell.width, shell.height = 80, 24
		notice := shell
		notice.setNotice("Theme saved")
		views := map[string]tea.View{
			"screenView":     screenView(80, 24, "Title", "", "body", ""),
			"fullScreenView": fullScreenView(80, 24, "body"),
			"history":        newHistorySelectModel(nil).View(),
			"shell home":     shell.View(),
			"shell notice":   notice.View(),
		}
		for label, v := range views {
			if v.BackgroundColor != currentTheme.Background || v.ForegroundColor != currentTheme.Title {
				t.Errorf("%s %s: background %v / foreground %v, want the theme's", name, label, v.BackgroundColor, v.ForegroundColor)
			}
		}
	}
	currentTheme = Theme{}
	if v := fullScreenView(80, 24, "body"); v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Fatal("a theme without a background must leave the terminal's colors alone")
	}
}

func TestFullScreenViewFillsAndClipsViewport(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(currentTheme.Accent).Render("书名")
	for _, size := range [][2]int{{80, 24}, {40, 16}, {20, 5}, {1, 1}} {
		v := fullScreenView(size[0], size[1], styled+"\n"+strings.Repeat("x", 100)+"\nlast")
		if !v.AltScreen {
			t.Fatal("full-screen view must use the alternate screen")
		}
		lines := strings.Split(v.Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: got %d rows", size, len(lines))
		}
		for _, line := range lines {
			if got := ansi.StringWidth(line); got != size[0] {
				t.Fatalf("%v: row width %d", size, got)
			}
		}
	}
}

func TestScreenViewUsesFullTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 16}, {20, 5}} {
		v := screenView(size[0], size[1], "Settings", "Network", "Domain", "Saved")
		lines := strings.Split(v.Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: got %d rows", size, len(lines))
		}
		for _, line := range lines {
			if got := ansi.StringWidth(line); got != size[0] {
				t.Fatalf("%v: row width %d", size, got)
			}
		}
	}
}

func TestTUICommandPagesFillTerminal(t *testing.T) {
	isolateConfigHome(t)
	settings, err := newSettingsModel(t.Context(), testSettingsStore(t), "home")
	if err != nil {
		t.Fatal(err)
	}
	wereadSettings, _ := newSettingsModel(t.Context(), testSettingsStore(t), "weread")
	wereadAccount := wereadSettings
	wereadAccount.openWeReadAccount()
	pages := []struct {
		name  string
		model tea.Model
	}{
		{"shell", newShellModel()},
		{"search", newSearchSelectModel("example", nil, nil, false)},
		{"recommended", newBookFeedSelectModel("Recommended", nil)},
		{"history", newHistorySelectModel(nil)},
		{"settings", settings},
		{"weread settings", wereadSettings},
		{"weread account", wereadAccount},
		{"theme", newThemePicker(defaultTheme, "")},
		{"form", newFormModel("Search", "Search", []formField{{label: "Query"}})},
		{"inline form", newInlineForm("Log in", "Log in", []formField{{label: "Email"}})},
		{"directory", newDownloadDirectoryModel(".")},
		{"kindle file", newKindleFileModel(t.TempDir())},
		{"kindle send", newKindleSendModel("example.epub", zlib.KindleConfig{}, "")},
		{"weread file", newWeReadFileModel(t.TempDir())},
		{"weread login", newWeReadLoginModel(nil)},
		{"weread import", newWeReadImportModel("example.epub", nil)},
		{"download", newDownloadModel("1:abc", ".", nil)},
		{"confirm", shellConfirm{title: "Log out?", action: "Log out"}},
	}
	for _, page := range pages {
		for _, size := range [][2]int{{80, 24}, {40, 16}, {20, 5}} {
			model, _ := page.model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := model.View()
			lines := strings.Split(view.Content, "\n")
			if !view.AltScreen || len(lines) != size[1] {
				t.Fatalf("%s at %v: alt=%t rows=%d", page.name, size, view.AltScreen, len(lines))
			}
			for _, line := range lines {
				if got := ansi.StringWidth(line); got != size[0] {
					t.Fatalf("%s at %v: row width %d", page.name, size, got)
				}
			}
		}
	}
}
