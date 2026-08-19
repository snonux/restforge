package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
)

// settingsDeleteBinding, settingsSaveBinding and settingsConfirmBinding are
// the Settings screen's own key bindings, kept out of keys.go's shared
// keyMap per that file's own doc comment: a screen-specific key is that
// screen's own to add. None carries a key.WithHelp -- see
// settingsModel.listHintLine/editView for where this screen's own hint text
// lives instead.
//
// settingsConfirmBinding doubles as both "activate the list selection" and
// "confirm the row being edited" -- one Enter binding, dispatched
// differently depending on s.mode (see updateSettings), the same split
// updateHome/updateDocument already make between list-navigation keys and
// their own screen-specific ones.
var (
	settingsDeleteBinding  = key.NewBinding(key.WithKeys("d"))
	settingsSaveBinding    = key.NewBinding(key.WithKeys("s"))
	settingsConfirmBinding = key.NewBinding(key.WithKeys("enter"))
)

// updateSettings routes a key event to whichever of the two modes is
// current -- called from Model.handleKey's fallback once the three global
// bindings (quit/back/help) have all missed. Esc/backspace never reaches
// here for the edit-mode-to-list-mode step down: Model.handleBack
// intercepts it first when s.mode is settingsModeEdit (see model.go), the
// same precedence session.Detail()/CanGoBack() already get there. Only the
// list-mode-to-Home step (Cancel) falls through to handleBack's own default
// case, since list mode has nothing of its own left to unwind.
func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.mode == settingsModeEdit {
		return m.updateSettingsEdit(msg)
	}
	return m.updateSettingsList(msg)
}

// updateSettingsList handles settingsModeList: Enter activates the row under
// the cursor (edit an existing backend, or start adding one on the trailing
// Add row), 'd' deletes the row under the cursor, 's' saves the whole
// working list, and everything else is forwarded to the list unchanged --
// mirrors updateHome's own shape for Home's lists.
func (m Model) updateSettingsList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, settingsConfirmBinding):
		m.settings = m.settings.activateSelection()
		return m, nil
	case key.Matches(msg, settingsDeleteBinding):
		m.settings = m.settings.deleteSelected()
		return m, nil
	case key.Matches(msg, settingsSaveBinding):
		m.settings.saving = true
		return m, settingsSaveCmd(m.settings.backends)
	}
	var cmd tea.Cmd
	m.settings.list, cmd = m.settings.list.Update(msg)
	return m, cmd
}

// activateSelection turns Enter on settingsModeList into either editing the
// backend under the cursor or starting a new one, depending on which
// list.Item type is there -- see settings_items.go's settingsBackendItem/
// settingsAddItem.
func (s settingsModel) activateSelection() settingsModel {
	switch s.list.SelectedItem().(type) {
	case settingsBackendItem:
		return s.startEdit(s.list.Index())
	case settingsAddItem:
		return s.startAdd()
	}
	return s
}

// startEdit switches to settingsModeEdit for backends[index]: the form is
// populated from that entry (see settingsForm.populate) and focused on the
// name field.
func (s settingsModel) startEdit(index int) settingsModel {
	s.mode = settingsModeEdit
	s.editIndex = index
	s.editError = ""
	s.form = s.form.populate(s.backends[index])
	s.form, _ = s.form.focusField(fieldName)
	return s
}

// startAdd switches to settingsModeEdit for a not-yet-added backend --
// editIndex -1 is confirmEdit's own signal to append rather than replace.
// No-op once backends is already at backend.MaxBackends: rebuildList
// (settings_items.go) already drops the trailing Add row at that point, so
// this is only reachable via a stale selection, not a case the UI actually
// offers -- kept as a guard rather than trusted to never happen.
func (s settingsModel) startAdd() settingsModel {
	if len(s.backends) >= backend.MaxBackends {
		return s
	}
	s.mode = settingsModeEdit
	s.editIndex = -1
	s.editError = ""
	s.form = s.form.blank()
	s.form, _ = s.form.focusField(fieldName)
	return s
}

// deleteSelected removes the backend under the cursor from the working
// list -- a no-op when the cursor is on the trailing Add row (nothing there
// to delete) or the list is empty.
func (s settingsModel) deleteSelected() settingsModel {
	if _, ok := s.list.SelectedItem().(settingsBackendItem); !ok {
		return s
	}
	index := s.list.Index()
	s.backends = append(s.backends[:index:index], s.backends[index+1:]...)
	s.notice = ""
	return s.rebuildList()
}

// --- settingsModeEdit ----------------------------------------------------

// settingsFieldUpBinding and settingsFieldDownBinding move focus between the
// form's five fields. Both Up/Down and Tab/Shift+Tab are bound to the same
// two directions -- a vertical form reads naturally with either, and
// textinput.Model's own DefaultKeyMap already claims up/down for suggestion
// navigation (never used here, since no field sets suggestions), so binding
// them here first is what keeps a plain Up/Down press from reaching
// textinput.Model's Update at all -- see updateSettingsEdit.
var (
	settingsFieldUpBinding   = key.NewBinding(key.WithKeys("up", "shift+tab"))
	settingsFieldDownBinding = key.NewBinding(key.WithKeys("down", "tab"))
)

// updateSettingsEdit handles settingsModeEdit: Up/Down/Tab/Shift+Tab move
// focus among the five fields, Enter confirms the row (confirmEdit), and
// everything else -- every printed character, left/right, backspace, and so
// on -- is forwarded to whichever field currently has focus, so
// textinput.Model's own editing keys (see its DefaultKeyMap) keep working
// unchanged. Esc/backspace never reaches here -- see updateSettings' own doc
// comment on why Model.handleBack intercepts it first.
func (m Model) updateSettingsEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, settingsConfirmBinding):
		m.settings = m.settings.confirmEdit()
		return m, nil
	case key.Matches(msg, settingsFieldDownBinding):
		return m.moveSettingsFocus(1)
	case key.Matches(msg, settingsFieldUpBinding):
		return m.moveSettingsFocus(-1)
	}
	var cmd tea.Cmd
	focused := m.settings.form.focus
	m.settings.form.inputs[focused], cmd = m.settings.form.inputs[focused].Update(msg)
	return m, cmd
}

// moveSettingsFocus steps the form's focused field by delta, wrapping
// around both ends -- so Down on the last field returns to the first and Up
// on the first wraps to the last, letting either direction cycle the whole
// form without a dead end at either edge.
func (m Model) moveSettingsFocus(delta int) (tea.Model, tea.Cmd) {
	next := (m.settings.form.focus + delta + fieldCount) % fieldCount
	var cmd tea.Cmd
	m.settings.form, cmd = m.settings.form.focusField(next)
	return m, cmd
}

// confirmEdit validates the form's current values through
// backend.Normalise/Validate -- never reimplemented here, see the
// package-level doc comment in settings.go -- and either writes the result
// into backends (appending for a new row, replacing in place for an
// existing one) and returns to settingsModeList, or records Validate's own
// message in editError and stays in settingsModeEdit for another attempt.
func (s settingsModel) confirmEdit() settingsModel {
	normalised := backend.Normalise(s.form.values())
	if err := backend.Validate(normalised); err != nil {
		s.editError = err.Error()
		return s
	}
	if s.editIndex < 0 {
		s.backends = append(s.backends, normalised)
	} else {
		s.backends[s.editIndex] = normalised
	}
	s.editError = ""
	s.notice = ""
	s.mode = settingsModeList
	return s.rebuildList()
}

// cancelEdit steps settingsModeEdit back to settingsModeList without writing
// the form's values anywhere -- called from Model.handleBack when the
// global Back key is pressed mid-edit (see updateSettings' own doc comment).
// backends is untouched either way: confirmEdit is the only thing that ever
// writes to it, so a cancelled add never appears and a cancelled edit never
// overwrites the entry it was editing.
func (s settingsModel) cancelEdit() settingsModel {
	s.mode = settingsModeList
	s.editError = ""
	return s
}
