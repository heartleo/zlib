package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/weread"
)

// sendTarget words the file picker for one delivery destination.
type sendTarget struct {
	title, confirmTitle, note, action, unsupported string
	allowed                                        []string
	// check, if set, validates a chosen file before confirmation and
	// returns the note shown in the confirmation instead of note.
	check func(path string) (string, error)
}

var (
	kindleTarget = sendTarget{
		title:        "Send to Kindle",
		confirmTitle: "Send to Kindle?",
		note:         "Send this file using your saved Kindle settings.",
		action:       "Send",
		unsupported:  "Unsupported file type. Choose an EPUB, PDF or another Kindle-supported file.",
		allowed:      kindleAllowedTypes,
	}
	wereadTarget = sendTarget{
		title:        "Send to WeRead",
		confirmTitle: "Import to WeRead?",
		note:         "Upload this file to your WeRead shelf.",
		action:       "Import",
		unsupported:  "Unsupported file type. Choose an EPUB, PDF, MOBI, AZW3 or TXT file.",
		allowed:      weread.ImportFormats,
		check:        wereadCheckFile,
	}
)

// sendFileModel picks a local file to send to target. Only folders and
// files the target accepts are listed.
type sendFileModel struct {
	target                              sendTarget
	files                               bookFileList
	input                               textinput.Model
	width, height                       int
	editing, accepted, confirm, focused bool
	path, message, note                 string
}

func newKindleFileModel(dir string) sendFileModel { return newSendFileModel(dir, kindleTarget) }

func newWeReadFileModel(dir string) sendFileModel { return newSendFileModel(dir, wereadTarget) }

func newSendFileModel(dir string, target sendTarget) sendFileModel {
	input := textinput.New()
	input.Prompt = "> "
	return sendFileModel{target: target, files: newBookFileList(dir, target.allowed), input: input, width: 80, height: 24}
}

// formatList names extensions for display: ".epub", ".pdf" → "EPUB, PDF".
func formatList(exts []string) string {
	names := make([]string, len(exts))
	for i, ext := range exts {
		names[i] = strings.ToUpper(strings.TrimPrefix(ext, "."))
	}
	return strings.Join(names, ", ")
}

func (m sendFileModel) Init() tea.Cmd { return m.files.load() }
func (m sendFileModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.files.SetHeight(max(1, size.Height-8))
		m.input.SetWidth(max(1, size.Width-4))
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if m.confirm {
			switch k.String() {
			case "esc", "ctrl+c", "n":
				m.confirm = false
			case "left", "right", "tab", "shift+tab":
				m.focused = !m.focused
			case "enter":
				if m.focused {
					m.accepted = true
					return m, tea.Quit
				}
				m.confirm = false
			}
			return m, nil
		}
		if k.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.editing {
			switch k.String() {
			case "esc":
				m.editing = false
				return m, nil
			case "enter":
				dir, err := validateSetting(downloadDirEnvVar, m.input.Value())
				if err != nil || dir == "" {
					m.message = "Enter an existing directory"
					return m, nil
				}
				m.editing = false
				m.message = ""
				return m, m.files.open(dir, "")
			}
		} else {
			switch k.String() {
			case "esc", "q":
				return m, tea.Quit
			case "/":
				m.editing = true
				m.input.SetValue(m.files.dir)
				m.input.CursorEnd()
				return m, m.input.Focus()
			}
		}
	}
	if m.editing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	files, cmd, path := m.files.Update(msg)
	m.files = files
	if path != "" {
		m.note = m.target.note
		if m.target.check != nil {
			note, err := m.target.check(path)
			if err != nil {
				m.message = err.Error()
				return m, cmd
			}
			m.note = note
		}
		m.path = path
		m.confirm = true
		m.focused = false
		m.message = ""
	}
	return m, cmd
}
func (m sendFileModel) View() tea.View {
	body := m.files.View(m.width)
	subtitle := bookCell(m.files.dir)
	if m.editing {
		body = m.input.View()
		subtitle = "Enter a directory path"
	}
	status := m.message
	if status == "" && !m.editing {
		status = "Showing folders and " + formatList(m.target.allowed) + " files"
	}
	v := screenView(m.width, m.height, m.target.title, subtitle, body, status, uiKey("esc", "cancel"), uiKey("enter", "open/select"), uiKey("←", "parent"), uiKey("/", "path"))
	if m.confirm {
		v.Content = confirmationDialog(v.Content, m.width, m.height, m.target.confirmTitle, filepath.Base(m.path), "", m.note, m.target.action, m.focused, false)
	}
	return v
}

// filePickerStartDir opens file pickers where downloads land: the default
// download directory when it exists, otherwise the working directory.
func filePickerStartDir() string {
	if dir := strings.TrimSpace(os.Getenv(downloadDirEnvVar)); dir != "" {
		if expanded, err := expandTilde(dir); err == nil {
			if info, err := os.Stat(expanded); err == nil && info.IsDir() {
				return expanded
			}
		}
	}
	dir, _ := os.Getwd()
	return dir
}

func chooseKindleFile() (string, bool, error) {
	if !isInteractive() {
		return "", false, fmt.Errorf("provide a file: zlib kindle send <file>")
	}
	result, err := tea.NewProgram(newKindleFileModel(filePickerStartDir())).Run()
	if err != nil {
		return "", false, err
	}
	m := result.(sendFileModel)
	return m.path, m.accepted, nil
}
