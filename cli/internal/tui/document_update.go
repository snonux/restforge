package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// documentEnterBinding and documentDismissBinding are the Document screen's
// own two key bindings, kept out of keys.go's shared keyMap per that file's
// own doc comment: a screen-specific key is that screen's own to add, not
// folded into the three bindings every screen shares. Neither carries a
// key.WithHelp -- see documentModel.hintLine (document.go) for where this
// screen's own hint text lives instead.
var (
	documentEnterBinding   = key.NewBinding(key.WithKeys("enter"))
	documentDismissBinding = key.NewBinding(key.WithKeys("d"))
)

// updateDocument routes a key event on the Document screen: Enter activates
// the row under the cursor (activateDocumentSelection), 'd' dismisses the
// failure banner when one is showing (documentModel.dismissedFailure's own
// doc comment explains why this screen needs a dismiss key the Dart
// original does not), and everything else -- arrows, page up/down -- is
// forwarded to the row list unchanged, the same split updateHome makes for
// Home's two lists. Called from Model.handleKey's fallback once the three
// global bindings (quit/back/help) have all missed and the current screen
// is Document.
func (m Model) updateDocument(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, documentDismissBinding) && m.session.Failure() != nil:
		m.document.dismissedFailure = m.session.Failure()
		return m, nil
	case key.Matches(msg, documentEnterBinding):
		return m.activateDocumentSelection()
	}
	var cmd tea.Cmd
	m.document.rows, cmd = m.document.rows.Update(msg)
	return m, cmd
}

// activateDocumentSelection activates the row the cursor is currently on:
// Session.Activate, wrapped in sessionCmd (cmd.go) since it may reach the
// network (render.FetchTarget) -- exactly the async pattern the tui shell
// task established for every Session method that performs I/O. The
// resulting screen transition (push Document again for a FetchTarget/
// EmbeddedTarget, or hand off to Confirm/ValuePrompt for an ActionTarget --
// built in task 631) is not decided here: it falls out of deriveScreen once
// Session's own state has changed, the same way every other sessionCmd
// caller in this package leaves that decision to Update. Nothing happens
// with nothing selected (an empty document).
func (m Model) activateDocumentSelection() (tea.Model, tea.Cmd) {
	item, ok := m.document.rows.SelectedItem().(documentRowItem)
	if !ok {
		return m, nil
	}
	target := item.row.Target
	return m, sessionCmd(func() { m.session.Activate(target) })
}
