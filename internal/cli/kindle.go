package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/heartleo/zlib"
	"github.com/heartleo/zlib/internal/config"
	"github.com/spf13/cobra"
)

const kindleSMTPPasswordEnv = "ZLIB_SMTP_PWD"
const defaultKindleSMTPHost = "smtp.gmail.com"
const defaultKindleSMTPPort = 587

// Kindle-supported file extensions for the file picker.
var kindleAllowedTypes = []string{".epub", ".pdf", ".txt", ".doc", ".docx", ".html", ".htm", ".rtf", ".jpg", ".jpeg", ".png", ".bmp", ".gif"}

var kindleCmd = &cobra.Command{
	Use:   "kindle",
	Short: "Kindle delivery",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadKindleConfig()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to load kindle config: %w", err)
		}

		if cfg.SMTPHost == "" {
			cfg.SMTPHost = defaultKindleSMTPHost
		}
		if cfg.SMTPPort == 0 {
			cfg.SMTPPort = defaultKindleSMTPPort
		}
		portStr := strconv.Itoa(cfg.SMTPPort)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Kindle address").
					Value(&cfg.To),
				huh.NewInput().
					Title("From address").
					Value(&cfg.From),
				huh.NewInput().
					Title("SMTP host").
					Value(&cfg.SMTPHost),
				huh.NewInput().
					Title("SMTP port").
					Value(&portStr),
			),
		).WithTheme(huhTheme())
		if err := form.Run(); err != nil {
			return err
		}

		port, err := strconv.Atoi(strings.TrimSpace(portStr))
		if err != nil || port <= 0 {
			return fmt.Errorf("invalid SMTP port: %s", portStr)
		}
		cfg.SMTPPort = port

		if err := cfg.Validate(); err != nil {
			return err
		}
		if err := config.SaveKindleConfig(cfg); err != nil {
			return fmt.Errorf("failed to save kindle config: %w", err)
		}

		fmt.Printf("%s Saved to: %s\n", colorGreen(symbolSuccess), tildePath(config.KindleConfigPath()))
		fmt.Printf("%s SMTP password via: export %s=...\n", colorFaint(symbolInfo), kindleSMTPPasswordEnv)
		return nil
	},
}

var kindleSendCmd = &cobra.Command{
	Use:   "send [file]",
	Short: "Send a local file to Kindle",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var filePath string
		if len(args) > 0 {
			filePath = args[0]
		} else {
			cwd, _ := os.Getwd()
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewFilePicker().
						Title("Select a file to send to Kindle").
						Picking(true).
						AllowedTypes(kindleAllowedTypes).
						ShowHidden(true).
						ShowSize(true).
						CurrentDirectory(cwd).
						Height(20).
						Value(&filePath),
				),
			).WithTheme(huhTheme())
			if err := form.Run(); err != nil {
				return err
			}
			if filePath == "" {
				return nil
			}
		}
		return sendDownloadedFileToKindle(filePath)
	},
}

func init() {
	kindleCmd.AddCommand(kindleSendCmd)
}

func sendDownloadedFileToKindle(filePath string) error {
	cfg, err := config.LoadKindleConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("kindle config not found; run: zlib kindle")
		}
		return fmt.Errorf("failed to load kindle config: %w", err)
	}

	password := os.Getenv(kindleSMTPPasswordEnv)
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("kindle smtp password not set; export %s", kindleSMTPPasswordEnv)
	}

	// Run spinner during send
	p := tea.NewProgram(newKindleSendModel(filePath, cfg, password))
	result, err := p.Run()
	if err != nil {
		return err
	}
	sm := result.(kindleSendModel)
	if sm.err != nil {
		return sm.err
	}

	fmt.Println()
	fmt.Printf("%s Email sent to Kindle address: %s\n", colorGreen(symbolSuccess), colorCyan(cfg.To))
	fmt.Printf("%s Final delivery depends on Amazon approval and file compatibility.\n", colorFaint(symbolInfo))
	return nil
}

// — kindle send model (spinner during SMTP send) —

type kindleSendModel struct {
	filePath string
	cfg      zlib.KindleConfig
	password string
	spinner  spinner.Model
	err      error
	done     bool
}

type kindleSendDoneMsg struct{}

func newKindleSendModel(filePath string, cfg zlib.KindleConfig, password string) kindleSendModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Accent)
	return kindleSendModel{
		filePath: filePath,
		cfg:      cfg,
		password: password,
		spinner:  s,
	}
}

func (m kindleSendModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.sendCmd())
}

func (m kindleSendModel) sendCmd() tea.Cmd {
	return func() tea.Msg {
		if err := zlib.SendToKindle(m.filePath, m.cfg, m.password); err != nil {
			return errMsg{err}
		}
		return kindleSendDoneMsg{}
	}
}

func (m kindleSendModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	case kindleSendDoneMsg:
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m kindleSendModel) View() string {
	return fmt.Sprintf("  %s Sending to Kindle...\n", m.spinner.View())
}
