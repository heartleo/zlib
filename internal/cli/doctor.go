package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	zlib "github.com/heartleo/zlib"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check Z-Library domain connectivity",
	Long:  "Check configured Z-Library candidate domains without logging in or changing your active domain.",
	RunE: func(cmd *cobra.Command, args []string) error {
		proxyValue, _ := cmd.Flags().GetString("proxy")
		eapi, _ := cmd.Flags().GetBool("eapi")
		proxyURL, err := validateDoctorProxy(proxyValue)
		if err != nil {
			return err
		}
		probeOptions := zlib.DomainProbeOptions{ProxyURL: proxyURL}
		if eapi {
			probeOptions.Path = "/eapi/info/domains"
		}
		results := zlib.ProbeDomainsWithOptions(
			cmd.Context(),
			zlib.KnownDomains(),
			probeOptions,
		)
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			return printJSON(results)
		}

		rows := make([][]string, 0, len(results))
		for _, result := range results {
			httpStatus := "-"
			if result.HTTPStatus != 0 {
				httpStatus = strconv.Itoa(result.HTTPStatus)
			}
			detail := result.Detail
			if detail == "" && result.FinalURL != "" && result.FinalURL != result.Domain {
				detail = "final: " + result.FinalURL
			}
			rows = append(rows, []string{result.Domain, string(result.Status), httpStatus, detail})
		}

		t := table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(currentTheme.Accent)).
			Headers("Domain", "Status", "HTTP", "Detail").
			Rows(rows...).
			StyleFunc(func(row, col int) lipgloss.Style {
				pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
				if row == table.HeaderRow {
					return pad.Bold(true).Foreground(currentTheme.Accent)
				}
				switch col {
				case 0, 2:
					return pad.Foreground(currentTheme.Muted)
				case 1:
					return pad.Foreground(doctorStatusColor(zlib.DomainStatus(rows[row][1])))
				default:
					return pad
				}
			})

		fmt.Println(t)
		return nil
	},
}

func validateDoctorProxy(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil
	}
	proxyURL, err := url.ParseRequestURI(rawURL)
	if err != nil || proxyURL.Host == "" {
		return "", fmt.Errorf("invalid proxy URL")
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
	if proxyURL.Path != "" && proxyURL.Path != "/" || proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return "", fmt.Errorf("proxy URL must not contain a path, query, or fragment")
	}
	return rawURL, nil
}

func doctorStatusColor(status zlib.DomainStatus) lipgloss.Color {
	switch status {
	case zlib.DomainStatusHealthy, zlib.DomainStatusChallenged:
		// Challenged domains are usable; the client clears the challenge.
		return currentTheme.Success
	case zlib.DomainStatusDiamWallBlocked, zlib.DomainStatusRedirectLoop:
		return currentTheme.Error
	default:
		return currentTheme.Muted
	}
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Output domain checks as JSON")
	doctorCmd.Flags().String("proxy", "", "Proxy URL for checks (http, https, socks5, or socks5h)")
	doctorCmd.Flags().Bool("eapi", false, "Check the EAPI health endpoint instead of the home page")
}
