package nav

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/failure"
)

// DocumentState is what is on screen right now, independent of *which*
// document it is. Mirrors DocumentState in nav_service.dart and the
// STATE_* constants in nav.js, minus STATE_NEEDS_CONFIG -- see the package
// comment for why that state does not carry over here.
type DocumentState int

const (
	// StateOK: the document in Nav.Document is exactly what the server
	// last sent for it.
	StateOK DocumentState = iota

	// StateLoading: a fetch is in flight. Nav.Document still holds
	// whatever was there before -- see the package comment on why this
	// port does not blank the screen the way nav.js's sendLoading does.
	StateLoading

	// StateError: the last fetch failed for a reason that says nothing
	// about whether the document in Nav.Document is still accurate, but
	// is not a pure connectivity problem either (auth, conflict, server,
	// client, parse or config -- see failure.Kind). Nav.Failure carries
	// the reason.
	StateError

	// StateUnreachable: the last fetch never got an answer at all --
	// failure.Unreachable or failure.Timeout. Kept distinct from
	// StateError because "I could not ask" and "the answer was no" are
	// different facts, and only one of them is about the server (see
	// docs/DESIGN.md).
	StateUnreachable
)

// String names a DocumentState for logs, mirroring failure.Kind.String --
// also how a test or printer enumerates the closed set.
func (s DocumentState) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateLoading:
		return "loading"
	case StateError:
		return "error"
	case StateUnreachable:
		return "unreachable"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// StateFor maps a fetch failure's Kind onto a DocumentState -- mirrors
// stateFor in nav_service.dart and nav.js. Every Kind other than
// failure.Unreachable/failure.Timeout becomes StateError; see the package
// comment for why failure.Config does not get a state of its own here the
// way it does on the watch (STATE_NEEDS_CONFIG).
func StateFor(kind failure.Kind) DocumentState {
	if kind == failure.Unreachable || kind == failure.Timeout {
		return StateUnreachable
	}
	return StateError
}
