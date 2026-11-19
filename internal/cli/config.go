package cli

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

func settingsCommand(name, page, key, description string) *cobra.Command {
	return &cobra.Command{Use: name, Short: description, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newSettingsStore()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		m, err := newSettingsModel(ctx, store, page)
		if err != nil {
			return err
		}
		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut || !isInteractive() {
			listPage := page
			if page == "domain" {
				listPage = "network"
			}
			var output []settingValue
			for _, k := range settingsPageKeys(listPage) {
				v := m.value(k)
				if page == "domain" && v.Key != zlib.EnvDomain {
					continue
				}
				if key != "" && key != v.Key {
					continue
				}
				v.Value = displaySetting(v.Key, v.Value)
				v.Saved = displaySetting(v.Key, v.Saved)
				output = append(output, v)
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
			}
			for _, v := range output {
				lipgloss.Fprintf(cmd.OutOrStdout(), "%s: %s\n  Source: %s\n", v.Key, v.Value, v.Source)
			}
			return nil
		}
		return runSettingsTUI(m, key)
	}}
}

// runSettingsTUI runs the settings UI on m's page, opening the editor for key
// when set.
func runSettingsTUI(m settingsModel, key string) error {
	initial := m.Init()
	if key == wereadAccountKey {
		initial = m.openWeReadAccount()
	} else if key != "" {
		initial = m.edit(key)
	}
	_, err := tea.NewProgram(settingsStart{settingsModel: m, initial: initial}).Run()
	return err
}

type settingsStart struct {
	settingsModel
	initial tea.Cmd
}

func (m settingsStart) Init() tea.Cmd { return m.initial }

func init() {
	root := settingsCommand("config", "home", "", "View or edit network, download, search, Kindle, WeRead and metadata settings")
	root.PersistentFlags().Bool("json", false, "Print settings and their sources as JSON")
	network := settingsCommand("network", "network", "", "Set domain and proxy")
	network.AddCommand(settingsCommand("domain", "domain", "", "Set Z-Library domain"), settingsCommand("proxy", "network", zlib.EnvProxy, "Set proxy URL"))
	download := settingsCommand("download", "download", "", "Set download directory and retries")
	download.AddCommand(settingsCommand("directory", "download", downloadDirEnvVar, "Set default download directory"), settingsCommand("retries", "download", zlib.EnvRetries, "Set download retry count"))
	search := settingsCommand("search", "search", "", "Set default search formats")
	search.AddCommand(settingsCommand("extensions", "search", searchExtensionsEnv, "Set default search extensions; empty allows any format"))
	kindle := settingsCommand("kindle", "kindle", "", "Set Kindle address and SMTP")
	kindle.AddCommand(settingsCommand("to", "kindle", kindleToKey, "Set Kindle address"), settingsCommand("from", "kindle", kindleFromKey, "Set sender address"), settingsCommand("host", "kindle", kindleHostKey, "Set SMTP host"), settingsCommand("port", "kindle", kindlePortKey, "Set SMTP port"), settingsCommand("security", "kindle", kindleSecurityKey, "Set SMTP security: tls or starttls"))
	wereadSettings := settingsCommand("weread", "weread", "", "Show the WeRead login and set its proxy")
	wereadSettings.AddCommand(settingsCommand("account", "weread", wereadAccountKey, "Log in to or out of WeRead"), settingsCommand("proxy", "weread", wereadProxyEnv, "Set the WeRead proxy: a URL, direct, or empty for the system proxy"))
	metadata := settingsCommand("metadata", "metadata", "", "Set the metadata lookup sources and the Google Books API key")
	metadata.AddCommand(settingsCommand("sources", "metadata", metaSourcesEnv, "Set the metadata lookup sources; empty picks them by book language"), settingsCommand("filename", "metadata", nameLayoutEnv, "Set the order of a file name: title-author, author-title or title-paren; empty restores title-author"), settingsCommand("results", "metadata", metaResultsEnv, "Set how many matches a lookup lists (1 to 50); empty restores 10"), settingsCommand("google-key", "metadata", googleBooksKeyEnv, "Set or remove the Google Books API key"), settingsCommand("weread-key", "metadata", wereadAPIKeyEnv, "Set or remove the WeRead API key (the WeRead skill key) the weread source needs"))
	root.AddCommand(network, download, search, kindle, wereadSettings, metadata)
	rootCmd.AddCommand(root)
}
