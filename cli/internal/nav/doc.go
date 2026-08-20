// Package nav owns the navigation stack, document fetching, and the
// four-state document machine.
//
// This is the Go port of the navigation-and-fetching half of
// flutter/lib/services/nav_service.dart (itself the Dart port of the same
// half of pebble/src/pkjs/nav.js -- see that file's header for the full
// reasoning, most of which carries over unchanged). A client following
// hypermedia links has one address book, the stack of documents it walked
// through to get here, and one question worth asking on every fetch: did the
// last one land, and if not, does that mean anything about the document
// already on screen?
//
// # The invariant this package exists to protect
//
// (docs/DESIGN.md, "A failed request is not an answer"): a failure never
// replaces the document on screen with an empty one. Nav.Document always
// reads the last stack frame that actually arrived; Nav.State and
// Nav.Failure are separate, layered fields about what is happening *to* it
// -- a loading fetch or a failed one never touches the stack at all, so a
// caller that keeps rendering the last Document while State is StateLoading
// or an error kind never has a moment where the screen goes blank.
//
// # The four states this package owns
//
// StateOK, StateLoading, StateError and StateUnreachable -- StateFor maps
// every failure.Kind onto one of the latter two. nav.js has a fifth,
// STATE_NEEDS_CONFIG, whose only purpose is to route the watch to a
// dedicated "open the Pebble app" screen when a request cannot even be
// built (a bad auth header name) or no backend is configured at all.
// Neither concern belongs to this package: an unconfigured backend never
// reaches Nav because there is nothing to open yet (a caller's backend
// picker owns that empty state), and a local configuration failure
// (failure.Config) is still, from this package's point of view, "the fetch
// did not produce a document" -- so it is folded into StateError rather
// than kept as a fifth state with no screen of its own to route to.
//
// # No observer pattern
//
// nav_service.dart is a ChangeNotifier because Flutter's state-management
// default asks for one. Go and Bubble Tea have no equivalent framework
// coupling to satisfy at this layer, so Nav adds none: every method simply
// mutates the Nav's own fields, and the caller -- a Bubble Tea Update
// handler or a one-shot CLI command -- reads the resulting State, Document
// and Failure directly once the call, or the goroutine wrapping it,
// completes. There is no pub-sub layer here; Bubble Tea's own message loop
// already gives every caller the state after each operation.
//
// No observer pattern is not the same as no synchronization, though: a
// Bubble Tea caller's own render goroutine can call these accessors while a
// mutating method is still running on a different goroutine (bubbletea runs
// each tea.Cmd on its own goroutine and keeps calling View while it is in
// flight -- see internal/tui/cmd.go's package comment for the concrete
// mechanism). Nav's own sync.RWMutex guards every field for exactly that
// reason: a mutating method takes the write lock only around its actual
// field writes, never across the HTTP call in between, so a concurrent
// accessor call sees a consistent snapshot -- the state before the call, or
// the state after -- never a torn read, and rendering is never blocked for
// the duration of a fetch.
//
// # Stale fetches must not win
//
// The same "HTTP call runs with the lock released" design that keeps
// rendering unblocked opens a second, easy-to-miss problem: nothing stops a
// *second* mutating call -- a Back, a second Fetch, an OpenRoot for a
// different backend -- from running to completion while the first one's GET
// is still in flight. Without a guard, the first call's response lands
// later and unconditionally appends/replaces a frame on n.stack as it
// stands at that moment, silently undoing whatever the second, more recent
// call did (see internal/tui/cmd.go's package comment for why two such
// calls can genuinely overlap in this app, not just in theory). Every
// mutating method here bumps Nav.generation as the first thing it does
// under the lock; fetch() and OpenRoot capture the value that bump produced
// before releasing the lock for their own HTTP call, and check it again
// before applying that call's result -- discarding it if some other call
// has bumped generation in the meantime. Mirrors internal/live's
// isCurrent/stopIfCurrent/scheduleIfCurrent pattern (see live/poll.go),
// adapted to a counter since Nav, unlike Live, has no single per-call
// object to compare pointer identity against.
//
// OpenRoot's own chained fetch -- followStart, when be.StartRel is
// configured -- is not just "another fetch() call": it threads OpenRoot's
// gen through explicitly (fetch.go, followStart) and re-checks it before
// even starting its own GET, rather than letting a generic fetch() read
// n.current fresh. Reading n.current there would be wrong for a more
// subtle reason than staleness alone -- if a second OpenRoot for a
// different backend has already run by the time followStart's own GET
// would start, n.current no longer names the backend be.StartRel's href
// belongs to, and sending that href to whatever backend happens to be
// current now would resolve a relative URL against the wrong BaseURL and
// send the wrong backend's auth secret. followStart's own doc comment
// covers this in full.
//
// # What this package does not own
//
// Left to their own packages exactly as nav_service.dart's own module
// comment maps them: the action policy -- filling an action's fields,
// confirming it, retrying a 409 -- and everything to do with a notice laid
// on top of a frame by an action's outcome (internal/action's job);
// following work that outlives its request (internal/live); the backend
// picker itself and the idle-refresh clock (a caller's job, composing this
// package with the other two). Adopt exists for exactly one caller outside
// this package: a saved-shortcut jump, mirroring nav.js's own
// adopt/openQuickDocument/openQuickHolder.
package nav
