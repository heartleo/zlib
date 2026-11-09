package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// Only execute the quit command from a model transition, never an HTTP or
// SMTP command. Completion messages below stand in for those services.
func finishListTransition(t *testing.T, m shellModel, cmd tea.Cmd) shellModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing transition command")
	}
	m, _ = shellStep(m, cmd())
	return m
}

func TestListKindleShortcutDownloadsAndSendsWithoutDialogs(t *testing.T) {
	isolateConfigHome(t)
	dir := t.TempDir()
	t.Setenv(downloadDirEnvVar, dir)
	t.Setenv(kindleSMTPPasswordEnv, "fixture-password")
	if err := config.SaveKindleConfig(zlib.KindleConfig{To: "reader@kindle.com", From: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPort: 587}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Search", "Favorites", "Recommended", "Most popular", "History", "History URL"} {
		t.Run(name, func(t *testing.T) {
			client := &zlib.Client{}
			m := newShellModel()
			if strings.HasPrefix(name, "History") {
				list := newHistorySelectModel(client)
				list.state = historyStateResults
				list.items = []zlib.DownloadHistoryItem{{ID: "1", Hash: "fixture", Name: "Book", Extension: "epub"}}
				if name == "History URL" {
					list.items[0].DownloadURL = "https://files.example/book.epub"
					list.items[0].ID, list.items[0].Hash = "", ""
				}
				m.open("list", list)
			} else {
				list := newSearchSelectModel("books", client, nil, false)
				switch name {
				case "Favorites":
					list = newSavedBooksSelectModel(nil).withBookDetails(client)
				case "Recommended", "Most popular":
					list = newBookFeedSelectModel(name, nil).withBookDetails(client)
				}
				list.state = searchStateResults
				list.books = []zlib.Book{{ID: "1", Hash: "fixture", Name: "Book", Extension: "epub"}}
				m.open("list", list)
			}
			m, cmd := shellStep(m, key("s"))
			m = finishListTransition(t, m, cmd)
			if m.kind != "download" {
				t.Fatalf("shortcut opened %q, want download", m.kind)
			}
			download := m.page.(downloadModel)
			if download.dir != dir || !download.autoKindle || m.previous == nil {
				t.Fatal("shortcut lost its default directory, intent or list")
			}
			if name == "History URL" && download.directURL == "" {
				t.Fatal("history shortcut lost the direct download URL")
			}
			path := filepath.Join(dir, "book.epub")
			m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: downloadDoneMsg{path: path, size: 7}})
			m = finishListTransition(t, m, cmd)
			if m.kind != "kindle-send" || m.page.(kindleSendModel).filePath != path {
				t.Fatalf("download completion opened %q, want Kindle send", m.kind)
			}
			oldGeneration := m.generation
			m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: kindleSendDoneMsg{}})
			m = finishListTransition(t, m, cmd)
			if m.kind != "download" || m.page.(downloadModel).autoKindle || !strings.Contains(ansi.Strip(m.View().Content), "Email sent to Kindle") {
				t.Fatal("send did not return to a reusable saved-file page")
			}
			m, _ = shellStep(m, shellEvent{generation: oldGeneration, msg: kindleSendDoneMsg{}})
			m, cmd = shellStep(m, key("esc"))
			m = finishListTransition(t, m, cmd)
			if m.kind != "list" {
				t.Fatal("Back did not return to the list")
			}
			if list, ok := m.page.(searchSelectModel); ok && (list.kindleShortcut || list.selectedID != "") {
				t.Fatal("return retained a pending send")
			}
			if list, ok := m.page.(historySelectModel); ok && (list.kindleShortcut || list.selected != nil) {
				t.Fatal("history retained a pending send")
			}
		})
	}
}

func TestListKindleShortcutIsVisibleAndInactiveWithoutASelection(t *testing.T) {
	for _, width := range []int{30, 40, 70, 80, 120} {
		list := newSearchSelectModel("books", nil, nil, false)
		list.state, list.width, list.height = searchStateResults, width, 24
		list.books = []zlib.Book{{ID: "1", Hash: "fixture", Name: "Book"}}
		view := ansi.Strip(list.View().Content)
		lines := strings.Split(view, "\n")
		if !strings.Contains(lines[len(lines)-1], "s Kindle") || !strings.Contains(lines[len(lines)-1], "q") {
			t.Fatalf("width %d hid Kindle or exit: %s", width, lines[len(lines)-1])
		}
	}
	for _, loading := range []bool{false, true} {
		list := newSearchSelectModel("books", nil, nil, false)
		if !loading {
			list.state = searchStateResults
		}
		next, cmd := list.Update(key("s"))
		if next.(searchSelectModel).selectedID != "" || cmd != nil {
			t.Fatal("shortcut started without a selected book")
		}
		history := newHistorySelectModel(nil)
		if !loading {
			history.state = historyStateResults
		}
		next, cmd = history.Update(key("s"))
		if next.(historySelectModel).selected != nil || cmd != nil {
			t.Fatal("history shortcut started without a selected book")
		}
	}
}

func TestListKindleShortcutFailureRetryAndUnsupportedFile(t *testing.T) {
	t.Setenv(downloadDirEnvVar, t.TempDir())
	m := newShellModel()
	list := newHistorySelectModel(&zlib.Client{})
	list.state = historyStateResults
	list.items = []zlib.DownloadHistoryItem{{Name: "Book", DownloadURL: "https://files.example/book.epub"}}
	m.open("list", list)
	m, cmd := shellStep(m, key("s"))
	m = finishListTransition(t, m, cmd)
	m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: errMsg{errors.New("fixture download failure")}})
	m = finishListTransition(t, m, cmd)
	if m.kind != "download-error" {
		t.Fatal("failed download did not stay on its error page")
	}
	m, cmd = shellStep(m, key("r"))
	m = finishListTransition(t, m, cmd)
	if m.kind != "download" || !m.page.(downloadModel).autoKindle || m.page.(downloadModel).directURL == "" {
		t.Fatal("retry lost the shortcut or history URL")
	}
	m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: downloadDoneMsg{path: filepath.Join(t.TempDir(), "book.mobi")}})
	m = finishListTransition(t, m, cmd)
	if m.kind != "download" || !strings.Contains(ansi.Strip(m.View().Content), "Kindle does not support") || m.page.(downloadModel).autoKindle {
		t.Fatal("unsupported file was sent or left an automatic send pending")
	}
}

func TestListKindleShortcutMissingPasswordAndCancellation(t *testing.T) {
	isolateConfigHome(t)
	t.Setenv(kindleSMTPPasswordEnv, "")
	t.Setenv(downloadDirEnvVar, t.TempDir())
	if err := config.SaveKindleConfig(zlib.KindleConfig{To: "reader@kindle.com", From: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPort: 587}); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	list := newSearchSelectModel("books", &zlib.Client{}, nil, false)
	list.state, list.detailOpen = searchStateResults, true
	list.books = []zlib.Book{{ID: "1", Hash: "fixture", Extension: "epub"}}
	m.open("list", list)
	m, cmd := shellStep(m, key("s"))
	m = finishListTransition(t, m, cmd)
	path := filepath.Join(t.TempDir(), "book.epub")
	m, cmd = shellStep(m, shellEvent{generation: m.generation, msg: downloadDoneMsg{path: path}})
	m = finishListTransition(t, m, cmd)
	if m.kind != "kindle-password" {
		t.Fatalf("missing password opened %q", m.kind)
	}
	m, cmd = shellStep(m, key("esc"))
	m = finishListTransition(t, m, cmd)
	if m.kind != "download" || m.page.(downloadModel).savedPath != path || os.Getenv(kindleSMTPPasswordEnv) != "" {
		t.Fatal("password cancellation lost the saved file")
	}
}
