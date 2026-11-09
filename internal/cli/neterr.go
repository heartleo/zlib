package cli

import (
	"errors"
	"net/url"
	"strings"

	keys "charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	zlib "github.com/heartleo/zlib/internal/zlib"
)

// Network failures are described and drawn in one place so every screen says the
// same thing and offers the same recovery keys: r retries, d picks another
// domain, esc goes back. Raw transport errors (request URLs, "net/http: …")
// never reach the screen except as one muted, URL-free detail line.

// netFailure is a network error in words a reader can act on.
type netFailure struct {
	host   string // failing host, "" when unknown
	reason string // what went wrong, one sentence
	detail string // short technical cause, URLs removed
}

// describeNetworkError reports whether err is a connection problem, such as a
// timeout, TLS handshake timeout, refused or dropped connection, DNS failure or
// DiamWall block, and words it. Session errors, cancellations and server
// replies are not connection problems and return false.
func describeNetworkError(err error) (netFailure, bool) {
	if err == nil || errors.Is(err, errCancelled) {
		return netFailure{}, false
	}
	bot := errors.Is(err, zlib.ErrBotProtection)
	if _, isNetwork := classifyNetworkError(err); !isNetwork && !bot {
		return netFailure{}, false
	}
	f := netFailure{host: failedHost(err), detail: networkDetail(err)}
	text := strings.ToLower(err.Error())
	switch {
	case bot:
		f.reason = "The domain is behind bot protection (DiamWall) and blocked the request."
	case strings.Contains(text, "tls handshake timeout"):
		f.reason = "The secure connection (TLS handshake) did not finish in time."
	case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"), strings.Contains(text, "timed out"):
		f.reason = "The server took too long to answer."
	case strings.Contains(text, "no such host"), strings.Contains(text, "server misbehaving"):
		f.reason = "The domain name could not be resolved."
	default:
		f.reason = "The connection was refused or dropped."
	}
	return f, true
}

func isNetworkError(err error) bool {
	_, ok := describeNetworkError(err)
	return ok
}

// networkDetail keeps the transport cause and drops the request URL, which can
// carry query strings and signed links.
func networkDetail(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	words := strings.Fields(safeText(err.Error()))
	for i, word := range words {
		if strings.Contains(word, "://") {
			words[i] = "[URL hidden]"
		}
	}
	return strings.Join(words, " ")
}

func (f netFailure) title() string {
	if f.host == "" {
		return "Can't reach the server"
	}
	return "Can't reach " + f.host
}

// summary is the one-line form for status bars and notices.
func (f netFailure) summary(canSwitch bool) string {
	next := "Try again, or run `zlib config domain` to pick another domain."
	if canSwitch {
		next = "Try again, or use /config domain to pick another domain."
	}
	return f.title() + ". " + f.reason + " " + next
}

// networkErrorBody draws the failure: a headline, the cause, what to do next,
// and the technical detail last and muted. canSwitch says whether the host
// screen offers the d key; otherwise the CLI command is named instead.
func networkErrorBody(f netFailure, width int, canSwitch bool) string {
	errStyle := lipgloss.NewStyle().Foreground(currentTheme.Error)
	plain := lipgloss.NewStyle()
	muted := lipgloss.NewStyle().Foreground(currentTheme.Muted)
	next := "Press r to retry, or run `zlib config domain` to pick another domain."
	if canSwitch {
		next = "Press d to choose another domain, or r to retry."
	}
	var lines []string
	lines = append(lines, wrapIndented(symbolError+" "+f.title(), width, "  ", errStyle)...)
	lines = append(lines, "")
	lines = append(lines, wrapIndented(f.reason+" This domain may be blocked or down; another one usually works.", width, "  ", plain)...)
	lines = append(lines, "")
	lines = append(lines, wrapIndented(next, width, "  ", plain)...)
	if f.detail != "" {
		lines = append(lines, "")
		lines = append(lines, wrapIndented("Details: "+f.detail, width, "  ", muted)...)
	}
	return strings.Join(lines, "\n")
}

// loadErrorView is what a page shows when its request failed. It returns the
// body, the subtitle and the key bindings; fallback is the page's own subtitle
// for errors that are not connection problems. Only connection problems offer
// the domain key, and only when canSwitch (the shell can open the domain page).
func loadErrorView(err error, width int, canSwitch bool, fallback string, base ...keys.Binding) (body, subtitle string, bindings []keys.Binding) {
	f, network := describeNetworkError(err)
	if !network {
		style := lipgloss.NewStyle().Foreground(currentTheme.Error)
		return strings.Join(wrapIndented(symbolError+" "+bookCell(err.Error()), width, "  ", style), "\n"), fallback, base
	}
	bindings = base
	if canSwitch {
		bindings = append([]keys.Binding{uiKey("d", "change domain")}, base...)
	}
	return networkErrorBody(f, width, canSwitch), "Connection problem", bindings
}

// domainSwitcher is implemented by pages that can ask the shell to open the
// domain page from a connection error and then reload themselves.
type domainSwitcher interface {
	// domainSwitchRequest returns the shell command that reopens the page.
	domainSwitchRequest() (resume string, ok bool)
}

// offersDomainSwitch is true when err is a connection problem on a page that
// the shell can resume.
func offersDomainSwitch(err error, inShell bool, resume string) bool {
	return inShell && resume != "" && isNetworkError(err)
}
