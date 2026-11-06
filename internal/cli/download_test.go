package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

// captureOutput swaps os.Stdout and os.Stderr for files while fn runs.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		outFile.Close()
		errFile.Close()
	}()
	fn()
	out, _ := os.ReadFile(outFile.Name())
	errOut, _ := os.ReadFile(errFile.Name())
	return string(out), string(errOut)
}

func newDownloadTestServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Dune.epub"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func downloadTestCommand(t *testing.T, jsonOut bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addDestDirFlag(cmd)
	cmd.Flags().Bool("json", false, "")
	if jsonOut {
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func TestDownloadJSONPrintsOnlyTheResultOnStdout(t *testing.T) {
	isolateConfigHome(t)
	body := bytes.Repeat([]byte("x"), 4096)
	server := newDownloadTestServer(t, body)
	client := zlib.NewClient(zlib.WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})
	dir := t.TempDir()
	// A relative --dir must still be reported as an absolute path.
	t.Chdir(dir)

	var runErr error
	stdout, stderr := captureOutput(t, func() {
		m := newDirectDownloadModel("Dune", server.URL+"/files/dune", ".", client)
		runErr = runDownload(m, deliveries{}, downloadTestCommand(t, true))
	})
	if runErr != nil {
		t.Fatalf("runDownload error = %v", runErr)
	}

	var result downloadResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	want, _ := filepath.Abs(filepath.Join(dir, "Dune.epub"))
	if result.Path != want || result.Size != int64(len(body)) || result.Name != "Dune" || result.Kindle != nil {
		t.Fatalf("result = %+v, want path %s", result, want)
	}
	if !strings.Contains(stderr, "Downloading: Dune") || !strings.Contains(stderr, "100%") {
		t.Fatalf("progress missing from stderr: %q", stderr)
	}
}

// history --download fetches from a known URL; its JSON must still carry the
// id:hash reference like every other download.
func TestHistoryDirectDownloadJSONKeepsReference(t *testing.T) {
	isolateConfigHome(t)
	server := newDownloadTestServer(t, []byte("book"))
	client := zlib.NewClient(zlib.WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})
	item := zlib.DownloadHistoryItem{ID: "7", Hash: "h1", Name: "Dune", DownloadURL: server.URL + "/files/dune"}

	var runErr error
	stdout, _ := captureOutput(t, func() {
		runErr = runDownload(historyDirectDownload(item, item.Name, t.TempDir(), client), deliveries{}, downloadTestCommand(t, true))
	})
	var result downloadResult
	if runErr != nil || json.Unmarshal([]byte(stdout), &result) != nil || result.ID != "7:h1" {
		t.Fatalf("error = %v, stdout = %q", runErr, stdout)
	}
}

func TestDownloadReportsKindleFailureAfterSavingAsPartial(t *testing.T) {
	isolateConfigHome(t)
	server := newDownloadTestServer(t, []byte("book"))
	client := zlib.NewClient(zlib.WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})
	dir := t.TempDir()

	var runErr error
	stdout, _ := captureOutput(t, func() {
		m := newDirectDownloadModel("Dune", server.URL+"/files/dune", dir, client)
		runErr = runDownload(m, deliveries{kindle: true}, downloadTestCommand(t, true))
	})
	var delivery *deliveryError
	if !errors.As(runErr, &delivery) || exitCode(runErr) != exitPartial {
		t.Fatalf("error = %v, want a kindle delivery error", runErr)
	}
	var result downloadResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout = %q: %v", stdout, err)
	}
	if result.Kindle == nil || result.Kindle.Sent || !strings.Contains(result.Kindle.Error, "kindle config not found") {
		t.Fatalf("kindle = %+v", result.Kindle)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
}

func TestPlainDownloadTextKeepsProgressOffStdout(t *testing.T) {
	isolateConfigHome(t)
	server := newDownloadTestServer(t, []byte("book"))
	client := zlib.NewClient(zlib.WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})
	dir := t.TempDir()

	var runErr error
	stdout, stderr := captureOutput(t, func() {
		m := newDirectDownloadModel("Dune", server.URL+"/files/dune", dir, client)
		runErr = runDownload(m, deliveries{}, downloadTestCommand(t, false))
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.Contains(stdout, "Downloading") || !strings.Contains(stdout, "Saved to: "+filepath.Join(dir, "Dune.epub")) {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "Downloading: Dune") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPlainDownloadStripsControlSequencesFromTitle(t *testing.T) {
	isolateConfigHome(t)
	server := newDownloadTestServer(t, []byte("book"))
	client := zlib.NewClient(zlib.WithDomain(server.URL))
	client.SetCookies(map[string]string{"remix_userid": "1", "remix_userkey": "key"})

	_, stderr := captureOutput(t, func() {
		m := newDirectDownloadModel("Dune\x1b]0;owned\x07\x1b[2J", server.URL+"/files/dune", t.TempDir(), client)
		if err := runDownload(m, deliveries{}, downloadTestCommand(t, true)); err != nil {
			t.Error(err)
		}
	})
	if strings.Contains(stderr, "\x1b") || !strings.Contains(stderr, "Downloading: Dune") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPlainProgressReportsEachTenPercentOnce(t *testing.T) {
	var out bytes.Buffer
	p := plainProgress{w: &out}
	for written := int64(0); written <= 1000; written += 7 {
		p.report(written, 1000)
	}
	p.report(1000, 1000)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 10 || !strings.HasPrefix(lines[0], "  10%") || !strings.HasPrefix(lines[9], "  100%") {
		t.Fatalf("progress lines = %q", lines)
	}

	out.Reset()
	p = plainProgress{w: &out}
	for written := int64(0); written <= 12<<20; written += 1 << 20 {
		p.report(written, 0)
	}
	if got := strings.Count(out.String(), "\n"); got != 2 {
		t.Fatalf("unknown-size progress = %q", out.String())
	}
}
