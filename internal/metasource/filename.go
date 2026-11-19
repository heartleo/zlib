package metasource

import (
	"regexp"
	"strings"
	"unicode"
)

// FileGuess is what a file's name says about the book it holds, read the way
// Z-Library saves names: "Title (Author)", with a second title after " = ",
// translators after the author, and a bracket group that may be an edition.
type FileGuess struct {
	// Stem is the name without its extension.
	Stem string
	// Title is the main title; AltTitle is the second title of a bilingual name.
	Title, AltTitle string
	// Edition is a trailing group that names an edition, such as "第8版".
	Edition string
	Authors []string
	// ISBNs are valid ISBN-13 values found anywhere in the name.
	ISBNs []string
	// DashAuthors and DashTitle are the reading of an "Author - Title" name, the
	// shape FileNameFor builds. The name is just as likely a title with a dash
	// in it, so Title is left whole and Extract uses this only when the file's
	// own metadata agrees with it.
	DashAuthors []string
	DashTitle   string
	// RevAuthors and RevTitle are the other reading of a dashed name, "Title -
	// Author", the layout FileNameFor builds by default.
	RevAuthors []string
	RevTitle   string
	// Structured is true when the name has a trailing "(...)" group, which is
	// what makes its title worth trusting over a bare name.
	Structured bool
}

// ebookExtensions are the extensions stripped from a name. A name with another
// dot, such as "R.I.P", keeps it.
var ebookExtensions = map[string]bool{
	"epub": true, "mobi": true, "azw": true, "azw3": true, "azw4": true, "kfx": true, "pdf": true, "fb2": true,
	"docx": true, "doc": true, "txt": true, "rtf": true, "djvu": true, "djv": true, "cbz": true, "cbr": true,
	"lit": true, "prc": true, "pdb": true, "lrf": true, "htm": true, "html": true, "zip": true,
}

// siteRe is one download site's name, with or without its domain: browser
// downloads list several of them in one group.
const siteRe = `(?:(?:z-?lib(?:rary)?|1lib|libgen|b-?ok|singlelogin)(?:\.[a-z]{2,})?|anna['’]?s\s*archive)`

var (
	// noiseGroupRe is a trailing group a download site or a duplicate adds: a
	// site name, a copy number, or a bare ISBN in brackets.
	noiseGroupRe = regexp.MustCompile(`(?i)\s*[(\[（【]\s*(?:` + siteRe + `(?:\s*[,，]\s*` + siteRe + `)*|\d{1,2}|(?:1[5-9]|20)\d{2}|[0-9Xx\- ]{9,17})\s*[)\]）】]\s*$`)
	// trailingISBNRe is an ISBN written after the title, with or without "ISBN".
	trailingISBNRe = regexp.MustCompile(`(?i)\s+(?:isbn[\s:：-]*)?[0-9][0-9\-]{8,16}[0-9Xx]?\s*$`)
	editionRe      = regexp.MustCompile(`(?i)^(?:第\s*[0-9一二三四五六七八九十]+\s*[版卷册部]|[0-9]+(?:st|nd|rd|th)?\s*ed(?:ition|\.)?|(?:revised|updated|new)\s+edition|[0-9.]+\s*版|修订版|增订版|增补版|全译版|珍藏版|纪念版|典藏版|精装版|套装.*|全\s*[0-9一二三四五六七八九十]+\s*[册卷部]|vol(?:ume)?\.?\s*[0-9]+|v[0-9]+(?:\.[0-9]+)*)$`)
	roleSplitRe    = regexp.MustCompile(`(?:编著|主编|等著|著|撰|编)(?:\s+|[,，、;；]\s*|$)`)
	translatorRe   = regexp.MustCompile(`(?:译者?|翻译)\s*$`)
	nameSplitRe    = regexp.MustCompile(`(?i)\s*[,，、;；&]\s*|\s+and\s+|\s+和\s+|\s{2,}`)
	spaceRunRe     = regexp.MustCompile(`\s{2,}`)
	hanDotRe       = regexp.MustCompile(`(\p{Han})\.(\p{Han})`)
	isbnFieldRe    = regexp.MustCompile(`^[0-9][0-9\-]{8,}[0-9Xx]?$`)
	nonIDRe        = regexp.MustCompile(`[^0-9A-Za-z\-]+`)
	uuidRe         = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	fileLikeRe     = regexp.MustCompile(`(?i)\.(?:docx?|pdf|indd|tmp|epub|mobi|txt|odt|qxd|azw3?)$`)
)

// GuessFromFileName reads a book's title, authors and ISBNs from its file
// name, the last element of its path. It trusts only what the name's shape
// shows: a name with no "(...)" group is Structured false, and a name that is
// only a number or an ISBN offers no title.
func GuessFromFileName(name string) FileGuess {
	stem := name
	if i := strings.LastIndex(stem, "."); i >= 0 && ebookExtensions[strings.ToLower(stem[i+1:])] {
		stem = stem[:i]
	}
	stem = strings.TrimSpace(strings.ReplaceAll(stem, " ", " "))
	g := FileGuess{Stem: stem, ISBNs: isbnsInName(stem)}

	s := stem
	for { // site names, copy numbers and bare ISBNs after the real name
		next := noiseGroupRe.ReplaceAllString(s, "")
		next = trailingISBNRe.ReplaceAllString(next, "")
		if next == s {
			break
		}
		s = next
	}
	s = strings.TrimSpace(s)

	title := s
	if head, inner, ok := trailingGroup(s); ok {
		g.Structured = true
		title = head
		if editionRe.MatchString(inner) {
			g.Edition = inner
		} else {
			g.Authors = parseFileAuthors(inner)
		}
	}
	for _, sep := range []string{" = ", " ＝ "} {
		if left, right, ok := strings.Cut(title, sep); ok {
			title, g.AltTitle = left, cleanFileTitle(right)
			if strings.TrimSpace(left) == "" {
				title, g.AltTitle = right, ""
			}
			break
		}
	}
	g.DashAuthors, g.DashTitle = dashedName(s)
	g.RevAuthors, g.RevTitle = reverseDashedName(s)
	g.Title = cleanFileTitle(title)
	if numberLike(g.Title) && !shortNumber(g.Title) {
		g.Title, g.Structured = "", false
	}
	if g.Title == "" {
		g.Structured = false
	}
	return g
}

// dashedName reads "Author - Title": exactly one spaced dash, with names on the
// left and something that is not a number on the right.
func dashedName(s string) (authors []string, title string) {
	if strings.Count(s, " - ") != 1 {
		return nil, ""
	}
	left, right, _ := strings.Cut(s, " - ")
	title = cleanFileTitle(right)
	if title == "" || numberLike(left) || (numberLike(title) && !shortNumber(title)) {
		return nil, ""
	}
	authors = parseFileAuthors(left)
	if len(authors) == 0 || len(authors) > maxNameAuthors {
		return nil, ""
	}
	return authors, title
}

// reverseDashedName reads "Title - Author": exactly one spaced dash, with
// something that is not a number on the left and names on the right.
func reverseDashedName(s string) (authors []string, title string) {
	if strings.Count(s, " - ") != 1 {
		return nil, ""
	}
	left, right, _ := strings.Cut(s, " - ")
	title = cleanFileTitle(left)
	if title == "" || numberLike(right) || (numberLike(title) && !shortNumber(title)) {
		return nil, ""
	}
	authors = parseFileAuthors(right)
	if len(authors) == 0 || len(authors) > maxNameAuthors {
		return nil, ""
	}
	return authors, title
}

// trailingGroup splits "head (inner)" at the last top-level bracket group
// that ends the name, counting nested brackets.
func trailingGroup(s string) (head, inner string, ok bool) {
	r := []rune(s)
	if len(r) == 0 || (r[len(r)-1] != ')' && r[len(r)-1] != '）') {
		return "", "", false
	}
	depth := 0
	for i := len(r) - 1; i >= 0; i-- {
		switch r[i] {
		case ')', '）':
			depth++
		case '(', '（':
			depth--
			if depth == 0 {
				inner = strings.TrimSpace(string(r[i+1 : len(r)-1]))
				head = strings.TrimSpace(string(r[:i]))
				if inner == "" || head == "" {
					return "", "", false
				}
				return head, inner, true
			}
		}
	}
	return "", "", false
}

// cleanFileTitle tidies a title read from a name. Z-Library writes a subtitle
// after two spaces where the book has a colon, so a run of spaces becomes
// ": ", which the title matching reads as the start of a subtitle.
func cleanFileTitle(s string) string {
	s = strings.TrimSpace(s)
	s = spaceRunRe.ReplaceAllString(s, ": ")
	return strings.Join(strings.Fields(s), " ")
}

// parseFileAuthors reads the authors from the "(...)" group of a name. A role
// word such as 著 ends the authors, and what follows it up to 译 is a
// translator, who is left out.
func parseFileAuthors(inner string) []string {
	var out []string
	parts := roleSplitRe.Split(inner, -1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// A part ending in 译 is the translators. Without a role word a group such
		// as "A, B 译" cannot say which name is the author, so it gives none.
		if translatorRe.MatchString(part) {
			continue
		}
		for _, name := range nameSplitRe.Split(bracketed.ReplaceAllString(part, " "), -1) {
			name = tidyAuthor(name)
			if name != "" && name != "等" && !containsFold(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// tidyAuthor normalizes a person's name from a file name: the dot between
// parts of a transliterated name is one character, with no spaces around it.
func tidyAuthor(s string) string {
	s = strings.NewReplacer("・", "·", "．", "·", "•", "·", "∙", "·", "‧", "·").Replace(s)
	s = hanDotRe.ReplaceAllString(s, "${1}·${2}") // "戴尔.卡耐基" is "戴尔·卡耐基"
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer(" · ", "·", " ·", "·", "· ", "·").Replace(s)
	return strings.TrimSpace(s)
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// isbnsInName finds valid ISBNs in a name. A name is cut into fields at every
// character that cannot be part of one, so a phone number, a date and two
// ISBNs run together are not read as an ISBN.
func isbnsInName(s string) []string {
	var out []string
	for _, field := range nonIDRe.Split(s, -1) {
		if !isbnFieldRe.MatchString(field) {
			continue
		}
		if isbn := NormalizeISBN(field); isbn != "" && !containsFold(out, isbn) {
			out = append(out, isbn)
		}
	}
	return out
}

// numberLike reports whether s is only digits, spaces and hyphens: an ISBN, a
// date or a counter, which names no book.
func numberLike(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case r == '-' || r == ' ' || r == 'X' || r == 'x':
		default:
			return false
		}
	}
	return digits > 0
}

// shortNumber reports whether s is three or four digits and nothing else: too
// short to be an ISBN, a date or a timestamp, so it can be a title such as 1984.
func shortNumber(s string) bool {
	if n := len(s); n < 3 || n > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// junkTitles are words a program writes where a title belongs.
var junkTitles = map[string]bool{
	"unknown": true, "untitled": true, "title": true, "document": true, "book": true, "ebook": true,
	"未知": true, "无标题": true, "未命名": true, "新建文档": true, "新建文本文档": true,
}

// UsefulTitle reports whether a title read from a file is worth searching: it
// is not empty, a placeholder such as "Unknown" or "未知", a word processor's
// default, a UUID, a number or a file name. The list is narrow on purpose, and
// a title is only judged whole, so "The Unknown Soldier" is useful.
func UsefulTitle(t string) bool {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	switch {
	case junkTitles[lower], strings.HasPrefix(lower, "microsoft word"), uuidRe.MatchString(t), fileLikeRe.MatchString(t), numberLike(t) && !shortNumber(t):
		return false
	case len([]rune(t)) < 2 && !hasHan(t):
		return false
	}
	return true
}
