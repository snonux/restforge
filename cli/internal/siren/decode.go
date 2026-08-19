package siren

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Tolerant JSON coercion. Every one of these accepts whatever a lenient or
// broken server sent -- or a caller building an Entity by hand from
// something that turned out not to be a document at all -- and returns the
// "nothing offered" shape instead of panicking. Mirrors
// isObject/isArray/array() in siren.js and the leading underscore helpers
// in flutter/lib/models/siren.dart.
// ---------------------------------------------------------------------------

// asMap coerces v to a JSON-object map, or nil when v is not one. A nil
// map reads exactly like an empty one (index, range, len all work), so
// callers never need to check for nil before using the result.
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// asList coerces v to a JSON-array slice, or nil when v is not one.
func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// asStringList coerces a Siren rel/class member to a slice of strings.
// Siren defines both as arrays of strings, but a lenient server may send a
// bare string; a non-string entry inside an array simply never matches a
// lookup, exactly as it would not in has() in siren.js.
func asStringList(v any) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// asNonEmptyString reads v as a string member, treating both "wrong type"
// and "empty string" as "the server did not send this" -- see the note on
// the "" sentinel in doc.go.
func asNonEmptyString(v any) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return ""
}

// asMethod reads an action's HTTP method, defaulting to Siren's own
// default, GET, when the server did not say one explicitly. Always
// returned uppercase -- mirrors method() in siren.js: a server that means
// to change something says so explicitly.
func asMethod(v any) string {
	if s, ok := v.(string); ok && s != "" {
		return strings.ToUpper(s)
	}
	return "GET"
}

// identifierKey returns the identifyingProperties key that names
// properties, or "" when none of them are present with a usable value.
// "Usable" is a non-empty string or any JSON number (which decodes to
// float64) -- mirrors identifier() in siren.js.
func identifierKey(properties map[string]any) string {
	for _, key := range identifyingProperties {
		switch v := properties[key].(type) {
		case string:
			if v != "" {
				return key
			}
		case float64:
			return key
		}
	}
	return ""
}

// label is the precedence logic shared by Field.Label, Link.Label,
// Action.Label and Entity.Label -- mirrors label() in siren.js. Preference
// order is the server's own wording first (title), then the name Siren
// gives an action or field, then the node's own identifying property, then
// its vocabulary (class, then rel), then nothing. A caller never invents a
// name for a thing it does not understand, because an invented name is a
// claim about what the thing is.
//
// A caller that has no properties (Field, Link, Action) passes nil, which
// identifierKey reads exactly like an empty map, so no separate "properties
// were never offered" branch is needed the way flutter/lib/models/siren.dart
// needs one for its nullable parameter.
func label(title, name string, properties map[string]any, classes, rel []string) string {
	if title != "" {
		return title
	}
	if name != "" {
		return name
	}
	if key := identifierKey(properties); key != "" {
		// identifierKey has already established the value is a non-empty
		// string or a float64 (JSON has no separate integer type); %v
		// renders a whole-number float64 without a trailing ".0", and a
		// string as itself, matching value.toString() in the Dart port.
		return fmt.Sprintf("%v", properties[key])
	}
	if len(classes) > 0 {
		return strings.Join(classes, " ")
	}
	if len(rel) > 0 {
		return strings.Join(rel, " ")
	}
	return ""
}
