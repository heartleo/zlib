package cli

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/heartleo/zlib/internal/config"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (m *shellModel) openKindle(arg string) tea.Cmd {
	if arg == "config" {
		cfg, err := config.LoadKindleConfig()
		if err != nil && !os.IsNotExist(err) {
			m.setError(err.Error())
			return nil
		}
		if cfg.SMTPHost == "" {
			cfg.SMTPHost = defaultKindleSMTPHost
		}
		if cfg.SMTPPort == 0 {
			cfg.SMTPPort = defaultKindleSMTPPort
		}
		form := newInlineForm("Kindle settings", "Save", []formField{{label: "Kindle address", value: cfg.To}, {label: "From address", value: cfg.From}, {label: "SMTP host", value: cfg.SMTPHost}, {label: "SMTP port", value: strconv.Itoa(cfg.SMTPPort)}, {label: "SMTP security", value: cfg.SMTPSecurity, hint: "tls (SSL/TLS) or starttls; empty follows the port"}})
		form.validate = func(fields []formField) error {
			draft, err := shellKindleConfig(fields)
			if err != nil {
				return err
			}
			return draft.Validate()
		}
		return m.open("kindle-config", form)
	}
	if path, ok := strings.CutPrefix(arg, "send "); ok {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "\"") {
			if !strings.HasSuffix(path, "\"") || len(path) < 2 {
				m.setError("Close the quoted file path")
				return nil
			}
			path = path[1 : len(path)-1]
		}
		if path == "" {
			m.setError("Use /kindle send <file>")
			return nil
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			m.setError("File not found: " + path)
			return nil
		}
		m.kindlePath = path
		return m.open("kindle-confirm", shellConfirm{title: "Send to Kindle?", detail: filepath.Base(path), action: "Send"})
	}
	if arg != "" && arg != "send" {
		m.setError("Use /kindle send <file> or /kindle config")
		return nil
	}
	dir := filePickerStartDir()
	return m.open("kindle-file", newKindleFileModel(dir))
}
func shellKindleConfig(fields []formField) (zlib.KindleConfig, error) {
	port, err := strconv.Atoi(strings.TrimSpace(fields[3].value))
	if err != nil {
		return zlib.KindleConfig{}, fmt.Errorf("invalid SMTP port")
	}
	security := strings.ToLower(strings.TrimSpace(fields[4].value))
	return zlib.KindleConfig{To: fields[0].value, From: fields[1].value, SMTPHost: fields[2].value, SMTPPort: port, SMTPSecurity: security}, nil
}
func (m *shellModel) saveKindle(fields []formField) tea.Cmd {
	cfg, err := shellKindleConfig(fields)
	if err == nil {
		err = cfg.Validate()
	}
	if err == nil {
		err = config.SaveKindleConfig(cfg)
	}
	if err != nil {
		if form, ok := m.page.(inlineForm); ok {
			form.accepted = false
			form.message = "Could not save Kindle settings: " + err.Error()
			m.page = form
		}
	} else {
		m.openInfo("Kindle settings saved", "Saved to\n"+bookCell(config.KindleConfigPath()))
	}
	return nil
}
func (m *shellModel) sendKindle(path string) tea.Cmd {
	cfg, err := config.LoadKindleConfig()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		if m.deliveryReturn != nil {
			return m.returnToDownloadCompletion(noticeInfo, "Set up Kindle in /config kindle")
		}
		m.commandErr = err
		m.openInfo("Kindle setup required", "Set up delivery with zlib kindle config or /kindle config.")
		return nil
	}
	password := os.Getenv(kindleSMTPPasswordEnv)
	if strings.TrimSpace(password) == "" {
		m.kindlePath = path
		form := newInlineForm("SMTP password", "Save and send", []formField{{
			label: "SMTP password", hint: "Saved to ~/.config/zlib/.env", secret: true,
		}})
		form.validate = func(fields []formField) error {
			_, err := validateSetting(kindleSMTPPasswordEnv, fields[0].value)
			return err
		}
		return m.open("kindle-password", form)
	}
	return m.open("kindle-send", newKindleSendModel(path, cfg, password))
}

func saveKindleSMTPPassword(password string) error {
	store, err := newSettingsStore()
	if err != nil {
		return err
	}
	if err := store.save(kindleSMTPPasswordEnv, password); err != nil {
		return err
	}
	return os.Setenv(kindleSMTPPasswordEnv, password)
}
