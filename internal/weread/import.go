package weread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ImportFormats lists the extensions WeRead accepts for personal books.
var ImportFormats = []string{".epub", ".pdf", ".mobi", ".azw3", ".txt"}

const (
	maxTXTSize  = 20 << 20
	maxBookSize = 300 << 20
)

// MaxImportSize is the largest file WeRead accepts for the format of path:
// 20 MiB for TXT, 300 MiB for every other format.
func MaxImportSize(path string) int64 {
	if strings.EqualFold(filepath.Ext(path), ".txt") {
		return maxTXTSize
	}
	return maxBookSize
}

var (
	// notifyInterval and notifyAttempts bound the wait for WeRead to add an
	// uploaded book: the WeRead app asks every 2 seconds, 90 times. Vars so
	// tests can shorten them.
	notifyInterval = 2 * time.Second
	notifyAttempts = 90
	// cancelTimeout bounds the best-effort request that abandons an import.
	cancelTimeout = 5 * time.Second
)

// Notify status values.
const (
	notifyNone       = -1
	notifyCompleted  = 0
	notifyProcessing = 1
	notifyParseError = 2
	notifyExists     = 3
)

// bookIDRe bounds the book ID WeRead returns before it reaches output.
var bookIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// SupportedFormat reports whether WeRead accepts the file at path.
func SupportedFormat(path string) bool {
	return slices.Contains(ImportFormats, strings.ToLower(filepath.Ext(path)))
}

// ImportResult describes a book WeRead holds for the user.
type ImportResult struct {
	BookID string `json:"book_id"`
	Name   string `json:"name"`
	// AlreadyExisted is true when WeRead already had this file and returned
	// the existing book instead of adding a copy.
	AlreadyExisted bool `json:"already_existed"`
}

// ImportStage is a step of an import.
type ImportStage int

const (
	// StagePrepare requests a temporary upload credential.
	StagePrepare ImportStage = iota
	// StageUpload sends the file to WeRead's storage.
	StageUpload
	// StageProcess waits for WeRead to read the file and add it to the shelf.
	StageProcess
)

// ImportProgress reports where an import is.
type ImportProgress struct {
	Stage ImportStage
	// Sent counts bytes uploaded; Total is the file size in every stage.
	Sent, Total int64
	// Attempt counts the checks made during StageProcess, starting at 1, out
	// of Attempts.
	Attempt, Attempts int
}

// CheckImportFile reports the size of the file at path, or why WeRead would
// refuse it, without any network request.
func CheckImportFile(path string) (int64, error) {
	name := filepath.Base(path)
	if !SupportedFormat(name) {
		return 0, fmt.Errorf("%w: %s (accepted: %s)", ErrUnsupportedFormat, filepath.Ext(name), strings.Join(ImportFormats, ", "))
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return checkImportInfo(name, info)
}

func checkImportInfo(name string, info os.FileInfo) (int64, error) {
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", name)
	}
	size := info.Size()
	if size == 0 {
		return 0, fmt.Errorf("%s is empty", name)
	}
	if limit := MaxImportSize(name); size > limit {
		return 0, fmt.Errorf("%w: %s is %s; the limit for %s files is %s",
			ErrFileTooLarge, name, formatSize(size), strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), ".")), formatSize(limit))
	}
	return size, nil
}

// ImportBook uploads the file at path to the user's WeRead shelf. progress,
// if set, receives each stage and the upload's byte count.
//
// The import runs in three steps: WeRead issues a temporary Tencent COS
// credential, the file is uploaded to COS, and WeRead is asked to add it
// until it reports a result. Cancelling ctx after the upload asks WeRead to
// abandon the import; if that request fails the error wraps
// ErrImportUnconfirmed, as it does when WeRead's answer is lost: the book may
// be on the shelf already.
func (c *Client) ImportBook(ctx context.Context, path string, progress func(ImportProgress)) (ImportResult, error) {
	report := func(p ImportProgress) {
		if progress != nil {
			progress(p)
		}
	}
	name := filepath.Base(path)
	if !SupportedFormat(name) {
		return ImportResult{}, fmt.Errorf("%w: %s (accepted: %s)", ErrUnsupportedFormat, filepath.Ext(name), strings.Join(ImportFormats, ", "))
	}
	f, err := os.Open(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ImportResult{}, err
	}
	size, err := checkImportInfo(name, info)
	if err != nil {
		return ImportResult{}, err
	}

	report(ImportProgress{Stage: StagePrepare, Total: size})
	cred, err := c.uploadCredential(ctx, name)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ImportResult{}, ctxErr
		}
		return ImportResult{}, err
	}

	report(ImportProgress{Stage: StageUpload, Total: size})
	body := &progressReader{r: f, report: func(sent int64) {
		report(ImportProgress{Stage: StageUpload, Sent: sent, Total: size})
	}}
	if err := c.putCOS(ctx, cred, body, size); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ImportResult{}, ctxErr
		}
		return ImportResult{}, err
	}
	if err := ctx.Err(); err != nil {
		// Nothing was notified, so WeRead has not started the import.
		return ImportResult{}, err
	}

	for attempt := 1; ; attempt++ {
		report(ImportProgress{Stage: StageProcess, Sent: size, Total: size, Attempt: attempt, Attempts: notifyAttempts})
		status, bookID, err := c.notify(ctx, name, cred.objectName, false)
		if err != nil {
			if ctx.Err() != nil {
				return ImportResult{}, c.abandon(name, cred.objectName, ctx.Err())
			}
			// Only a rejection of the first notify proves nothing was added.
			// Later failures leave the previously started import unconfirmed.
			var apiErr *APIError
			if attempt == 1 && (errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError ||
				errors.Is(err, ErrVerificationRequired) || errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrNotLoggedIn)) {
				return ImportResult{}, err
			}
			return ImportResult{}, fmt.Errorf("%w: %w", ErrImportUnconfirmed, err)
		}
		switch status {
		case notifyCompleted, notifyExists:
			if !bookIDRe.MatchString(bookID) {
				return ImportResult{}, fmt.Errorf("%w: no book ID in the reply", ErrImportUnconfirmed)
			}
			return ImportResult{BookID: bookID, Name: name, AlreadyExisted: status == notifyExists}, nil
		case notifyParseError:
			return ImportResult{}, ErrImportParseFailed
		case notifyProcessing, notifyNone:
		default:
			err := fmt.Errorf("WeRead rejected the import (status %d)", status)
			if attempt > 1 {
				err = fmt.Errorf("%w: %w", ErrImportUnconfirmed, err)
			}
			return ImportResult{}, err
		}
		if attempt >= notifyAttempts {
			return ImportResult{}, fmt.Errorf("%w: WeRead was still processing the book", ErrImportUnconfirmed)
		}
		t := time.NewTimer(notifyInterval)
		select {
		case <-ctx.Done():
			t.Stop()
			return ImportResult{}, c.abandon(name, cred.objectName, ctx.Err())
		case <-t.C:
		}
	}
}

// uploadCredential holds what the upload and notify steps need.
type uploadCredential struct {
	bucket, key, objectName string
	creds                   cosCredentials
	expires                 int64
}

func (c *Client) uploadCredential(ctx context.Context, name string) (uploadCredential, error) {
	var reply struct {
		Bucket     string `json:"bucket"`
		ObjectName string `json:"ObjectName"`
		Response   struct {
			Credentials cosCredentials `json:"Credentials"`
			ExpiredTime int64          `json:"ExpiredTime"`
		} `json:"Response"`
	}
	query := url.Values{"name": {strings.TrimSuffix(name, filepath.Ext(name))}, "from": {""}}
	if err := c.call(ctx, http.MethodGet, "/cos/getcredential", query, nil, &reply); err != nil {
		return uploadCredential{}, err
	}
	key := strings.TrimPrefix(reply.ObjectName, "/")
	tmp := reply.Response.Credentials
	if !cosBucketRe.MatchString(reply.Bucket) || key == "" || strings.Contains(key, "..") ||
		tmp.TmpSecretID == "" || tmp.TmpSecretKey == "" || tmp.Token == "" {
		return uploadCredential{}, errors.New("WeRead upload credential response is incomplete")
	}
	return uploadCredential{bucket: reply.Bucket, key: key, objectName: reply.ObjectName, creds: tmp, expires: reply.Response.ExpiredTime}, nil
}

// notify asks WeRead to add, or with cancel to abandon, an uploaded object.
// A reply without a status reports notifyNone.
func (c *Client) notify(ctx context.Context, name, objectName string, cancel bool) (int, string, error) {
	flag := "0"
	if cancel {
		flag = "1"
	}
	var reply struct {
		Status *int   `json:"status"`
		BookID string `json:"bookId"`
	}
	query := url.Values{"name": {name}, "path": {objectName}, "cancel": {flag}}
	if err := c.call(ctx, http.MethodPost, "/cos/notify", query, map[string]any{}, &reply); err != nil {
		return 0, "", err
	}
	status := notifyNone
	if reply.Status != nil {
		status = *reply.Status
	}
	return status, reply.BookID, nil
}

// abandon asks WeRead to drop an import the user cancelled. It returns cause
// when WeRead accepted, and an ErrImportUnconfirmed error when it could not
// be told: the import may still finish.
func (c *Client) abandon(name, objectName string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	if _, _, err := c.notify(ctx, name, objectName, true); err != nil {
		return fmt.Errorf("%w: could not cancel the import: %w", ErrImportUnconfirmed, err)
	}
	return cause
}

type progressReader struct {
	r      io.Reader
	sent   int64
	report func(sent int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		p.report(p.sent)
	}
	return n, err
}

// formatSize formats n with binary units.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n)/unit, "KiB"
	for _, s := range []string{"MiB", "GiB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, s
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}
