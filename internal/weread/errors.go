package weread

import (
	"errors"
	"fmt"
)

var (
	// ErrNotLoggedIn means no WeRead credentials are saved.
	ErrNotLoggedIn = errors.New("not logged in to WeRead")
	// ErrSessionExpired means WeRead rejected the saved refresh token.
	ErrSessionExpired = errors.New("WeRead session expired or was revoked")
	// ErrVerificationRequired is WeRead's risk-control answer (errCode
	// -2041): only an official client can complete the challenge.
	ErrVerificationRequired = errors.New("WeRead requires verification in its official app")
	// ErrQRExpired means the login QR code expired before it was confirmed.
	ErrQRExpired = errors.New("WeRead login QR code expired")
	// ErrQRDeclined means the user declined the login in WeChat.
	ErrQRDeclined = errors.New("WeRead login was declined in WeChat")
	// ErrUnsupportedFormat means WeRead does not accept the file's format.
	ErrUnsupportedFormat = errors.New("WeRead does not accept this file type")
	// ErrFileTooLarge means the file exceeds MaxImportSize for its format.
	ErrFileTooLarge = errors.New("file is too large for WeRead")
	// ErrImportUnconfirmed means the upload finished but WeRead's answer to
	// the final notify step was lost, so the book may already be on the
	// shelf. Retrying can spend another import.
	ErrImportUnconfirmed = errors.New("WeRead did not confirm the import; check the shelf before retrying")
	// ErrImportParseFailed means WeRead received the file but could not
	// read it as a book.
	ErrImportParseFailed = errors.New("WeRead could not read the file as a book")
	// ErrImportLimit is WeRead's import quota (errCode -2999). The server
	// message, kept in APIError.Message, says which limit applies.
	ErrImportLimit = errors.New("WeRead import limit reached")
	// ErrRateLimited means WeRead is refusing requests for now (errCode -2014).
	ErrRateLimited = errors.New("WeRead is limiting requests; try again later")
)

// WeRead error codes with a fixed meaning.
const (
	errCodeImportLimit = -2999
	errCodeRateLimited = -2014
)

// sessionError is ErrSessionExpired with the response that caused it. It reads
// as the sentinel alone, so a raw status, code and server text never reach a
// user; errors.As still finds the underlying *APIError for diagnosis.
type sessionError struct{ cause error }

func (e *sessionError) Error() string   { return ErrSessionExpired.Error() }
func (e *sessionError) Unwrap() []error { return []error{ErrSessionExpired, e.cause} }

// APIError is a WeRead response that carried an error code or an
// unexpected HTTP status.
type APIError struct {
	Path    string
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	// Codes with a fixed meaning read as their sentinel, followed by WeRead's
	// own wording, which names the limit that applies.
	var known error
	switch e.Code {
	case errCodeImportLimit:
		known = ErrImportLimit
	case errCodeRateLimited:
		known = ErrRateLimited
	}
	if known != nil {
		if e.Message != "" {
			return known.Error() + ": " + e.Message
		}
		return known.Error()
	}
	msg := fmt.Sprintf("WeRead %s failed (HTTP %d", e.Path, e.Status)
	if e.Code != 0 {
		msg += fmt.Sprintf(", errCode %d", e.Code)
	}
	msg += ")"
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Is maps the error codes with a fixed meaning to their sentinels, so
// callers can use errors.Is and still read the server message.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrImportLimit:
		return e.Code == errCodeImportLimit
	case ErrRateLimited:
		return e.Code == errCodeRateLimited
	}
	return false
}
