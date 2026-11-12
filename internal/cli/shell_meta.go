package cli

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/ebookmeta"
)

const metaUsage = "Use /meta or /meta <file>"

// metaTarget words the file picker for the metadata page.
var metaTarget = sendTarget{
	title:        "Fix metadata",
	confirmTitle: "Look up metadata?",
	note:         "Search online catalogues for this book. Nothing changes until you choose fields.",
	action:       "Look up",
	unsupported:  "Unsupported file type. Choose an EPUB, FB2, DOCX, MOBI, AZW3 or PDF file.",
	allowed:      ebookmeta.Extensions,
}

// openMeta handles /meta and /meta <file>. It works without a Z-Library
// login: the lookup uses public catalogues.
func (m *shellModel) openMeta(arg string) tea.Cmd {
	if arg == "" {
		return m.open("meta-file", newSendFileModel(filePickerStartDir(), metaTarget))
	}
	path := strings.TrimSpace(arg)
	if strings.HasPrefix(path, "\"") {
		if !strings.HasSuffix(path, "\"") || len(path) < 2 {
			m.setError("Close the quoted file path")
			return nil
		}
		path = path[1 : len(path)-1]
	}
	if path == "" {
		m.setError(metaUsage)
		return nil
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		m.setError("File not found: " + path)
		return nil
	}
	return m.openMetaFile(path)
}

// openMetaFile opens the lookup page for path, or reports why it cannot.
func (m *shellModel) openMetaFile(path string) tea.Cmd {
	page, err := newMetaPageModel(path, newDefaultMetaLookup(), ebookmeta.WriteFile)
	if err != nil {
		m.setError(safeText(err.Error()))
		return nil
	}
	return m.open("meta", page)
}

// metaDone returns from the metadata page, saying what was written. A page
// that ended without writing (cancelled) leaves no message.
func (m *shellModel) metaDone(page metaPageModel) tea.Cmd {
	page.stop()
	m.restore()
	if !page.cancelled && len(page.applied) > 0 {
		m.setSuccess(page.summary())
	}
	return nil
}

// metaShellCommand is the shell command that opens the page for path in a
// standalone program.
func metaShellCommand(path string) string {
	return "/meta \"" + filepath.ToSlash(path) + "\""
}
