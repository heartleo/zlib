package zlib

import "testing"

const testSearchHTML = `<html><body>
<div id="searchResultBox">
  <div class="book-item">
    <z-bookcard id="12047606" isbn="978-111" href="/book/2RAqApzDRL/biology.html"
      publisher="Bio Press" language="english" year="2021" extension="pdf"
      filesize="25.19 MB" rating="4.0" quality="5.0">
      <img data-src="https://covers.z-library.sk/cover.jpg">
      <div slot="title">Biology Today</div>
      <div slot="author">John Smith; Jane Doe</div>
    </z-bookcard>
  </div>
  <div class="book-item">
    <z-bookcard id="99999" isbn="" href="/book/XYZABC/cell.html"
      publisher="" language="russian" year="2019" extension="epub"
      filesize="10.5 MB" rating="3.5" quality="4.0">
      <img data-src="https://covers.z-library.sk/cover2.jpg">
      <div slot="title">Cell Biology</div>
      <div slot="author">Alice</div>
    </z-bookcard>
  </div>
</div>
<script>var pagerOptions = { pagesTotal: 5, pageCurrent: 1 };</script>
</body></html>`

func TestParseSearchResults(t *testing.T) {
	mirror := "https://zh.z-library.sk"
	books, totalPages, err := parseSearchResults(testSearchHTML, mirror)
	if err != nil {
		t.Fatalf("parseSearchResults() error = %v", err)
	}
	if totalPages != 5 {
		t.Errorf("totalPages = %d, want 5", totalPages)
	}
	if len(books) != 2 {
		t.Fatalf("len(books) = %d, want 2", len(books))
	}

	b := books[0]
	if b.ID != "12047606" {
		t.Errorf("ID = %q, want %q", b.ID, "12047606")
	}
	if b.Name != "Biology Today" {
		t.Errorf("Name = %q, want %q", b.Name, "Biology Today")
	}
	if b.URL != mirror+"/book/2RAqApzDRL/biology.html" {
		t.Errorf("URL = %q", b.URL)
	}
	if len(b.Authors) != 2 || b.Authors[0] != "John Smith" {
		t.Errorf("Authors = %v", b.Authors)
	}
	if b.Year != "2021" {
		t.Errorf("Year = %q", b.Year)
	}
	if b.Extension != "pdf" {
		t.Errorf("Extension = %q", b.Extension)
	}
}

const testSearchEmptyHTML = `<html><body>
<div id="searchResultBox">
  <div class="notFound">Nothing found</div>
</div>
</body></html>`

func TestParseSearchResults_NotFound(t *testing.T) {
	books, _, err := parseSearchResults(testSearchEmptyHTML, "https://x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(books) != 0 {
		t.Errorf("expected empty result, got %d books", len(books))
	}
}

const testDetailHTML = `<html><body>
<z-cover id="12047606" title="Biology Today - April 2021">
  <img class="image" src="https://covers.z-library.sk/full.jpg">
</z-cover>
<i class="authors"><a href="/author/Biology Today">Biology Today</a></i>
<div id="bookDescriptionBox">A great book about biology.</div>
<div class="bookDetailsBox">
  <div class="bookProperty property_year"><div class="property_label">Year:</div><div class="property_value">2021</div></div>
  <div class="bookProperty property_publisher"><div class="property_label">Publisher:</div><div class="property_value">Bio Press</div></div>
  <div class="bookProperty property_language"><div class="property_label">Language:</div><span class="property_value">english</span></div>
  <div class="bookProperty property__file"><div class="property_label">File:</div><div class="property_value">PDF, 25.19 MB</div></div>
  <div class="bookProperty property_categories"><div class="property_label">Categories:</div><div class="property_value"><a href="/category/94/Biology">Biology</a></div></div>
</div>
<div class="book-rating">4.0 / 5.0</div>
<a class="btn btn-default addDownloadedBook" href="/dl/aZ6zON1aZ4">PDF, 25.19 MB</a>
</body></html>`

func TestParseBookDetail(t *testing.T) {
	mirror := "https://zh.z-library.sk"
	b, err := parseBookDetail(testDetailHTML, mirror)
	if err != nil {
		t.Fatalf("parseBookDetail() error = %v", err)
	}
	if b.ID != "12047606" {
		t.Errorf("ID = %q", b.ID)
	}
	if b.Name != "Biology Today - April 2021" {
		t.Errorf("Name = %q", b.Name)
	}
	if b.Description != "A great book about biology." {
		t.Errorf("Description = %q", b.Description)
	}
	if b.Year != "2021" {
		t.Errorf("Year = %q", b.Year)
	}
	if b.Extension != "PDF" {
		t.Errorf("Extension = %q", b.Extension)
	}
	if b.DownloadURL != mirror+"/dl/aZ6zON1aZ4" {
		t.Errorf("DownloadURL = %q", b.DownloadURL)
	}
	if b.Categories != "Biology" {
		t.Errorf("Categories = %q", b.Categories)
	}
}

const testDetailFallbackDownloadHTML = `<html><body>
<z-cover id="12047606" title="Biology Today">
  <img class="image" src="https://covers.z-library.sk/full.jpg">
</z-cover>
<a class="btn btn-primary" href="/dl/abc123">Download now</a>
</body></html>`

func TestParseBookDetailFallbackDownloadLink(t *testing.T) {
	mirror := "https://zh.z-library.sk"
	b, err := parseBookDetail(testDetailFallbackDownloadHTML, mirror)
	if err != nil {
		t.Fatalf("parseBookDetail() error = %v", err)
	}
	if b.DownloadURL != mirror+"/dl/abc123" {
		t.Errorf("DownloadURL = %q", b.DownloadURL)
	}
}

const testDownloadHistoryHTML = `<html><body>
<div class="dstats-content">
  <table>
    <tr class="dstats-row">
      <td><a href="/book/2RAqApzDRL/biology.html"><span class="book-title">Biology Today</span></a></td>
      <td><a href="/dl/history1">Download</a></td>
      <td>PDF</td>
      <td>25.19 MB</td>
      <td class="lg-w-120">2026-03-30</td>
    </tr>
    <tr class="dstats-row">
      <td><a href="/book/XYZABC/cell.html"><span class="book-title">Cell Biology</span></a></td>
      <td><a href="/dl/history2">Download</a></td>
      <td>EPUB</td>
      <td>10.5 MB</td>
      <td class="lg-w-120">2026-03-31</td>
    </tr>
  </table>
</div>
</body></html>`

func TestParseDownloadHistory(t *testing.T) {
	mirror := "https://zh.z-library.sk"
	items, _, err := parseDownloadHistory(testDownloadHistoryHTML, mirror)
	if err != nil {
		t.Fatalf("parseDownloadHistory() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Name != "Biology Today" {
		t.Errorf("Name = %q, want %q", items[0].Name, "Biology Today")
	}
	if items[0].Extension != "PDF" {
		t.Errorf("Extension = %q, want %q", items[0].Extension, "PDF")
	}
	if items[0].Size != "25.19 MB" {
		t.Errorf("Size = %q, want %q", items[0].Size, "25.19 MB")
	}
	if items[0].Date != "2026-03-30" {
		t.Errorf("Date = %q, want %q", items[0].Date, "2026-03-30")
	}
	if items[0].URL != mirror+"/book/2RAqApzDRL/biology.html" {
		t.Errorf("URL = %q", items[0].URL)
	}
	if items[0].DownloadURL != mirror+"/dl/history1" {
		t.Errorf("DownloadURL = %q", items[0].DownloadURL)
	}
}

const testDownloadHistoryFallbackHTML = `<html><body>
<table class="downloads-table">
  <tbody>
    <tr>
      <td><a href="/book/ABC123/example.html">Example Book</a></td>
      <td><a href="/file/direct-download">Get</a></td>
      <td>PDF</td>
      <td>15.2 MB</td>
      <td>2026-04-01 10:00</td>
    </tr>
  </tbody>
</table>
</body></html>`

func TestParseDownloadHistoryFallback(t *testing.T) {
	mirror := "https://zh.z-library.sk"
	items, _, err := parseDownloadHistory(testDownloadHistoryFallbackHTML, mirror)
	if err != nil {
		t.Fatalf("parseDownloadHistory() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "Example Book" {
		t.Errorf("Name = %q, want %q", items[0].Name, "Example Book")
	}
	if items[0].Extension != "PDF" {
		t.Errorf("Extension = %q, want %q", items[0].Extension, "PDF")
	}
	if items[0].Size != "15.2 MB" {
		t.Errorf("Size = %q, want %q", items[0].Size, "15.2 MB")
	}
	if items[0].Date != "2026-04-01 10:00" {
		t.Errorf("Date = %q, want %q", items[0].Date, "2026-04-01 10:00")
	}
	if items[0].URL != mirror+"/book/ABC123/example.html" {
		t.Errorf("URL = %q", items[0].URL)
	}
	if items[0].DownloadURL != mirror+"/file/direct-download" {
		t.Errorf("DownloadURL = %q", items[0].DownloadURL)
	}
}

const testDownloadHistoryIgnoresNonDateHTML = `<html><body>
<table>
  <tbody>
    <tr>
      <td><a href="/book/ABC123/example.html">Example Book</a></td>
      <td>PDF 2024</td>
      <td>15.2 MB</td>
      <td>2026-04-01 10:00</td>
    </tr>
  </tbody>
</table>
</body></html>`

func TestParseDownloadHistoryIgnoresNonDateColumns(t *testing.T) {
	items, _, err := parseDownloadHistory(testDownloadHistoryIgnoresNonDateHTML, "https://zh.z-library.sk")
	if err != nil {
		t.Fatalf("parseDownloadHistory() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Extension != "PDF" {
		t.Fatalf("Extension = %q, want %q", items[0].Extension, "PDF")
	}
	if items[0].Size != "15.2 MB" {
		t.Fatalf("Size = %q, want %q", items[0].Size, "15.2 MB")
	}
	if items[0].Date != "2026-04-01 10:00" {
		t.Fatalf("Date = %q, want %q", items[0].Date, "2026-04-01 10:00")
	}
}

const testDownloadHistoryAttributeFallbackHTML = `<html><body>
<table>
  <tbody>
    <tr>
      <td><a href="/book/DEF456/attr.html">Attr Book</a></td>
      <td><a href="/dl/attr" title="EPUB, 12,5 MB">Download</a></td>
      <td>2026-04-02 09:30</td>
    </tr>
  </tbody>
</table>
</body></html>`

func TestParseDownloadHistoryAttributeFallback(t *testing.T) {
	items, _, err := parseDownloadHistory(testDownloadHistoryAttributeFallbackHTML, "https://zh.z-library.sk")
	if err != nil {
		t.Fatalf("parseDownloadHistory() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Extension != "EPUB" {
		t.Fatalf("Extension = %q, want %q", items[0].Extension, "EPUB")
	}
	if items[0].Size != "12,5 MB" {
		t.Fatalf("Size = %q, want %q", items[0].Size, "12,5 MB")
	}
}
