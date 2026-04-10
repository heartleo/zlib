package cli

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tildePath replaces the home directory prefix in path with ~.
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

const (
	symbolSuccess = "✓"
	symbolError   = "✗"
	symbolInfo    = "→"
)

// themed color helpers — resolve from currentTheme at call time so theme
// switching takes effect even after init.
func colorCyanFn(s string) string {
	return lipgloss.NewStyle().Foreground(currentTheme.Info).Render(s)
}
func colorFaintFn(s string) string {
	return lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(s)
}
func colorBoldFn(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}
func colorGreenFn(s string) string {
	return lipgloss.NewStyle().Foreground(currentTheme.Success).Render(s)
}
func colorRedFn(s string) string {
	return lipgloss.NewStyle().Foreground(currentTheme.Error).Render(s)
}

var (
	colorCyan  = colorCyanFn
	colorFaint = colorFaintFn
	colorBold  = colorBoldFn
	colorGreen = colorGreenFn
	colorRed   = colorRedFn
)

// extLipglossColor returns the lipgloss.Color for a file extension.
func extLipglossColor(ext string) lipgloss.Color {
	switch strings.ToUpper(strings.TrimSpace(ext)) {
	case "EPUB":
		return currentTheme.ExtEPUB
	case "PDF":
		return currentTheme.ExtPDF
	case "MOBI", "AZW", "AZW3":
		return currentTheme.ExtMOBI
	case "FB2", "DJVU", "DJV":
		return currentTheme.ExtFB2
	default:
		return currentTheme.Info
	}
}

// remainingColor returns the appropriate color for remaining download quota.
func remainingColor(remaining int) lipgloss.Color {
	switch {
	case remaining <= 0:
		return currentTheme.Error
	case remaining <= 3:
		return currentTheme.Warning
	default:
		return currentTheme.Success
	}
}
