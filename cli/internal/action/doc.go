// Package action is the Siren action policy: the safe/unsafe method split,
// filling an action's fields, phrasing the confirmation the user is asked
// for, and invoking it.
//
// This is the Go port of the parts of pebble/src/pkjs/actions.js that
// flutter/lib/services/action_service.dart already ported to Dart -- see
// that file's module comment for the full reasoning, most of which carries
// over unchanged and is repeated here only where the port itself needed a
// decision:
//
// Never carry a document across an action (docs/DESIGN.md, same section):
// sending a request is the last thing this package does with the document
// it was invoked from -- InvokeSucceeded carries the raw
// httpclient.HTTPResponse, never a parsed, ready-to-render siren.Entity, so
// nothing here can tempt a caller into showing the action's own response in
// place of re-fetching. The one exception the contract allows for a 409 is
// a required checkbox the user actually ticked, re-sent once, within a
// minute -- implemented entirely inside Action's private invoke/send
// machinery (see confirmedRetry and Action.retryable), so the retry never
// reaches a caller as a separate step: by the time InvokeSucceeded or
// InvokeFailed comes back, the one retry the contract allows has already
// happened, and that outcome is final. A 409 that is not eligible for it is
// reported the same as any other failure -- re-fetching what it means is
// still the caller's decision, but retrying the exact same request is never
// on the table again.
//
// Do not invent a value (docs/DESIGN.md): a required field with no default
// and no confirmation to stand in for it is asked for out loud, or the
// action is refused -- never guessed at. FillFields is where that decision
// is made: a checkbox fills itself from the confirmation already given, a
// field with a server-supplied default takes it as-is, and a required field
// with neither becomes FieldValueMissing (the first one) or FieldsRefused
// (a second one -- dictating several fields one at a time is worse than
// saying plainly this cannot be done without asking). Action.AnswerValue is
// the reply to that question, mirroring answerSpoken() in actions.js. Only
// siren.Field.Required makes any of this possible to tell apart from an
// optional field with no value -- see internal/siren for how the server
// signals it.
//
// Ask before acting (docs/DESIGN.md): anything whose method is not in
// SafeMethods gets a confirmation before anything is sent. IsSafeMethod is
// RFC 9110's safe/unsafe division, not a blocklist of scary-sounding action
// names -- an action called "detonate" over GET is still safe by this rule,
// and one called "refresh" over POST still asks.
//
// The pending action never reaches the caller. Ask and Answer hand back a
// rendered ConfirmationRequired -- a heading and a body sentence, both
// plain strings -- never the href or method behind it. Those stay in
// Action's own private pendingAction state, for the same reason the watch
// never learns an href in the Pebble build: the caller only has to be
// trusted with what it shows on screen, not with an address it could
// otherwise be tricked into hitting.
//
// What this package does not own. Sending the request is the last thing
// this package does with it -- deciding what happens next (re-fetching the
// document the action was invoked from, watching a 202/still-running reply)
// is a coordinator's job, mirroring session.js wiring nav.js and actions.js
// together on the watch side. That coordinator does not exist yet in this
// client, so InvokeOutcome simply reports what came back and leaves the
// next step to whoever calls Action.
//
// This package depends only on internal/siren, internal/httpclient,
// internal/backend and internal/failure -- it never imports internal/nav or
// internal/render, mirroring action_service.dart taking a Backend and an
// Entity from its caller rather than holding a reference to NavService.
//
// # Concurrent callers (r31)
//
// action_service.dart has exactly one caller at a time by construction --
// Flutter's widget tree calls into it on its own single UI isolate. This Go
// port's caller (internal/tui, via internal/session) has no equivalent
// guarantee: internal/tui/cmd.go's sessionCmd runs every Session method
// that does I/O (Activate, Answer, AnswerValue) on its own goroutine that
// bubbletea does not wait for, and nothing gates a second keypress from
// dispatching another one before the first has returned -- Confirm's own
// 'n' binding and the shell's global Back key go further still, calling
// Session.Answer(false)/Back directly and synchronously on Update's own
// goroutine (see confirm_update.go and model.go's handleBack), so a
// sessionCmd goroutine and Update's own can both be inside this same
// *Action at once, not only two sessionCmd goroutines racing each other.
//
// Two overlapping calls into the same *Action are therefore not
// theoretical: a double press of Confirm's 'y' binding before the first
// press's HTTP round trip returns, or a 'y' press racing the Back key's
// direct Answer(false), each spawn or run a call into Answer/AnswerValue/
// Ask concurrently with another. Action.pending and Action.confirmedRetry
// guard against this with a sync.Mutex (Action.mu) plus two small
// primitives building on it: takePendingIf atomically claims and clears
// a.pending (so two overlapping Answer/AnswerValue calls cannot both see
// the same question and both send it -- only the first to the lock wins;
// the second gets the same nil these methods already returned for "nothing
// to do"), and casPending compare-and-swaps a.pending by pointer identity
// before writing the "now awaiting a value" pendingAction FieldValueMissing
// produces, discarding that write if a concurrent CancelPending or a fresh
// Ask has already moved a.pending on to something else in the meantime --
// mirrors live.Live's isCurrent/stopIfCurrent pointer-identity check
// (internal/live/poll.go) rather than nav.Nav's generation counter, since
// pendingAction is itself Action's one per-call object to compare against,
// the same reason live needs no counter of its own either. See
// takePendingIf's and casPending's own doc comments (action.go) for the
// concrete race each closes, and internal/action/race_test.go for the
// -race-provable regression tests: reverting either primitive and
// rerunning that file with -race reliably reports a race.
package action
