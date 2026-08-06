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

// destDirFlag reads the --dir flag shared by the download, search and history
// commands, with a leading ~ expanded.
func destDirFlag(cmd *cobra.Command) (string, error) {
	dir, _ := cmd.Flags().GetString("dir")
	return expandTilde(dir)
}
