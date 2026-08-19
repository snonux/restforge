package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/session"
)

// homeTabBinding and homeEnterBinding are Home's own two key bindings, kept
// out of keys.go's shared keyMap per that file's own doc comment: a
// screen-specific key is that screen's own to add, not folded into the
// three bindings every screen shares. Neither carries a key.WithHelp -- see
// homeModel.hintLine (home.go) for where Home's own hint text lives
// instead.
var (
	homeTabBinding         = key.NewBinding(key.WithKeys("tab"))
	homeEnterBinding       = key.NewBinding(key.WithKeys("enter"))
	homeSettingsBinding    = key.NewBinding(key.WithKeys("s"))
	homeDeleteQuickBinding = key.NewBinding(key.WithKeys("d"))
)

// updateHome routes a key event to whichever of Home's two lists has focus,
// intercepting Tab (switch focus), Enter (activate the current selection)
// and, while the Quick list has focus, 'd' (delete the shortcut under the
// cursor, deleteSelectedQuick) itself -- everything else (arrows, j/k/h/l,
// page up/down, and so on) is bubbles/list's own DefaultKeyMap plus this
// package's vi remap, forwarded unchanged by updateHomeList. Called from
// Model.handleKey's fallback once the three global bindings (quit/back/
// help) have all missed.
//
// Skips straight to updateHomeList, none of the above applied, while the
// focused list's own filter input has focus (list.Filtering) -- 'd', 's'
// and Tab must reach a filter query being typed the same way Document's own
// Enter/'d' overrides do (see updateDocument's own doc comment) rather than
// firing on every keystroke of a search.
func (m Model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.currentFilterState() == list.Filtering {
		return m.updateHomeList(msg)
	}
	switch {
	case key.Matches(msg, homeTabBinding) && m.home.hasShortcuts && len(m.home.backends.Items()) > 0:
		m.home.focus = m.home.focus.toggle()
		return m, nil
	case key.Matches(msg, homeEnterBinding):
		return m.activateHomeSelection()
	case key.Matches(msg, homeSettingsBinding):
		return m.openSettings(), nil
	case key.Matches(msg, homeDeleteQuickBinding) && m.home.focus == homeFocusQuick:
		return m.deleteSelectedQuick()
	}
	return m.updateHomeList(msg)
}

// openSettings switches the shell to the Settings screen, seeded from
// Home's own backend list at this exact moment (homeModel.backendsSnapshot)
// -- see settingsModel's own doc comment (settings.go) for why Settings
// reads Home's list rather than internal/config a second time. Resized
// immediately against the shell's last known window size, since Settings
// did not exist yet to receive whichever tea.WindowSizeMsg last resized
// Home -- mirrors how New (model.go) cannot size home/document until the
// first WindowSizeMsg either, except here that message has typically
// already arrived by the time this runs.
func (m Model) openSettings() Model {
	m.base = screenSettings
	m.settings = newSettingsModel(m.home.backendsSnapshot()).resize(m.width, m.height)
	return m
}

// updateHomeList forwards msg to whichever list has focus -- everything
// updateHome does not intercept itself, so bubbles/list's own arrow/page/
// home/end handling keeps working unchanged. A focused-but-empty list (Home
// only starts on Quick focus when there are no backends at all -- see
// applyLoaded in home.go) is forwarded to anyway; an empty list.Model's own
// Update is a harmless no-op.
func (m Model) updateHomeList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.home.focus {
	case homeFocusBackends:
		m.home.backends, cmd = m.home.backends.Update(msg)
	case homeFocusQuick:
		m.home.shortcuts, cmd = m.home.shortcuts.Update(msg)
	}
	return m, cmd
}

// activateHomeSelection turns Enter on Home into either opening the
// selected backend (Session.OpenBackend, via openBackendCmd) or running the
// selected shortcut (Session.RunQuick, via runQuickCmd) -- see each cmd's
// own doc comment (home_cmd.go) for why neither is a plain sessionCmd call.
// Nothing happens on Enter with nothing selected (an empty list, or focus
// stuck on Quick because there is nothing there either).
func (m Model) activateHomeSelection() (tea.Model, tea.Cmd) {
	switch m.home.focus {
	case homeFocusBackends:
		be, ok := m.home.selectedBackend()
		if !ok {
			return m, nil
		}
		return m, openBackendCmd(m.session, be)
	case homeFocusQuick:
		item, ok := m.home.selectedQuick()
		if !ok {
			return m, nil
		}
		m.home.notice = ""
		return m, runQuickCmd(m.session, item)
	}
	return m, nil
}

// deleteSelectedQuick removes the shortcut the Quick list's cursor is
// currently on, via quick.RemoveItem (deleteQuickCmd, home_cmd.go) -- a no-op
// with nothing selected (an empty list). Mirrors home_screen.dart's own
// remove-shortcut affordance (a dedicated icon button on each row, see
// _QuickTile's onRemove) translated to a key rather than a pointer target,
// the same way this screen's other row actions already are (Enter to
// activate, 's' to open Settings).
func (m Model) deleteSelectedQuick() (tea.Model, tea.Cmd) {
	item, ok := m.home.selectedQuick()
	if !ok {
		return m, nil
	}
	return m, deleteQuickCmd(item)
}

// applyQuickDeleted folds a homeQuickDeletedMsg into the shell: a failure
// (either the underlying Save erroring, or RemoveItem finding nothing left
// to remove -- the shortcut list on screen and the stored one having
// drifted apart is the only way that second case happens, since the row
// just came from a load of the same file) is reported as Home's own notice,
// the same field applyQuickRan already uses for QuickRunBackendMissing;
// success reloads Home's lists (homeInitCmd) exactly the way a Settings save
// does (applySettingsSaved, model.go), so the removed row disappears and
// nothing else about the reload logic is duplicated here.
func (m Model) applyQuickDeleted(msg homeQuickDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.home.notice = fmt.Sprintf("could not remove %q: %v", msg.label, msg.err)
		return m, nil
	}
	if !msg.ok {
		m.home.notice = fmt.Sprintf("%q was already removed", msg.label)
		return m, homeInitCmd()
	}
	m.home.notice = ""
	return m, homeInitCmd()
}

// applyQuickRan folds a homeQuickRanMsg into the shell: QuickRunOpened
// switches the base screen to Document -- Session has already adopted the
// shortcut's backend and fetched (or attempted to fetch) its target, see
// Session.RunQuick's own doc comment on why it still navigates on a failed
// fetch -- while QuickRunBackendMissing stays on Home and reports it as
// Home's own notice instead, mirroring home_screen.dart's _runShortcut
// SnackBar for that one outcome. A non-nil err is quick.BackendFor's own
// config read failing, reported the same way.
func (m Model) applyQuickRan(msg homeQuickRanMsg) Model {
	if msg.err != nil {
		m.home.notice = fmt.Sprintf("could not run %q: %v", msg.label, msg.err)
		return m
	}
	switch msg.outcome {
	case session.QuickRunOpened:
		m.base = screenDocument
	case session.QuickRunBackendMissing:
		m.home.notice = fmt.Sprintf("%q: its backend is no longer configured", msg.label)
	}
	return m
}
