// vikeys.go adds vi-style navigation (h/j/k/l) on top of the cursor keys
// every screen already accepts, and centralises the "is this key event
// actually a keystroke belonging to some free-text input, not a navigation
// command" question every one of the checks below needs answered the same
// way. Two independent sources of "this is text entry, not navigation":
//
//   - A screen whose whole body is a text field: ValuePrompt's own input
//     (valueprompt.go) and Settings while a backend's fields are being
//     edited (settingsModeEdit, settings_form.go). See isTextEntryScreen.
//   - Any screen-owned bubbles/list.Model while its own filter input has
//     focus (list.Filtering) -- see currentFilterState. This is also why
//     filtering was left disabled when Home/Document/Settings' lists were
//     first built (see newHomeModel/newDocumentModel/newSettingsModel's own
//     doc comments): Esc is list.Model's own filter-cancel key, and would
//     collide with the shell's global Back binding (keys.go) the same way
//     h/l would collide with typing a literal "h" or "l" into a filter
//     query or a ValuePrompt/Settings field. Model.handleKey defers to
//     list.Model's own filter handling first, in both directions, rather
//     than reimplementing any part of it -- see handleKey's own doc
//     comment.
package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// remapViKey rewrites a vi-style "h" or "l" press into the ordinary key
// event it stands in for -- esc for h (back/cancel, the shell's global Back
// binding), enter for l (select/confirm, whichever binding a screen's own
// Enter already triggers) -- so every existing key.Binding match (m.keys.Back,
// homeEnterBinding, documentEnterBinding, settingsConfirmBinding, ...) fires
// exactly as it would for the key it stands in for, with no per-screen
// duplication of what "back" or "select" means on that screen.
//
// j/k need no such remap: bubbles/list.Model and bubbles/viewport.Model
// already bind them to down/up in their own DefaultKeyMap (see
// updateHomeList/updateDocument's list forwarding and updateDetail's
// viewport forwarding), so their vi equivalents already work wherever a
// cursor moves, with nothing for this package to add.
//
// Returns msg unchanged whenever isTextEntryScreen is true: a literal "h" or
// "l" must reach the field being typed into, not be reinterpreted as
// back/select, or a backend name, API key, field value or filter query
// containing either letter could never be typed. Also unchanged for every
// key other than a bare "h"/"l" -- alt+h, ctrl+h and so on are left alone,
// since only the plain keystroke is the vi binding being added here.
func (m Model) remapViKey(msg tea.KeyMsg) tea.KeyMsg {
	if m.isTextEntryScreen() {
		return msg
	}
	switch msg.String() {
	case "h":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "l":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return msg
}

// isTextEntryScreen reports whether the current screen's whole body is a
// text field right now, as opposed to a list or overlay a keystroke
// navigates -- see this file's own package comment for the two cases and
// why each needs a literal "h"/"l" to survive remapViKey untouched.
func (m Model) isTextEntryScreen() bool {
	if m.currentScreen() == screenValuePrompt {
		return true
	}
	return m.currentScreen() == screenSettings && m.settings.mode == settingsModeEdit
}

// currentFilterState reports the list.FilterState of whichever screen-owned
// bubbles/list.Model is current -- see currentListModel's own doc comment
// (model.go) for exactly which one that is and why this delegates to it
// rather than running its own copy of that switch -- or list.Unfiltered for
// a screen with no list of its own (Confirm, ValuePrompt, Detail, and
// Settings while settingsModeEdit is current). Model.handleKey uses this to
// decide, on every keystroke, whether the shell's global bindings, the vi
// remap above, and a screen's own Enter/d/s overrides apply at all, or
// whether the key belongs to list.Model's own filter input instead -- see
// handleKey's own doc comment for exactly which keys defer and when.
func (m Model) currentFilterState() list.FilterState {
	lm := m.currentListModel()
	if lm == nil {
		return list.Unfiltered
	}
	return lm.FilterState()
}
