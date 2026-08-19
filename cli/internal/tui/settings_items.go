package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
)

// settingsBackendItem adapts backend.Backend to list.Item for the Settings
// screen's backend list -- mirrors backendItem in home_items.go, kept as its
// own type rather than reused because this list also holds a
// settingsAddItem, which backendItem has no equivalent of.
type settingsBackendItem struct {
	backend backend.Backend
}

var _ list.Item = settingsBackendItem{}

// FilterValue exists only to satisfy list.Item -- this screen's list has
// filtering disabled (see newSettingsModel), so nothing ever calls this.
func (i settingsBackendItem) FilterValue() string { return i.backend.Name }

// settingsAddItem is the list's trailing "+ Add backend" row -- mirrors the
// Dart screen's own OutlinedButton below its ListView, folded into the list
// itself here rather than rendered as a separate widget below it, since a
// terminal list has no natural "below the list" slot the way a Flutter
// Column does. Only present while len(backends) < backend.MaxBackends -- see
// rebuildList -- the same cap Dart's own Add button disables against.
type settingsAddItem struct{}

var _ list.Item = settingsAddItem{}

func (settingsAddItem) FilterValue() string { return "" }

// settingsDelegate renders both settingsBackendItem and settingsAddItem rows
// through the shared styles.go palette -- mirrors homeDelegate's own role
// for Home's two item types.
type settingsDelegate struct{}

var _ list.ItemDelegate = settingsDelegate{}

// Height is two lines per row: a title line and a subtitle underneath --
// same shape as homeDelegate/documentDelegate. settingsAddItem uses only the
// title line; see Render.
func (settingsDelegate) Height() int { return 2 }

// Spacing is one blank line between rows -- same reasoning as
// homeDelegate.Spacing.
func (settingsDelegate) Spacing() int { return 1 }

// Update does nothing -- see homeDelegate.Update's own doc comment; the same
// reasoning applies here.
func (settingsDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// Render writes one row: a backend's name and base URL, or the trailing "+
// Add backend" prompt.
func (settingsDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	selected := index == m.Index()
	switch v := item.(type) {
	case settingsBackendItem:
		_, _ = fmt.Fprintln(w, rowTitle(v.backend.Name, selected))
		_, _ = fmt.Fprint(w, MutedStyle.Render("  "+v.backend.BaseURL))
	case settingsAddItem:
		_, _ = fmt.Fprint(w, rowTitle("+ Add backend", selected))
	}
}

// rebuildList replaces the list's items with one settingsBackendItem per
// entry in s.backends, plus a trailing settingsAddItem while there is still
// room for one more -- called after every change to s.backends (confirmEdit,
// deleteSelected in settings_update.go) so the list always reflects the
// working copy, never a stale snapshot of it.
func (s settingsModel) rebuildList() settingsModel {
	items := make([]list.Item, 0, len(s.backends)+1)
	for _, b := range s.backends {
		items = append(items, settingsBackendItem{backend: b})
	}
	if len(s.backends) < backend.MaxBackends {
		items = append(items, settingsAddItem{})
	}
	s.list.SetItems(items)
	return s
}

// listView renders settingsModeList: a heading, the backend list (always at
// least one row -- the trailing Add item -- so this screen never needs
// homeModel's own emptyStateView treatment), a hint line and any notice.
func (s settingsModel) listView() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("Settings") + "\n\n")
	b.WriteString(SubtitleStyle.Render("Backends") + "\n")
	b.WriteString(s.list.View())
	if s.saving {
		// settingsSaveCmd (settings_cmd.go) is a local file write, not a
		// network call, so this rarely renders for more than one frame --
		// still shown rather than left silent, since 's' otherwise gives no
		// feedback at all until the screen jumps back to Home a moment
		// later.
		b.WriteString("\n" + MutedStyle.Render("Saving..."))
	}
	b.WriteString("\n" + MutedStyle.Render(s.listHintLine()))
	if s.notice != "" {
		b.WriteString("\n" + ErrorStyle.Render(s.notice))
	}
	return b.String()
}

// listHintLine is list mode's own short key hint -- 'd' and 's' are this
// screen's own bindings (settings_update.go), kept out of keys.go per that
// file's own doc comment. 'd' only applies to a backend row, never the
// trailing Add item, but is still worth advertising unconditionally: the
// same "harmless when nothing is selected" reasoning applies here.
func (s settingsModel) listHintLine() string {
	return "↑/↓ move · enter edit/add · d delete · s save · esc cancel"
}
