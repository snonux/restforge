package action

import (
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

	pending *pendingAction

	// confirmedRetry is the confirmation the user gave, kept just long
	// enough to answer a server that comes back 409 after it -- see
	// confirmedRetry and Action.retryable.
	confirmedRetry *confirmedRetry
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
	return a.pending != nil
}

// CancelPending abandons whatever question is pending, without sending
// anything. Mirrors cancelPending() in actions.js -- called when the
// screen an action was offered on is left before it is answered, so the
// question does not outlive the document it was about.
func (a *Action) CancelPending() {
	a.pending = nil
}

// retryable answers the narrow question the contract permits: did the
// user confirm *this* action, by ticking a required checkbox, within the
// last confirmationRetryTTL? Anything else -- no confirmation remembered,
// a confirmation for a different action, or one old enough to have
// expired -- is a no. Mirrors retryable() in actions.js.
func (a *Action) retryable(name string) bool {
	retry := a.confirmedRetry
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
