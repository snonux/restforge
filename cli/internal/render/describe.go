package render

import (
	"strings"

	"github.com/snonux/restforge/cli/internal/siren"
)

// Describe builds a one-line, generic summary of every property on
// entity -- "key: value" pairs joined by wide gaps of three spaces, each
// value through Text so a number, list or nested object renders the same
// way it does in the document itself. This is the body of an
// action-outcome banner (the message is the server's own "state"; this is
// the rest of what it sent). Mirrors describeEntity() in actions.js and
// describe() in render_service.dart, including its exact "   " (three
// regular spaces) separator.
//
// Like propertyRows and summarise in rows.go, this walks
// entity.Properties in sorted-key order rather than the server's own
// property order -- see sortedPropertyKeys for why that information is
// already gone by the time an Entity reaches this package.
func Describe(entity siren.Entity) string {
	keys := sortedPropertyKeys(entity.Properties)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+": "+Text(entity.Properties[key]))
	}
	return strings.Join(parts, "   ")
}
