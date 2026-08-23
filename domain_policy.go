package zlib

import (
	"fmt"
	"net/url"
	"strings"
)

var allowedDomainSuffixes = []string{
	"z-library.gs",
	"1lib.sk",
	"z-lib.fm",
	"z-lib.gd",
	"z-lib.gl",
	"zliba.ru",
	"z-lib.sk",
	"z-library.ec",
}

// AllowedDomainSuffixes returns the only upstream hostname suffixes accepted
// by the CLI. A hostname must equal a suffix or be one of its subdomains.
func AllowedDomainSuffixes() []string {
	return append([]string(nil), allowedDomainSuffixes...)
}

// ParseAllowedDomain validates an HTTPS origin against AllowedDomainSuffixes
// and returns its normalized origin.
func ParseAllowedDomain(raw string) (string, error) {
	target, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return "", fmt.Errorf("domain must be an HTTPS origin")
	}
	if target.User != nil || target.Port() != "" || target.RawQuery != "" || target.Fragment != "" || (target.Path != "" && target.Path != "/") {
		return "", fmt.Errorf("domain must be an HTTPS origin without credentials, port, path, query, or fragment")
	}

	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	for _, suffix := range allowedDomainSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return "https://" + host, nil
		}
	}
	return "", fmt.Errorf("domain %q is not in the allowed Z-Library suffix list", host)
}
