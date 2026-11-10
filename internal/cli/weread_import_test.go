package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/weread"
	"github.com/heartleo/zlib/internal/zlib"
)

func importStep(t *testing.T, m wereadImportModel, msg any) wereadImportModel {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(wereadImportModel)
}

func importScreen(m wereadImportModel) string { return ansi.Strip(m.View().Content) }

func TestWeReadImportPageShowsEachStage(t *testing.T) {
	path := writeTempFile(t, "Dune.epub")
	c := weread.NewClient(weread.WithCredentials(weread.Credentials{VID: "1001", RefreshToken: "r", DeviceID: "d", Name: "Reader"}))
	m := newWeReadImportModel(path, c)
	m = importStep(t, m, wereadProgressMsg{weread.ImportProgress{Stage: weread.StagePrepare, Total: 7}})
	screen := importScreen(m)
	for _, want := range []string{"Send to WeRead", "Dune.epub · 7 B", "Prepare upload", "○ Upload file", "○ Add to shelf", "To Reader (1001)", "esc cancel"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("prepare screen lacks %q:\n%s", want, screen)
		}
	}

	m = importStep(t, m, wereadProgressMsg{weread.ImportProgress{Stage: weread.StageUpload, Sent: 3 << 20, Total: 6 << 20}})
	screen = importScreen(m)
	for _, want := range []string{symbolSuccess + " Prepare upload", "3.0 MiB / 6.0 MiB", "○ Add to shelf"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("upload screen lacks %q:\n%s", want, screen)
		}
	}

	m = importStep(t, m, wereadProgressMsg{weread.ImportProgress{Stage: weread.StageProcess, Sent: 6 << 20, Total: 6 << 20, Attempt: 3, Attempts: 90}})
	screen = importScreen(m)
	for _, want := range []string{symbolSuccess + " Upload file", "Add to shelf", "check 3 of 90"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("process screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "MiB / ") {
		t.Fatalf("process screen still shows the upload bar:\n%s", screen)
	}
}

func TestWeReadImportCancelWaitsForWeRead(t *testing.T) {
	path := writeTempFile(t, "Dune.epub")
	m := newWeReadImportModel(path, weread.NewClient())
	m = importStep(t, m, wereadProgressMsg{weread.ImportProgress{Stage: weread.StageProcess, Attempt: 2, Attempts: 90}})

	next, cmd := m.Update(key("esc"))
	m = next.(wereadImportModel)
	if cmd != nil || !m.cancelling || m.ctx.Err() == nil {
		t.Fatalf("esc should cancel and keep the page: cancelling=%v cmd=%v", m.cancelling, cmd != nil)
	}
	if screen := importScreen(m); !strings.Contains(screen, "asking WeRead to drop the upload") || !strings.Contains(screen, "ctrl+c stop waiting") {
		t.Fatalf("cancelling screen:\n%s", screen)
	}

	got := importStep(t, m, errMsg{context.Canceled})
	if !errors.Is(got.err, errCancelled) {
		t.Fatalf("err = %v, want errCancelled once WeRead accepted", got.err)
	}
	unconfirmed := fmt.Errorf("%w: could not cancel", weread.ErrImportUnconfirmed)
	if got := importStep(t, m, errMsg{unconfirmed}); !errors.Is(got.err, weread.ErrImportUnconfirmed) {
		t.Fatalf("err = %v, want the unconfirmed import kept", got.err)
	}

	next, cmd = m.Update(key("ctrl+c"))
	if got := next.(wereadImportModel); cmd == nil || !errors.Is(got.err, weread.ErrImportUnconfirmed) {
		t.Fatalf("second ctrl+c: err = %v, want unconfirmed and quit", got.err)
	}
}

func TestWeReadPlainProgressPrintsStages(t *testing.T) {
	var out bytes.Buffer
	p := wereadPlainProgress{w: &out, bytes: plainProgress{w: &out}, stage: -1}
	for _, ip := range []weread.ImportProgress{
		{Stage: weread.StagePrepare, Total: 10 << 20},
		{Stage: weread.StageUpload, Total: 10 << 20},
		{Stage: weread.StageUpload, Sent: 5 << 20, Total: 10 << 20},
		{Stage: weread.StageProcess, Attempt: 1},
		{Stage: weread.StageProcess, Attempt: 2},
	} {
		p.report(ip)
	}
	want := "  Preparing the upload…\n  Uploading 10.0 MiB…\n  50% (5.0 MB / 10.0 MB)\n  Waiting for WeRead to add the book…\n"
	if out.String() != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestWeReadConfirmChecksFileAndNamesAccount(t *testing.T) {
	isolateConfigHome(t)
	path := writeTempFile(t, "Dune.epub")
	confirm, err := wereadConfirmPage(path)
	if err != nil || confirm.note != "7 B · log in with WeChat first" {
		t.Fatalf("signed out: note = %q, err = %v", confirm.note, err)
	}
	if err := config.SaveWeRead(weread.Credentials{VID: "1001", RefreshToken: "r", DeviceID: "d", Name: "Reader"}); err != nil {
		t.Fatal(err)
	}
	confirm, _ = wereadConfirmPage(path)
	if confirm.note != "7 B · to Reader (1001)" {
		t.Fatalf("signed in: note = %q", confirm.note)
	}
	screen := ansi.Strip(confirm.View().Content)
	if !strings.Contains(screen, "to Reader (1001)") || !strings.Contains(screen, "Import") {
		t.Fatalf("confirmation lacks the account:\n%s", screen)
	}

	big := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(big, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, 21<<20); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	m.dispatch("/weread import " + big)
	if m.page != nil || !strings.Contains(m.status, "the limit for TXT files is 20.0 MiB") {
		t.Fatalf("oversized TXT: status = %q, page = %T", m.status, m.page)
	}
}

func TestShellDownloadWeReadRejectsOversizedFileOnCompletion(t *testing.T) {
	isolateConfigHome(t)
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 21<<20); err != nil {
		t.Fatal(err)
	}
	m := newShellModel()
	list := newBookFeedSelectModel("Recommended", nil)
	list.state = searchStateResults
	list.books = []zlib.Book{{ID: "1", Hash: "abc", Name: "Book"}}
	m.open("list", list)
	m.previous = m.page
	download := newDownloadModel("1:abc", filepath.Dir(path), nil)
	download.state = stateComplete
	download.savedPath = path
	download.bookName = "Book"
	m.open("download", download)

	m, _ = shellStep(m, key("left"))
	m, cmd := shellStep(m, key("enter"))
	m, _ = shellStep(m, cmd())
	if m.kind != "download" {
		t.Fatalf("kind = %q, want the completion page", m.kind)
	}
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, symbolError+" file is too large for WeRead") {
		t.Fatalf("completion page lacks the size error:\n%s", screen)
	}
}

func TestShellWeReadImportResultNamesOutcome(t *testing.T) {
	path := writeTempFile(t, "Dune.epub")
	for _, tt := range []struct {
		existed     bool
		title, note string
	}{
		{false, "Imported to WeRead", "find it on the shelf"},
		{true, "Already in WeRead", "kept the existing copy"},
	} {
		m := newShellModel()
		page := newWeReadImportModel(path, weread.NewClient())
		page.account = "Reader (1001)"
		page.result, page.done = weread.ImportResult{BookID: "CB_1", Name: "Dune.epub", AlreadyExisted: tt.existed}, true
		m.wereadImportDone(page)
		if m.infoTitle != tt.title || !strings.Contains(m.status, tt.note) || !strings.Contains(m.status, "Account\nReader (1001)") {
			t.Fatalf("existed=%v: title %q, body %q", tt.existed, m.infoTitle, m.status)
		}
	}

	m := newShellModel()
	page := newWeReadImportModel(path, weread.NewClient())
	page.err = fmt.Errorf("%w: lost", weread.ErrImportUnconfirmed)
	m.wereadImportDone(page)
	errPage, ok := m.page.(shellErrorModel)
	if !ok || errPage.title != "WeRead import not confirmed" || !strings.Contains(errPage.detail, "Check the WeRead shelf") {
		t.Fatalf("unconfirmed page = %#v", m.page)
	}
}

func TestWeReadImportErrorsHaveNextSteps(t *testing.T) {
	limit := &weread.APIError{Path: "/cos/getcredential", Status: 200, Code: -2999, Message: "limit"}
	for err, want := range map[error]string{
		limit:                          "200 imported books",
		weread.ErrRateLimited:          "Wait a while",
		weread.ErrImportParseFailed:    "convert it to EPUB",
		weread.ErrFileTooLarge:         "Convert a large TXT",
		weread.ErrImportUnconfirmed:    "Check the WeRead shelf",
		weread.ErrVerificationRequired: "Open WeRead on your phone",
	} {
		if msg := shellErrorText(err); !strings.Contains(msg, want) {
			t.Errorf("shellErrorText(%v) = %q, want %q", err, msg, want)
		}
	}
	if msg := shellErrorText(limit); !strings.HasPrefix(msg, "WeRead import limit reached: limit.") {
		t.Errorf("limit message = %q, want the server reason first", msg)
	}
}

func TestWeReadResultJSONReportsExistingBook(t *testing.T) {
	got := newWeReadResult(weread.ImportResult{BookID: "CB_1", AlreadyExisted: true}, nil)
	if !got.Imported || !got.AlreadyExisted || got.Error != "" {
		t.Fatalf("result = %+v", got)
	}
	failed := newWeReadResult(weread.ImportResult{}, weread.ErrImportParseFailed)
	if failed.Imported || failed.Error != weread.ErrImportParseFailed.Error() {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestSettingsWeReadAccountShowsImportRulesWhenTheyFit(t *testing.T) {
	isolateConfigHome(t)
	m, _ := newSettingsModel(t.Context(), testSettingsStore(t), "weread")
	m.openWeReadAccount()
	m.width, m.height = 80, 24
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Import rules", "Formats   EPUB, PDF, MOBI, AZW3, TXT"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("account page lacks %q:\n%s", want, screen)
		}
	}
	for _, gone := range []string{"Size ", "Count ", "20 MiB", "300 MiB", "200 imported"} {
		if strings.Contains(screen, gone) {
			t.Fatalf("account page still shows %q:\n%s", gone, screen)
		}
	}
	m.height = 12
	screen = ansi.Strip(m.View().Content)
	if strings.Contains(screen, "Import rules") || !strings.Contains(screen, wereadActionLogIn) || !strings.Contains(screen, "esc back") {
		t.Fatalf("short account page should keep the action and hints, not the rules:\n%s", screen)
	}
}

func TestShellWeReadExpiredLoginOffersSignInAndResumes(t *testing.T) {
	isolateConfigHome(t)
	path := writeTempFile(t, "Dune.epub")
	raw := fmt.Errorf("refresh: %w", &weread.APIError{Path: "/login", Status: 401, Code: -2013, Message: "raw server text"})
	expired := fmt.Errorf("%w: %w", weread.ErrSessionExpired, raw)

	m := newShellModel()
	m, _ = shellStep(m, tea.WindowSizeMsg{Width: 90, Height: 24})
	page := newWeReadImportModel(path, weread.NewClient())
	page.err = expired
	m.wereadImportDone(page)
	errPage, ok := m.page.(shellErrorModel)
	if !ok || errPage.signInPath != path {
		t.Fatalf("expired import page = %#v", m.page)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"WeRead login expired", "Dune.epub", "sign in again", "esc back"} {
		if !strings.Contains(view, want) {
			t.Errorf("page lacks %q:\n%s", want, view)
		}
	}
	for _, leak := range []string{"401", "-2013", "errCode", "/login", "raw server text"} {
		if strings.Contains(view, leak) {
			t.Errorf("page leaks %q:\n%s", leak, view)
		}
	}

	back, cmd := shellStep(m, key("esc"))
	if cmd == nil {
		t.Fatal("esc did not leave the page")
	}
	back, _ = shellStep(back, cmd())
	if back.kind == "weread-login" || back.wereadPath != "" {
		t.Fatalf("esc started a login: kind %q, path %q", back.kind, back.wereadPath)
	}

	m, cmd = shellStep(m, key("enter"))
	if cmd == nil {
		t.Fatal("enter did not leave the page")
	}
	m, _ = shellStep(m, cmd())
	if _, ok := m.page.(wereadLoginModel); !ok || m.kind != "weread-login" || m.wereadPath != path {
		t.Fatalf("enter did not open the login that resumes the import: %T, kind %q, path %q", m.page, m.kind, m.wereadPath)
	}
}

func TestWeReadExpiredHintPointsToSignIn(t *testing.T) {
	expired := fmt.Errorf("import: %w", weread.ErrSessionExpired)
	for step, want := range map[string]string{wereadLoginCLI: "`zlib weread login`", wereadLoginShell: "/weread login", wereadLoginAgent: "Ask the user"} {
		hint := wereadErrorHint(expired, step)
		if !strings.Contains(hint, want) || !strings.Contains(hint, "no longer accepts the saved login") {
			t.Errorf("hint for %q = %q", step, hint)
		}
	}
}
