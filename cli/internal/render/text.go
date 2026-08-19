package render

import (
	"encoding/json"
	"fmt"
)

// Text renders one JSON value for display: nil as the word "null", a map
// or slice as its JSON encoding, and anything else through fmt.Sprint. A
// map or slice is JSON-encoded rather than summarised: a nested structure
// is rare in a property, and showing its JSON is at least true, where a
// bare Go %v of a map is not (it is not valid JSON, and -- see
// sortedPropertyKeys in rows.go -- Go's own map iteration order is
// unstable on top of that; encoding/json.Marshal sorts a map's string keys
// itself, so this is also the one place in this package that does not need
// to call sortedPropertyKeys by hand). Mirrors text() in render.js and
// text() in render_service.dart.
//
// The map and slice cases only match map[string]any and []any -- the two
// shapes encoding/json ever produces for a JSON object or array, and the
// same two shapes siren.Entity.Properties, siren.Field.Value and every
// other any-typed member in package siren is documented to hold. Any other
// value, including a different concrete map or slice type, falls through
// to fmt.Sprint like every other non-JSON value would.
//
// Exposed (not just used internally) so a future action pipeline can reuse
// exactly this function the way pebble/src/pkjs/actions.js reuses
// render.text when it echoes a filled-in field value back for
// confirmation, rather than a second copy of it.
func Text(value any) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case map[string]any, []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "(unreadable)"
		}
		return string(encoded)
	default:
		return fmt.Sprint(value)
	}
}
