package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	keys "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/spf13/cobra"
)

// errRenameTaken means another file already has the wanted name.
var errRenameTaken = errors.New("a file with that name already exists")

// bookExt is the extension of a book's path when zlib knows it as an e-book's,
// in lower case with its dot, and "" otherwise: the format is detected from the
// contents, so a file may have no extension, or a dot that is part of its name
// ("J.R.R. Tolkien - The Hobbit").
func bookExt(path string) string {
	if e := strings.ToLower(filepath.Ext(path)); slices.Contains(ebookmeta.Extensions, e) {
		return e
	}
	return ""
}

// checkRename says whether the file at path can be given name, and returns the
// path it would have. The name must be a plain file name that keeps the book's
// extension, so this can never move a file out of its directory, and it must
// not belong to another file: os.Rename replaces one on both Unix and Windows,
// so the target is looked at first. A name that differs only in case from the
// current one is allowed, as it is the same file.
func checkRename(path, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", errors.New("that is not a file name")
	}
	if ext := bookExt(path); !strings.HasSuffix(strings.ToLower(name), ext) {
		return "", fmt.Errorf("keep the extension %s", ext)
	}
	target := filepath.Join(filepath.Dir(path), name)
	if target == path {
		return path, nil
	}
	src, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	dst, err := os.Lstat(target)
	switch {
	case err == nil:
		// The same file under a name that differs in case is fine. Another name
		// for it (a hard link) or another file is not: rename would drop one name
		// silently or replace the other file.
		if !os.SameFile(src, dst) || !strings.EqualFold(filepath.Base(path), name) {
			return "", errRenameTaken
		}
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	return target, nil
}

// renameBook renames the file at path to name, in the same directory, after
// checkRename accepts it, and returns the new path.
func renameBook(path, name string) (string, error) {
	target, err := checkRename(path, name)
	if err != nil || target == path {
		return path, err
	}
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

// renameOption is one name the user can give the file.
type renameOption struct {
	label string
	name  string // "" for the row that asks for a typed name
	note  string // why the name cannot be used, or that it is the current one
}

func (o renameOption) usable() bool { return o.name != "" && o.note == "" }

// renameOptions lists the names on offer for the file at path, built from what
// is known now: the search terms (the title and authors read from the file name
// and the file, as the prepare page shows them, or as the user edited them), the
// file's own embedded title and authors (read again, since the page may have
// written them), and the match the user has selected. Each is
// "Authors - Title.ext". The search terms come first: an embedded title often
// carries a marketing line, which would make a poor name. A name that equals the
// current one, or one already offered, is marked or left out.
func (m metaPageModel) renameOptions() []renameOption {
	ext := bookExt(m.path)
	current := filepath.Base(m.path)
	var opts []renameOption
	add := func(label, title string, authors []string) {
		name := metasource.FileNameFor(title, authors, ext)
		if name == "" {
			return
		}
		for _, o := range opts {
			if o.name == name {
				return
			}
		}
		o := renameOption{label: label, name: name}
		if name == current {
			o.note = "current"
		}
		opts = append(opts, o)
	}
	add("From the search terms", m.query.Title, m.query.Authors)
	if cur, _, err := ebookmeta.ReadFile(m.path); err == nil {
		e := metasource.EmbeddedOf(cur)
		add("From the file's own title", e.Title, e.Authors)
	}
	switch m.stage {
	case metaCandidates:
		if m.cursor < len(m.res.Candidates) {
			c := m.res.Candidates[m.cursor]
			add("From the selected match", c.Title, c.Authors)
		}
	case metaFields:
		add("From the selected match", m.cand.Title, m.cand.Authors)
	}
	return append(opts, renameOption{label: "Type a name…"})
}

// openRename shows the names on offer. It is a page of its own so the file can
// be renamed from any stage, with or without a search; Esc returns to it.
func (m metaPageModel) openRename() (tea.Model, tea.Cmd) {
	m.renameBack, m.renameOpts, m.renameCursor, m.message = m.stage, m.renameOptions(), 0, ""
	for i, o := range m.renameOpts {
		if o.usable() { // start on the first name that would change something
			m.renameCursor = i
			break
		}
	}
	m.stage = metaRename
	return m, nil
}

func (m metaPageModel) renameKey(k string) (tea.Model, tea.Cmd) {
	n := len(m.renameOpts)
	switch k {
	case "esc", "ctrl+c", "q":
		m.stage, m.message = m.renameBack, ""
	case "up", "shift+tab", "k":
		m.renameCursor = (m.renameCursor + n - 1) % n
		m.message = ""
	case "down", "tab", "j":
		m.renameCursor = (m.renameCursor + 1) % n
		m.message = ""
	case "e":
		return m, m.beginNameEdit()
	case "enter", "ctrl+s":
		o := m.renameOpts[m.renameCursor]
		switch {
		case o.name == "":
			return m, m.beginNameEdit()
		case o.note != "":
			m.message = "That is already the file's name"
		default:
			return m.renameTo(o.name)
		}
	}
	return m, nil
}

// beginNameEdit opens the editor on the highlighted name, without its
// extension, or on the current name when the row asks for one.
func (m *metaPageModel) beginNameEdit() tea.Cmd {
	stem := strings.TrimSuffix(filepath.Base(m.path), bookExt(m.path))
	if o := m.renameOpts[m.renameCursor]; o.name != "" {
		stem = strings.TrimSuffix(o.name, bookExt(m.path))
	}
	var cmd tea.Cmd
	m.editor, cmd = newMetaEditor("Rename the file", []string{"File name"}, []string{stem}, m.width)
	m.editor.help = "The extension is kept. Characters a file system rejects are replaced."
	if ext := bookExt(m.path); ext != "" {
		m.editor.help = "The extension " + ext + " is kept. Characters a file system rejects are replaced."
	}
	m.editing, m.editKind, m.message = true, editName, ""
	return cmd
}

// acceptNameEdit renames to the typed name, or keeps the editor open with the
// reason it cannot.
func (m metaPageModel) acceptNameEdit() (tea.Model, tea.Cmd) {
	name := metasource.SanitizeFileName(m.editor.values()[0], bookExt(m.path))
	if name == "" {
		m.editor.err = "Enter a name"
		return m, nil
	}
	if name == filepath.Base(m.path) {
		m.editor.err = "The file already has that name"
		return m, nil
	}
	next, cmd := m.renameTo(name)
	page := next.(metaPageModel)
	if page.message != "" && page.stage == metaRename { // refused: stay in the editor
		m.editor.err = page.message
		return m, nil
	}
	page.editing = false
	return page, cmd
}

// renameTo renames the file and goes back to the page the user came from. The
// metadata is written to the new path from now on. A refusal stays on the page
// with the reason; the file is untouched.
func (m metaPageModel) renameTo(name string) (tea.Model, tea.Cmd) {
	newPath, err := renameBook(m.path, name)
	if err != nil {
		m.message = "Could not rename: " + safeText(strings.ToLower(renameErrorText(err)))
		return m, nil
	}
	if m.renamedFrom == "" {
		m.renamedFrom = m.path
	}
	m.path = newPath
	m.stage, m.message = m.renameBack, "Renamed to "+filepath.Base(newPath)
	return m, nil
}

// renameErrorText gives the reason without the paths an os error carries.
func renameErrorText(err error) string {
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &linkErr):
		return linkErr.Err.Error()
	case errors.As(err, &pathErr):
		return pathErr.Err.Error()
	}
	return err.Error()
}

// renameBody is the rename page: the current name, the names on offer, and the
// highlighted name in full. A row gives its name only as far as it fits, with
// its note first so that is never cut; the lines under the list wrap the
// highlighted name, so it can be read whatever the width.
func (m metaPageModel) renameBody() string {
	width := max(20, m.width)
	lines := []string{"  " + colorBold("Rename the file")}
	if m.height >= 16 { // the subtitle already names the file; short terminals need the room
		lines = append(lines, ansi.Truncate("    "+"Now       "+safeText(filepath.Base(m.path)), width, "…"), "")
	}
	for i, o := range m.renameOpts {
		label := o.label
		if o.note != "" {
			label += " (" + o.note + ")"
		}
		if room := width - ansi.StringWidth(label) - 8; o.name != "" && room >= 8 {
			label += "  " + ansi.Truncate(safeText(o.name), room, "…")
		}
		lines = append(lines, styledListRow(label, i == m.renameCursor, width, currentTheme))
	}
	if o := m.renameOpts[m.renameCursor]; o.name != "" {
		lines = append(lines, "")
		wrapped := strings.Split(ansi.Hardwrap(safeText(o.name), max(10, width-6), true), "\n")
		if len(wrapped) > 3 {
			wrapped = append(wrapped[:2], ansi.Truncate(wrapped[2], max(1, width-7), "…"))
		}
		for _, l := range wrapped {
			lines = append(lines, "    "+l)
		}
	}
	return strings.Join(lines, "\n")
}

// renameKeys are the key hints of the rename page.
func (m metaPageModel) renameKeys() []keys.Binding {
	return m.keysFor(withHelpKey(
		[][2]string{{"↑/↓", "choose"}, {"enter", "rename"}, {"e", "edit name"}, {"?", "keys"}, {"esc", "back"}},
		[][2]string{{"↑/↓", "choose"}, {"enter", "rename"}, {"e", "edit name"}, {"esc", "back"}},
		[][2]string{{"↑/↓", ""}, {"enter", "rename"}, {"e", "edit"}, {"esc", "back"}},
		[][2]string{{"↑/↓", ""}, {"enter", ""}, {"e", ""}, {"esc", ""}},
		[][2]string{{"enter", ""}, {"esc", ""}})...)
}

// renameSummary words a rename for the notice shown when the page ends.
func (m metaPageModel) renameSummary() string {
	return "renamed to " + filepath.Base(m.path)
}

// renameWanted reads --rename and --rename-to. Both is a usage error.
func renameWanted(cmd *cobra.Command) (byTitle bool, typed string, hasTyped bool, err error) {
	flags := cmd.Flags()
	byTitle, _ = flags.GetBool("rename")
	typed, _ = flags.GetString("rename-to")
	hasTyped = flags.Changed("rename-to")
	if byTitle && hasTyped {
		return false, "", false, &usageError{errors.New("use --rename or --rename-to, not both")}
	}
	return byTitle, typed, hasTyped, nil
}

// renameName is the name --rename or --rename-to gives the file at path whose
// metadata is m, or "" with a reason. --rename uses the title and authors the
// lookup would search for (the file name first, then the file), since an embedded
// title often carries a marketing line. When the same command set the title, the
// authors or an OPF, the file's own metadata is what the user just chose, and it
// is used instead.
func renameName(cmd *cobra.Command, path string, m *ebookmeta.Metadata) (string, error) {
	byTitle, typed, _, err := renameWanted(cmd)
	if err != nil {
		return "", err
	}
	ext := bookExt(path)
	if !byTitle {
		name := metasource.SanitizeFileName(typed, ext)
		if name == "" {
			return "", &usageError{errors.New("--rename-to needs a name")}
		}
		return name, nil
	}
	flags := cmd.Flags()
	e := metasource.EmbeddedOf(m)
	title, authors := e.Title, e.Authors
	if !flags.Changed("title") && !flags.Changed("authors") && !flags.Changed("from-opf") {
		q := metasource.Extract(filepath.Base(path), m, nil, metasource.Hint{}).Query
		title, authors = q.Title, q.Authors
	}
	name := metasource.FileNameFor(title, authors, ext)
	if name == "" {
		return "", errors.New("the file has no title to name it by: set one with -t, or use --rename-to")
	}
	return name, nil
}

// precheckRename refuses what it can before any option writes the file: both
// flags, an empty name, and, when the name is already known (--rename-to, or
// --rename with no option that changes the title or authors), a name that
// cannot be used. Something found only after the fields are written, such as a
// clash with a file that appeared meanwhile, leaves them written.
func precheckRename(cmd *cobra.Command, path string) error {
	byTitle, _, hasTyped, err := renameWanted(cmd)
	if err != nil || (!byTitle && !hasTyped) {
		return err
	}
	flags := cmd.Flags()
	var m *ebookmeta.Metadata
	if byTitle {
		if flags.Changed("title") || flags.Changed("authors") || flags.Changed("from-opf") {
			return nil // the name depends on what the command writes
		}
		if m, _, err = ebookmeta.ReadFile(path); err != nil {
			return metaError(path, err)
		}
	}
	name, err := renameName(cmd, path, m)
	if err != nil {
		return err
	}
	if _, err := checkRename(path, name); err != nil {
		return renameFailure(path, name, err)
	}
	return nil
}

func renameFailure(path, name string, err error) error {
	return fmt.Errorf("cannot rename %s to %s: %s", safeText(filepath.Base(path)), safeText(name), safeText(renameErrorText(err)))
}

// renameFromFlags renames the file at path, whose metadata is m (read after any
// change the other options made), as --rename or --rename-to ask. It returns the
// file's path, new if it was renamed.
func renameFromFlags(cmd *cobra.Command, path string, m *ebookmeta.Metadata) (string, error) {
	byTitle, _, hasTyped, err := renameWanted(cmd)
	if err != nil || (!byTitle && !hasTyped) {
		return path, err
	}
	name, err := renameName(cmd, path, m)
	if err != nil {
		return path, err
	}
	newPath, err := renameBook(path, name)
	if err != nil {
		return path, renameFailure(path, name, err)
	}
	return newPath, nil
}
