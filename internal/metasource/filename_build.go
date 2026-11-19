package metasource

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of a built name. Most file systems allow 255 bytes in a name; the stem
// stops well short, so the extension and a " (2)" that avoids a clash still fit.
const (
	maxStemBytes    = 200
	maxAuthorsBytes = 80
	maxNameAuthors  = 3
)

// subtitleMark stands for a place where a title was split, until the name is
// put together: a colon, a spaced colon or a spaced dash.
const subtitleMark = "\x01"

var (
	// typedSeparators is titleSeparators without the dash: a name someone typed
	// may say "Author - Title" on purpose.
	// A run of two spaces is a subtitle in a name FileNameFor built, so it stays
	// one.
	typedSeparators = strings.NewReplacer(" : ", subtitleMark, ": ", subtitleMark, "  ", subtitleMark)
	titleSeparators = strings.NewReplacer(" : ", subtitleMark, ": ", subtitleMark, " - ", subtitleMark)
	// pathLike become a hyphen, and nothing else is kept that a file system or
	// a shell would take for syntax.
	pathLike = strings.NewReplacer("/", "-", `\`, "-", ":", "-", `"`, "'", "<", "", ">", "", "|", "", "?", "", "*", "")
)

// FileNameFor builds the name an e-book file is given from its title and
// authors: "Title - Authors.ext" (see NameLayout for the other orders), or
// "Title.ext" when no author is known. It
// returns "" when there is no title, because a name of authors alone says
// nothing about the book.
//
// The name is plain text any system takes: no path separators, no characters
// Windows rejects, no control or bidi marks (the text may come from a server),
// no leading or trailing dot, no reserved Windows device name, and at most 255
// bytes, cut on a character boundary in the title. At most three authors are
// named, and "Last, First" for an author is written "First Last", as a comma
// would split it into two people. A subtitle is written after two spaces
// and not a colon, which Windows rejects; GuessFromFileName reads it back as a
// colon, so the name finds the same book again.
func FileNameFor(title string, authors []string, ext string) string {
	return FileNameWith(DefaultNameLayout, title, authors, ext)
}

// FileNameWith is FileNameFor in the given layout. The title is cut to what is
// left of the length limit after the authors, whatever the layout.
func FileNameWith(layout NameLayout, title string, authors []string, ext string) string {
	t := endingGroupAsBrackets(nameText(title, true))
	if t == "" || placeholderTitle(strings.TrimSpace(title)) || placeholderWord(t) {
		return ""
	}
	// A layout that names the authors gives the bare title when none is known;
	// a custom pattern with no {authors} in it never names them.
	prefix := ""
	if layout.usesAuthors() {
		prefix = authorsPart(authors)
	}
	dressed := prefix != "" || !layout.usesAuthors()
	budget := maxStemBytes - len(prefix)
	if dressed {
		budget -= layout.glueBytes()
	}
	t = cutAtBoundary(t, budget)
	stem := t
	if dressed {
		stem = layout.join(t, prefix)
	}
	name := strings.TrimRight(stem, " .") + cleanExt(ext)
	name = strings.TrimLeft(name, ". ")
	if reservedDeviceName(name) {
		name = "_" + name
	}
	return name
}

// placeholderTitle reports whether a title names no book: a word a program writes
// where a title belongs, a UUID, or a number too long to be a title.
func placeholderTitle(t string) bool {
	return placeholderWord(t) || (numberLike(t) && !shortNumber(t))
}

// placeholderWord is placeholderTitle without the numbers: "11/22/63" becomes
// "11-22-63" in a name, and that is a title, not a number.
func placeholderWord(t string) bool {
	lower := strings.ToLower(t)
	return junkTitles[lower] || strings.HasPrefix(lower, "microsoft word") || uuidRe.MatchString(t)
}

var endingGroupRe = regexp.MustCompile(`^(.*\S)\s*[(（]([^()（）]*)[)）]$`)

// endingGroupAsBrackets writes a "(...)" group at the end of a title with square
// brackets. In "Authors - Title (Series, Book 1)", or a name with no authors, the group
// would be read as the authors, as it is in a Z-Library name.
func endingGroupAsBrackets(t string) string {
	if m := endingGroupRe.FindStringSubmatch(t); m != nil {
		return m[1] + " [" + m[2] + "]"
	}
	return t
}

// SanitizeFileName makes a name someone typed safe: the same rules as
// FileNameFor for characters, length and reserved names, but a spaced dash stays
// as written. The extension is added, or replaced when the name has it already.
// It returns "" when nothing is left of the name.
func SanitizeFileName(stem, ext string) string {
	e := cleanExt(ext)
	if e != "" && strings.EqualFold(strings.TrimSpace(stem), "") {
		return ""
	}
	if e != "" && strings.HasSuffix(strings.ToLower(strings.TrimSpace(stem)), e) {
		stem = strings.TrimSpace(stem)
		stem = stem[:len(stem)-len(e)]
	}
	t := cutAtBoundary(nameTextWith(stem, true, typedSeparators), maxStemBytes)
	if t == "" {
		return ""
	}
	name := strings.TrimLeft(strings.TrimRight(t, " .")+e, ". ")
	if reservedDeviceName(name) {
		name = "_" + name
	}
	return name
}

// nameText makes text safe for a file name. With subtitles, a colon or dash
// that separates a subtitle becomes two spaces; without, a single space.
func nameText(s string, subtitles bool) string {
	return nameTextWith(s, subtitles, titleSeparators)
}

func nameTextWith(s string, subtitles bool, separators *strings.Replacer) string {
	s = strings.Map(func(r rune) rune {
		// Format characters go (bidi marks, zero-width spaces, a BOM), but the joiners
		// stay: they spell words in Persian and Indic scripts.
		if unicode.IsSpace(r) { // a tab or a line break between words is a space
			return ' '
		}
		if r == utf8.RuneError || unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '‌' && r != '‍') {
			return -1
		}
		return r
	}, s)
	s = separators.Replace(s)
	s = pathLike.Replace(s)
	joiner := " "
	if subtitles {
		joiner = "  "
	}
	var parts []string
	for part := range strings.SplitSeq(s, subtitleMark) {
		if part = strings.Join(strings.Fields(part), " "); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, joiner)
}

// authorsPart is the authors as a name shows them: up to three, comma
// separated, and short enough to leave room for the title.
func authorsPart(authors []string) string {
	var names []string
	for _, a := range authors {
		if a = nameText(strings.ReplaceAll(flipLastFirst(a), ",", " "), false); a != "" && !containsFold(names, a) {
			names = append(names, a)
		}
	}
	if len(names) > maxNameAuthors {
		names = names[:maxNameAuthors]
	}
	part := strings.Join(names, ", ")
	if len(part) > maxAuthorsBytes && len(names) > 1 {
		part = names[0]
	}
	return strings.TrimRight(cutAtBoundary(part, maxAuthorsBytes), " ,")
}

// flipLastFirst turns "Herbert, Frank" into "Frank Herbert". Only a single
// comma between two words not written in Chinese is read as a surname first.
func flipLastFirst(name string) string {
	last, first, ok := strings.Cut(name, ",")
	if !ok || strings.Contains(first, ",") || hasHan(name) {
		return name
	}
	last, first = strings.TrimSpace(last), strings.TrimSpace(first)
	if last == "" || first == "" || strings.ContainsAny(last, " \t") {
		return name
	}
	return first + " " + last
}

// cutAtBoundary shortens s to at most n bytes without splitting a character,
// and drops the space or punctuation the cut leaves at the end.
func cutAtBoundary(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) > n {
		cut := n
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return strings.TrimRight(s, " ,.;:-")
}

// cleanExt makes an extension lower case, with its dot, and drops anything that
// is not a letter or a digit.
func cleanExt(ext string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(ext) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "." + b.String()
}

// reservedDeviceName reports whether Windows would take name for a device,
// whatever its extension.
func reservedDeviceName(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	switch strings.ToUpper(strings.TrimSpace(stem)) {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
		"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}
