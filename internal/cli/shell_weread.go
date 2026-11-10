package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
)

const wereadUsage = "Use /weread, /weread import <file>, /weread login or /weread logout"

func (m *shellModel) openWeRead(arg string) tea.Cmd {
	switch arg {
	case "login":
		m.wereadPath = ""
		return m.openWeReadLogin()
	case "logout":
		return m.open("weread-logout", shellConfirm{title: "Log out of WeRead?", detail: "Delete the saved WeRead login", action: "Log out"})
	case "", "import":
		dir := filePickerStartDir()
		return m.open("weread-file", newWeReadFileModel(dir))
	}
	raw, ok := strings.CutPrefix(arg, "import ")
	if !ok {
		m.setError(wereadUsage)
		return nil
	}
	path := strings.TrimSpace(raw)
	if strings.HasPrefix(path, "\"") {
		if !strings.HasSuffix(path, "\"") || len(path) < 2 {
			m.setError("Close the quoted file path")
			return nil
		}
		path = path[1 : len(path)-1]
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		m.setError("File not found: " + path)
		return nil
	}
	if !weread.SupportedFormat(path) {
		m.setError(wereadTarget.unsupported)
		return nil
	}
	confirm, err := wereadConfirmPage(path)
	if err != nil {
		m.setError(err.Error())
		return nil
	}
	m.wereadPath = path
	return m.open("weread-confirm", confirm)
}

// wereadConfirmPage checks path before anything is uploaded, then asks to
// import it, naming its size and the account it goes to.
func wereadConfirmPage(path string) (shellConfirm, error) {
	note, err := wereadCheckFile(path)
	if err != nil {
		return shellConfirm{}, err
	}
	return shellConfirm{title: "Import to WeRead?", detail: filepath.Base(path), note: note, action: "Import"}, nil
}

// wereadCheckFile applies WeRead's format and size rules to path and
// returns the line that describes the import.
func wereadCheckFile(path string) (string, error) {
	size, err := weread.CheckImportFile(path)
	if err != nil {
		return "", err
	}
	return wereadImportNote(size), nil
}

func (m *shellModel) openWeReadLogin() tea.Cmd {
	return m.open("weread-login", newWeReadLoginModel(newWeReadLoginClient()))
}

// importWeRead uploads path, signing in first when no WeRead login is saved.
// The login page resumes the import when it succeeds.
func (m *shellModel) importWeRead(path string) tea.Cmd {
	m.wereadPath = path
	c, err := loadWeReadClient()
	if errors.Is(err, weread.ErrNotLoggedIn) {
		return m.openWeReadLogin()
	}
	if err != nil {
		return m.wereadFailed("WeRead import failed", path, err)
	}
	return m.open("weread-import", newWeReadImportModel(path, c))
}

func (m *shellModel) wereadLoginDone(page wereadLoginModel) tea.Cmd {
	if errors.Is(page.err, errCancelled) {
		m.wereadPath = ""
		if m.deliveryReturn != nil {
			return m.returnToDownloadCompletion(noticeInfo, "WeRead login cancelled")
		}
		m.restore()
		m.setNotice("WeRead login cancelled")
		return nil
	}
	if page.err != nil {
		path := m.wereadPath
		m.wereadPath = ""
		return m.wereadFailed("WeRead login failed", path, page.err)
	}
	if path := m.wereadPath; path != "" {
		return m.importWeRead(path)
	}
	m.openInfo("Logged in to WeRead", strings.Join(formatDetailRows(wereadAccountRows(page.creds), "", nil), "\n"))
	return nil
}

func (m *shellModel) wereadImportDone(page wereadImportModel) tea.Cmd {
	m.wereadPath = ""
	switch {
	case errors.Is(page.err, errCancelled):
		if m.deliveryReturn != nil {
			return m.returnToDownloadCompletion(noticeInfo, "WeRead import cancelled")
		}
		m.restore()
		m.setNotice("WeRead import cancelled")
		return nil
	case errors.Is(page.err, weread.ErrImportUnconfirmed):
		return m.wereadFailed("WeRead import not confirmed", page.path, page.err)
	case page.err != nil:
		return m.wereadFailed("WeRead import failed", page.path, page.err)
	case m.deliveryReturn != nil:
		return m.returnToDownloadCompletion(noticeSuccess, wereadResultTitle(page.result))
	}
	path, _ := filepath.Abs(page.path)
	m.openInfo(wereadResultTitle(page.result), strings.Join([]string{
		"File", bookCell(path), "",
		"Book ID", page.result.BookID, "",
		"Account", page.account, "",
		wereadResultNote(page.result),
	}, "\n"))
	return nil
}

// wereadFailed shows err on an error page; leaving it returns to the
// download completion page when the import started there.
func (m *shellModel) wereadFailed(title, path string, err error) tea.Cmd {
	m.commandErr = err
	if errors.Is(err, weread.ErrSessionExpired) && path != "" {
		// Offer to sign in again and resume this import instead of a bare error.
		return m.open("weread-error", shellErrorModel{heading: "Send to WeRead", title: "WeRead login expired", subtitle: bookCell(filepath.Base(path)),
			detail:     "Sign in again with WeChat to continue importing this book.",
			signInPath: path})
	}
	detail := shellErrorText(err)
	switch {
	case errors.Is(err, weread.ErrImportUnconfirmed):
		detail = "This book may already be in your library. Check the WeRead shelf on your phone before importing again."
	case errors.Is(err, weread.ErrVerificationRequired):
		title = "WeRead needs verification"
		detail = "Open WeRead on your phone and complete any verification prompt, then try importing again."
	case errors.Is(err, weread.ErrRateLimited):
		title = "WeRead needs a little time"
		detail = "Too many requests. Wait a while before importing again."
	case errors.Is(err, weread.ErrImportParseFailed):
		title = "WeRead could not read this book"
		detail = "Check that the file opens in an ebook reader, or convert it to EPUB and import it again."
	}
	subtitle := ""
	if path != "" {
		subtitle = bookCell(filepath.Base(path))
	}
	return m.open("weread-error", shellErrorModel{heading: "Send to WeRead", title: title, subtitle: subtitle, detail: detail})
}

func (m *shellModel) logoutWeRead() tea.Cmd {
	removed, err := config.RemoveWeRead()
	m.restore()
	switch {
	case err != nil:
		m.setError("Could not log out of WeRead: " + err.Error())
	case removed:
		m.setSuccess("Logged out of WeRead")
	default:
		m.setNotice("Not logged in to WeRead")
	}
	return nil
}
