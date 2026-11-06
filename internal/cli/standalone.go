package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Standalone commands share shell pages and transitions, then exit on return.
func newStandaloneShellCommand(command string) (shellModel, error) {
	m := newShellModel()
	m.standalone = true
	m.standaloneTitle = "Kindle"
	if strings.HasPrefix(command, "/weread") {
		m.standaloneTitle = "WeRead"
	}
	m.dispatch(command)
	if m.page == nil && m.kind == "" {
		return m, fmt.Errorf("%s", m.status)
	}
	return m, nil
}

func runStandaloneShellCommand(command string) error {
	if !isInteractive() {
		return fmt.Errorf("this command requires a terminal; use zlib kindle send <file> for non-interactive delivery")
	}
	m, err := newStandaloneShellCommand(command)
	if err != nil {
		return err
	}
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return err
	}
	return result.(shellModel).commandErr
}
