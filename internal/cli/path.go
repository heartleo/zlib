package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// expandTilde resolves a leading ~ in path to the user's home directory. A
// shell only expands ~ when it is unquoted, so `--dir "~/books"` reaches us
// verbatim; expanding here makes the quoted and unquoted forms behave the same.
//
// Only a leading ~ followed by a path separator (or a bare ~) is expanded.
// ~user is left untouched: resolving another user's home needs platform
// specific account lookup, and failing on the literal path beats silently
// pointing somewhere else.
func expandTilde(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	if len(path) > 1 && !os.IsPathSeparator(path[1]) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand %q: %w", path, err)
	}
	// filepath.Join cleans the separator, so this covers both "~" and "~/sub".
	return filepath.Join(home, path[1:]), nil
}

// tildePath replaces the home directory prefix in path with ~. It is the
// inverse of expandTilde, used to keep printed paths short.
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// downloadDirEnvVar names the directory downloads land in when --dir is
// omitted. It saves users from repeating the same path on every command, and
// like the other ZLIB_* variables it can live in ~/.config/zlib/.env rather
// than in shell history.
const downloadDirEnvVar = "ZLIB_DOWNLOAD_DIR"

// addDestDirFlag registers the --dir flag on a command that saves files. The
// download, search and history commands share one definition so their help text
// cannot drift apart from what destDirFlag actually resolves.
func addDestDirFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("dir", "d", ".",
		"Destination directory. Falls back to $"+downloadDirEnvVar+" when omitted.")
}

// destDirFlag reads the --dir flag shared by the download, search and history
// commands, with a leading ~ expanded.
//
// Resolution is --dir > ZLIB_DOWNLOAD_DIR > the flag default. The flag is
// checked with Changed rather than by comparing against ".", so an explicit
// `--dir .` still means "here" even when the variable points elsewhere.
func destDirFlag(cmd *cobra.Command) (string, error) {
	dir, _ := cmd.Flags().GetString("dir")
	if !cmd.Flags().Changed("dir") {
		// A blank value is what a half-finished .env line leaves behind, so
		// treat it as unset rather than as a path.
		if env := strings.TrimSpace(os.Getenv(downloadDirEnvVar)); env != "" {
			dir = env
		}
	}
	return expandTilde(dir)
}
