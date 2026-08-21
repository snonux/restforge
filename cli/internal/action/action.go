package action

import (
	"sync"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// confirmationRetryTTL is how long a confirmation stays eligible for the
// one retry the contract allows. Mirrors CONFIRMATION_TTL_MS in actions.js
// and confirmationRetryTtl in action_service.dart.
const confirmationRetryTTL = time.Minute

// requester is the minimal seam Action needs against httpclient.Client --
// just Request, the one method this package calls. *httpclient.Client
// already satisfies this structurally (Go's implicit interface
// satisfaction), so production code passes one straight to New with no
// adapter; a test substitutes a fake, or (as this package's own tests do)
// a real *httpclient.Client pointed at an httptest.Server, the same way
// action_service_test.dart drives HttpService against a MockClient.
// Mirrors nav.httpGetter's reasoning for the same kind of seam.
type requester interface {
	Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error)
}

// pendingAction is the action awaiting confirmation. Carries the href and
// method a caller must never see -- see the package comment -- so it is
// deliberately unexported and never returned from, or accepted by, any
// exported method of Action.
type pendingAction struct {
	name   string
	href   string
	method string

	// awaitingField is set once a required field has been asked for out
	// loud and the pending question has changed from "yes/no" to "what
	// value" -- mirrors pending.awaiting in actions.js. Empty the rest of
	// the time.
	awaitingField string
}

// awaiting returns a copy of p with awaitingField set, once invoke finds
// exactly one field to ask for out loud.
func (p pendingAction) awaiting(fieldName string) pendingAction {
	p.awaitingField = fieldName
	return p
}

// confirmedRetry is a confirmation the user gave, kept just long enough to
// answer a server that comes back 409 after it.
//
// The contract allows this narrow case: an action whose required checkbox
// the user explicitly ticked can still come back 409, because the server
// judges the request twice against budgets that can change in between.
// Re-sending the same confirmed field once is legitimate; synthesising a
// confirmation nobody gave is not, which is why this records only what
// was actually confirmed (values, filled by FillFields with
// hasRequiredCheckbox already true), and only briefly
// (confirmationRetryTTL, checked by Action.retryable). Mirrors
// confirmedAt in actions.js and _ConfirmedRetry in action_service.dart.
type confirmedRetry struct {
	name   string
	href   string
	method string
	values map[string]string
	at     time.Time
}

// Action fills an action's fields, phrases its confirmation, and sends
// it.
//
// The requester is injected, exactly as nav.Nav is composed with an
// httpGetter -- production code passes a *httpclient.Client, a test
// substitutes a fake or a real client pointed at a loopback server.
// backend.Backend and the current siren.Entity are passed into Ask,
// Answer and AnswerValue by the caller rather than read from a held
// reference to nav.Nav: this package has no navigation state of its own
// (mirroring actions.js sitting on top of nav.js rather than reaching
// into it), and no coordinator wiring the two together exists yet -- see
// the package comment.
type Action struct {
	http requester
	log  func(message string)

	// now is the clock Action reads to place a confirmation's age against
	// confirmationRetryTTL. Injected the same way live_service_test.dart's
	// FakeClock is, so a test can place a confirmation's age exactly on
	// either side of confirmationRetryTTL without a real minute passing.
	now func() time.Time

	// mu guards pending and confirmedRetry below -- see r31 and the package
	// comment's "Concurrent callers" section for why this exists: a Bubble
	// Tea caller can have more than one goroutine calling into this same
	// *Action at once (two sessionCmd goroutines from a double keypress, or
	// a sessionCmd goroutine racing the Back key's direct, synchronous
	// call -- internal/tui/cmd.go's package comment and model.go's
	// handleBack), and without a lock that is a genuine data race on these
	// two fields, not just a theoretical one.
	mu sync.Mutex

	// pending is the action awaiting confirmation or a spoken value, or nil
	// when nothing is. Claimed atomically (takePendingIf) by Answer/
	// AnswerValue and compare-and-swapped (casPending) by invoke's own
	// FieldValueMissing branch -- see both methods' doc comments -- rather
	// than read and written directly, so two overlapping calls can never
	// both see the same question and both act on it.
	pending *pendingAction

	// confirmedRetry is the confirmation the user gave, kept just long
	// enough to answer a server that comes back 409 after it -- see
	// confirmedRetry and Action.retryable. Always read and written with mu
	// held; never touched while a.http.Request itself is in flight (send
	// sets it before, afterSend reads/clears it after -- see both).
	confirmedRetry *confirmedRetry

	// userValues is the caller-supplied field map the one-shot CLI passes
	// via --field key=value. It fills a field ahead of FillFields's
	// missing-required decision, so a required field the caller supplied
	// is never asked for out loud. Interactive callers (internal/session)
	// leave it nil; a one-shot caller sets it through [WithUserValues].
	userValues map[string]string
}

// Option configures an Action built by New. Mirrors httpclient.Option: the
// With* functions below are declared before New itself, the same order
// httpclient.go uses, since New's own doc comment ("with the production
// defaults... overridden by opts") reads naturally once a reader already
// knows what an Option can override.
type Option func(*Action)

// WithLog overrides where request/decision log lines go. Defaults to a
// no-op, mirroring debugPrint in action_service.dart being the production
// default and a capturing function being the test double.
func WithLog(logf func(message string)) Option {
	return func(a *Action) { a.log = logf }
}

// WithClock overrides the clock Action uses. Defaults to time.Now; a test
// injects one it can move by hand -- see the "bounded 409 retry" test
// cases, which place a conflict's response on either side of
// confirmationRetryTTL without sleeping a real minute.
func WithClock(now func() time.Time) Option {
	return func(a *Action) { a.now = now }
}

// WithUserValues sets the caller-supplied field values the one-shot CLI
// passes via --field key=value. They take precedence over a field's
// server-declared default (so a caller can override it) but not over a
// checkbox confirmation or a value asked for out loud (those are the
// user's explicit answers to a question, not a default to override). A
// one-shot Action is built per invocation, so this is set once per act.
// Interactive callers do not set it.
func WithUserValues(values map[string]string) Option {
	return func(a *Action) { a.userValues = values }
}

// New builds an Action that sends requests through client, with the
// production defaults (a no-op log, time.Now as the clock) overridden by
// opts -- mirrors httpclient.New's Option pattern.
func New(client requester, opts ...Option) *Action {
	a := &Action{
		http: client,
		log:  func(string) {},
		now:  time.Now,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// HasPending reports whether a question is currently awaiting an answer.
// Mirrors hasPending() in actions.js -- a future idle-refresh timer needs
// to know this before it re-fetches out from under a question nobody has
// answered yet, the same reason nav.js's idle refresh consults it there.
func (a *Action) HasPending() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending != nil
}

// CancelPending abandons whatever question is pending, without sending
// anything. Mirrors cancelPending() in actions.js -- called when the
// screen an action was offered on is left before it is answered, so the
// question does not outlive the document it was about. May race a
// concurrent Answer/AnswerValue/invoke -- see r31 -- so this only ever
// nils a.pending under the lock rather than touching anything an in-flight
// call has already captured into its own local variables.
func (a *Action) CancelPending() {
	a.mu.Lock()
	a.pending = nil
	a.mu.Unlock()
}

// casPending swaps a.pending from old to new, but only if a.pending is
// still old when the lock is taken -- discards new otherwise. Mirrors
// live.Live's isCurrent/stopIfCurrent pointer-identity check (poll.go),
// used here instead of nav.Nav's generation counter because pendingAction
// is itself Action's one per-call object to compare against, the same
// reason live compares *watch directly rather than keeping a counter of
// its own (see nav's package comment for why nav needs a counter where
// live does not).
//
// Needed because invoke's own FillFields decision (pure computation, no
// I/O) still straddles a window a concurrent call can land in: the Back
// key's CancelPending (handleBack, called directly on Update's own
// goroutine, never through sessionCmd) or a fresh Ask for a different
// action (a second Activate dispatched while this call's own sessionCmd
// goroutine has not returned yet) can each move a.pending on to something
// else -- nil, or a different question -- while this call is still
// deciding what its own outcome is. Writing that outcome over either would
// resurrect a question the user already cancelled, or clobber a different
// action's freshly-asked one; casPending's old check makes that a
// silent no-op instead, the same "stale, discard" contract nav's
// generation check and live's isCurrent enforce.
func (a *Action) casPending(old, new *pendingAction) {
	a.mu.Lock()
	if a.pending == old {
		a.pending = new
	}
	a.mu.Unlock()
}

// takePendingIf atomically claims the current pending action and clears
// it, but only when pred(a.pending) is true -- Answer accepts any pending
// action, AnswerValue only one already awaiting a spoken value
// (pendingAction.awaitingField != ""), matching the checks Answer/
// AnswerValue made directly against a.pending before r31.
//
// Atomic so two overlapping calls -- e.g. two rapid presses of Confirm's
// 'y' binding, each its own sessionCmd goroutine (internal/tui/cmd.go)
// dispatched before the first one's outcome has changed Session.Question()
// enough for the screen to move on -- cannot both see the same
// pendingAction and both send it: only the first to take the lock claims
// it here; the second finds pending already nil (or failing pred) and
// reports "nothing to do", exactly the existing single-threaded contract
// Answer/AnswerValue already promised their own callers, now actually true
// under concurrency too.
func (a *Action) takePendingIf(pred func(*pendingAction) bool) *pendingAction {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil || !pred(a.pending) {
		return nil
	}
	p := a.pending
	a.pending = nil
	return p
}

// retryable answers the narrow question the contract permits: did the
// user confirm *this* action, by ticking a required checkbox, within the
// last confirmationRetryTTL? Anything else -- no confirmation remembered,
// a confirmation for a different action, or one old enough to have
// expired -- is a no. Mirrors retryable() in actions.js.
func (a *Action) retryable(name string) bool {
	a.mu.Lock()
	retry := a.confirmedRetry
	a.mu.Unlock()
	if retry == nil || retry.name != name {
		return false
	}
	return a.now().Sub(retry.at) < confirmationRetryTTL
}

// hasRequiredCheckbox reports whether act has a required checkbox -- the
// one field type whose value *is* the user's confirmation (see
// fieldValues), and therefore the only case the bounded 409 retry applies
// to. A checkbox that exists but isn't required does not count, same
// distinction ConfirmationText draws. Mirrors hasRequiredCheckbox() in
// actions.js.
func hasRequiredCheckbox(act siren.Action) bool {
	for _, field := range act.Fields {
		if field.Type == "checkbox" && field.Required {
			return true
		}
	}
	return false
}
