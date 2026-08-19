package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
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
	homeTabBinding   = key.NewBinding(key.WithKeys("tab"))
	homeEnterBinding = key.NewBinding(key.WithKeys("enter"))
)

// updateHome routes a key event to whichever of Home's two lists has focus,
// intercepting Tab (switch focus) and Enter (activate the current
// selection) itself -- everything else (arrows, page up/down, and so on) is
// bubbles/list's own DefaultKeyMap, forwarded unchanged by updateHomeList.
// Called from Model.handleKey's fallback once the three global bindings
// (quit/back/help) have all missed.
func (m Model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, homeTabBinding) && m.home.hasShortcuts && len(m.home.backends.Items()) > 0:
		m.home.focus = m.home.focus.toggle()
		return m, nil
	case key.Matches(msg, homeEnterBinding):
		return m.activateHomeSelection()
	}
	return m.updateHomeList(msg)
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
