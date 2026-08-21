// Package live watches something that is still happening.
//
// This is the Go port of flutter/lib/services/live_service.dart, itself the
// Dart port of pebble/src/pkjs/live.js -- see that file's header and
// docs/DESIGN.md ("Do not claim a job finished") for the full reasoning.
// Some actions do not finish when the request does. A server that answers
// 202, or answers with an entity that says it is still running, is telling
// the client to come back and look -- and until it stops saying that, the
// screen is showing something that is no longer true.
//
// Everything here is generic, and three details are worth restating because
// each one is a way a naive poller gets it wrong:
//
//   - Where to poll. If the document the action came from has a link whose
//     rel matches one of the returned entity's classes, that link is the
//     thing to watch -- PollTarget does ordinary rel/class matching, an
//     ordinary hypermedia idiom that needs no knowledge of what the resource
//     is called. Failing that, Start refuses to watch at all rather than
//     falling back to the origin document: the origin does not report the
//     job's state, so the first poll would read its silence as completion
//     and announce the work had finished seconds after it started.
//
//   - Which answer is ours. A load-balanced deployment can route a poll to
//     a machine that never saw the job. Such a reply is "no news", not "no
//     job": relevant skips a response whose id differs from the one the
//     watch started with, or whose state is the server's word for
//     nothing-here, rather than treating it as completion. Reading it as
//     completion is how a client reports a job as finished seconds after
//     starting it.
//
//   - How long to wait. The deadline comes from the server's own
//     staleAfterSeconds and is re-derived on every poll that carries one,
//     never fixed when polling began -- an early poll that landed on a
//     machine with no job carries no budget to derive one from, and a later
//     one will. FallbackBudget is deliberately generous, because giving up
//     early on a job that is still running is worse than waiting. A failed
//     poll is news about the network, not the job, and is never folded into
//     a deadline check that also decides completion. Giving up is reported
//     through Handlers.OnGiveUp, distinctly from both success
//     (Handlers.OnDone) and a network failure, which is not reported to the
//     handlers at all: it is only ever "still watching, ask again".
//
// # Concurrency
//
// Unlike live_service.dart, which runs on Dart's single-threaded event
// loop, a *Live here drives its polling off a real (or injected) timer that
// fires its callback on its own goroutine, while Start/Stop/IsLive may be
// called concurrently from a caller's goroutine. A mutex guards the shared
// "which watch is current" state, and every terminal transition (finishing,
// giving up, rescheduling) re-checks that the watch driving it is still the
// current one before mutating state or firing a handler -- see poll.go. No
// exported method blocks while holding that mutex: the HTTP round trip
// happens with it released, and every Handlers callback is invoked outside
// it too, so a handler is free to call Stop on the very *Live that invoked
// it without deadlocking.
//
// Since q31, that "still current" check is a backstop, not the only guard:
// each watch also carries its own cancellable context (see watch.ctx and
// Live.cancel in live.go), cancelled by Stop and by a superseding Start, so
// a poll's in-flight GET is actually aborted when the watch it belongs to
// ends, rather than merely having its eventual answer discarded -- the
// generation-cancellation pattern cli/internal/nav's own httpGetter uses,
// applied here against *watch identity instead of a counter, since Live
// already had exactly one watch "current" at a time to key off of.
//
// # A note for whoever wires this into a Bubble Tea program
//
// Handlers.OnProgress, OnDone and OnGiveUp are invoked synchronously, from
// whatever goroutine is driving the poll (the injected timer's callback in
// production). That is enough at this layer, but it is NOT enough once a
// caller in internal/session or internal/tui wires this into a Bubble Tea
// Program: Bubble Tea's Update loop is single-threaded, and mutating shared
// UI state directly from one of these callbacks -- from a goroutine Update
// never scheduled -- is a data race on that state. That future caller must
// have its handlers do nothing but send a tea.Msg through the Program (via
// Program.Send), and let Update apply the result on its own goroutine, the
// same way any other background work reports into a Bubble Tea program.
// This package does not implement, and must not depend on, any part of
// that wiring itself.
package live
