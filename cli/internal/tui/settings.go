// Part of the Settings screen (task 931): the backend editor, the Go/Bubble
// Tea port of flutter/lib/screens/settings_screen.dart. Split across this
// file (settingsModel itself, construction, sizing, top-level View), plus
// settings_items.go (the backend list and its list.Item adapters),
// settings_form.go (the five-field edit form) and settings_update.go (key
// handling for both modes) and settings_cmd.go (the async save) -- the same
// four/five-way split home.go/home_items.go/home_update.go/home_cmd.go and
// document.go/document_items.go/document_update.go already establish for a
// screen this shell owns.
//
// # Why two modes, where the Dart original has one
//
// settings_screen.dart renders every configured backend as a card with all
// five fields live at once -- there is room for that on a phone screen and
// nothing else competing for it. A terminal has neither the width nor the
// height to lay out N backends' worth of five-field forms side by side
// without truncating everything into illegibility, so this port splits what
// the Dart screen does in one view into two: settingsModeList shows one line
// per backend (its own bubbles/list.Model, mirroring homeModel's own two
// lists) and settingsModeEdit shows the five fields of exactly one backend
// at a time (five bubbles/textinput.Models). Enter switches List -> Edit;
// confirming a row's edits (also Enter) switches back. This is a UI-shape
// change only -- the data this screen edits, the validation it enforces
// (internal/backend.Normalise/Validate, never reimplemented here) and the
// save contract (internal/config.SaveBackends) are unchanged from the Dart
// original.
//
// # Why validation happens per-row, not once at Save
//
// settings_screen.dart validates every row in one pass when Save is pressed,
// stopping at the first problem and reporting "Backend N: <message>" --
// workable there because every row's fields are already on screen to relate
// that index back to. Here, only one row's fields are ever on screen at a
// time (the one being edited), so this port validates at the point a row's
// edit is confirmed instead: backend.Validate's own message is shown right
// next to the fields it is complaining about, and an invalid row can never
// make it back into the list to begin with -- so by the time Save runs,
// every entry in settingsModel.backends has already passed
// backend.Normalise/Validate once. Save (settings_cmd.go) still goes through
// config.SaveBackends, which normalises and validates again on its own
// (store.go's filterBackends) -- this is a second, cheap safety net, not
// this screen's only line of defence.
package tui

import (
	"github.com/charmbracelet/bubbles/list"

	"github.com/snonux/restforge/cli/internal/backend"
)

// settingsMode is which half of the screen is showing: the backend list, or
// the five-field form for one backend. See the package-level doc comment
// above for why this screen has a mode at all where the Dart original does
// not.
type settingsMode int

const (
	settingsModeList settingsMode = iota
	settingsModeEdit
)

// settingsModel is the Settings screen's own state. Mirrors homeModel's role
// for Home (home.go) and documentModel's for Document (document.go): a
// screen keeps its own bubbles/list.Model (and, here, its own
// bubbles/textinput.Models) rather than rebuilding either fresh on every
// View call.
//
// backends is a working copy, seeded from whatever Home's own backend list
// held at the moment the user opened Settings (see home.go's
// backendsSnapshot and home_update.go's openSettings) -- never read from or
// written to internal/config until Save (the 's' binding in list mode,
// settings_update.go) actually runs. This mirrors the Dart screen's own
// _rows/_settings split: nothing reaches storage until Save, and Cancel (the
// global Back key, handled by Model.handleBack falling through to its
// default case) simply discards this whole struct and returns to Home,
// leaving whatever Home already loaded untouched.
type settingsModel struct {
	mode settingsMode

	backends []backend.Backend
	list     list.Model

	// editIndex is which entry in backends the form is currently editing:
	// -1 means "not yet in backends" -- a new row started by selecting the
	// list's trailing "+ Add backend" item (see settings_items.go) -- any
	// other value is an index into backends being replaced in place once
	// the edit is confirmed. See startAdd/startEdit/confirmEdit
	// (settings_update.go).
	editIndex int

	// form holds five bubbles/textinput.Models (one per backend field) plus
	// which one currently has focus -- see settings_form.go.
	form settingsForm

	// editError is the message backend.Validate returned for the row
	// currently being edited, shown under the form -- see confirmEdit
	// (settings_update.go). Cleared whenever a fresh edit starts.
	editError string

	// notice is list mode's own one-line report: a save failure from
	// settingsSaveCmd (settings_cmd.go), or "" once nothing is worth
	// reporting. Mirrors homeModel.notice's same role for Home.
	notice string

	saving bool
}

// newSettingsModel seeds a fresh Settings screen from initial -- Home's own
// backend list at the moment the user opened this screen (see
// backendsSnapshot, home.go). The list is built at zero size; resize below
// gives it real dimensions, called immediately by openSettings
// (home_update.go) rather than waiting for the next tea.WindowSizeMsg, since
// Settings did not exist to receive whichever WindowSizeMsg last resized
// Home.
func newSettingsModel(initial []backend.Backend) settingsModel {
	backends := append([]backend.Backend(nil), initial...) // copy: never alias Home's own slice
	m := settingsModel{
		mode:      settingsModeList,
		backends:  backends,
		list:      list.New(nil, settingsDelegate{}, 0, 0),
		editIndex: -1,
		form:      newSettingsForm(),
	}
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	m.list.SetFilteringEnabled(false)
	m.list.DisableQuitKeybindings()
	return m.rebuildList()
}

// resize gives the backend list its share of the available body height --
// mirrors homeModel.resize/documentModel.resize's own reasoning, generous
// rather than exact for the same stated reason. The edit form does not need
// resizing: five single-line textinput.Models take a fixed amount of
// vertical space regardless of terminal height.
func (s settingsModel) resize(width, height int) settingsModel {
	const chrome = 8
	avail := height - chrome
	if avail < 3 {
		avail = 3
	}
	s.list.SetSize(width, avail)
	return s
}

// View renders whichever mode is current -- see settings_items.go for
// listView and settings_form.go for editView.
func (s settingsModel) View() string {
	if s.mode == settingsModeEdit {
		return s.editView()
	}
	return s.listView()
}
