package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/quick"
)

// homeFocus is which of Home's two lists -- backends or shortcuts -- the
// cursor is currently in. Tab toggles between them (see updateHome in
// home_update.go); Enter always activates whichever one has focus.
type homeFocus int

const (
	homeFocusBackends homeFocus = iota
	homeFocusQuick
)

// toggle flips focus to the other list -- homeFocus only ever has these two
// values, so there is nothing to switch on.
func (f homeFocus) toggle() homeFocus {
	if f == homeFocusBackends {
		return homeFocusQuick
	}
	return homeFocusBackends
}

// homeModel is the Home screen's own state: the configured-backend list and
// the saved-shortcut list, mirroring the two sections of
// flutter/lib/screens/home_screen.dart's Column (_BackendList and
// _QuickSection). Two separate bubbles/list.Models rather than one combined
// list with header rows -- backend.Backend and quick.QuickItem are two
// different types with two different activation behaviours (open vs. run),
// and a combined list would need a non-selectable header item invented just
// to hold list.Item's place; two lists plus a focus flag needs neither.
//
// Reads internal/config and internal/quick once, when Home mounts (see
// homeInitCmd in home_cmd.go, wired from Model.Init) -- Session itself does
// not own backend-listing, mirroring nav_service.dart's module comment that
// the backend picker is out of NavService's scope.
type homeModel struct {
	backends     list.Model
	shortcuts    list.Model
	hasShortcuts bool
	focus        homeFocus

	// notice is Home's own one-line report -- a config/quick load failure
	// from homeInitCmd, or a QuickRunBackendMissing outcome from running a
	// shortcut whose backend has since been removed (see home_update.go's
	// applyQuickRan). Mirrors home_screen.dart's SnackBar for the same two
	// cases, as a persistent line under the lists rather than a transient
	// popup -- this shell has no snackbar equivalent.
	notice string
}

// newHomeModel builds both lists on the shared homeDelegate (home_items.go)
// at zero size -- Model.Update's tea.WindowSizeMsg case calls resize once
// the real terminal size is known, the same lazy-sizing every bubbles/list
// example uses. Each list.Model's own chrome (title, status bar, help,
// filtering, and its own quit keybindings) is turned off: Home renders its
// own heading and hint line (see View), and filtering is not worth the
// "esc" key it binds by default colliding with the shell's global Back
// binding (keys.go) for a list capped at backend.MaxBackends/quick.MaxQuick
// items apiece -- there is nothing here filtering would meaningfully speed
// up. Quit is the shell's job (keys.go's global Quit), never a list's own.
func newHomeModel() homeModel {
	backends := list.New(nil, homeDelegate{}, 0, 0)
	shortcuts := list.New(nil, homeDelegate{}, 0, 0)
	for _, l := range []*list.Model{&backends, &shortcuts} {
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowHelp(false)
		l.SetFilteringEnabled(false)
		l.DisableQuitKeybindings()
	}
	return homeModel{backends: backends, shortcuts: shortcuts}
}

// View renders Home: a heading, the backend list (or emptyStateView in its
// place -- this task's own requirement that Home never render a blank
// screen when nothing is configured), the shortcuts list underneath when
// there is at least one saved shortcut, a key hint line, and any notice
// last. Mirrors home_screen.dart's Column of two Expanded sections, minus
// the shortcuts section entirely when there are none, rather than rendering
// it present-but-empty.
func (h homeModel) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("RESTForge") + "\n\n")

	if len(h.backends.Items()) == 0 {
		b.WriteString(h.emptyStateView())
	} else {
		b.WriteString(SubtitleStyle.Render("Backends") + "\n")
		b.WriteString(h.backends.View())
	}

	if h.hasShortcuts {
		b.WriteString("\n" + SubtitleStyle.Render("Shortcuts") + "\n")
		b.WriteString(h.shortcuts.View())
	}

	b.WriteString("\n" + MutedStyle.Render(h.hintLine()))
	if h.notice != "" {
		b.WriteString("\n" + ErrorStyle.Render(h.notice))
	}
	return b.String()
}

// emptyStateView is what Backends shows in place of the list when nothing
// is configured. Points at both ways to add one -- the Settings screen
// (task 931, not yet reachable from here, so named rather than linked) and
// the restforge backends add CLI command, which already exists -- mirrors
// home_screen.dart's _EmptyState, minus the button: there is nowhere for an
// Enter press on this text to go yet.
func (h homeModel) emptyStateView() string {
	return SubtitleStyle.Render("No backends configured") + "\n" +
		MutedStyle.Render(
			"A backend is an API root, an auth header and a secret.\n"+
				"Add one from the Settings screen, or run:\n\n"+
				"  restforge backends add --name ... --base-url ... --secret ...",
		) + "\n"
}

// hintLine is Home's own short key hint, shown under both lists --
// keys.go's own doc comment reserves the shell's global three (quit/back/
// help) for bindings every screen shares, so a screen-specific one (Enter,
// Tab) is composed here instead rather than folded into that key map. Tab
// is only worth mentioning once there is a second list to switch to.
func (h homeModel) hintLine() string {
	if h.hasShortcuts && len(h.backends.Items()) > 0 {
		return "↑/↓ move · enter select · tab switch list"
	}
	return "↑/↓ move · enter select"
}

// resize gives both lists their share of the available body height, after
// setting aside room for Home's own heading, section subtitles and hint
// line (see View). Called from Model.Update's tea.WindowSizeMsg case, the
// same place the shell's own width/height are recorded.
func (h homeModel) resize(width, height int) homeModel {
	// chrome approximates the fixed lines View wraps the lists in (heading
	// + blank line, two subtitles, the hint line) plus AppStyle's padding
	// and the help line Model.View appends below every screen -- generous
	// rather than exact, since a list a little short of the terminal's
	// bottom row is a smaller problem than one that overflows it.
	const chrome = 10
	avail := height - chrome
	if avail < 3 {
		avail = 3
	}

	if h.hasShortcuts {
		backendsHeight := avail * 3 / 5
		if backendsHeight < 1 {
			backendsHeight = 1
		}
		h.backends.SetSize(width, backendsHeight)
		h.shortcuts.SetSize(width, avail-backendsHeight)
	} else {
		h.backends.SetSize(width, avail)
	}
	return h
}

// selectedBackend returns the backend the backends list's cursor is
// currently on, or the zero value and false when the list is empty.
func (h homeModel) selectedBackend() (backend.Backend, bool) {
	item, ok := h.backends.SelectedItem().(backendItem)
	if !ok {
		return backend.Backend{}, false
	}
	return item.backend, true
}

// selectedQuick returns the shortcut the shortcuts list's cursor is
// currently on, or the zero value and false when the list is empty.
func (h homeModel) selectedQuick() (quick.QuickItem, bool) {
	item, ok := h.shortcuts.SelectedItem().(quickItem)
	if !ok {
		return quick.QuickItem{}, false
	}
	return item.item, true
}

// applyLoaded installs what homeInitCmd read from internal/config and
// internal/quick into both lists, and picks which one starts focused: the
// backend list when it has anything to pick, otherwise the shortcut list,
// so Enter never lands on an empty list expecting a selection. A load error
// (config.LoadBackends' own doc comment: the config path itself could not
// be resolved -- the one thing that can fail here) is reported as Home's
// own notice rather than crashing the shell.
func (h homeModel) applyLoaded(msg homeLoadedMsg) homeModel {
	if msg.err != nil {
		h.notice = "could not load configuration: " + msg.err.Error()
		return h
	}
	h.notice = ""

	backendItems := make([]list.Item, len(msg.backends))
	for i, be := range msg.backends {
		backendItems[i] = backendItem{backend: be}
	}
	h.backends.SetItems(backendItems)

	quickItems := make([]list.Item, len(msg.rows))
	for i, row := range msg.rows {
		quickItems[i] = quickItem(row)
	}
	h.shortcuts.SetItems(quickItems)
	h.hasShortcuts = len(quickItems) > 0

	h.focus = homeFocusBackends
	if len(backendItems) == 0 {
		h.focus = homeFocusQuick
	}
	return h
}
