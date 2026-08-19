package siren

import (
	"encoding/json"
	"fmt"

	"github.com/snonux/restforge/cli/internal/failure"
)

// ParseDocument decodes a Siren document from an HTTP response body.
//
// Never panics. Invalid JSON syntax and a body whose top level is not a
// JSON object both come back as a *failure.Failure with Kind Parse -- a
// document the caller can render a reason for, not a panic unwinding past
// the last good document still on screen (see docs/DESIGN.md, "A failed
// request is not an answer"). A well-formed object missing links, actions,
// sub-entities or properties is not a failure; EntityFromJSON treats those
// exactly like every other accessor in this package -- as simply absent.
//
// Mirrors parseSirenDocument's never-throws contract in
// flutter/lib/models/siren.dart. siren.js has no equivalent: it is only
// ever handed an already-decoded object, since pebble/src/pkjs/http.js
// owns JSON.parse there. Here the JSON-text boundary lives in this
// package instead, so its never-panic guarantee is tested at that boundary
// too.
func ParseDocument(body []byte) (Entity, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Entity{}, &failure.Failure{
			Kind:    failure.Parse,
			Message: fmt.Sprintf("invalid JSON: %v", err),
		}
	}
	document, ok := decoded.(map[string]any)
	if !ok {
		return Entity{}, &failure.Failure{
			Kind:    failure.Parse,
			Message: fmt.Sprintf("expected a JSON object, got %T", decoded),
		}
	}
	return EntityFromJSON(document), nil
}
