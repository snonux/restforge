package tui

import tea "github.com/charmbracelet/bubbletea"

// updateDetail routes a key event on the Detail screen: everything is
// forwarded to the viewport unchanged, so bubbles/viewport.Model's own
// scrolling keys (up/down/j/k, page up/down, half page up/down) keep
// working -- mirrors updateValuePrompt's own forwarding of everything but
// its one submit binding, for the same reason. There is nothing else on
// this screen to move a cursor over or type into: dismissing it is the
// shell's global Back key (Model.handleBack), which calls
// Session.DismissDetail before this screen's own key handling ever runs --
// see updateConfirm's doc comment for the fuller reasoning, which applies
// here unchanged.
func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.detail.viewport, cmd = m.detail.viewport.Update(msg)
	return m, cmd
}
