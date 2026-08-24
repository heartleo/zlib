package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/heartleo/zlib"
	"github.com/heartleo/zlib/internal/config"
)

const maxCookieFileLineBytes = 1 << 20

// loadNetscapeCookies reads a Netscape/Mozilla cookie export and returns only
// cookies that are applicable to the target domain. The CLI's session format
// stores authentication cookies at the target root, so path-scoped cookies are
// deliberately ignored rather than broadening their scope.
func loadNetscapeCookies(path, rawDomain string, now time.Time) (map[string]string, error) {
	target, err := parseCookieTarget(rawDomain)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cookie file: %w", err)
	}
	defer f.Close()

	cookies := make(map[string]string)
	headerSeen := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), maxCookieFileLineBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			if strings.Contains(line, "HTTP Cookie File") {
				headerSeen = true
			}
			continue
		}
		if !headerSeen {
			return nil, fmt.Errorf("cookie file is not in Netscape format")
		}

		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 {
			return nil, fmt.Errorf("invalid Netscape cookie at line %d", lineNumber)
		}
		includeSubdomains, err := parseNetscapeBool(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid subdomain flag at line %d: %w", lineNumber, err)
		}
		secure, err := parseNetscapeBool(fields[3])
		if err != nil {
			return nil, fmt.Errorf("invalid secure flag at line %d: %w", lineNumber, err)
		}
		expires, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid expiry at line %d: %w", lineNumber, err)
		}

		if !cookieDomainMatches(target.Hostname(), fields[0], includeSubdomains) {
			continue
		}
		if fields[2] != "" && fields[2] != "/" {
			continue
		}
		if secure && target.Scheme != "https" {
			continue
		}
		if expires > 0 && !time.Unix(expires, 0).After(now) {
			continue
		}
		if fields[5] == "" {
			continue
		}
		cookies[fields[5]] = fields[6]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read cookie file: %w", err)
	}
	if !headerSeen {
		return nil, fmt.Errorf("cookie file is not in Netscape format")
	}
	return cookies, nil
}

func parseCookieTarget(rawDomain string) (*url.URL, error) {
	target, err := url.ParseRequestURI(strings.TrimSpace(rawDomain))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("invalid cookie target domain %q", rawDomain)
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Path != "" && target.Path != "/") {
		return nil, fmt.Errorf("cookie target must be an HTTP origin, got %q", rawDomain)
	}
	return target, nil
}

func parseNetscapeBool(value string) (bool, error) {
	switch strings.ToUpper(value) {
	case "TRUE":
		return true, nil
	case "FALSE":
		return false, nil
	default:
		return false, fmt.Errorf("expected TRUE or FALSE, got %q", value)
	}
}

func cookieDomainMatches(targetHost, cookieDomain string, includeSubdomains bool) bool {
	targetHost = strings.ToLower(strings.TrimSuffix(targetHost, "."))
	cookieDomain = strings.ToLower(strings.Trim(strings.TrimSpace(cookieDomain), "."))
	if targetHost == cookieDomain {
		return true
	}
	return includeSubdomains && strings.HasSuffix(targetHost, "."+cookieDomain)
}

func importCookieSession(cookieFile, domain string, now time.Time, modes ...string) (int, error) {
	path, err := expandTilde(cookieFile)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(domain) == "" {
		return 0, fmt.Errorf("cookie domain is not configured")
	}
	domain, err = zlib.ParseAllowedDomain(domain)
	if err != nil {
		return 0, err
	}

	cookies, err := loadNetscapeCookies(path, domain, now)
	if err != nil {
		return 0, err
	}
	if len(cookies) == 0 {
		return 0, fmt.Errorf("no unexpired root cookies match %s", domain)
	}
	// A file can match the domain and still carry nothing that authenticates.
	// Rejecting it here beats saving a useless session and failing every later
	// command with an opaque "session expired".
	for _, name := range []string{"remix_userid", "remix_userkey"} {
		if strings.TrimSpace(cookies[name]) == "" {
			return 0, fmt.Errorf("cookie file has no %s cookie for %s; export it while logged in", name, domain)
		}
	}

	mode := ""
	if len(modes) > 0 {
		mode = modes[0]
	}
	if err := config.SaveSession(config.Session{Cookies: cookies, Domain: domain, Mode: mode}); err != nil {
		return 0, fmt.Errorf("save imported session: %w", err)
	}
	return len(cookies), nil
}
