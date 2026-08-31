package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/render"
)

// documentRowItem adapts render.Row to list.Item for the Document screen's
// row list -- mirrors backendItem/quickItem in home_items.go. Carries the
// whole Row, not just its label: activateDocumentSelection (document_update.go)
// needs Row.Target back once the cursor's row is picked.
type documentRowItem struct {
	row render.Row
}

var _ list.Item = documentRowItem{}

// FilterValue is what "/" filters this row against: the row's Label (its
// key -- a property name, a link's rel, an action's title) plus its
// Sublabel (its value -- a property's rendered value, or an embedded
// entity's own summary; empty for a link/action, which have none) -- see
// backendItem.FilterValue's own doc comment for the same "match key or
// value" reasoning.
func (i documentRowItem) FilterValue() string { return i.row.Label + " " + i.row.Sublabel }

// documentDelegate renders one row of a render.RenderedDocument, styled
// through the shared styles.go palette and distinguished by RowKind --
// mirrors _RowTile's four-way switch in document_rows.dart (see that
// file's own module comment on why an action row must never look like the
// three read-only kinds around it).
type documentDelegate struct{}

var _ list.ItemDelegate = documentDelegate{}

// Height is two lines per row: a title line (the row's glyph plus label)
// and a subtitle line underneath it for the row's Sublabel, when it has
// one.
func (documentDelegate) Height() int { return 2 }

// Spacing is zero -- same reasoning as homeDelegate.Spacing: the title/
// subtitle pair each row already renders is break enough, and a document can
// hold far more rows than a backend list, so the extra blank line cost more
// screen real estate here than anywhere else in this package.
func (documentDelegate) Spacing() int { return 0 }

// Update does nothing -- see homeDelegate.Update's own doc comment; the
// same reasoning applies here: a row has no state of its own for a
// keypress to change.
func (documentDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// Render writes one row's title line and, when present, its Sublabel
// underneath, muted -- mirrors each row widget's ListTile title/subtitle in
// document_rows.dart.
func (documentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	row, ok := item.(documentRowItem)
	if !ok {
		return
	}
	selected := index == m.Index()
	// Errors from w are discarded explicitly -- see renderBackendRow's own
	// doc comment (home_items.go) for why: w is list.Model's own
	// strings.Builder, whose Write never fails.
	_, _ = fmt.Fprintln(w, documentRowTitle(row.row, selected))
	if row.row.Sublabel != "" {
		_, _ = fmt.Fprint(w, MutedStyle.Render("  "+row.row.Sublabel))
	}
}

// documentRowGlyph is the short kind marker shown before a row's label --
// mirrors the leading Icon each row kind gets in document_rows.dart
// (label_outline / account_tree / link / bolt), translated to a single
// glyph a terminal font can render. Unlike render.RowKind's own switches in
// this codebase, which panic on an unreachable RowTarget case (see
// render.RowTarget's own doc comment), RowKind here follows the lenient
// convention internal/cli/output.go's kindToken already set for this exact
// enum: a RowKind is a plain closed int, not a sealed interface, and a
// presentation fallback is a reasonable response to a value nothing here
// recognises, not a bug to crash on.
func documentRowGlyph(kind render.RowKind) string {
	switch kind {
	case render.RowKindProperty:
		return "▪"
	case render.RowKindEntity:
		return "▸"
	case render.RowKindLink:
		return "↗"
	case render.RowKindAction:
		return "⚡"
	default:
		return "?"
	}
}

// documentRowTitle styles one row's title line: an action row is always
// bold and coloured through SelectedItemStyle -- the same weight a
// currently-selected row gets -- so it reads as a deliberate, separate
// gesture even when the cursor is not on it, mirroring _ActionRow's
// filled-tile treatment in document_rows.dart ("ask before acting",
// docs/DESIGN.md: "the row that opens a property and the row that changes
// the world must not act the same"). A property/sub-entity/link row only
// gets that treatment while the cursor is actually on it.
func documentRowTitle(row render.Row, selected bool) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	text := prefix + documentRowGlyph(row.Kind) + " " + row.Label
	if selected || row.Kind == render.RowKindAction {
		return SelectedItemStyle.Render(text)
	}
	return NormalItemStyle.Render(text)
}
