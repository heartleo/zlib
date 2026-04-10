package zlib

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var pagerTotalRe = regexp.MustCompile(`pagesTotal:\s*(\d+)`)
var resetDurationRe = regexp.MustCompile(`\d+[hH]\s*\d+[mM]|\d+[hH]|\d+[mM]`)
var historyDateRe = regexp.MustCompile(`(?i)\b(?:\d{4}[-/\.]\d{1,2}[-/\.]\d{1,2}(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?|\d{1,2}[-/\.]\d{1,2}[-/\.]\d{2,4}(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?|(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\s+\d{1,2},?\s+\d{4}(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?)\b`)
var historySizeRe = regexp.MustCompile(`(?i)\b\d+(?:[\.,]\d+)?\s*(?:kb|mb|gb|tb|bytes?)\b`)
var historyFormatRe = regexp.MustCompile(`(?i)\b(?:pdf|epub|txt|docx?|html?|rtf|fb2|mobi|azw3?|djvu?|lit)\b`)

func parseSearchResults(html, mirror string) ([]Book, int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	box := doc.Find("#searchResultBox")
	if box.Length() == 0 {
		return nil, 0, fmt.Errorf("%w: searchResultBox not found", ErrParseFailed)
	}

	if box.Find(".notFound").Length() > 0 {
		return nil, 0, nil
	}

	var books []Book
	box.Find(".book-item z-bookcard").Each(func(_ int, s *goquery.Selection) {
		b := Book{}
		b.ID, _ = s.Attr("id")
		b.ISBN, _ = s.Attr("isbn")
		if href, ok := s.Attr("href"); ok {
			b.URL = mirror + href
		}
		b.Publisher, _ = s.Attr("publisher")
		b.Year, _ = s.Attr("year")
		b.Language, _ = s.Attr("language")
		b.Extension, _ = s.Attr("extension")
		b.Size, _ = s.Attr("filesize")
		b.Rating, _ = s.Attr("rating")
		b.Quality, _ = s.Attr("quality")

		if img := s.Find("img"); img.Length() > 0 {
			b.Cover, _ = img.Attr("data-src")
		}
		if title := s.Find(`div[slot="title"]`); title.Length() > 0 {
			b.Name = strings.TrimSpace(title.Text())
		}
		if author := s.Find(`div[slot="author"]`); author.Length() > 0 {
			parts := strings.Split(author.Text(), ";")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					b.Authors = append(b.Authors, p)
				}
			}
		}
		books = append(books, b)
	})

	totalPages := 1
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		text := s.Text()
		if m := pagerTotalRe.FindStringSubmatch(text); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				totalPages = n
			}
		}
	})

	return books, totalPages, nil
}

func parseBookDetail(html, mirror string) (Book, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Book{}, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	b := Book{}

	zcover := doc.Find("z-cover")
	if zcover.Length() > 0 {
		b.ID, _ = zcover.Attr("id")
		if title, ok := zcover.Attr("title"); ok {
			b.Name = strings.TrimSpace(title)
		}
		if img := zcover.Find("img.image"); img.Length() > 0 {
			b.Cover, _ = img.Attr("src")
		}
	}

	doc.Find("i.authors a").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Text())
		if name != "" {
			b.Authors = append(b.Authors, name)
		}
	})

	if desc := doc.Find("#bookDescriptionBox"); desc.Length() > 0 {
		b.Description = strings.TrimSpace(desc.Text())
	}

	details := doc.Find(".bookDetailsBox")
	for _, prop := range []string{"year", "edition", "publisher", "language"} {
		sel := details.Find(".property_" + prop + " .property_value")
		if sel.Length() > 0 {
			val := strings.TrimSpace(sel.Text())
			switch prop {
			case "year":
				b.Year = val
			case "edition":
				b.Edition = val
			case "publisher":
				b.Publisher = val
			case "language":
				b.Language = val
			}
		}
	}

	cat := details.Find(".property_categories .property_value")
	if cat.Length() > 0 {
		b.Categories = strings.TrimSpace(cat.Text())
	}

	file := details.Find(".property__file .property_value")
	if file.Length() > 0 {
		parts := strings.SplitN(file.Text(), ",", 2)
		if len(parts) >= 1 {
			b.Extension = strings.TrimSpace(parts[0])
		}
		if len(parts) >= 2 {
			b.Size = strings.TrimSpace(parts[1])
		}
	}

	if rating := doc.Find(".book-rating"); rating.Length() > 0 {
		text := strings.TrimSpace(rating.Text())
		text = strings.Join(strings.Fields(text), " ")
		b.Rating = text
	}

	for _, sel := range []string{
		"a.addDownloadedBook",
		"a[href*='" + downloadPathPrefix + "']",
		"a[href*='" + filePathPrefix + "']",
		"a.btn[href]",
	} {
		dlBtn := doc.Find(sel).First()
		if dlBtn.Length() == 0 {
			continue
		}
		text := strings.ToLower(strings.Join(strings.Fields(dlBtn.Text()), " "))
		if strings.Contains(text, "unavailable") {
			b.DownloadURL = ""
			break
		}
		if href, ok := dlBtn.Attr("href"); ok && href != "" {
			b.DownloadURL = absolutizeURL(mirror, href)
			break
		}
	}

	return b, nil
}

func parseDownloadHistory(html, mirror string) ([]DownloadHistoryItem, int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	pageText := strings.TrimSpace(doc.Text())
	if strings.Contains(pageText, "Downloads not found") {
		return nil, 0, nil
	}

	totalPages := 1
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if m := pagerTotalRe.FindStringSubmatch(s.Text()); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > totalPages {
				totalPages = n
			}
		}
	})

	box := doc.Find(".dstats-content")
	if box.Length() > 0 {
		if items := extractDownloadHistoryItems(box, mirror); len(items) > 0 {
			return items, totalPages, nil
		}
	}

	if items := extractDownloadHistoryItems(doc.Selection, mirror); len(items) > 0 {
		return items, totalPages, nil
	}

	return nil, 0, fmt.Errorf("%w: download history rows not found", ErrParseFailed)
}

func extractDownloadHistoryItems(root *goquery.Selection, mirror string) []DownloadHistoryItem {
	var (
		items []DownloadHistoryItem
		seen  = make(map[string]struct{})
	)

	root.Find("tr").Each(func(_ int, s *goquery.Selection) {
		item := DownloadHistoryItem{}

		if title := s.Find(".book-title").First(); title.Length() > 0 {
			item.Name = strings.TrimSpace(title.Text())
		}

		link := s.Find("a[href*='" + bookPathPrefix + "']").First()
		if link.Length() > 0 {
			if item.Name == "" {
				item.Name = strings.TrimSpace(link.Text())
			}
			if href, ok := link.Attr("href"); ok {
				item.URL = absolutizeURL(mirror, href)
			}
		}

		for _, sel := range []string{
			"a[href*='" + downloadPathPrefix + "']",
			"a[href*='" + filePathPrefix + "']",
			"a[download]",
		} {
			dlLink := s.Find(sel).First()
			if dlLink.Length() == 0 {
				continue
			}
			if href, ok := dlLink.Attr("href"); ok && href != "" {
				item.DownloadURL = absolutizeURL(mirror, href)
				break
			}
		}

		if date := s.Find("td.lg-w-120").First(); date.Length() > 0 {
			item.Date = normalizeHistoryDate(date.Text())
		}
		if item.Date == "" {
			s.Find("td").Each(func(i int, td *goquery.Selection) {
				if item.Date == "" && i > 0 {
					item.Date = normalizeHistoryDate(td.Text())
				}
			})
		}

		item.Extension = extractHistoryFormat(s)
		item.Size = extractHistorySize(s)

		if item.Name == "" && item.URL == "" && item.DownloadURL == "" {
			return
		}

		key := item.Name + "|" + item.URL + "|" + item.DownloadURL + "|" + item.Extension + "|" + item.Size + "|" + item.Date
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	})

	return items
}

func normalizeHistoryDate(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}

	matched := historyDateRe.FindString(text)
	return strings.TrimSpace(matched)
}

func normalizeHistorySize(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	return strings.TrimSpace(historySizeRe.FindString(text))
}

func normalizeHistoryFormat(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	matched := strings.TrimSpace(historyFormatRe.FindString(text))
	return strings.ToUpper(matched)
}

func extractHistoryFormat(row *goquery.Selection) string {
	for _, candidate := range historyMetadataCandidates(row) {
		if format := normalizeHistoryFormat(candidate); format != "" {
			return format
		}
	}
	return ""
}

func extractHistorySize(row *goquery.Selection) string {
	for _, candidate := range historyMetadataCandidates(row) {
		if size := normalizeHistorySize(candidate); size != "" {
			return size
		}
	}
	return ""
}

func historyMetadataCandidates(row *goquery.Selection) []string {
	candidates := []string{}
	seen := map[string]struct{}{}
	appendCandidate := func(value string) {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	for _, attr := range []string{"data-extension", "extension", "data-format", "format", "data-filetype", "data-book-format", "data-filesize", "filesize", "data-size", "size", "title", "aria-label"} {
		if value, ok := row.Attr(attr); ok {
			appendCandidate(value)
		}
	}

	row.Find("td, a, span, div, button, z-bookcard, z-book").Each(func(_ int, s *goquery.Selection) {
		appendCandidate(s.Text())
		for _, attr := range []string{"data-extension", "extension", "data-format", "format", "data-filetype", "data-book-format", "data-filesize", "filesize", "data-size", "size", "title", "aria-label"} {
			if value, ok := s.Attr(attr); ok {
				appendCandidate(value)
			}
		}
	})

	appendCandidate(row.Text())
	return candidates
}

func parseDownloadLimits(html string) (DownloadLimit, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return DownloadLimit{}, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	dstats := doc.Find(".dstats-info")
	if dstats.Length() == 0 {
		return DownloadLimit{}, fmt.Errorf("%w: dstats-info not found", ErrParseFailed)
	}

	dlInfo := dstats.Find(".d-count")
	if dlInfo.Length() == 0 {
		return DownloadLimit{}, fmt.Errorf("%w: d-count not found", ErrParseFailed)
	}

	parts := strings.SplitN(strings.TrimSpace(dlInfo.Text()), "/", 2)
	if len(parts) != 2 {
		return DownloadLimit{}, fmt.Errorf("%w: unexpected limit format", ErrParseFailed)
	}

	daily, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	allowed, _ := strconv.Atoi(strings.TrimSpace(parts[1]))

	resetText := ""
	if dlReset := dstats.Find(".d-reset"); dlReset.Length() > 0 {
		raw := strings.TrimSpace(dlReset.Text())
		if m := resetDurationRe.FindString(raw); m != "" {
			resetText = strings.TrimSpace(m)
		} else {
			resetText = raw
		}
	}

	return DownloadLimit{
		DailyAmount:    daily,
		DailyAllowed:   allowed,
		DailyRemaining: allowed - daily,
		DailyReset:     resetText,
	}, nil
}
