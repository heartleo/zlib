package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// stubToolUpdate replaces the package-manager command and the version probe.
func stubToolUpdate(t *testing.T, run func(name string, args []string, out io.Writer) error, version string, ok bool) {
	t.Helper()
	oldRun, oldProbe := runUpdateCommand, installedVersion
	t.Cleanup(func() { runUpdateCommand, installedVersion = oldRun, oldProbe })
	runUpdateCommand = func(_ context.Context, _ io.Reader, out, _ io.Writer, name string, args []string) error {
		return run(name, args, out)
	}
	installedVersion = func(context.Context, updateMethod, string) (string, bool) { return version, ok }
}

// Homebrew can keep the old keg (HOMEBREW_NO_INSTALL_CLEANUP), so the running
// executable may still answer with the old version; the symlink on PATH is the
// one the upgrade moved. Go and winget replace the file in place.
func TestVersionProbeOrder(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method updateMethod
		onPath string
		want   []string
	}{
		{"brew prefers PATH", updateBrew, "/p/bin/zlib", []string{"/p/bin/zlib", "/k/zlib"}},
		{"go prefers the executable", updateGo, "/p/bin/zlib", []string{"/k/zlib", "/p/bin/zlib"}},
		{"winget prefers the executable", updateWinget, "/p/bin/zlib", []string{"/k/zlib", "/p/bin/zlib"}},
		{"brew without zlib on PATH", updateBrew, "", []string{"/k/zlib"}},
		{"same file is probed once", updateBrew, "/k/zlib", []string{"/k/zlib"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionProbeOrder(tt.method, "/k/zlib", tt.onPath); !slices.Equal(got, tt.want) {
				t.Errorf("versionProbeOrder = %q, want %q", got, tt.want)
			}
		})
	}
}

func printingTool(line string) func(string, []string, io.Writer) error {
	return func(_ string, _ []string, out io.Writer) error {
		_, err := io.WriteString(out, line+"\n")
		return err
	}
}

func TestToolUpdateShowsVersionChange(t *testing.T) {
	var gotName string
	var gotArgs []string
	stubToolUpdate(t, func(name string, args []string, out io.Writer) error {
		gotName, gotArgs = name, args
		return printingTool("==> Upgrading heartleo/tap/zlib-cli")(name, args, out)
	}, "v0.0.14", true)

	var out bytes.Buffer
	if err := runToolUpdate(t.Context(), nil, &out, io.Discard, updateBrew, "/x/zlib", "v0.0.13"); err != nil {
		t.Fatal(err)
	}
	if gotName != "brew" || strings.Join(gotArgs, " ") != "upgrade heartleo/tap/zlib-cli" {
		t.Errorf("ran %q %q", gotName, gotArgs)
	}
	text := ansi.Strip(out.String())
	for _, want := range []string{
		"Updating zlib with brew (brew upgrade heartleo/tap/zlib-cli)",
		"==> Upgrading heartleo/tap/zlib-cli",
		"brew finished in ",
		" - zlib v0.0.13",
		" + zlib v0.0.14",
		"Restart zlib",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	// The tool's own output comes before the summary, never after it.
	if strings.Index(text, "==> Upgrading") > strings.Index(text, " - zlib v0.0.13") {
		t.Errorf("tool output printed after the summary:\n%s", text)
	}
}

func TestToolUpdateAlreadyCurrent(t *testing.T) {
	stubToolUpdate(t, printingTool("Warning: heartleo/tap/zlib-cli 0.0.14 already installed"), "v0.0.14", true)

	var out bytes.Buffer
	if err := runToolUpdate(t.Context(), nil, &out, io.Discard, updateBrew, "/x/zlib", "v0.0.14"); err != nil {
		t.Fatal(err)
	}
	text := ansi.Strip(out.String())
	if !strings.Contains(text, "zlib v0.0.14 is up to date") {
		t.Errorf("output lacks the up-to-date line:\n%s", text)
	}
	if strings.Contains(text, " + zlib") || strings.Contains(text, "Restart zlib") {
		t.Errorf("unchanged version reported as an update:\n%s", text)
	}
}

func TestToolUpdateWithoutProbeFallsBackToHint(t *testing.T) {
	stubToolUpdate(t, printingTool(""), "", false)

	var out bytes.Buffer
	if err := runToolUpdate(t.Context(), nil, &out, io.Discard, updateGo, "/x/zlib", "v0.0.13"); err != nil {
		t.Fatal(err)
	}
	text := ansi.Strip(out.String())
	if !strings.Contains(text, "go finished in ") || !strings.Contains(text, "Run `zlib version` to confirm.") {
		t.Errorf("fallback output:\n%s", text)
	}
	if strings.Contains(text, " - zlib") || strings.Contains(text, " + zlib") {
		t.Errorf("version change printed without a probe result:\n%s", text)
	}
}

func TestToolUpdateFailureHasNoSummary(t *testing.T) {
	boom := errors.New("exit status 1")
	stubToolUpdate(t, func(string, []string, io.Writer) error { return boom }, "v0.0.14", true)

	var out bytes.Buffer
	err := runToolUpdate(t.Context(), nil, &out, io.Discard, updateWinget, "/x/zlib", "v0.0.13")
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "winget update failed") {
		t.Fatalf("err = %v", err)
	}
	if text := ansi.Strip(out.String()); strings.Contains(text, "finished") || strings.Contains(text, "zlib v0.0.14") {
		t.Errorf("failed update printed a result:\n%s", text)
	}
}

func TestParseInstalledVersion(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"zlib version v0.0.14\n", "v0.0.14", true},
		{"zlib version v0.0.14-0.20261113-abcdef123456\r\n", "v0.0.14-0.20261113-abcdef123456", true},
		{"zlib version dev\n", "", false},
		{"v0.0.14\n", "", false},
		{"", "", false},
		{"command not found\n", "", false},
	} {
		got, ok := parseInstalledVersion(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseInstalledVersion(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
