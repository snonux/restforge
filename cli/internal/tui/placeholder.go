// Package tui is the interactive Bubble Tea front end for restforge -- the
// terminal sibling of the Flutter app's UI, driven by internal/session.
//
// This file is a placeholder only: Run builds a minimal Bubble Tea program
// that shows a "not yet implemented" message and waits for the user to
// quit, so the binary's one-binary mode-by-invocation dispatch (see
// internal/cli) can be wired and exercised now, before the real TUI's
// task arrives. The real TUI composes internal/session and adapts its
// live-poll callbacks onto Bubble Tea's single-threaded Update loop (see
// internal/session's package comment on the threading constraint this
// placeholder deliberately does not address).
//
// Run needs a terminal: Bubble Tea opens the controlling TTY for input,
// and `program.Run()` returns an error when there is none (a piped stdin,
// no /dev/tty, or a headless CI run). That error is reported as a general
// failure by the dispatch layer, not worked around here -- a TUI that
// cannot reach a terminal genuinely cannot run.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// notImplementedMsg is the single line the placeholder model renders. Kept
// as a constant rather than inline so the test of the no-subcommand path
// (once the real TUI lands and a pty harness is worth the complexity) has
// an obvious string to assert against.
const notImplementedMsg = "restforge TUI: not yet implemented (press q or Ctrl+C to quit)"

// Run builds and runs the placeholder Bubble Tea program. It returns the
// error Bubble Tea reports when the program could not start or exited
// badly -- most usefully, the "could not open a new TTY" error when there
// is no terminal, which the caller maps to a general-failure exit code.
func Run() error {
	p := tea.NewProgram(placeholderModel{})
	_, err := p.Run()
	return err
}

// placeholderModel is the Bubble Tea model for the placeholder program:
// it renders the not-yet-implemented line and quits on 'q' or Ctrl+C,
// exactly the controls the message tells the user about. It carries no
// state, so it is a value type with value receivers.
type placeholderModel struct{}

// Init has nothing to kick off -- the placeholder just waits for a key.
func (placeholderModel) Init() tea.Cmd { return nil }

// Update quits on 'q' or Ctrl+C and ignores every other key.
func (placeholderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "ctrl+c":
			return placeholderModel{}, tea.Quit
		}
	}
	return placeholderModel{}, nil
}

// View renders the placeholder line.
func (placeholderModel) View() string {
	return fmt.Sprintf("%s\n", notImplementedMsg)
}
