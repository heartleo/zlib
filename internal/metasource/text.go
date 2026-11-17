package metasource

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// foldText reduces s to the characters that identify it: lower case, no
// punctuation or spaces, full-width ASCII narrowed, Roman numeral glyphs
// turned into digits. CJK text has no spaces to split on, so every
// comparison here works on runes, not words.
func foldText(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E { // full-width ASCII
			r -= 0xFEE0
		}
		// Ⅰ to Ⅻ and ⅰ to ⅻ: a volume number, as in "三体Ⅱ". They are not
		// letters or digits to Unicode, so without this "三体Ⅱ" would fold to
		// the same key as "三体".
		if n := romanGlyphValue(r); n > 0 {
			out = append(out, []rune(strconv.Itoa(n))...)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, unicode.ToLower(r))
		}
	}
	return out
}

// romanGlyphValue returns the value of a single-glyph Roman numeral from 1 to
// 12 (U+2160 to U+216B, U+2170 to U+217B), or 0.
func romanGlyphValue(r rune) int {
	switch {
	case r >= 0x2160 && r <= 0x216B:
		return int(r-0x2160) + 1
	case r >= 0x2170 && r <= 0x217B:
		return int(r-0x2170) + 1
	}
	return 0
}

var (
	bracketed    = regexp.MustCompile(`[(\[{（［【].*?[)\]}）］】]`)
	subtitleCut  = regexp.MustCompile(`\s*[:：]\s.*$|\s+[-–—]\s+.*$|[:：].*$`)
	leadArticle  = regexp.MustCompile(`(?i)^(the|a|an)\s+`)
	roleSuffixes = regexp.MustCompile(`\s*[著译譯编編撰]+$`)
)

// mainTitle strips a subtitle, bracketed notes such as "(2nd edition)" and a
// leading English article, so "The War of Art: Winning…" compares and
// searches as "War of Art".
func mainTitle(s string) string {
	t := bracketed.ReplaceAllString(s, " ")
	if cut := subtitleCut.ReplaceAllString(t, ""); len(foldText(cut)) >= 2 {
		t = cut
	}
	t = leadArticle.ReplaceAllString(strings.TrimSpace(t), "")
	return strings.TrimSpace(t)
}

// personKey normalizes an author name for comparison: "Herbert, Frank" and
// "Frank Herbert" agree, and country tags such as "[美]" and role words such
// as "著" are dropped.
func personKey(name string) string {
	name = bracketed.ReplaceAllString(name, " ")
	name = roleSuffixes.ReplaceAllString(strings.TrimSpace(name), "")
	if before, after, ok := strings.Cut(name, ","); ok && !strings.Contains(after, ",") {
		name = after + " " + before
	}
	return string(foldText(name))
}

// dice is the Sørensen–Dice coefficient over rune bigrams (unigrams for a
// single character), 0 to 1.
func dice(a, b []rune) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) == 1 || len(b) == 1 {
		if len(a) == len(b) && a[0] == b[0] {
			return 1
		}
		return 0
	}
	counts := map[[2]rune]int{}
	for i := 0; i+1 < len(a); i++ {
		counts[[2]rune{a[i], a[i+1]}]++
	}
	shared := 0
	for i := 0; i+1 < len(b); i++ {
		k := [2]rune{b[i], b[i+1]}
		if counts[k] > 0 {
			counts[k]--
			shared++
		}
	}
	return 2 * float64(shared) / float64(len(a)-1+len(b)-1)
}

// titleSimilarity compares a wanted title with a found one. A subtitle on
// either side does not count against a match.
func titleSimilarity(want, got string) float64 {
	if len(foldText(want)) == 0 || len(foldText(got)) == 0 {
		return 0
	}
	best := dice(foldText(want), foldText(got))
	for _, pair := range [][2]string{{want, mainTitle(got)}, {mainTitle(want), got}, {mainTitle(want), mainTitle(got)}} {
		best = max(best, dice(foldText(pair[0]), foldText(pair[1])))
	}
	return best
}

// authorSimilarity averages, over the wanted authors, the best match among
// the found ones. Wanting no author matches anything.
func authorSimilarity(want, got []string) float64 {
	var keys []string
	for _, w := range want {
		if k := personKey(w); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return 1
	}
	total := 0.0
	for _, w := range keys {
		best := 0.0
		for _, g := range got {
			gk := personKey(g)
			s := dice([]rune(w), []rune(gk))
			// A surname alone, or a name with a middle initial, still counts.
			if gk != "" && (strings.Contains(gk, w) || strings.Contains(w, gk)) && min(len(gk), len(w)) >= 3 {
				s = max(s, 0.85)
			}
			best = max(best, s)
		}
		total += best
	}
	return total / float64(len(keys))
}

var (
	tagRe      = regexp.MustCompile(`(?s)<[^>]*>`)
	lineBreak  = regexp.MustCompile(`(?i)<\s*(br|/li)\s*/?\s*>`)
	blockBreak = regexp.MustCompile(`(?i)<\s*/(p|div|h[1-6]|blockquote)\s*>`)
	blankRuns  = regexp.MustCompile(`\n{3,}`)
)

// PlainText is the HTML a description often holds (an EPUB's dc:description
// may carry escaped XHTML) as plain text, for display only: what is written
// back is always the value as it was.
func PlainText(s string) string { return stripHTML(s) }

// stripHTML turns the HTML that sources put in descriptions into plain text:
// a line break for <br>, a blank line between paragraphs.
func stripHTML(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return strings.TrimSpace(s)
	}
	s = lineBreak.ReplaceAllString(s, "\n")
	s = blockBreak.ReplaceAllString(s, "\n\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

var languageCodes = map[string]string{
	"eng": "en", "fre": "fr", "fra": "fr", "ger": "de", "deu": "de", "spa": "es", "ita": "it", "por": "pt",
	"rus": "ru", "chi": "zh", "zho": "zh", "jpn": "ja", "kor": "ko", "ara": "ar", "dut": "nl", "nld": "nl",
	"swe": "sv", "nor": "no", "dan": "da", "fin": "fi", "pol": "pl", "tur": "tr", "gre": "el", "ell": "el",
	"heb": "he", "hin": "hi", "cze": "cs", "ces": "cs", "hun": "hu", "lat": "la", "ukr": "uk", "vie": "vi",
	"tha": "th", "ind": "id",
}

var langTagRe = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]{2,8})*$`)

// normalizeLanguage maps a language code from a source to a BCP 47 tag, or
// "" when it is unknown.
func normalizeLanguage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "/languages/")
	if code, ok := languageCodes[s]; ok {
		return code
	}
	if langTagRe.MatchString(s) {
		return s
	}
	return ""
}

var (
	months = map[string]string{
		"jan": "01", "feb": "02", "mar": "03", "apr": "04", "may": "05", "jun": "06",
		"jul": "07", "aug": "08", "sep": "09", "oct": "10", "nov": "11", "dec": "12",
	}
	isoDateRe = regexp.MustCompile(`^(\d{4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?(?:[T ].*)?$`)
	// "2008年1月5日", "2008年1月" and "2008年", as Chinese catalogues write them.
	cnDateRe   = regexp.MustCompile(`(\d{4})\s*年(?:\s*(\d{1,2})\s*月(?:\s*(\d{1,2})\s*[日号])?)?`)
	textDateRe = regexp.MustCompile(`(?i)\b(?:(\d{1,2})\s+)?([a-z]{3})[a-z]*\.?\s*(?:(\d{1,2}),?\s+)?(\d{4})\b`)
	// A four-digit year not part of a longer number; "c1999" counts.
	yearOnlyRe = regexp.MustCompile(`(?:^|\D)(1[0-9]{3}|20[0-9]{2})(?:\D|$)`)
)

// normalizeDate returns s as YYYY, YYYY-MM or YYYY-MM-DD, or "" when it
// cannot be read.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if m := isoDateRe.FindStringSubmatch(s); m != nil {
		return joinDate(m[1], m[2], m[3])
	}
	if m := cnDateRe.FindStringSubmatch(s); m != nil {
		return joinDate(m[1], m[2], m[3])
	}
	if m := textDateRe.FindStringSubmatch(s); m != nil {
		if mon, ok := months[strings.ToLower(m[2])]; ok {
			day := m[1]
			if day == "" {
				day = m[3]
			}
			return joinDate(m[4], mon, day)
		}
	}
	if m := yearOnlyRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// joinDate builds YYYY, YYYY-MM or YYYY-MM-DD from the parts of a date, at the
// precision that is real: an impossible month drops to the year, and an
// impossible day (0, or past the end of that month) drops to the month.
func joinDate(y, m, d string) string {
	year, _ := strconv.Atoi(y)
	if year < 1000 {
		return ""
	}
	month, _ := strconv.Atoi(m) // "" reads as 0
	if month < 1 || month > 12 {
		return y
	}
	day, _ := strconv.Atoi(d)
	if day < 1 || day > daysIn(year, month) {
		return fmt.Sprintf("%s-%02d", y, month)
	}
	return fmt.Sprintf("%s-%02d-%02d", y, month, day)
}

// daysIn is the number of days in a month of a year.
func daysIn(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
