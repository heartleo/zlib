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
	EnvRetries    = "ZLIB_DOWNLOAD_RETRIES"

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

// defaultDomainOverride is set only by SetDefaultDomain. Empty means "not
// overridden", which lets CurrentDefaultDomain fall through to the environment.
var defaultDomainOverride string

// CurrentDefaultDomain resolves the domain new clients start from:
// SetDefaultDomain > ZLIB_DOMAIN > DefaultDomain.
//
// ZLIB_DOMAIN is read on every call rather than in an init function: the CLI
// loads .env after all init functions have run, so an init-time read would
// never observe a .env-supplied value.
func CurrentDefaultDomain() string {
	if defaultDomainOverride != "" {
		return defaultDomainOverride
	}
	if domain := normalizeDomain(os.Getenv(EnvDomain)); domain != "" {
		return domain
	}
	return DefaultDomain
}

// SetDefaultDomain overrides the default domain for clients created afterwards.
// Passing an empty string clears the override, restoring ZLIB_DOMAIN-or-default
// resolution.
func SetDefaultDomain(domain string) {
	defaultDomainOverride = normalizeDomain(domain)
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
