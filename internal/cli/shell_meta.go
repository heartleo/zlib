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
	note:         "Search online. Nothing changes until you save.",
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
	names := metaSourcesForFile(path, metaHint{})
	page, err := newMetaPageModel(path, newMetaLookupFor(names), ebookmeta.WriteFile, withMetaCover(newDefaultMetaCover()),
		withMetaSources(metaSourceChoices(names), newMetaLookupFor), withMetaSourceSaver(saveMetaSources), withMetaLayoutSaver(saveNameLayout))
	if err != nil {
		m.setError(safeText(err.Error()))
		return nil
	}
	return m.open("meta", page)
}

// openMetaForDownload opens the lookup page for a file just downloaded. The
// Z-Library record's ISBN is searched along with whatever the file says, and
// its title and authors fill in what the file lacks. Leaving the page returns
// to the saved-file page.
func (m *shellModel) openMetaForDownload(dl downloadModel) tea.Cmd {
	hint := metaHint{Title: dl.book.Name, Authors: dl.book.Authors, ISBNs: []string{dl.book.ISBN13, dl.book.ISBN10, dl.book.ISBN}}
	names := metaSourcesForFile(dl.savedPath, hint)
	page, err := newMetaPageModel(dl.savedPath, newMetaLookupFor(names), ebookmeta.WriteFile, withMetaCover(newDefaultMetaCover()), withMetaHint(hint),
		withMetaSources(metaSourceChoices(names), newMetaLookupFor), withMetaSourceSaver(saveMetaSources), withMetaLayoutSaver(saveNameLayout))
	if err != nil {
		return m.returnToDownloadCompletion(noticeError, safeText(err.Error()))
	}
	return m.open("meta", page)
}

// metaDone returns from the metadata page, saying what was written. A page
// that ended without writing (cancelled) leaves no message. After a
// download it goes back to the saved-file page.
func (m *shellModel) metaDone(page metaPageModel) tea.Cmd {
	page.stop()
	wrote := !page.cancelled && len(page.applied) > 0
	changed := wrote || page.renamedFrom != "" // a rename stands even if the page was left with Esc
	if !wrote {
		page.applied = nil
	}
	if m.deliveryReturn != nil {
		// The saved-file page sends this path to Kindle and WeRead: it must be
		// the file's name now.
		m.deliveryReturn.savedPath = page.path
		if changed {
			return m.returnToDownloadCompletion(noticeSuccess, page.summary())
		}
		return m.returnToDownloadCompletion(noticeInfo, "")
	}
	m.restore()
	if changed {
		m.setSuccess(page.summary())
	}
	return nil
}

// metaShellCommand is the shell command that opens the page for path in a
// standalone program.
func metaShellCommand(path string) string {
	return "/meta \"" + filepath.ToSlash(path) + "\""
}
