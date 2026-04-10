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
}

// currentTheme is the active theme, set during init via resolveTheme().
var currentTheme = themes["mocha"]

const themeEnvVar = "ZLIB_THEME"

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

// resolveTheme picks theme: env > config.json > default.
func resolveTheme() {
	var name string
	if env, ok := os.LookupEnv(themeEnvVar); ok && env != "" {
		name = env
	}
	if name == "" {
		if cfg, err := config.LoadConfig(); err == nil && cfg.Theme != "" {
			name = cfg.Theme
		}
	}
	if name == "" {
		name = "mocha"
	}
	if t, ok := themes[name]; ok {
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

var themeCmd = &cobra.Command{
	Use:   "theme [name]",
	Short: "Show or set color theme",
	Long:  "Show current theme or set it globally. Available: " + strings.Join(themeNames(), ", "),
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// Show current theme
			cfg, _ := config.LoadConfig()
			current := cfg.Theme
			if current == "" {
				current = "mocha"
			}
			fmt.Printf("Current theme: %s\n", colorBold(current))
			fmt.Printf("Available: %s\n", strings.Join(themeNames(), ", "))
			return nil
		}

		name := strings.ToLower(args[0])
		if _, ok := themes[name]; !ok {
			return fmt.Errorf("unknown theme %q, available: %s", name, strings.Join(themeNames(), ", "))
		}

		cfg, _ := config.LoadConfig()
		cfg.Theme = name
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save theme: %w", err)
		}

		currentTheme = themes[name]
		fmt.Printf("%s Theme set to %s\n", colorGreen(symbolSuccess), colorBold(name))
		fmt.Printf("%s Saved to: %s\n", colorFaint(symbolInfo), tildePath(config.ConfigPath()))
		return nil
	},
}
