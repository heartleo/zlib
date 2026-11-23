package metasource

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Soft evidence. An exact ISBN decides a match; everything else is weighed here,
// as findings that each say what they saw. Three rules run through them:
//
//   - conflicts cost more than agreements earn: a finding that shows a different
//     book or edition caps the score, an agreement only adds a little;
//   - missing data is neutral: no year, no language, no ISBN on a side counts for
//     nothing either way;
//   - every finding carries its reason, so the details view, --json and the AI
//     judge are told the same facts.
//
// A cost is a subtraction, not a cap: rows that share a conflict keep their
// order among themselves (the edition hints still tell them apart), and a row
// stays in the list, ranked lower and annotated, instead of vanishing.

// What a conflict costs a title match.
const (
	costOtherVolume  = 0.30 // the same title, a different volume number
	costDerivative   = 0.35 // a summary, study guide or workbook of the book
	costSequel       = 0.12 // a title that extends (or is a stem of) the wanted one
	costOtherEdition = 0.10 // an edition-level row whose ISBN differs from the file's confirmed one

	yearTolerance = 2    // years between editions that still count as agreeing
	yearPenalty   = 0.05 // past the tolerance, a mild cost
	langPenalty   = 0.05 // both languages known and different
)

// finding is one piece of evidence about a candidate.
type finding struct {
	note  string
	delta float64 // added to the score (negative: a cost); 0 for information only
}

// findings lists what the candidate's fields say about it against q, for a
// title match. Findings that are only information have no delta and no cap.
func findings(q Query, c Candidate) []finding {
	var out []finding
	add := func(note string, delta float64) { out = append(out, finding{note, delta}) }

	if q.Title != "" && c.Title != "" {
		switch {
		case volumeConflict(q.Title, c.Title):
			add("volume number differs from the file's title: another volume", -costOtherVolume)
		case derivativeTitle(c.Title) && !derivativeTitle(q.Title):
			add("looks like a summary or study guide, not the book itself", -costDerivative)
		default:
			switch titleExtension(q.Title, c.Title) {
			case 1:
				add("its title extends the file's: may be a sequel or companion", -costSequel)
			case -1:
				add("its title is shorter than the file's: may be another volume or the series", -costSequel)
			}
		}
	}

	if conflict := authorGivenNameConflict(q.Authors, c.Authors); conflict != "" {
		add(conflict, 0) // already weighed by authorSimilarity; the note says why
	}

	if qy, cy := q.Year, yearOf(c.Published); len(qy) == 4 && cy != "" && c.EditionLevel {
		a, _ := strconv.Atoi(qy)
		b, _ := strconv.Atoi(cy)
		switch d := max(a-b, b-a); {
		case d == 0:
		case d <= yearTolerance:
			add(fmt.Sprintf("published %s, %d year(s) from the file's %s: a reprint is possible", cy, d, qy), 0)
		default:
			add(fmt.Sprintf("published %s, %d years from the file's %s", cy, d, qy), -yearPenalty)
		}
	}

	if ql, cl := primaryLanguage(q.Language), candidateLanguages(c); ql != "" && len(cl) > 0 && !containsString(cl, ql) {
		add(fmt.Sprintf("language %s differs from the file's %s", strings.Join(cl, ", "), ql), -langPenalty)
	}

	if isbn := c.ISBN(); isbn != "" && c.EditionLevel {
		confirmed := false
		for _, v := range q.ISBNs {
			if !containsString(q.Unconfirmed, v) {
				confirmed = true
			}
		}
		if confirmed && !containsString(q.ISBNs, isbn) {
			add("its ISBN differs from the file's: another edition of the book", -costOtherEdition)
		}
	}
	return out
}

// applyFindings turns the findings into the score of a title match. A cost
// lowers a row's rank but never takes a row out of the list that it was in: the
// score does not fall below the lookup's floor when it started above it, so a
// conflict is shown (and its note says why) instead of silently dropped.
func applyFindings(score float64, fs []finding) float64 {
	floor := min(score, defaultMinScore)
	for _, f := range fs {
		score += f.delta
	}
	return max(score, floor)
}

// candidateNotes are the reasons behind a candidate's rank, as text.
func candidateNotes(q Query, c Candidate) []string {
	var notes []string
	if isbn := c.ISBN(); isbn != "" && containsString(q.ISBNs, isbn) {
		if containsString(q.Unconfirmed, isbn) {
			notes = append(notes, "shares an ISBN with a hint (not confirmed)")
		} else {
			notes = append(notes, "shares the file's ISBN")
		}
	}
	for _, f := range findings(q, c) {
		notes = append(notes, f.note)
	}
	return notes
}

func yearOf(published string) string {
	if len(published) >= 4 && strings.Trim(published[:4], "0123456789") == "" {
		return published[:4]
	}
	return ""
}

// primaryLanguage is the language part of a tag, whatever the separator: "en",
// "en-US", "zh_CN" and the three-letter "eng" give "en", "en", "zh" and "en".
// "" when it is unknown.
func primaryLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	primary, _, _ := strings.Cut(normalizeLanguage(tag), "-")
	return primary
}

func candidateLanguages(c Candidate) []string {
	var out []string
	for _, l := range c.Languages {
		if p := primaryLanguage(l); p != "" && !containsString(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func containsString(list []string, s string) bool { return slices.Contains(list, s) }

// --- Titles -----------------------------------------------------------------

var (
	asciiRomanTail = regexp.MustCompile(`(?i)\s+(i{1,3}|iv|vi{0,3}|ix|x)\s*$`)
	trailingNumber = regexp.MustCompile(`^(.*[^\d])(\d{1,3})[册卷部集]?$`)
	volumeLabels   = []string{"volume", "vol", "book", "part", "no", "tome", "第"}
	derivative     = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(summary|study guide|analysis|guide to|workbook|review of|summary of|chapter[- ]by[- ]chapter|chapter summary)\b`),
		regexp.MustCompile(`(?i)\b(summary|study guide|book analysis|reading guide|literature guide|workbook|digest)\b`),
	}
	// editionWords are what a title may add without becoming another book.
	editionWords = map[string]bool{
		"edition": true, "ed": true, "revised": true, "updated": true, "illustrated": true, "annotated": true,
		"unabridged": true, "abridged": true, "paperback": true, "hardcover": true, "ebook": true, "novel": true,
		"anniversary": true, "deluxe": true, "special": true, "classics": true, "library": true, "movie": true,
		"tie": true, "in": true, "the": true, "a": true, "an": true, "and": true, "of": true, "complete": true,
	}
)

// volumeOf splits a title into its stem and a trailing volume number: "Dune 02"
// is ("dune", 2), "三体Ⅱ" is ("三体", 2). A bracketed "(Vol. 2)" counts; a
// subtitle after a colon or dash does not.
func volumeOf(title string) (stem string, n int, ok bool) {
	t := strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "（", " ", "）", " ", "【", " ", "】", " ").Replace(title)
	t = subtitleCut.ReplaceAllString(t, "")
	t = asciiRomanTail.ReplaceAllStringFunc(t, func(m string) string {
		return " " + strconv.Itoa(romanValue(strings.TrimSpace(m)))
	})
	m := trailingNumber.FindStringSubmatch(string(foldText(t)))
	if m == nil {
		return "", 0, false
	}
	stem = m[1]
	for changed := true; changed; {
		changed = false
		for _, label := range volumeLabels {
			if s := strings.TrimSuffix(stem, label); s != stem && s != "" {
				stem, changed = s, true
			}
		}
	}
	n, _ = strconv.Atoi(m[2])
	// "Book 1" and "Book 2" are not a series: the stem is only a label.
	return stem, n, len([]rune(stem)) >= 2 && !slices.Contains(volumeLabels, stem)
}

func romanValue(s string) int {
	switch strings.ToLower(s) {
	case "i":
		return 1
	case "ii":
		return 2
	case "iii":
		return 3
	case "iv":
		return 4
	case "v":
		return 5
	case "vi":
		return 6
	case "vii":
		return 7
	case "viii":
		return 8
	case "ix":
		return 9
	case "x":
		return 10
	}
	return 0
}

// volumeConflict reports whether a and b are one title with different volume
// numbers. It fires only when everything else is equal, so "1984", "Fahrenheit
// 451" and "2001" (whose number is the title) are never taken for volumes.
func volumeConflict(a, b string) bool {
	sa, na, oka := volumeOf(a)
	sb, nb, okb := volumeOf(b)
	return oka && okb && sa == sb && na != nb
}

// titleTokens are the words of a title's main part: letters and digits of any
// script, lower case, split at everything else. A run of Han text has no
// spaces, so it is one token.
func titleTokens(title string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range mainTitle(title) {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		if n := romanGlyphValue(r); n > 0 {
			cur = append(cur, []rune(strconv.Itoa(n))...)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

// titleExtension compares the main parts of two titles (subtitles and brackets
// are already cut). It returns 1 when c continues want with more words, -1 when
// c is only the start of want, and 0 otherwise, including when what is added is
// an edition word ("Special Edition"). "Dune" and "Dune Messiah" are 1; a
// subtitle ("Dune: Messiah") is not an extension, since the main parts agree.
func titleExtension(want, got string) int {
	w, g := titleTokens(want), titleTokens(got)
	if len(w) == 0 || len(g) == 0 || len(string(foldText(mainTitle(want)))) < 2 {
		return 0
	}
	extra := func(short, long []string) bool {
		if len(long) <= len(short) {
			return false
		}
		for i, t := range short {
			if long[i] != t {
				return false
			}
		}
		for _, t := range long[len(short):] {
			if !editionWords[t] {
				return true
			}
		}
		return false
	}
	switch {
	case extra(w, g):
		return 1
	case extra(g, w):
		return -1
	}
	// A title with no spaces (Chinese) is one token: a longer run that starts with
	// the other is the same extension.
	if len(w) == 1 && len(g) == 1 && w[0] != g[0] && (hasHan(w[0]) || hasHan(g[0])) {
		switch {
		case strings.HasPrefix(g[0], w[0]) && !cjkEditionTail(strings.TrimPrefix(g[0], w[0])):
			return 1
		case strings.HasPrefix(w[0], g[0]) && !cjkEditionTail(strings.TrimPrefix(w[0], g[0])):
			return -1
		}
	}
	return 0
}

// derivativeTitle reports whether a title names a summary, study guide or
// workbook: a book about a book, which shares its words and not its content.
func derivativeTitle(title string) bool {
	for _, re := range derivative {
		if re.MatchString(title) {
			return true
		}
	}
	return false
}

// --- Authors ----------------------------------------------------------------

// personTokens are the words of a Latin name, "Last, First" put in reading
// order. It returns nil for a name that is not spaced words (a Chinese name),
// which the character-based comparison handles.
func personTokens(name string) []string {
	name = bracketed.ReplaceAllString(name, " ")
	name = roleSuffixes.ReplaceAllString(strings.TrimSpace(name), "")
	if before, after, ok := strings.Cut(name, ","); ok && !strings.Contains(after, ",") {
		name = after + " " + before
	}
	var out []string
	for _, f := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if hasHan(f) {
			return nil
		}
		out = append(out, strings.ToLower(f))
	}
	return out
}

// namePair compares two names that are spaced words. The surname must agree
// first; then the given names decide: equal (or one an initial of the other)
// agree, absent on one side is a partial match (a lone surname is never a full
// one), and different given names are a conflict: Frank and Brian Herbert are
// two writers. ok is false when the surnames differ, and the caller falls back
// to the character comparison (typos, transliterations).
func namePair(want, got []string) (score float64, conflict, ok bool) {
	if len(want) == 0 || len(got) == 0 || want[len(want)-1] != got[len(got)-1] {
		return 0, false, false
	}
	wf, gf := want[:len(want)-1], got[:len(got)-1]
	switch {
	case len(wf) == 0 || len(gf) == 0:
		return 0.7, false, true
	case wf[0] == gf[0]:
		return 1, false, true
	case len([]rune(wf[0])) == 1 && strings.HasPrefix(gf[0], wf[0]), len([]rune(gf[0])) == 1 && strings.HasPrefix(wf[0], gf[0]):
		return 0.95, false, true
	}
	return 0.3, true, true
}

// authorGivenNameConflict names the first wanted author whose surname a found
// author shares with a different given name, when no found author is the same
// person. "" when there is none.
func authorGivenNameConflict(want, got []string) string {
	for _, w := range want {
		wt := personTokens(w)
		sawConflict, agreed := "", false
		for _, g := range got {
			if _, conflict, ok := namePair(wt, personTokens(g)); ok {
				if conflict {
					sawConflict = g
				} else {
					agreed = true
				}
			}
		}
		if sawConflict != "" && !agreed {
			return fmt.Sprintf("author %s shares a surname with the file's %s but not the given name", sawConflict, w)
		}
	}
	return ""
}

// cjkEditionTail reports whether what follows a Chinese title is only an
// edition word (全集, 修订版, 典藏版, 插图版...): the same book, not a sequel.
func cjkEditionTail(tail string) bool {
	if tail == "" {
		return true
	}
	digits := false
	for _, r := range tail {
		switch {
		case unicode.IsDigit(r):
			digits = true
		case !strings.ContainsRune("全集版修订增订典藏珍藏精装平装插图纪念新译本合订本第", r):
			return false
		}
	}
	// A number is a volume ("三体2") unless it numbers an edition ("第2版").
	return !digits || strings.ContainsAny(tail, "版本")
}
