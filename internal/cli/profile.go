package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Show profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		limits, err := c.GetLimits()
		if err != nil {
			return err
		}

		// Progress bar
		barWidth := 30
		var filled int
		if limits.DailyAllowed > 0 {
			filled = limits.DailyAmount * barWidth / limits.DailyAllowed
			if filled > barWidth {
				filled = barWidth
			}
		}
		empty := barWidth - filled

		barColor := remainingColor(limits.DailyRemaining)
		filledStyle := lipgloss.NewStyle().Foreground(barColor)
		emptyStyle := lipgloss.NewStyle().Foreground(currentTheme.Surface)
		bar := filledStyle.Render(strings.Repeat("█", filled)) +
			emptyStyle.Render(strings.Repeat("░", empty))

		ratio := lipgloss.NewStyle().Bold(true).Render(
			fmt.Sprintf("%d / %d", limits.DailyAmount, limits.DailyAllowed))
		remainingStyle := lipgloss.NewStyle().Foreground(barColor).Bold(true)

		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(currentTheme.Accent)
		labelStyle := lipgloss.NewStyle().Foreground(currentTheme.Muted).Width(12)

		var rows []string
		rows = append(rows, titleStyle.Render("Daily Downloads"))
		rows = append(rows, "")
		rows = append(rows, fmt.Sprintf("  %s  %s", bar, ratio))
		rows = append(rows, "")
		rows = append(rows, fmt.Sprintf("  %s%s",
			labelStyle.Render("Remaining"),
			remainingStyle.Render(fmt.Sprintf("%d", limits.DailyRemaining))))
		if limits.DailyReset != "" {
			rows = append(rows, fmt.Sprintf("  %s%s",
				labelStyle.Render("Resets at"),
				lipgloss.NewStyle().Foreground(currentTheme.Muted).Render(limits.DailyReset)))
		}

		card := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(currentTheme.Accent).
			Padding(1, 2).
			Render(strings.Join(rows, "\n"))

		fmt.Println(card)
		return nil
	},
}
