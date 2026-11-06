package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// bookFileEntry is one row of a bookFileList.
type bookFileEntry struct {
	name string
	dir  bool
}

// bookFileListMsg carries a directory read started by bookFileList.load.
type bookFileListMsg struct {
	dir     string
	entries []bookFileEntry
	err     error
}

// bookFileList lists one directory's folders and the files whose extension
// is in allowed. Other files are left out, so every file row can be chosen.
// The Bubbles file picker can only grey such files out.
type bookFileList struct {
	dir     string
	allowed []string
	entries []bookFileEntry
	loaded  bool
	err     error
	// focus names the entry to select after the next load, such as the
	// folder just left.
	focus          string
	cursor, offset int
	height         int
}

func newBookFileList(dir string, allowed []string) bookFileList {
	return bookFileList{dir: dir, allowed: allowed, height: 16}
}

// accepts reports whether name has an allowed extension, ignoring case.
func (l bookFileList) accepts(name string) bool {
	return slices.Contains(l.allowed, strings.ToLower(filepath.Ext(name)))
}

// load reads l.dir in a command.
func (l bookFileList) load() tea.Cmd {
	dir := l.dir
	accepts := l.accepts
	return func() tea.Msg {
		items, err := os.ReadDir(dir)
		if err != nil {
			return bookFileListMsg{dir: dir, err: err}
		}
		var entries []bookFileEntry
		for _, item := range items {
			name := item.Name()
			info, err := os.Stat(filepath.Join(dir, name)) // follows symlinks
			switch {
			case err != nil:
				// A broken link or an unreadable entry cannot be opened.
			case info.IsDir():
				entries = append(entries, bookFileEntry{name: name, dir: true})
			case info.Mode().IsRegular() && accepts(name):
				entries = append(entries, bookFileEntry{name: name})
			}
		}
		slices.SortFunc(entries, func(a, b bookFileEntry) int {
			if a.dir != b.dir {
				if a.dir {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
		})
		return bookFileListMsg{dir: dir, entries: entries}
	}
}

// open switches to dir and reads it.
func (l *bookFileList) open(dir, focus string) tea.Cmd {
	l.dir, l.focus = dir, focus
	l.entries, l.loaded, l.err = nil, false, nil
	l.cursor, l.offset = 0, 0
	return l.load()
}

func (l *bookFileList) SetHeight(h int) {
	l.height = max(1, h)
	l.scroll()
}

// scroll keeps the cursor inside the visible rows.
func (l *bookFileList) scroll() {
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.height {
		l.offset = l.cursor - l.height + 1
	}
	l.offset = min(max(0, l.offset), max(0, len(l.entries)-l.height))
}

// Update handles a directory read or a key. It returns the chosen file's
// path when Enter selects a file.
func (l bookFileList) Update(msg tea.Msg) (bookFileList, tea.Cmd, string) {
	switch msg := msg.(type) {
	case bookFileListMsg:
		if msg.dir != l.dir {
			return l, nil, "" // a read for a folder already left
		}
		l.entries, l.err, l.loaded = msg.entries, msg.err, true
		l.cursor = max(0, slices.IndexFunc(l.entries, func(e bookFileEntry) bool { return e.name == l.focus }))
		l.focus = ""
		l.scroll()
	case tea.KeyPressMsg:
		k := msg.String()
		switch k {
		case "g":
			k = "home"
		case "G":
			k = "end"
		}
		switch k {
		case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
			l.cursor = listCursor(k, l.cursor, len(l.entries), l.height)
			l.scroll()
		case "enter", "right", "l":
			if l.cursor >= len(l.entries) {
				return l, nil, ""
			}
			entry := l.entries[l.cursor]
			path := filepath.Join(l.dir, entry.name)
			if entry.dir {
				return l, l.open(path, ""), ""
			}
			if k == "enter" {
				return l, nil, path
			}
		case "left", "h", "backspace":
			if parent := filepath.Dir(l.dir); parent != l.dir {
				return l, l.open(parent, filepath.Base(l.dir)), ""
			}
		}
	}
	return l, nil, ""
}

// View renders the visible rows, padded to the list height.
func (l bookFileList) View(width int) string {
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	var lines []string
	switch {
	case l.err != nil:
		lines = append(lines, muted.Render(ansi.Truncate("Cannot open this folder: "+safeText(l.err.Error()), max(1, width), "…")))
	case !l.loaded:
		lines = append(lines, muted.Render("Reading folder…"))
	case len(l.entries) == 0:
		lines = append(lines, muted.Render(ansi.Truncate("No supported files here. Press / to enter another folder.", max(1, width), "…")))
	}
	folder := lipgloss.NewStyle().Foreground(currentTheme.Link)
	file := lipgloss.NewStyle().Foreground(currentTheme.Title)
	for i := l.offset; i < min(len(l.entries), l.offset+l.height); i++ {
		entry := l.entries[i]
		label := bookCell(entry.name)
		if entry.dir {
			label += string(filepath.Separator)
		}
		switch {
		case i == l.cursor:
			lines = append(lines, styledListRow(label, true, width, currentTheme))
		case entry.dir:
			lines = append(lines, "  "+folder.Render(ansi.Truncate(label, max(1, width-2), "…")))
		default:
			lines = append(lines, "  "+file.Render(ansi.Truncate(label, max(1, width-2), "…")))
		}
	}
	for len(lines) < l.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
