package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/heartleo/zlib/internal/config"
	"github.com/spf13/cobra"
)

// Theme defines the color palette for the CLI.
type Theme struct {
	Accent  lipgloss.Color // borders, headers, titles, spinners
	Link    lipgloss.Color // ID columns, clickable elements
	Title   lipgloss.Color // book titles, primary content
	Success lipgloss.Color // ✓ checkmarks
	Error   lipgloss.Color // ✗ errors, cancellation
	Warning lipgloss.Color // remaining ≤ 3
	Info    lipgloss.Color // ▸ hints, default format color
	Muted   lipgloss.Color // secondary text, labels
	Surface lipgloss.Color // progress bar empty, subtle backgrounds
	ExtEPUB lipgloss.Color
	ExtPDF  lipgloss.Color
	ExtMOBI lipgloss.Color
	ExtFB2  lipgloss.Color
}

// Built-in themes
var themes = map[string]Theme{
	"mocha": {
		Accent:  lipgloss.Color("183"), // Mauve  #cba6f7
		Link:    lipgloss.Color("111"), // Blue   #89b4fa
		Title:   lipgloss.Color("189"), // Lavender #b4befe
		Success: lipgloss.Color("114"), // Green  #a6e3a1
		Error:   lipgloss.Color("204"), // Red    #f38ba8
		Warning: lipgloss.Color("223"), // Yellow #f9e2af
		Info:    lipgloss.Color("109"), // Teal   #94e2d5
		Muted:   lipgloss.Color("243"), // Overlay0 #6c7086
		Surface: lipgloss.Color("238"), // Surface0 #313244
		ExtEPUB: lipgloss.Color("114"), // Green
		ExtPDF:  lipgloss.Color("111"), // Blue
		ExtMOBI: lipgloss.Color("223"), // Yellow
		ExtFB2:  lipgloss.Color("183"), // Mauve
	},
	"dracula": {
		Accent:  lipgloss.Color("141"), // Purple  #bd93f9
		Link:    lipgloss.Color("117"), // Cyan    #8be9fd
		Title:   lipgloss.Color("231"), // Foreground #f8f8f2
		Success: lipgloss.Color("84"),  // Green   #50fa7b
		Error:   lipgloss.Color("210"), // Red     #ff5555
		Warning: lipgloss.Color("228"), // Yellow  #f1fa8c
		Info:    lipgloss.Color("117"), // Cyan
		Muted:   lipgloss.Color("61"),  // Comment #6272a4
		Surface: lipgloss.Color("236"), // Current #44475a
		ExtEPUB: lipgloss.Color("84"),
		ExtPDF:  lipgloss.Color("117"),
		ExtMOBI: lipgloss.Color("228"),
		ExtFB2:  lipgloss.Color("141"),
	},
	"tokyo": {
		Accent:  lipgloss.Color("75"),  // Blue    #7aa2f7
		Link:    lipgloss.Color("117"), // Cyan    #7dcfff
		Title:   lipgloss.Color("189"), // Fg      #a9b1d6
		Success: lipgloss.Color("108"), // Green   #9ece6a
		Error:   lipgloss.Color("203"), // Red     #f7768e
		Warning: lipgloss.Color("223"), // Yellow  #e0af68
		Info:    lipgloss.Color("73"),  // Teal    #73daca
		Muted:   lipgloss.Color("59"),  // Comment #565f89
		Surface: lipgloss.Color("236"), // Surface #24283b
		ExtEPUB: lipgloss.Color("108"),
		ExtPDF:  lipgloss.Color("117"),
		ExtMOBI: lipgloss.Color("223"),
		ExtFB2:  lipgloss.Color("75"),
	},
	"nord": {
		Accent:  lipgloss.Color("110"), // Frost   #81a1c1
		Link:    lipgloss.Color("110"), // Frost   #81a1c1
		Title:   lipgloss.Color("253"), // Snow    #eceff4
		Success: lipgloss.Color("108"), // Green   #a3be8c
		Error:   lipgloss.Color("174"), // Red     #bf616a
		Warning: lipgloss.Color("222"), // Yellow  #ebcb8b
		Info:    lipgloss.Color("73"),  // Frost   #88c0d0
		Muted:   lipgloss.Color("60"),  // Comment #4c566a
		Surface: lipgloss.Color("236"), // Polar   #3b4252
		ExtEPUB: lipgloss.Color("108"),
		ExtPDF:  lipgloss.Color("110"),
		ExtMOBI: lipgloss.Color("222"),
		ExtFB2:  lipgloss.Color("110"),
	},
	"gruvbox": {
		Accent:  lipgloss.Color("208"), // Orange  #fe8019
		Link:    lipgloss.Color("109"), // Blue    #83a598
		Title:   lipgloss.Color("223"), // Fg      #ebdbb2
		Success: lipgloss.Color("142"), // Green   #b8bb26
		Error:   lipgloss.Color("167"), // Red     #fb4934
		Warning: lipgloss.Color("214"), // Yellow  #fabd2f
		Info:    lipgloss.Color("108"), // Aqua    #8ec07c
		Muted:   lipgloss.Color("245"), // Gray    #928374
		Surface: lipgloss.Color("237"), // Bg1     #3c3836
		ExtEPUB: lipgloss.Color("142"), // Green
		ExtPDF:  lipgloss.Color("109"), // Blue
		ExtMOBI: lipgloss.Color("214"), // Yellow
		ExtFB2:  lipgloss.Color("175"), // Purple  #d3869b
	},
	"latte": { // Catppuccin Latte — light background palette
		Accent:  lipgloss.Color("98"),  // Mauve  #8839ef
		Link:    lipgloss.Color("27"),  // Blue   #1e66f5
		Title:   lipgloss.Color("60"),  // Text   #4c4f69 (dark, for light bg)
		Success: lipgloss.Color("28"),  // Green  #40a02b
		Error:   lipgloss.Color("160"), // Red    #d20f39
		Warning: lipgloss.Color("136"), // Yellow #df8e1d
		Info:    lipgloss.Color("30"),  // Teal   #179299
		Muted:   lipgloss.Color("247"), // Overlay0 #9ca0b0
		Surface: lipgloss.Color("254"), // Surface0 #ccd0da
		ExtEPUB: lipgloss.Color("28"),  // Green
		ExtPDF:  lipgloss.Color("27"),  // Blue
		ExtMOBI: lipgloss.Color("136"), // Yellow
		ExtFB2:  lipgloss.Color("98"),  // Mauve
	},
}

// autoThemeName maps a detected terminal background to a built-in theme.
func autoThemeName(dark bool) string {
	if dark {
		return "mocha"
	}
	return "latte"
}

// detectAutoTheme resolves "auto" by querying the terminal background via
// OSC 11. Detection falls back to a dark background (mocha) when it fails —
// non-TTY output, multiplexers, or terminals that ignore the query.
func detectAutoTheme() string {
	return autoThemeName(lipgloss.HasDarkBackground())
}

// currentTheme is the active theme, set during init via resolveTheme().
var currentTheme = themes["mocha"]

const themeEnvVar = "ZLIB_THEME"

// themeAuto is the sentinel that resolves to a light/dark palette from the
// terminal background rather than naming a fixed built-in theme.
const themeAuto = "auto"

// huhTheme builds a huh.Theme from the current CLI theme.
func huhTheme() *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = lipgloss.NewStyle().
		PaddingLeft(1).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(currentTheme.Accent)
	t.Focused.Title = lipgloss.NewStyle().Foreground(currentTheme.Accent).Bold(true)
	t.Focused.Description = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(currentTheme.Error).SetString(" *")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(currentTheme.Error)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(currentTheme.Accent).SetString("> ")
	t.Focused.Option = lipgloss.NewStyle().Foreground(currentTheme.Title)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(currentTheme.Success)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(currentTheme.Success).SetString("[✓] ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(currentTheme.Muted).SetString("[ ] ")
	t.Focused.FocusedButton = lipgloss.NewStyle().
		Foreground(currentTheme.Surface).
		Background(currentTheme.Accent).
		Padding(0, 2).MarginRight(1)
	t.Focused.BlurredButton = lipgloss.NewStyle().
		Foreground(currentTheme.Muted).
		Padding(0, 2).MarginRight(1)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(currentTheme.Title)
	t.Focused.Directory = lipgloss.NewStyle().Foreground(currentTheme.Link)
	t.Focused.File = lipgloss.NewStyle().Foreground(currentTheme.Title)

	t.Focused.Card = t.Focused.Base
	t.Focused.NoteTitle = t.Focused.Title
	t.Focused.Next = lipgloss.NewStyle().Foreground(currentTheme.Muted)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	return t
}

// configuredThemeName picks the configured theme name: env > config.json >
// default ("auto").
func configuredThemeName() string {
	if env, ok := os.LookupEnv(themeEnvVar); ok && env != "" {
		return env
	}
	if cfg, err := config.LoadConfig(); err == nil && cfg.Theme != "" {
		return cfg.Theme
	}
	return themeAuto
}

func resolvedThemeName(name string) string {
	if name == themeAuto {
		return detectAutoTheme()
	}
	return name
}

func themeDisplayLabel(name string) string {
	if name == themeAuto {
		return fmt.Sprintf("%s (%s)", themeAuto, detectAutoTheme())
	}
	return name
}

// resolveTheme picks theme: env > config.json > default ("auto").
// "auto" selects a light or dark palette from the terminal background.
func resolveTheme() {
	if t, ok := themes[resolvedThemeName(configuredThemeName())]; ok {
		currentTheme = t
	}
}

// themeNames returns sorted list of available theme names.
func themeNames() []string {
	names := make([]string, 0, len(themes))
	for k := range themes {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// selectableThemes lists everything the user may set, including the "auto"
// sentinel, with auto first.
func selectableThemes() []string {
	return append([]string{themeAuto}, themeNames()...)
}

// isSelectableTheme reports whether name is a settable theme value.
func isSelectableTheme(name string) bool {
	if name == themeAuto {
		return true
	}
	_, ok := themes[name]
	return ok
}

var themeCmd = &cobra.Command{
	Use:   "theme [name]",
	Short: "Show or set color theme",
	Long:  fmt.Sprintf("Show current theme or set it globally.\n\nAvailable: %s\n", strings.Join(selectableThemes(), ", ")),
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			current := configuredThemeName()
			fmt.Printf("Current theme: %s\n", colorBold(themeDisplayLabel(current)))
			fmt.Printf("Available: %s\n", strings.Join(selectableThemes(), ", "))
			return nil
		}

		name := strings.ToLower(args[0])
		if !isSelectableTheme(name) {
			return fmt.Errorf("unknown theme %q, available: %s", name, strings.Join(selectableThemes(), ", "))
		}

		cfg, _ := config.LoadConfig()
		cfg.Theme = name
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save theme: %w", err)
		}

		resolved := resolvedThemeName(name)
		currentTheme = themes[resolved]
		msg := colorBold(name)
		if name == themeAuto {
			msg = colorBold(fmt.Sprintf("%s (%s)", themeAuto, resolved))
		}
		fmt.Printf("%s Theme set to %s\n", colorGreen(symbolSuccess), msg)
		fmt.Printf("%s Saved to: %s\n", colorFaint(symbolInfo), tildePath(config.ConfigPath()))
		return nil
	},
}
