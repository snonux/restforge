package render

import (
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/siren"
)

// RowKind is what kind of Siren part a Row renders. Mirrors the KIND_*
// constants in render.js (which matched DocRowKind in the watch's
// src/c/doc.h) and RowKind in render_service.dart; the watch/phone
// row-index protocol those served does not carry over to a terminal
// client, so this is a plain enum rather than a wire-format string.
type RowKind int

const (
	RowKindProperty RowKind = iota
	RowKindEntity
	RowKindLink
	RowKindAction
)

// Row is one row of a RenderedDocument. A plain value with no behaviour
// beyond holding its own fields: the UI layer reads a row's Label,
// Sublabel and Kind, and hands Target back to whichever code knows what it
// means, never inspecting Target's internals itself.
type Row struct {
	Label    string
	Sublabel string
	Kind     RowKind
	Target   RowTarget
}

// RenderedDocument is an Entity turned into something a screen can list: a
// title and its rows, in Siren's own order.
type RenderedDocument struct {
	Title string
	Rows  []Row
}

// Document turns entity into the rows a screen lists, plus the row targets
// a caller needs to act on a press. fallbackTitle is shown when the
// document has no wording of its own -- see siren.Entity.Label. be is the
// backend entity was fetched from (or is currently open against, for the
// zero-stack case); it is stamped onto every FetchTarget this call produces
// so a later nav.Fetch pins its GET to the backend the row's href actually
// belongs to, rather than re-reading whatever backend happens to be current
// when the row is finally activated -- see FetchTarget's own doc comment
// and p31.
//
// entity is a pointer so a caller holding "nothing fetched yet" can render
// an empty screen without a special case of its own, mirroring
// document(null, fallbackTitle) in render.js and render_service.dart,
// neither of which throws (or in Go's terms, panics) on a missing
// document.
//
// Rendering a well-formed Entity cannot fail, so Document returns a
// RenderedDocument directly rather than a *failure.Failure -- that type is
// reserved for operations with an actual failure mode, and there is not
// one here.
func Document(entity *siren.Entity, fallbackTitle string, be backend.Backend) RenderedDocument {
	if entity == nil {
		return RenderedDocument{Title: fallbackTitle}
	}

	rows := make([]Row, 0, len(entity.Properties)+len(entity.Entities)+len(entity.Links)+len(entity.Actions))
	rows = append(rows, propertyRows(*entity)...)
	rows = append(rows, entityRows(*entity, be)...)
	rows = append(rows, linkRows(*entity, be)...)
	rows = append(rows, actionRows(*entity)...)

	title := entity.Label()
	if title == "" {
		title = fallbackTitle
	}
	return RenderedDocument{Title: title, Rows: rows}
}
