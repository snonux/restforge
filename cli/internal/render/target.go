package render

import "github.com/snonux/restforge/cli/internal/backend"

// RowTarget is what activating a Row does. This is Go's nearest equivalent
// to the sealed RowTarget hierarchy in render_service.dart: an unexported
// marker method closes the interface to the four types defined in this
// file, so nothing outside this package can add a fifth. The UI layer
// never reads inside one of these -- it hands the whole RowTarget to the
// code that knows what to do with it.
//
// Go has no "sealed" keyword and no compiler check that a type switch over
// an interface covers every implementation the way Dart's analyzer checks
// a switch over a sealed class. So every switch over a RowTarget anywhere
// in this codebase must carry a default case that panics with an
// unreachable-style message. That panic is a deliberate canary, not
// defensive clutter: it turns a fifth RowTarget implementation added later
// with a forgotten case into a loud failure the first time that code path
// runs, instead of a silently-wrong fallthrough. See targetKind in
// render_test.go for the one such switch this package's own code contains.
type RowTarget interface {
	isRowTarget()
}

// DetailTarget opens the full value in a reading view. It carries the full
// value, not the (possibly truncated) Row.Sublabel: this target exists for
// values too long to read on the row itself.
type DetailTarget struct {
	Heading string
	Body    string
}

func (DetailTarget) isRowTarget() {}

// FetchTarget follows an href -- a link, or a sub-entity that is only a
// reference.
//
// Backend is the backend the document this target came from was rendered
// against -- populated by Document/entityRow/linkRows from whichever
// backend.Backend the caller passed in, itself read from the same nav.Nav
// frame the row's own href was captured from (see nav.frame.be). Carrying it
// here, rather than leaving the caller to re-read whatever backend happens
// to be current when the target is finally activated, is what lets
// nav.Fetch/nav.Refresh pin their GET to the backend href actually belongs
// to instead of racing nav.Nav.current -- see p31 and nav.fetch's own doc
// comment for the gap this closes.
type FetchTarget struct {
	Backend backend.Backend
	Href    string
}

func (FetchTarget) isRowTarget() {}

// EmbeddedTarget opens a sub-entity already embedded in the document in
// hand, by its index into the parent Entity's Entities slice. Opening it
// costs nothing and asks the server nothing, unlike FetchTarget -- Siren
// allows a sub-entity to be either, and the difference is invisible past
// this point.
type EmbeddedTarget struct {
	Index int
}

func (EmbeddedTarget) isRowTarget() {}

// ActionTarget performs a named action.
type ActionTarget struct {
	Name string
}

func (ActionTarget) isRowTarget() {}
