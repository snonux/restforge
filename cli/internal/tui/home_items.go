package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/quick"
)

// backendItem adapts backend.Backend to list.Item for Home's backend list.
type backendItem struct {
	backend backend.Backend
}

var _ list.Item = backendItem{}

// FilterValue is what "/" filters this row against: the name (the row's
// key) plus its base URL (the row's value, per renderBackendRow's own
// subtitle) -- so filtering by either finds it, not just by name.
func (i backendItem) FilterValue() string { return i.backend.Name + " " + i.backend.BaseURL }

// quickItem adapts homeQuickRow (home_cmd.go) to list.Item for Home's
// shortcuts list. backend is the shortcut's *current* backend -- nil once
// it has been renamed away or deleted, rendered as "Backend removed" by
// homeDelegate rather than dropping the row (see homeQuickRow's own doc
// comment) -- not the backend it was saved against.
type quickItem homeQuickRow

var _ list.Item = quickItem{}

// FilterValue is what "/" filters this row against: the shortcut's label
// (the row's key) plus quickSubtitle's own text (the row's value: which
// backend and kind, or "Backend removed") -- mirrors backendItem's own
// reasoning.
func (i quickItem) FilterValue() string { return i.item.Label + " " + quickSubtitle(i) }

// homeDelegate renders both backendItem and quickItem rows through the
// shared styles.go palette -- one delegate for both of Home's lists, so a
// style change touches this one file rather than two.
type homeDelegate struct{}

var _ list.ItemDelegate = homeDelegate{}

// Height is two lines per row: a title line (backend name, or shortcut
// label) and a subtitle line underneath it (base URL, or the shortcut's
// current backend and kind).
func (homeDelegate) Height() int { return 2 }

// Spacing is one blank line between rows, so a list of several backends or
// shortcuts does not read as a single wall of text.
func (homeDelegate) Spacing() int { return 1 }

// Update does nothing: neither backendItem nor quickItem has any state of
// its own for a keypress to change -- selection and activation are both
// Home's own job (see home_update.go), not this delegate's.
func (homeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// Render writes one row, styled through SelectedItemStyle/NormalItemStyle
// for whichever row the list's cursor (m.Index()) is currently on.
func (homeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	selected := index == m.Index()
	switch v := item.(type) {
	case backendItem:
		renderBackendRow(w, v.backend, selected)
	case quickItem:
		renderQuickRow(w, v, selected)
	}
}

// renderBackendRow writes a backend's name as the title line and its base
// URL, muted, as the subtitle -- mirrors _BackendList's ListTile
// (title/subtitle) in home_screen.dart.
func renderBackendRow(w io.Writer, be backend.Backend, selected bool) {
	// Errors from w are discarded explicitly: w is list.Model's own
	// strings.Builder (see list.go's populatedView), whose Write never
	// fails -- there is no partial-write or closed-writer case to react to
	// here, unlike a real os.File or network connection.
	_, _ = fmt.Fprintln(w, rowTitle(be.Name, selected))
	_, _ = fmt.Fprint(w, MutedStyle.Render("  "+be.BaseURL))
}

// renderQuickRow writes a shortcut's label as the title line and, as the
// subtitle, the backend it currently resolves to plus its kind -- or, once
// that backend is gone, "Backend removed" styled through ErrorStyle instead
// of MutedStyle, mirroring _QuickTile's red-on-removed treatment in
// home_screen.dart.
func renderQuickRow(w io.Writer, q quickItem, selected bool) {
	// See renderBackendRow's comment on why w's errors are discarded
	// explicitly rather than checked.
	_, _ = fmt.Fprintln(w, rowTitle(q.item.Label, selected))

	style := MutedStyle
	if q.backend == nil {
		style = ErrorStyle
	}
	_, _ = fmt.Fprint(w, style.Render("  "+quickSubtitle(q)))
}

// rowTitle styles a row's title line, prefixing the cursor's own row with
// "> " so the selection reads even where the terminal's own colour support
// is limited, on top of SelectedItemStyle's bold-plus-colour treatment.
func rowTitle(title string, selected bool) string {
	if selected {
		return SelectedItemStyle.Render("> " + title)
	}
	return NormalItemStyle.Render(title)
}

// quickSubtitle names a shortcut's backend and kind ("My API - link"), or
// "Backend removed" once BackendsFor could no longer resolve it -- see
// quickItem's own doc comment.
func quickSubtitle(q quickItem) string {
	if q.backend == nil {
		return "Backend removed"
	}
	kind := "link"
	if q.item.Kind == quick.KindAction {
		kind = "action"
	}
	return fmt.Sprintf("%s - %s", q.backend.Name, kind)
}
