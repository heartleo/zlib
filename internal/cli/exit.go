package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/heartleo/zlib/internal/weread"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

// Process exit codes. Scripts and agents branch on these, so they are part of
// the CLI contract (docs/usage.md#exit-codes): never renumber one.
const (
	exitOK        = 0
	exitError     = 1   // any failure without a more specific code
	exitUsage     = 2   // unknown command, bad flag or wrong argument count
	exitAuth      = 3   // not logged in, or the saved session was rejected
	exitNetwork   = 4   // mirror unreachable, blocked, or no usable domain
	exitPartial   = 5   // the book was saved, but Send to Kindle or WeRead failed
	exitCancelled = 130 // the user cancelled the requested action (128+SIGINT)
)

var (
	// errNotLoggedIn means no saved session exists. Each surface words the
	// remedy itself: the CLI names `zlib login`, the shell names /login.
	errNotLoggedIn = errors.New("not logged in")
	// errCancelled reports that the user stopped an action the command was
	// asked to perform, such as a running download.
	errCancelled = errors.New("cancelled")
)

// usageError marks command-line mistakes so they exit with exitUsage.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Delivery targets named in deliveryError.
const (
	targetKindle = "Kindle"
	targetWeRead = "WeRead"
)

// deliveryError reports a download that reached disk but could not be
// delivered to Kindle or WeRead, so callers can tell it from a failed
// download.
type deliveryError struct {
	target string
	path   string
	err    error
}

func (e *deliveryError) Error() string {
	return fmt.Sprintf("book saved to %s, but Send to %s failed: %v", e.path, e.target, e.err)
}

func (e *deliveryError) Unwrap() error { return e.err }

// exitCode maps a command error to the documented process exit code.
func exitCode(err error) int {
	var usage *usageError
	var delivery *deliveryError
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errCancelled):
		return exitCancelled
	case errors.As(err, &usage):
		return exitUsage
	// Checked before the network codes: an SMTP timeout after a saved
	// download is still a partial success.
	case errors.As(err, &delivery):
		return exitPartial
	case errors.Is(err, errNotLoggedIn), errors.Is(err, zlib.ErrNotLoggedIn), errors.Is(err, zlib.ErrSessionExpired),
		errors.Is(err, weread.ErrNotLoggedIn), errors.Is(err, weread.ErrSessionExpired):
		return exitAuth
	case errors.Is(err, zlib.ErrBotProtection), errors.Is(err, zlib.ErrNoDomain), errors.Is(err, zlib.ErrRedirectLoop), errors.Is(err, zlib.ErrMirrorUnverified):
		return exitNetwork
	}
	if _, ok := classifyNetworkError(err); ok && !errors.Is(err, zlib.ErrSMTPSend) {
		return exitNetwork
	}
	return exitError
}

// markUsageErrors tags argument-count and flag errors on every command as
// usage errors. Cobra returns them as plain errors, indistinguishable from a
// failing RunE.
func markUsageErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err}
	})
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if err := validate(c, args); err != nil {
				return &usageError{err}
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		markUsageErrors(child)
	}
}

// isUnknownCommandError reports cobra's error for an unknown subcommand,
// which it raises before any Args validator runs and exposes only as text.
func isUnknownCommandError(err error) bool {
	return strings.HasPrefix(err.Error(), "unknown command ")
}
