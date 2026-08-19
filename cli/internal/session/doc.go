// Package session is the coordinator: the only package both the TUI and the
// one-shot CLI talk to. It composes internal/nav, internal/action,
// internal/live and internal/quick into the single state machine a caller
// drives, ported from flutter/lib/services/session.dart (itself the Dart
// port of pebble/src/pkjs/session.js -- see that file's header for the full
// reasoning, most of which carries over unchanged).
//
// # What belongs here, and why
//
// Split along the five concerns nav/action/live/quick each own, what is
// left here is what genuinely needs more than one of them:
//
//   - Activate, because a row's render.RowTarget can mean either "go
//     somewhere" (nav) or "ask about doing something" (action) -- deciding
//     which is the one thing that needs to know about both. A
//     render.DetailTarget is neither; it is a value already in hand, and --
//     exactly as session.js's activate() handles it inline rather than
//     asking nav to decide -- this package just opens it (see Activate).
//   - Everything internal/action's own package comment defers to a
//     coordinator: re-fetching the document an action was invoked from
//     (docs/DESIGN.md, "Never carry a document across an action"), and
//     handing a still-running response to internal/live instead of
//     reporting it as finished. This mirrors actions.js's
//     afterActionSuccess/afterActionError/liveHandlers -- in the watchapp
//     those lived inside actions.js because it could reach nav.js and
//     live.js directly; here internal/action and internal/live deliberately
//     hold no reference to either internal/nav or each other (see their own
//     package comments), so the wiring moves to this package, the
//     coordinator.
//   - SaveQuick and RunQuick, for the same reason: internal/quick only
//     stores and resolves a shortcut (its own package comment is explicit
//     that composing that with a fetch or an action is deliberately left to
//     a caller), and internal/nav/internal/action each know nothing of the
//     other or of internal/quick. Saving needs nav's notion of "the current
//     backend and document" to turn a pressed row into a quick.QuickItem;
//     running needs quick.BackendFor plus nav.Adopt/nav.Fetch plus, for an
//     action shortcut, the same askAction a hand-pressed
//     render.ActionTarget goes through -- so a shortcut gets exactly the
//     confirmation flow a hand-reached action would, never a silent invoke.
//
// # State management -- no observer
//
// Unlike session.dart (a ChangeNotifier the Flutter UI listens to), this
// package has no ChangeNotifier equivalent and does not need one: both
// internal/tui (Bubble Tea) and internal/cli (Cobra, one-shot -- both later
// tasks) call Session's methods synchronously, and each caller decides what
// to do with the resulting state. There is deliberately no observer or
// pub-sub layer here on spec -- Bubble Tea's own message loop already gives
// a TUI caller the state after each operation, and a one-shot CLI reads the
// return values directly. So Session's methods mutate its own fields in
// place and return, exactly the way internal/nav does (see its package
// comment on why no observer pattern is needed there either).
//
// The overlay state neither composed package has anywhere to put -- a
// DetailView opened for reading, a SessionQuestion awaiting an answer, and
// a SessionNotice reporting what the last action (or the job it started)
// produced -- lives on Session itself. nav.js could fold the equivalent of
// the last two onto the very next frame it sent (setNotice/applyNotice,
// overlay) because every navigation event rebuilt the frame from scratch;
// this port's fields persist until told otherwise, so clearTransient is
// this file's replacement for "a fresh frame has nothing on top of it" --
// called on every navigation event that changes which document is on
// screen, deliberately NOT called by the refresh an action's own outcome
// triggers, since that refresh is exactly the moment the notice it just set
// is meant to be seen next to.
//
// # Live watches and the two callers
//
// internal/live's Handlers (OnProgress/OnDone/OnGiveUp) are invoked
// synchronously, from whatever goroutine is driving the poll (the injected
// timer's callback in production, a fake timer's in tests). The handlers
// Session registers with live.Start mutate Session's own notice field and
// re-fetch via nav, the same wiring session.dart's liveHandlers do through
// notifyListeners -- minus the notification, which has no equivalent here.
//
// That is enough for this package, but it is NOT enough once a TUI caller
// wires Session into a Bubble Tea Program: a poll fires on its own goroutine
// while Update runs on another, so mutating shared UI state directly from a
// handler is a data race. internal/tui (a later task) adapts that by driving
// live's poll onto its single-threaded Update loop (for example by injecting
// a createTimer that posts a tea.Msg to trigger the poll), so the handlers
// run on the right goroutine. This package does not implement, and must not
// depend on, any part of that wiring itself.
//
// The one-shot CLI has no event loop to forward the callbacks onto, so a
// watchable action outcome must block until the job is done or given up
// rather than be reported through live.Start's callbacks. live.WaitForLive
// -- the blocking variant alongside Start and Stop -- does that, polling
// synchronously and returning the outcome directly.
//
// There is a composition question this package deliberately does NOT settle,
// because it belongs to the later internal/cli task, not this one:
// Session.handleSuccess already starts a callback watch via live.Start on
// every watchable outcome, and the (backend, origin, actionOutcome) the
// blocking watch needs are consumed inside that call rather than exposed.
// So a one-shot CLI cannot both go through Session.Answer/Activate (which
// start the callback watch) AND call live.WaitForLive on the same outcome
// without double-watching. internal/cli will resolve that one way or
// another -- an entry point on Session that returns the watch parameters
// instead of starting the callback watch, a Session-owned blocking
// wrapper, or routing watchable outcomes through live directly -- and this
// package exposes live.WaitForLive (and documents this open question) so
// that task has the primitive it needs. Session itself keeps the
// callback-based Start, which is what the ported tests drive via an
// injected fake timer and the handlers it fires, and what the TUI path
// uses once internal/tui adapts its threading.
//
// # What does not carry over from session.dart
//
// Beyond the AppMessage/PebbleKit layer the Dart port already dropped:
//
//   - The idle-refresh clock and setAppForeground. The Dart SessionService
//     owns an IdleRefreshClock and wires ActionService.hasPending into it at
//     construction; here the idle-refresh clock is a caller's job, not
//     this package's -- see internal/nav's package comment, which states the
//     clock is "a caller's job, composing this package". So there is no
//     clock, no setAppForeground, and no setActionPendingCheck wiring in
//     this port; the corresponding test group in session_test.dart is
//     deliberately not ported (see session_test.go's header).
//   - dispose and the "used after dispose" guard. With no ChangeNotifier
//     there is nothing to dispose, and with synchronous methods there is no
//     async tail landing after a caller has backed away -- so the
//     dispose-during-in-flight test (task 821 in the Dart history) is
//     deliberately not ported either, for the same reason internal/nav's
//     own tests drop it.
package session
