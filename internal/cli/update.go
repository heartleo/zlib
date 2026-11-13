package cli

import (
	"context"
	"encoding/json"
	"errors"
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

	// updateBrewLegacy is an install from the heartleo/tap/zlib formula, which
	// was renamed to zlib-cli because it clashed with homebrew/core's zlib.
	updateBrewLegacy updateMethod = "brew-legacy"
)

// legacyBrewError tells a user of the retired formula how to move to zlib-cli.
func legacyBrewError() error {
	return errors.New("this copy of zlib was installed from the retired Homebrew formula heartleo/tap/zlib\n" +
		"(renamed to zlib-cli: it clashed with the zlib library in homebrew/core). Switch with:\n" +
		"  brew uninstall heartleo/tap/zlib\n" +
		"  brew install heartleo/tap/zlib-cli")
}

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
	if method == updateBrewLegacy {
		return legacyBrewError()
	}
	if method != updateRelease {
		name, args := updateCommand(method)
		lipgloss.Fprintf(out, "Updating zlib with %s %s\n", name, colorFaint("("+name+" "+strings.Join(args, " ")+")"))
		start := time.Now()
		process := exec.CommandContext(cmd.Context(), name, args...)
		process.Stdout = out
		process.Stderr = cmd.ErrOrStderr()
		process.Stdin = cmd.InOrStdin()
		if err := process.Run(); err != nil {
			return fmt.Errorf("%s update failed: %w", name, err)
		}
		lipgloss.Fprintf(out, "%s %s finished %s. Run `zlib version` to confirm.\n",
			colorGreen(symbolSuccess), name, colorFaint("in "+formatElapsed(time.Since(start))))
		return nil
	}

	currentVersion := zlib.Version
	if !semver.IsValid(currentVersion) {
		return fmt.Errorf("cannot update a development build (%s); install a release or update the source checkout", zlib.Version)
	}
	return runReleaseUpdate(cmd.Context(), cmd.InOrStdin(), out, isInteractive(), newReleaseUpdater(), executable, currentVersion)
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
	// homebrew/core's zlib installs no bin/zlib, so this keg is ours.
	if strings.Contains(path, "/cellar/zlib/") {
		return updateBrewLegacy
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
