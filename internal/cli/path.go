package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// expandTilde resolves a leading ~ in a user-supplied path to the current user's
// home directory. The shell only expands ~ when it is unquoted, so a quoted
// --dir "~/Downloads" arrives verbatim and would otherwise be treated as a
// literal "~" directory. Forms we cannot resolve — ~user/…, or a ~ that is not
// the first segment — are returned untouched.
func expandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(filepath.ToSlash(path), "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}
