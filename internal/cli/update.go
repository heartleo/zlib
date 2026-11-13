package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

const goInstallPackage = "github.com/heartleo/zlib/cmd/zlib@latest"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update zlib to the latest release",
	Long:  "Update zlib through the channel that installed it: Homebrew, winget, go install, or a release download.",
	Args:  cobra.NoArgs,
	RunE:  runUpdate,
}

type updateMethod string

const (
	updateRelease updateMethod = "release"
	updateBrew    updateMethod = "brew"
	updateWinget  updateMethod = "winget"
	updateGo      updateMethod = "go"
)

func runUpdate(cmd *cobra.Command, _ []string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find running executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve running executable: %w", err)
	}

	out := cmd.OutOrStdout()
	method := detectUpdateMethod(cmd.Context(), executable)
	if method != updateRelease {
		return runToolUpdate(cmd.Context(), cmd.InOrStdin(), out, cmd.ErrOrStderr(), method, executable, zlib.Version)
	}

	currentVersion := zlib.Version
	if !semver.IsValid(currentVersion) {
		return fmt.Errorf("cannot update a development build (%s); install a release or update the source checkout", zlib.Version)
	}
	return runReleaseUpdate(cmd.Context(), cmd.InOrStdin(), out, isInteractive(), newReleaseUpdater(), executable, currentVersion)
}

// runUpdateCommand runs a package manager with the terminal attached, so its
// own progress bars and prompts keep working. A variable so tests can stub it.
var runUpdateCommand = func(ctx context.Context, in io.Reader, out, errOut io.Writer, name string, args []string) error {
	process := exec.CommandContext(ctx, name, args...)
	process.Stdin, process.Stdout, process.Stderr = in, out, errOut
	return process.Run()
}

// runToolUpdate updates through a package manager, passes its output through,
// then reports the version change the way the release update does. A package
// manager exits 0 when the version is already current, so the finished
// command alone does not say whether anything changed.
func runToolUpdate(ctx context.Context, in io.Reader, out, errOut io.Writer, method updateMethod, executable, current string) error {
	name, args := updateCommand(method)
	lipgloss.Fprintf(out, "Updating zlib with %s %s\n", name, colorFaint("("+name+" "+strings.Join(args, " ")+")"))
	start := time.Now()
	if err := runUpdateCommand(ctx, in, out, errOut, name, args); err != nil {
		return fmt.Errorf("%s update failed: %w", name, err)
	}
	lipgloss.Fprintf(out, "%s %s finished %s\n", colorGreen(symbolSuccess), name, colorFaint("in "+formatElapsed(time.Since(start))))

	after, ok := installedVersion(ctx, executable)
	if !ok {
		lipgloss.Fprintln(out, colorFaint("Run `zlib version` to confirm."))
		return nil
	}
	printUpdateSummary(out, current, updateResult{version: after, updated: after != current}, semver.Compare(current, after) > 0)
	return nil
}

// installedVersion asks the installed zlib for its version after an update.
// Homebrew removes the old keg, so the running executable may be gone; the
// zlib on PATH is the next candidate. A variable so tests can stub it.
var installedVersion = func(ctx context.Context, executable string) (string, bool) {
	candidates := []string{executable}
	if found, err := exec.LookPath("zlib"); err == nil {
		candidates = append(candidates, found)
	}
	for _, candidate := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		output, err := exec.CommandContext(probeCtx, candidate, "version").Output()
		cancel()
		if err != nil {
			continue
		}
		if version, ok := parseInstalledVersion(string(output)); ok {
			return version, true
		}
	}
	return "", false
}

// parseInstalledVersion reads "zlib version v0.0.14" and returns the version.
func parseInstalledVersion(output string) (string, bool) {
	fields := strings.Fields(output)
	if len(fields) < 3 || fields[0] != "zlib" || fields[1] != "version" || !semver.IsValid(fields[2]) {
		return "", false
	}
	return fields[2], true
}

// runReleaseUpdate replaces executable with the latest release asset, showing
// live progress on a terminal and one line per step elsewhere.
func runReleaseUpdate(ctx context.Context, in io.Reader, out io.Writer, interactive bool, u releaseUpdater, executable, current string) error {
	var result updateResult
	var err error
	if interactive {
		result, err = runInteractiveUpdate(ctx, in, out, u, executable, current, runtime.GOOS, runtime.GOARCH)
	} else {
		// Without the TUI, Ctrl+C would kill the process before the updater
		// removes its temporary archive and staged binary.
		signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
		result, err = runPlainUpdate(signalCtx, out, u, executable, current, runtime.GOOS, runtime.GOARCH)
	}
	if err != nil {
		return err
	}
	printUpdateSummary(out, current, result, semver.Compare(current, result.version) > 0)
	return nil
}

func updateCommand(method updateMethod) (string, []string) {
	switch method {
	case updateBrew:
		return "brew", []string{"upgrade", "heartleo/tap/zlib-cli"}
	case updateWinget:
		return "winget", []string{"upgrade", "--id", "heartleo.zlib", "--exact"}
	case updateGo:
		return "go", []string{"install", goInstallPackage}
	default:
		return "", nil
	}
}

func detectUpdateMethod(ctx context.Context, executable string) updateMethod {
	path := strings.ToLower(strings.ReplaceAll(executable, `\`, "/"))
	if strings.Contains(path, "/cellar/zlib-cli/") {
		return updateBrew
	}
	if strings.Contains(path, "/microsoft/winget/packages/heartleo.zlib_") {
		return updateWinget
	}
	if isModuleBuild(debug.ReadBuildInfo()) && goBinPath(ctx, executable) {
		return updateGo
	}
	return updateRelease
}

// isModuleBuild reports whether the binary is the zlib command built from a
// versioned module, as go install produces. info.Path is the main package;
// info.Main.Path is the module path and never names cmd/zlib.
func isModuleBuild(info *debug.BuildInfo, ok bool) bool {
	return ok && info.Path == "github.com/heartleo/zlib/cmd/zlib" && semver.IsValid(info.Main.Version)
}

func goBinPath(ctx context.Context, executable string) bool {
	output, err := exec.CommandContext(ctx, "go", "env", "-json", "GOBIN", "GOPATH").Output()
	if err != nil {
		return false
	}
	var env struct{ GOBIN, GOPATH string }
	if err := json.Unmarshal(output, &env); err != nil {
		return false
	}
	bin := env.GOBIN
	if bin == "" {
		paths := filepath.SplitList(env.GOPATH)
		if len(paths) == 0 {
			return false
		}
		bin = filepath.Join(paths[0], "bin")
	}
	return sameExecutable(filepath.Join(bin, filepath.Base(executable)), executable)
}

func sameExecutable(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
