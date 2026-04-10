package zlib

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultDomain = "https://z-lib.sk"
	TorDomain     = "http://bookszlibb74ugqojhzhg2a63w5i2atv5bqarulgczawnbmsb6s6qead.onion"
	EnvDomain     = "ZLIB_DOMAIN"
	EnvProxy      = "ZLIB_PROXY"

	loginRPCPath        = "/rpc.php"
	searchPathFormat    = "/s/%s?"
	fullTextPathFormat  = "/fulltext/%s?type=%s"
	bookPathFormat      = "/book/%s"
	bookPathPrefix      = "/book/"
	userDownloadsPath   = "/users/downloads"
	historyPathFormat   = "/users/dstats.php?date_from=&date_to=&page=%d"
	downloadsPageFormat = "/users/downloads?page=%d"
	downloadPathPrefix  = "/dl/"
	filePathPrefix      = "/file/"
)

var defaultDomainValue = DefaultDomain

func init() {
	if domain := strings.TrimSpace(os.Getenv(EnvDomain)); domain != "" {
		defaultDomainValue = normalizeDomain(domain)
	}
}

func CurrentDefaultDomain() string {
	return defaultDomainValue
}

func SetDefaultDomain(domain string) {
	domain = normalizeDomain(domain)
	if domain == "" {
		defaultDomainValue = DefaultDomain
		return
	}
	defaultDomainValue = domain
}

func normalizeDomain(domain string) string {
	return strings.TrimRight(strings.TrimSpace(domain), "/")
}

func buildLoginURL(domain string) string {
	return domain + loginRPCPath
}

func BuildSearchURL(domain, query string) string {
	return fmt.Sprintf("%s"+searchPathFormat, domain, url.PathEscape(query))
}

func BuildFullTextSearchURL(domain, query, searchType string) string {
	return fmt.Sprintf("%s"+fullTextPathFormat, domain, url.PathEscape(query), searchType)
}

func BuildBookURL(domain, bookID string) string {
	return fmt.Sprintf("%s"+bookPathFormat, domain, bookID)
}

func BuildHistoryURL(domain string, page int) string {
	return fmt.Sprintf("%s"+historyPathFormat, domain, page)
}

func BuildDownloadsURL(domain string) string {
	return domain + userDownloadsPath
}

func BuildDownloadsPageURL(domain string, page int) string {
	return fmt.Sprintf("%s"+downloadsPageFormat, domain, page)
}

func absolutizeURL(base, href string) string {
	if href == "" {
		return ""
	}
	if isAbsoluteURL(href) {
		return href
	}
	return base + href
}

func isAbsoluteURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")
}
