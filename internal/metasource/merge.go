package metasource

import (
	"cmp"
	"slices"
	"strings"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

const (
	defaultMinScore   = 0.45
	defaultMaxResults = DefaultMaxResults
	maxMergedTags     = 10
	maxTagRunes       = 40
	// A found title must be at least this similar to the wanted one to
	// count at all, however well the authors agree.
	minTitleSimilarity = 0.5
	// An unconfirmed ISBN (see Query.Unconfirmed) needs the stricter bar the
	// download uses before it writes an ISBN, so a hint cannot be promoted to
	// the book's own ISBN by a sequel or a neighbouring title.
	hintTitleSimilarity = embedMinTitleSimilarity
)

// noiseTags are catalogue housekeeping entries, not subjects.
var noiseTags = map[string]bool{
	"accessible book": true, "protected daisy": true, "in library": true, "lending library": true,
	"internet archive wishlist": true, "overdrive": true, "large type books": true, "open library staff picks": true,
}

// rankAndMerge pools candidates that describe the same book, merges each
// pool into one candidate, scores it against q and returns the best first.
//
// Pooling: candidates sharing an ISBN are one book. Candidates without a
// shared ISBN are pooled when their title and first author agree, unless
// that would join two different ISBNs, since two ISBNs are two editions.
//
// It pools whatever it is given, including the answers of several sources. Lookup
// gives it one source's answers at a time, because the answers of different
// sources are never merged: a pool there holds one source's duplicates, names one
// source, and gets no bonus for agreeing sources.
func rankAndMerge(q Query, in []Candidate, minScore float64, limit int) []Candidate {
	parent := make([]int, len(in))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// editionsOf returns the identifiers in the pool that name one edition, by
	// scheme: an ISBN, a Goodreads, Douban or WeRead book ID. Two pools that hold different
	// values of the same scheme are two editions and are never joined; pools
	// that share no scheme say nothing against each other.
	editionsOf := func(root int) map[string]string {
		out := map[string]string{}
		for i := range in {
			if find(i) != root {
				continue
			}
			for _, id := range in[i].Identifiers {
				if (id.Scheme == "isbn" || id.Scheme == "goodreads" || id.Scheme == "douban" || id.Scheme == "weread") && id.Value != "" && out[id.Scheme] == "" {
					out[id.Scheme] = id.Value
				}
			}
		}
		return out
	}
	editionLevel := func(root int) bool {
		for i := range in {
			if find(i) == root && in[i].EditionLevel {
				return true
			}
		}
		return false
	}
	// cannotJoin reports whether pools a and b must stay apart: they hold
	// different values of one edition scheme, or both describe an edition
	// and share no scheme, so nothing shows they are the same one. Joining
	// them would hand one edition's ISBN to the other and mix their
	// publisher and date. A work-level pool describes no edition and may
	// join either.
	cannotJoin := func(ra, rb int) bool {
		a, b := editionsOf(ra), editionsOf(rb)
		shared := false
		for scheme, v := range a {
			if w, ok := b[scheme]; ok {
				if w != v {
					return true
				}
				shared = true
			}
		}
		return !shared && editionLevel(ra) && editionLevel(rb) && (len(a) > 0 || len(b) > 0)
	}

	byISBN := map[string]int{}
	for i, c := range in {
		if v := c.ISBN(); v != "" {
			if j, ok := byISBN[v]; ok {
				parent[find(i)] = find(j)
			} else {
				byISBN[v] = i
			}
		}
	}
	byKey := map[string]int{}
	for i, c := range in {
		key := string(foldText(mainTitle(c.Title)))
		if key == "" {
			continue
		}
		if len(c.Authors) > 0 {
			key += "|" + personKey(c.Authors[0])
		}
		j, ok := byKey[key]
		if !ok {
			byKey[key] = i
			continue
		}
		ri, rj := find(i), find(j)
		if ri == rj {
			continue
		}
		if cannotJoin(ri, rj) {
			continue
		}
		parent[ri] = rj
	}

	pools := map[int][]Candidate{}
	var order []int
	for i, c := range in {
		r := find(i)
		if _, ok := pools[r]; !ok {
			order = append(order, r)
		}
		pools[r] = append(pools[r], c)
	}

	var out []Candidate
	for _, r := range order {
		c := mergePool(q, pools[r])
		c.Match, c.Score = scoreCandidate(q, c)
		if c.Match == MatchISBN || c.Score >= minScore {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		if (a.Match == MatchISBN) != (b.Match == MatchISBN) {
			if a.Match == MatchISBN {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			// A perfect title and author match is at the score cap whichever
			// edition it is, so the edition the file agrees with goes first.
			cmp.Compare(editionBonus(q, b), editionBonus(q, a)),
			cmp.Compare(len(b.Sources), len(a.Sources)), // more sources first; equal for a lookup
			cmp.Compare(completeness(b), completeness(a)),
			strings.Compare(a.Title, b.Title),
		)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// scoreCandidate relates c to q. Sharing an ISBN and agreeing on the title is
// the only way to score 1 (a hint ISBN needs a title that did not come from the
// same record as the hint). An ISBN match whose title disagrees with the
// wanted one scores 0.5: files often carry the ISBN of another book, so the
// result is kept but flagged. Otherwise the title (65%) and authors (35%)
// decide, with a small bonus for each further source that agrees. A candidate
// that Lookup returns names one source, so it gets no such bonus.
func scoreCandidate(q Query, c Candidate) (Match, float64) {
	if isbn := c.ISBN(); isbn != "" && slices.Contains(q.ISBNs, isbn) {
		need := minTitleSimilarity
		unconfirmed := slices.Contains(q.Unconfirmed, isbn)
		if unconfirmed {
			need = hintTitleSimilarity // nothing but the title vouches for it
		}
		if q.Title != "" && titleSimilarity(q.Title, c.Title) < need {
			return MatchISBN, 0.5
		}
		// A title from a file name or a download's record comes from the same
		// record as a recorded ISBN, so it cannot vouch for that ISBN.
		// With no title to compare there is nothing to vouch for a hint at all.
		if unconfirmed && (q.TitleGuessed || q.Title == "") {
			return MatchISBN, 0.5
		}
		return MatchISBN, 1
	}
	ts := titleSimilarity(q.Title, c.Title)
	if ts < minTitleSimilarity {
		return MatchTitle, ts * 0.5
	}
	s := ts
	if len(q.Authors) > 0 {
		s = 0.65*ts + 0.35*authorSimilarity(q.Authors, c.Authors)
	}
	s += min(0.06, 0.03*float64(len(c.Sources)-1))
	s += editionBonus(q, c)
	return MatchTitle, min(s, 0.99)
}

// editionBonus is the little a matching publisher or year adds to a title
// match, so that of several editions of one title the one that agrees with the
// file ranks first. It is far below what an ISBN decides, and it is not applied
// to an ISBN match.
func editionBonus(q Query, c Candidate) float64 {
	bonus := 0.0
	if p, cp := string(foldText(q.Publisher)), string(foldText(c.Publisher)); len([]rune(p)) >= 2 && len([]rune(cp)) >= 2 &&
		(p == cp || strings.Contains(p, cp) || strings.Contains(cp, p)) {
		bonus += 0.04
	}
	if len(q.Year) == 4 && strings.HasPrefix(c.Published, q.Year) {
		bonus += 0.03
	}
	return bonus
}

func completeness(c Candidate) int {
	n := 0
	for _, ok := range []bool{c.Title != "", len(c.Authors) > 0, c.Publisher != "", len(c.Languages) > 0,
		len(c.Tags) > 0, c.Published != "", c.Description != "", c.ISBN() != "", c.CoverURL != ""} {
		if ok {
			n++
		}
	}
	return n
}

// mergePool combines candidates for one book. Each field has its own rule:
//
//	title        most agreed (by folded text); ties go to the one closest to
//	             the wanted title, then the shortest
//	authors      most agreed list; ties go to the longer list
//	publisher    most agreed; ties go to the shortest
//	language     most agreed
//	published    most agreed year; then the most specific date, then the earliest
//	description  the longest
//	tags         union, most-mentioned first, housekeeping tags dropped, capped
//	ISBN         the one the query named, else the most common
//	cover        from the source with the best cover priority
func mergePool(q Query, pool []Candidate) Candidate {
	var out Candidate
	names := map[string]bool{}
	for _, c := range pool {
		for _, n := range c.Sources {
			names[n] = true
		}
		out.EditionLevel = out.EditionLevel || c.EditionLevel
	}
	for n := range names {
		out.Sources = append(out.Sources, n)
	}
	slices.Sort(out.Sources)

	pick := func(values []string, key func(string) string, better func(a, b string) bool) string {
		counts := map[string]int{}
		first := map[string]string{}
		for _, v := range values {
			if v == "" {
				continue
			}
			k := key(v)
			counts[k]++
			if _, ok := first[k]; !ok {
				first[k] = v
			}
		}
		best, bestN := "", 0
		for k, n := range counts {
			v := first[k]
			if n > bestN || n == bestN && better(v, best) {
				best, bestN = v, n
			}
		}
		return best
	}
	fold := func(s string) string { return string(foldText(s)) }
	shorter := func(a, b string) bool { return len(a) < len(b) || len(a) == len(b) && a < b }

	var titles, pubs, langs, dates, descs []string
	var authorLists []string
	listOf := map[string][]string{}
	for _, c := range pool {
		titles = append(titles, c.Title)
		pubs = append(pubs, c.Publisher)
		if len(c.Languages) > 0 {
			langs = append(langs, c.Languages[0])
		}
		dates = append(dates, c.Published)
		descs = append(descs, c.Description)
		if len(c.Authors) > 0 {
			k := strings.Join(c.Authors, "\x00")
			authorLists = append(authorLists, k)
			listOf[k] = c.Authors
		}
	}
	out.Title = pick(titles, fold, func(a, b string) bool {
		if q.Title != "" {
			if sa, sb := titleSimilarity(q.Title, a), titleSimilarity(q.Title, b); sa != sb {
				return sa > sb
			}
		}
		return shorter(a, b)
	})
	if k := pick(authorLists, func(s string) string {
		parts := strings.Split(s, "\x00")
		for i, p := range parts {
			parts[i] = personKey(p)
		}
		return strings.Join(parts, "|")
	}, func(a, b string) bool {
		la, lb := len(listOf[a]), len(listOf[b])
		return la > lb || la == lb && a < b
	}); k != "" {
		out.Authors = listOf[k]
	}
	out.Publisher = pick(pubs, fold, shorter)
	if l := pick(langs, strings.ToLower, shorter); l != "" {
		out.Languages = []string{l}
	}
	out.Published = pick(dates, func(s string) string { return s[:min(4, len(s))] }, func(a, b string) bool {
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return a < b
	})
	// The winning year may have several dates; take the most specific.
	if out.Published != "" {
		year := out.Published[:min(4, len(out.Published))]
		for _, d := range dates {
			if strings.HasPrefix(d, year) && (len(d) > len(out.Published) || len(d) == len(out.Published) && d < out.Published) {
				out.Published = d
			}
		}
	}
	for _, d := range descs {
		if len([]rune(d)) > len([]rune(out.Description)) {
			out.Description = d
		}
	}
	// The first source that names a series wins, with its spelling; another
	// that names the same series with a position adds the position when the
	// first had none.
	for _, c := range pool {
		switch {
		case c.Series == "":
		case out.Series == "":
			out.Series, out.SeriesIndex = c.Series, c.SeriesIndex
		case fold(c.Series) == fold(out.Series) && out.SeriesIndex == 0:
			out.SeriesIndex = c.SeriesIndex
		}
	}
	out.Tags = mergeTags(pool)
	out.Identifiers = mergeIdentifiers(q, pool)
	bestRank := 1 << 30
	for _, c := range pool {
		if c.CoverURL != "" && c.coverRank < bestRank {
			out.CoverURL, out.coverRank, bestRank = c.CoverURL, c.coverRank, c.coverRank
		}
	}
	return out
}

func mergeTags(pool []Candidate) []string {
	counts := map[string]int{}
	display := map[string]string{}
	var order []string
	for _, c := range pool {
		for _, t := range c.Tags {
			k := strings.ToLower(t)
			if isNoiseTag(k) || len([]rune(t)) > maxTagRunes {
				continue
			}
			if _, ok := display[k]; !ok {
				display[k] = t
				order = append(order, k)
			}
			counts[k]++
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(counts[b], counts[a]) })
	if len(order) > maxMergedTags {
		order = order[:maxMergedTags]
	}
	out := make([]string, len(order))
	for i, k := range order {
		out[i] = display[k]
	}
	return out
}

func mergeIdentifiers(q Query, pool []Candidate) []ebookmeta.Identifier {
	var out []ebookmeta.Identifier
	seen := map[string]bool{}
	isbns := map[string]int{}
	for _, c := range pool {
		if v := c.ISBN(); v != "" {
			isbns[v]++
		}
	}
	// The ISBN the query named wins; otherwise the most common, then the
	// smallest, so the choice never depends on map order.
	candidates := make([]string, 0, len(isbns))
	for v := range isbns {
		candidates = append(candidates, v)
	}
	slices.SortFunc(candidates, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(b2i(slices.Contains(q.ISBNs, b)), b2i(slices.Contains(q.ISBNs, a))),
			cmp.Compare(isbns[b], isbns[a]),
			strings.Compare(a, b),
		)
	})
	best := ""
	if len(candidates) > 0 {
		best = candidates[0]
	}
	if best != "" {
		out = append(out, ebookmeta.Identifier{Scheme: "isbn", Value: best})
		seen["isbn"] = true
	}
	for _, c := range pool {
		for _, id := range c.Identifiers {
			if !seen[id.Scheme] {
				seen[id.Scheme] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isNoiseTag reports catalogue housekeeping and subject headings that are
// not useful as tags: the fixed list above, language subjects such as
// "Spanish language", "Translations from …", and headings with commas
// ("Romans, nouvelles, etc. pour la jeunesse"). k is lower case.
func isNoiseTag(k string) bool {
	return noiseTags[k] || strings.HasSuffix(k, " language") || strings.HasPrefix(k, "translations from") || strings.Contains(k, ",")
}
