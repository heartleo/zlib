package cli

import (
	"os"

	"github.com/mattn/go-isatty"
)

// isInteractive reports whether both stdin and stdout are attached to a
// terminal. The bubbletea progress/spinner UIs need a real terminal on both
// ends; when either is a pipe, redirect, or /dev/null — scripts, CI, or another
// program driving the CLI — the UI renders nothing and, worse, never exits.
// Callers use this to fall back to a plain, non-interactive path instead.
func isInteractive() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

// isTerminal reports whether f is a terminal, including MinTTY/Cygwin
// terminals on Windows (where IsTerminal alone returns false).
func isTerminal(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
