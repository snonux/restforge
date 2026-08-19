package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// confirmYesBinding and confirmNoBinding are Confirm's own two key bindings,
// kept out of keys.go's shared keyMap per that file's own doc comment: a
// screen-specific key is that screen's own to add. Deliberately no Enter
// binding: this task's own "no default and no timeout-to-yes" rule -- the
// interactive-mode half of docs/DESIGN.md's "ask before acting" -- means an
// explicit y/n press is the only way to answer a confirmation, never a bare
// Enter a user could trigger out of habit.
var (
	confirmYesBinding = key.NewBinding(key.WithKeys("y", "Y"))
	confirmNoBinding  = key.NewBinding(key.WithKeys("n", "N"))
)

// updateConfirm routes a key event on the Confirm screen: 'y' answers yes --
// Session.Answer(true), wrapped in sessionCmd (cmd.go) since a confirmed
// action may reach the network (action.Answer's own doc comment) -- and 'n'
// answers no. Esc/backspace already answers no too, one level up: see
// Model.handleBack, which declines a pending question (Session.Answer(false))
// before this screen's own key handling ever runs -- mirrors
// confirmation_sheet.dart's ConfirmationSheetHost cancelling the question on
// every dismissal that is not the sheet's own Confirm button (tap-outside,
// drag, back gesture, or Cancel's plain pop all funnel through the same
// answer(false) call there; Esc/backspace funnels through handleBack's here).
// Everything else is ignored -- there is nothing else on this screen to move
// a cursor over or type into.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, confirmYesBinding):
		return m, sessionCmd(func() { m.session.Answer(true) })
	case key.Matches(msg, confirmNoBinding):
		// Answer(false) never sends anything (action.Answer's own contract:
		// it only clears the pending question), so this is safe to call
		// directly -- the same "no I/O, no sessionCmd" reasoning
		// Model.handleBack's own doc comment gives for DismissDetail and
		// Session.Back.
		m.session.Answer(false)
		return m, nil
	}
	return m, nil
}
