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
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/ebookmeta"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/spf13/cobra"
)

// errRenameTaken means another file already has the wanted name.
var errRenameTaken = errors.New("a file with that name already exists")

// nameLayoutEnv is the order a file name gives the title and the authors:
// title-author, author-title or title-paren. Unset means title-author.
const nameLayoutEnv = "ZLIB_FILENAME_FORMAT"

// metaNameLayout is the configured layout. A value that cannot be one (a
// hand-edited .env) counts as unset rather than failing a rename.
func metaNameLayout() metasource.NameLayout {
	l, err := metasource.ParseNameLayout(os.Getenv(nameLayoutEnv))
	if err != nil {
		return metasource.DefaultNameLayout
	}
	return l
}

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

// renameOption is one place a name can come from: a title and authors the file
// can be named by, or the row that asks for a typed name.
type renameOption struct {
	label   string
	title   string
	authors []string
	typed   bool // the row that asks for a typed name
}

// renameChoice is what a renameOption names the file now, in the page's layout.
type renameChoice struct {
	name string // "" for the typed row
	note string // "current", or why the name cannot be used
	ok   bool   // renaming to it would change the name
}

// renameOptions lists where a name can come from for the file at path, from
// what is known now: the search terms (the title and authors read from the file
// name and the file, as the prepare page shows them, or as the user edited
// them), the file's own embedded title and authors (read again, since the page
// may have written them), and the match the user has selected. The search terms
// come first: an embedded title often carries a marketing line, which would
// make a poor name. A source that gives no title, or the same title and authors
// as one already offered, is left out.
func (m metaPageModel) renameOptions() []renameOption {
	var opts []renameOption
	add := func(label, title string, authors []string) {
		if metasource.FileNameWith(metasource.DefaultNameLayout, title, authors, ".x") == "" {
			return
		}
		key := strings.ToLower(title + "\x00" + strings.Join(authors, "\x00"))
		for _, o := range opts {
			if strings.ToLower(o.title+"\x00"+strings.Join(o.authors, "\x00")) == key {
				return
			}
		}
		opts = append(opts, renameOption{label: label, title: title, authors: authors})
	}
	add("Search terms", m.query.Title, m.query.Authors)
	if cur, _, err := ebookmeta.ReadFile(m.path); err == nil {
		e := metasource.EmbeddedOf(cur)
		add("The file's own title", e.Title, e.Authors)
	}
	switch m.stage {
	case metaCandidates:
		if m.cursor < len(m.res.Candidates) {
			c := m.res.Candidates[m.cursor]
			add("Selected match", c.Title, c.Authors)
		}
	case metaFields:
		add("Selected match", m.cand.Title, m.cand.Authors)
	}
	return append(opts, renameOption{label: "Type a name…", typed: true})
}

// renameChoiceFor says what o would name the file, and whether that can be done.
func (m metaPageModel) renameChoiceFor(o renameOption) renameChoice {
	if o.typed {
		return renameChoice{ok: true}
	}
	name := metasource.FileNameWith(m.nameLayout, o.title, o.authors, bookExt(m.path))
	switch _, err := checkRename(m.path, name); {
	case name == filepath.Base(m.path):
		return renameChoice{name: name, note: "current"}
	case err != nil:
		return renameChoice{name: name, note: strings.ToLower(renameErrorText(err))}
	}
	return renameChoice{name: name, ok: true}
}

// openRename shows the names on offer. It is a page of its own so the file can
// be renamed from any stage, with or without a search; Esc returns to it.
func (m metaPageModel) openRename() (tea.Model, tea.Cmd) {
	if !m.nameLayout.Valid() {
		m.nameLayout = metaNameLayout()
	}
	m.renameBack, m.renameOpts, m.renameCursor, m.message = m.stage, m.renameOptions(), 0, ""
	for i, o := range m.renameOpts {
		if !o.typed && m.renameChoiceFor(o).ok { // start on the first name that would change something
			m.renameCursor = i
			break
		}
	}
	m.stage = metaRename
	return m, nil
}

// layoutChoices are the formats the page switches between: the presets, then
// the user's custom pattern when one is configured.
func (m metaPageModel) layoutChoices() []metasource.NameLayout {
	out := slices.Clone(metasource.NameLayouts)
	if m.customLayout != "" && !slices.Contains(out, m.customLayout) {
		out = append(out, m.customLayout)
	}
	return out
}

// stepLayout moves the layout by delta, wrapping around.
func (m metaPageModel) stepLayout(delta int) metaPageModel {
	choices := m.layoutChoices()
	i := slices.Index(choices, m.nameLayout)
	m.nameLayout = choices[(max(i, 0)+delta+len(choices))%len(choices)]
	m.message = ""
	return m
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
	case "left", "h":
		return m.stepLayout(-1), nil
	case "right", "l":
		return m.stepLayout(1), nil
	case "1", "2", "3", "4":
		if choices := m.layoutChoices(); int(k[0]-'1') < len(choices) {
			m.nameLayout = choices[int(k[0]-'1')]
			m.message = ""
		}
	case "ctrl+d":
		return m.rememberLayout()
	case "e":
		return m, m.beginNameEdit()
	case "enter", "ctrl+s":
		o := m.renameOpts[m.renameCursor]
		if o.typed {
			return m, m.beginNameEdit()
		}
		c := m.renameChoiceFor(o)
		switch {
		case c.ok:
			return m.renameTo(c.name)
		case c.note == "current":
			m.message = "That is already the file's name"
		default:
			m.message = "Cannot use that name: " + safeText(c.note)
		}
	}
	return m, nil
}

// rememberLayout saves the layout on the page as the default, when the user
// asks for it.
func (m metaPageModel) rememberLayout() (tea.Model, tea.Cmd) {
	if m.saveLayout == nil {
		m.message = "The format cannot be saved here"
		return m, nil
	}
	switch overriddenBy, err := m.saveLayout(m.nameLayout); {
	case err != nil:
		m.message = "Could not save the format: " + safeText(err.Error())
	case overriddenBy != "":
		m.message = "Saved, but " + safeText(overriddenBy) + " takes precedence"
	default:
		m.message = "Saved " + m.nameLayout.Label() + " as the default format"
	}
	return m, nil
}

// saveNameLayout writes the layout as the default in the global .env.
func saveNameLayout(l metasource.NameLayout) (string, error) {
	store, err := newSettingsStore()
	if err != nil {
		return "", err
	}
	return saveNameLayoutTo(store, l)
}

// saveNameLayoutTo saves through store and returns the name of the setting that
// overrides the saved layout, or "" when it takes effect. A saved layout that
// takes effect is also applied to this process, so the next page opened in the
// same session starts from it.
func saveNameLayoutTo(store settingsStore, l metasource.NameLayout) (string, error) {
	if err := store.save(nameLayoutEnv, string(l)); err != nil {
		return "", err
	}
	values, err := store.values()
	if err != nil {
		return "", err
	}
	for _, v := range values {
		if v.Key != nameLayoutEnv {
			continue
		}
		if v.Override != "" {
			return v.Override, nil
		}
		return "", os.Setenv(nameLayoutEnv, v.Value)
	}
	return "", nil
}

// withMetaLayoutSaver lets the rename page save its format as the default.
func withMetaLayoutSaver(save func(metasource.NameLayout) (string, error)) metaOption {
	return func(m *metaPageModel) { m.saveLayout = save }
}

// beginNameEdit opens the editor on the highlighted name, without its
// extension, or on the current name when the row asks for one.
func (m *metaPageModel) beginNameEdit() tea.Cmd {
	ext := bookExt(m.path)
	stem := strings.TrimSuffix(filepath.Base(m.path), ext)
	if o := m.renameOpts[m.renameCursor]; !o.typed {
		if name := m.renameChoiceFor(o).name; name != "" {
			stem = strings.TrimSuffix(name, ext)
		}
	}
	var cmd tea.Cmd
	m.editor, cmd = newMetaEditor("Rename the file", []string{"File name"}, []string{stem}, m.width)
	m.editor.help = "Characters a file system rejects are replaced."
	if ext != "" {
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

// nameVerdict is the line under a name: whether renaming to it would work.
func (m metaPageModel) nameVerdict(c renameChoice) string {
	switch {
	case c.ok:
		return colorGreen(symbolSuccess) + " " + colorFaint("ready: Enter renames the file")
	case c.note == "current":
		return colorFaint("● this is the file's name now")
	}
	return colorRed(symbolError) + " " + safeText(c.note)
}

// fileNameLine is the line the lookup pages show for the file's name: the name
// now and, when the title and authors would give it another, that name too, so
// the user sees at a glance whether the file is named by its book. The key that
// renames it (n, or F2) is in the footer of every page, so it is not repeated
// here. It gives up the arrow before it cuts a name.
func (m metaPageModel) fileNameLine(indent string, width int, title string, authors []string) string {
	current := safeText(filepath.Base(m.path))
	proposal := metasource.FileNameWith(m.nameLayout, title, authors, bookExt(m.path))
	label := colorFaint("File  ")
	type variant struct{ plain, styled string }
	var variants []variant
	if proposal != "" && proposal != filepath.Base(m.path) {
		p := safeText(proposal)
		variants = []variant{{current + " → " + p, current + " " + colorFaint("→") + " " + colorBold(p)}}
	}
	variants = append(variants, variant{current, current})
	for _, v := range variants {
		if ansi.StringWidth(indent)+ansi.StringWidth("File  ")+ansi.StringWidth(v.plain) <= width {
			return indent + label + v.styled
		}
	}
	return ansi.Truncate(indent+label+current, width, "…")
}

// pendingFileNameParts are the title and authors the workbench would write, or
// the file's own where nothing changes them: what the file's name should say.
func (m metaPageModel) pendingFileNameParts() (string, []string) {
	title, authors := m.current.Title, m.current.Authors
	for _, r := range m.benchRows() {
		value, _ := m.pending(r)
		switch r.field {
		case metasource.FieldTitle:
			title = value
		case metasource.FieldAuthors:
			authors = nil
			for _, a := range strings.Split(value, " & ") {
				if a = strings.TrimSpace(a); a != "" {
					authors = append(authors, a)
				}
			}
		}
	}
	return title, authors
}

// layoutRow is the format switch: every layout with the active one marked, or
// only the active one when they do not fit.
func (m metaPageModel) layoutRow(width int) string {
	const label = "Format  "
	// A radio mark says which is chosen where colour cannot be told.
	var chips []string
	plain := 0
	choices := m.layoutChoices()
	for _, l := range choices {
		text := safeText(l.Label())
		plain += ansi.StringWidth(text) + 4
		if l == m.nameLayout {
			chips = append(chips, lipgloss.NewStyle().Bold(true).Foreground(currentTheme.Title).Background(currentTheme.Surface).Render(" ● "+text+" "))
		} else {
			chips = append(chips, colorFaint(" ○ "+text+" "))
		}
	}
	plain += 2 * len(choices)
	if 2+len(label)+plain > width {
		i := slices.Index(choices, m.nameLayout) + 1
		return "  " + colorFaint(label) + "‹ " + colorBold(safeText(m.nameLayout.Label())) + " › " + colorFaint(fmt.Sprintf("%d/%d", i, len(choices)))
	}
	return "  " + colorFaint(label) + strings.Join(chips, " ")
}

// renameBody is the rename page: the file's name now, the name it would get,
// the format switch, and where the name can come from. The new name is given in
// full, wrapped, whatever the width; each row gives its name only as far as it
// fits, with its note first so that is never cut. A short terminal drops the
// spacing and the headings first, then rows around the highlighted one.
func (m metaPageModel) renameBody() string {
	width := max(20, m.width)
	avail := max(1, m.height-6)
	o := m.renameOpts[m.renameCursor]
	c := m.renameChoiceFor(o)

	// The new name, wrapped in full up to three lines (one when space is short).
	newLines := []string{"  " + colorFaint("New  ") + colorFaint("press Enter to type a name")}
	if !o.typed {
		maxLines := 3
		if avail < 14 {
			maxLines = 1
		}
		style := colorGreen
		if !c.ok {
			style = colorFaint
		}
		wrapped := strings.Split(ansi.Hardwrap(safeText(c.name), max(10, width-7), true), "\n")
		if len(wrapped) > maxLines {
			wrapped = append(wrapped[:maxLines-1], ansi.Truncate(wrapped[maxLines-1], max(1, width-8), "…"))
		}
		newLines = nil
		for i, l := range wrapped {
			prefix := "       "
			if i == 0 {
				prefix = "  " + colorFaint("New  ")
			}
			newLines = append(newLines, prefix+style(l))
		}
	}
	verdict := "       " + m.nameVerdict(c)
	if o.typed {
		verdict = ""
	}

	rowLines := make([]string, len(m.renameOpts))
	for i, opt := range m.renameOpts {
		rc := m.renameChoiceFor(opt)
		label := opt.label
		if rc.note != "" {
			label += " (" + rc.note + ")"
		}
		if room := width - ansi.StringWidth(label) - 8; rc.name != "" && room >= 8 {
			label += "  " + ansi.Truncate(safeText(rc.name), room, "…")
		}
		rowLines[i] = styledListRow(label, i == m.renameCursor, width, currentTheme)
	}

	full := m.renameLayoutLines(width, true, newLines, verdict, rowLines)
	if len(full) > avail {
		full = m.renameLayoutLines(width, false, newLines, verdict, rowLines)
	}
	if len(full) > avail { // rows around the highlighted one, then what is left
		extra := len(full) - avail
		drop := min(extra, len(rowLines)-1)
		first := min(max(0, m.renameCursor-(len(rowLines)-drop)/2), drop)
		full = m.renameLayoutLines(width, false, newLines, verdict, rowLines[first:first+len(rowLines)-drop])
	}
	return strings.Join(full, "\n")
}

// renameLayoutLines puts the parts of the rename page in order, with the
// spacing and headings (roomy) or without.
func (m metaPageModel) renameLayoutLines(width int, roomy bool, newLines []string, verdict string, rows []string) []string {
	var lines []string
	if roomy {
		lines = append(lines, ansi.Truncate("  "+colorFaint("Now  ")+safeText(filepath.Base(m.path)), width, "…"))
	}
	lines = append(lines, newLines...)
	if verdict != "" {
		lines = append(lines, verdict)
	}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.layoutRow(width))
	if roomy {
		lines = append(lines, "", "  "+colorFaint("Name from"))
	}
	return append(lines, rows...)
}

// renameKeys are the key hints of the rename page.
func (m metaPageModel) renameKeys() []keys.Binding {
	return m.keysFor(withHelpKey(
		[][2]string{{"↑/↓", "name"}, {"←/→", "format"}, {"enter", "rename"}, {"e", "edit"}, {"ctrl+d", "save format"}, {"?", "keys"}, {"esc", "back"}},
		[][2]string{{"↑/↓", "name"}, {"←/→", "format"}, {"enter", "rename"}, {"e", "edit"}, {"esc", "back"}},
		[][2]string{{"↑/↓", ""}, {"←/→", ""}, {"enter", "rename"}, {"e", "edit"}, {"esc", "back"}},
		[][2]string{{"↑/↓", ""}, {"←/→", ""}, {"enter", ""}, {"e", ""}, {"esc", ""}},
		[][2]string{{"enter", ""}, {"esc", ""}})...)
}

// nameEditView is the editor for a typed name with what it will be saved as:
// the typed text made safe, and whether that name can be used.
func (m metaPageModel) nameEditView() string {
	width := max(20, m.width)
	body := m.editor.view(width)
	name := metasource.SanitizeFileName(m.editor.values()[0], bookExt(m.path))
	var verdict string
	switch {
	case name == "":
		verdict = colorFaint("Type a name")
	case name == filepath.Base(m.path):
		verdict = colorFaint("● this is the file's name now")
	default:
		if _, err := checkRename(m.path, name); err != nil {
			verdict = colorRed(symbolError) + " " + safeText(strings.ToLower(renameErrorText(err)))
		} else {
			verdict = colorGreen(symbolSuccess) + " " + colorFaint("ready: Enter renames the file")
		}
	}
	lines := []string{body, "", "  " + colorFaint("Saves as  ") + safeText(name)}
	if name != "" {
		lines[2] = ansi.Truncate(lines[2], width, "…")
	}
	return strings.Join(append(lines, "  "+verdict), "\n")
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
		q := metasource.Extract(filepath.Base(path), m, nil, metasource.Hint{NameLayout: metaNameLayout()}).Query
		title, authors = q.Title, q.Authors
	}
	name := metasource.FileNameWith(metaNameLayout(), title, authors, ext)
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
