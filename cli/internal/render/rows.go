package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/snonux/restforge/cli/internal/siren"
)

// summaryProperties is how many of an embedded entity's properties to
// summarise on its row. Carried over from render.js's SUMMARY_PROPERTIES
// and render_service.dart's _summaryProperties: two fit on a row at a
// glance, and the value is kept identical across every port rather than
// the number quietly drifting apart between them. The whole entity is one
// row-press away regardless.
const summaryProperties = 2

// sortedPropertyKeys returns entity's property keys in ascending order.
//
// siren.Entity.Properties is a plain map[string]any, decoded generically
// by encoding/json, which keeps no record of the order the server wrote
// its properties in -- unlike the ordered maps siren.js and
// flutter/lib/models/siren.dart give render.js and render_service.dart to
// iterate. Go additionally randomises map iteration order by design (a
// well-known pitfall: ranging over a map without sorting first gives a
// different order on every run). So unlike the two other ports, this
// package cannot preserve the server's own property order -- the
// information is already gone by the time an Entity reaches here. Sorting
// the keys trades that lost ordering for a stable, reproducible one
// instead, which is the least surprising choice available and the only
// one that keeps this package's own tests (and a person staring at the
// same document twice) from seeing properties reshuffle at random.
func sortedPropertyKeys(properties map[string]any) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// propertyRows renders each of entity's properties as one Row. Mirrors
// propertyRows() in render.js and _propertyRows() in render_service.dart.
func propertyRows(entity siren.Entity) []Row {
	keys := sortedPropertyKeys(entity.Properties)
	rows := make([]Row, 0, len(keys))
	for _, key := range keys {
		rendered := Text(entity.Properties[key])
		rows = append(rows, Row{
			Label:    key,
			Sublabel: rendered,
			Kind:     RowKindProperty,
			// The full value, not the truncated Sublabel: activating the
			// row opens it in the reading view, which exists for values
			// this long.
			Target: DetailTarget{Heading: key, Body: rendered},
		})
	}
	return rows
}

// summarise builds the second line of a sub-entity's row from its first
// few properties, in sorted-key order (see sortedPropertyKeys). Which
// properties matter is the server's business; taking the first two is a
// presentation decision, not a semantic one, and the whole entity is one
// row-press away.
//
// The property already used as the row's label (skipKey) is skipped: on a
// screen this size, spending one of two summary slots repeating the
// heading is a waste of the only two facts the row can carry. Mirrors
// summarise() in render.js and _summarise() in render_service.dart.
func summarise(entity siren.Entity, skipKey string) string {
	parts := make([]string, 0, summaryProperties)
	for _, key := range sortedPropertyKeys(entity.Properties) {
		if key == skipKey {
			continue
		}
		if len(parts) >= summaryProperties {
			break
		}
		parts = append(parts, key+" "+Text(entity.Properties[key]))
	}
	return strings.Join(parts, "  ")
}

// entityRows renders each of entity's sub-entities as one Row -- embedded
// or reference, per siren.Entity.IsReference. Mirrors entityRows() in
// render.js and _entityRows() in render_service.dart.
func entityRows(entity siren.Entity) []Row {
	rows := make([]Row, 0, len(entity.Entities))
	for i, child := range entity.Entities {
		rows = append(rows, entityRow(i, child))
	}
	return rows
}

// entityRow renders one sub-entity, split out of entityRows to keep that
// loop body short.
func entityRow(index int, child siren.Entity) Row {
	// When the label came from a property, that property is redundant in
	// the summary below it. When the label came from the title instead,
	// nothing needs to be skipped.
	skipKey := ""
	if child.Title == "" {
		skipKey = child.Identifier()
	}
	label := child.Label()
	if label == "" {
		label = fmt.Sprintf("entity %d", index+1)
	}
	sublabel := ""
	var target RowTarget
	if child.IsReference {
		target = FetchTarget{Href: child.Href}
	} else {
		sublabel = summarise(child, skipKey)
		target = EmbeddedTarget{Index: index}
	}
	return Row{
		Label:    label,
		Sublabel: sublabel,
		Kind:     RowKindEntity,
		// An embedded entity is already in hand, so opening it costs
		// nothing and asks the server nothing. A reference is only an
		// href, so it has to be fetched -- Siren allows either, and the
		// difference is invisible here.
		Target: target,
	}
}

// linkRows renders each of entity's links as one Row. Mirrors linkRows()
// in render.js and _linkRows() in render_service.dart.
func linkRows(entity siren.Entity) []Row {
	rows := make([]Row, 0, len(entity.Links))
	for _, link := range entity.Links {
		rels := strings.Join(link.Rel, " ")
		label := link.Title
		if label == "" {
			label = rels
		}
		if label == "" {
			label = "link"
		}
		sublabel := ""
		if link.Title != "" {
			sublabel = rels
		}
		rows = append(rows, Row{
			Label:    label,
			Sublabel: sublabel,
			Kind:     RowKindLink,
			Target:   FetchTarget{Href: link.Href},
		})
	}
	return rows
}

// actionRows renders each action entity currently offers as one Row.
// Mirrors actionRows() in render.js and _actionRows() in
// render_service.dart.
func actionRows(entity siren.Entity) []Row {
	rows := make([]Row, 0, len(entity.Actions))
	for _, action := range entity.Actions {
		sublabel := action.Method
		if n := len(action.Fields); n > 0 {
			sublabel = fmt.Sprintf("%s, %d field(s)", action.Method, n)
		}
		rows = append(rows, Row{
			// The title is the server's own wording for what this does,
			// and it is always preferred: the name is an identifier, the
			// title is a sentence written for a person. Action.Label
			// already applies that order.
			Label:    action.Label(),
			Sublabel: sublabel,
			Kind:     RowKindAction,
			Target:   ActionTarget{Name: action.Name},
		})
	}
	return rows
}
