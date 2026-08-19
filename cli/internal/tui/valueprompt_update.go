package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// valuePromptSubmitBinding is ValuePrompt's own key binding: Enter submits
// whatever is currently typed. Kept out of keys.go's shared keyMap per that
// file's own doc comment. No separate cancel binding is declared here --
// Esc/backspace is the shell's own global Back key (keys.go), already wired
// by Model.handleBack to decline a pending question (Session.Answer(false))
// before this screen's own key handling ever runs -- see confirm_update.go's
// updateConfirm doc comment for the fuller reasoning, which applies here
// unchanged.
var valuePromptSubmitBinding = key.NewBinding(key.WithKeys("enter"))

// updateValuePrompt routes a key event on the ValuePrompt screen: Enter
// submits the input's current text through Session.AnswerValue, wrapped in
// sessionCmd (cmd.go) since sending an answer may reach the network -- the
// same async pattern updateConfirm's 'y' case and
// activateDocumentSelection (document_update.go) use. Everything else is
// forwarded to the text input unchanged, so bubbles/textinput.Model's own
// editing keys (typing, left/right, and the ctrl+h/ctrl+d/ctrl+w/ctrl+u/
// ctrl+k alternates to its DefaultKeyMap's backspace/delete/word-delete
// bindings) keep working -- mirrors updateSettingsEdit's own forwarding
// (settings_update.go) for the same reason: a literal backspace or esc
// keypress never reaches here, intercepted by the shell's global Back
// binding first.
//
// An empty submission is not special-cased here: whatever the input holds
// -- including "" when nothing was typed -- is sent to Session.AnswerValue
// exactly as is. internal/action.AnswerValue's own guard ("Nothing was
// heard, so nothing was sent", invoke.go) is what turns an empty text into
// InvokeRefused rather than an actual request; duplicating that guard here
// would only risk the two drifting apart, and this screen has no way to
// tell "the user meant to send nothing" apart from "the field is still
// empty" that the action layer does not already have.
func (m Model) updateValuePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, valuePromptSubmitBinding) {
		text := m.valuePrompt.input.Value()
		return m, sessionCmd(func() { m.session.AnswerValue(text) })
	}
	var cmd tea.Cmd
	m.valuePrompt.input, cmd = m.valuePrompt.input.Update(msg)
	return m, cmd
}
