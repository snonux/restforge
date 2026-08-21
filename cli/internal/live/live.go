package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// PollInterval is how often to ask. Ten seconds is frequent enough that a
// step change is visible while watching, and rare enough that a long job is
// not a hundred requests. Mirrors POLL_MS in live.js.
const PollInterval = 10 * time.Second

// BudgetBuffer is slack added to the server's own staleness budget,
// covering the poll interval and the round trip. Mirrors BUFFER_MS.
const BudgetBuffer = 60 * time.Second

// FallbackBudget is used only when no response has ever carried a budget to
// derive one from -- an older server, or a run of polls that all landed
// somewhere without the job. Generous on purpose: this can only make the
// client wait longer than necessary, never give up on a job that is still
// going. Mirrors FALLBACK_MS.
const FallbackBudget = 25 * time.Minute

// StateNone is the state a server reports for "there is no such job here".
// The one string in this file that names a value rather than a structure,
// and a guess that costs nothing if wrong: an unrecognised state that is
// not StateRunning simply ends the watch, which is the same thing that
// happens for any other terminal state. Mirrors STATE_NONE.
const StateNone = "none"

// StateRunning mirrors STATE_RUNNING.
const StateRunning = "running"

// Origin is the document an action was invoked from -- mirrors the origin
// argument ({ entity, href }) to start in live.js.
type Origin struct {
	Entity siren.Entity
	Href   string
}

// ActionOutcome is what an action's response looked like, as far as
// deciding whether to watch it is concerned -- mirrors the result argument
// ({ status, entity }) to start in live.js. Deliberately not
// internal/action's own InvokeOutcome type: this package receives an
// action's outcome as plain data (a status code and the entity that came
// back), not the action package's own vocabulary -- see the package
// comment on internal/action for why this stays decoupled from it.
type ActionOutcome struct {
	Status int
	Entity siren.Entity
}

// Handlers are the callbacks a watch reports through -- mirrors the
// handlers argument (onProgress, onDone, onGiveUp) to start in live.js.
// Each is invoked synchronously, from whatever goroutine is driving the
// poll -- see the package comment for what that means for a caller running
// inside a Bubble Tea program.
type Handlers struct {
	OnProgress func(entity siren.Entity)
	OnDone     func(entity siren.Entity)
	OnGiveUp   func()
}

// watch is the state of one in-progress watch -- mirrors the `watching`
// object in live.js. Not exposed: everything a caller needs is read
// through Live's own methods and the handlers it was started with.
//
// Every field here is only ever read or written from the goroutine
// currently running a poll for this watch (see poll.go): only one such
// goroutine is ever active for a given watch at a time, so these fields
// need no lock of their own -- unlike Live.current/Live.timer, which are
// read and written from arbitrary caller goroutines (Start/Stop/IsLive) as
// well, and so are guarded by Live.mu.
type watch struct {
	backend backend.Backend
	href    string

	// id is the job id this watch started with, or nil when the entity
	// carried none. Compared against later replies by relevant.
	id any

	// budget is the current deadline budget, re-derived by deadlineExceeded
	// (decide.go) on every poll that carries one. nil means "no budget seen
	// yet, use FallbackBudget".
	budget *time.Duration

	startedAt time.Time
	handlers  Handlers

	// ctx is this watch's own cancellable context, for the callback-based
	// poll (poll.go's poll) to pass into httpGetter.GetContext. Set once by
	// Start (paired 1:1 with the context.CancelFunc Live.cancel stores) and
	// never reassigned -- immutable per-watch data, same as href/backend/id
	// above, safe to read from the goroutine driving a poll for this watch
	// without a lock. Cancelled from a caller's goroutine, through
	// Live.cancel, by Stop or a superseding Start -- see clearLocked.
	//
	// nil for a watch WaitForLive builds: that blocking API is not
	// registered against Live.current (see WaitForLive's own doc comment
	// on why a concurrent Stop cannot reach it), so there is nothing for a
	// per-watch context to be cancelled by; blockPoll passes
	// context.Background() straight to GetContext instead of reading this
	// field. Never dereferenced on that path -- see blockPoll.
	ctx context.Context
}

// httpGetter is the minimal seam Live needs against httpclient.Client --
// just GetContext, the only method this package calls (poll.go's poll and
// waitforlive.go's blockPoll). *httpclient.Client already satisfies this
// structurally, so production code passes one straight to New with no
// adapter; a test substitutes a fake, the same pattern internal/nav uses
// for its own httpGetter seam.
//
// Takes a context (q31, following n31's httpclient/nav precedent): Start's
// watch carries its own cancellable context (see watch.ctx and Live.cancel
// below), cancelled by Stop or a superseding Start, so a poll's in-flight
// GET is actually aborted when the watch it belongs to ends -- not left
// running to its own httpclient.GetTimeout for nothing, the way it was
// before q31 (Stop only stopped scheduling the *next* poll; an already
// in-flight one kept going untouched).
type httpGetter interface {
	GetContext(ctx context.Context, be backend.Backend, href string) (httpclient.HTTPResponse, error)
}

// Timer is the seam Live needs against a scheduled, cancellable one-shot
// callback. *time.Timer (returned by time.AfterFunc) satisfies this
// structurally; a test substitutes a fake driven by hand instead of the
// wall clock -- see the injected clock/timer note on New.
type Timer interface {
	Stop() bool
}

// *time.Timer, as returned by the production default's time.AfterFunc,
// satisfies Timer without an adapter -- checked here at compile time so a
// future signature change to time.Timer.Stop is caught here, not by a
// test.
var _ Timer = (*time.Timer)(nil)

// Live follows work that outlives the request that started it.
//
// http is injected exactly as nav.Nav's httpGetter is. The clock (now) and
// timer factory (createTimer) are injected too, so a test can drive a
// 24-second server-side job or a 25-minute fallback deadline without an
// actual wall-clock wait -- mirrors the fake setTimeout/Date.now
// live_service_test.dart installs via LiveService's own now/createTimer
// constructor parameters.
type Live struct {
	http        httpGetter
	now         func() time.Time
	createTimer func(d time.Duration, callback func()) Timer
	log         func(message string)

	// mu guards timer, current and cancel -- the fields touched from a
	// caller's goroutine (Start, Stop, IsLive) as well as from a poll
	// goroutine (poll.go). See the package comment's Concurrency section.
	mu      sync.Mutex
	timer   Timer
	current *watch

	// cancel, when non-nil, cancels current's own context (current.ctx) --
	// the context the poll now in flight for current, if any, was issued
	// under. Set by Start alongside current and cleared by clearLocked,
	// mirroring nav.Nav.cancel: bumping/replacing current alone (what this
	// package already did before q31) only makes a stale poll's answer get
	// discarded once it lands (see isCurrent/stopIfCurrent in poll.go);
	// calling this is what actually aborts that poll's HTTP round trip
	// instead of leaving it running for up to httpclient.GetTimeout for
	// nothing. Calling an already-fired or nil cancel func is a safe no-op
	// (context.CancelFunc's contract), so clearLocked can call it
	// unconditionally whenever it is set, whether or not a poll happened to
	// be in flight at the time.
	cancel context.CancelFunc
}

// Option configures a Live built by New.
type Option func(*Live)

// WithNow overrides the clock. For tests only -- production code relies on
// the time.Now default.
func WithNow(now func() time.Time) Option {
	return func(l *Live) { l.now = now }
}

// WithCreateTimer overrides the timer factory. For tests only -- production
// code relies on the time.AfterFunc default, so a test can drive a watch
// through minutes of deadline without an actual wait.
func WithCreateTimer(createTimer func(d time.Duration, callback func()) Timer) Option {
	return func(l *Live) { l.createTimer = createTimer }
}

// WithLog overrides where log lines go. Defaults to a no-op, mirroring
// debugPrint being the production default in live_service.dart and a
// capturing function being the test double.
func WithLog(logf func(message string)) Option {
	return func(l *Live) { l.log = logf }
}

// New builds a Live that performs its polls through client, with the
// production defaults for clock and timer, overridden by opts.
func New(client httpGetter, opts ...Option) *Live {
	l := &Live{
		http: client,
		now:  time.Now,
		createTimer: func(d time.Duration, callback func()) Timer {
			return time.AfterFunc(d, callback)
		},
		log: func(string) {},
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// IsLive reports whether a watch is currently running. Mirrors isLive.
func (l *Live) IsLive() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current != nil
}

// Start begins watching, if there is anything to watch. Returns true when a
// watch was started, so the caller can tell "in progress" from "finished".
// Mirrors start.
func (l *Live) Start(be backend.Backend, origin Origin, result ActionOutcome, handlers Handlers) bool {
	l.Stop()
	if !ShouldWatch(result) {
		return false
	}

	href := PollTarget(origin.Entity, result.Entity)
	if href == "" {
		// Only a document the server pointed us at will do. Falling back
		// to the origin document looks helpful and is not -- see the
		// package comment.
		l.log("live: nothing the server offers tracks this, not watching")
		return false
	}

	// ctx is this watch's own cancellable context (see watch.ctx and
	// Live.cancel) -- rooted at context.Background() rather than derived
	// from anything a caller passed in, since Start's own signature has no
	// context parameter to derive from (mirrors nav.beginFetchLocked doing
	// the same for the same reason).
	ctx, cancel := context.WithCancel(context.Background())
	w := &watch{
		backend:   be,
		href:      href,
		id:        result.Entity.Properties["id"],
		budget:    budgetFor(result.Entity),
		startedAt: l.now(),
		handlers:  handlers,
		ctx:       ctx,
	}
	l.logStart(href, w)

	l.mu.Lock()
	l.current = w
	l.cancel = cancel
	l.timer = l.createTimer(PollInterval, l.poll)
	l.mu.Unlock()
	return true
}

// logStart writes Start's "now watching" log line. Split out of Start to
// keep that function focused on the decision to watch, not on formatting
// what it decided.
func (l *Live) logStart(href string, w *watch) {
	id := "(none)"
	if w.id != nil {
		id = fmt.Sprintf("%v", w.id)
	}
	budget := "default"
	if w.budget != nil {
		budget = fmt.Sprintf("%dms", w.budget.Milliseconds())
	}
	l.log(fmt.Sprintf("live: watching %s id %s, budget %s", href, id, budget))
}

// Stop stops watching, if anything was. Mirrors stop. Idempotent, same as
// the original: called both to tear down an old watch before Start begins a
// new one, and directly by a caller that navigated away.
func (l *Live) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearLocked()
}

// clearLocked cancels the pending timer and the current watch's context, if
// either is set, and clears the current watch. l.mu must be held by the
// caller.
//
// Cancelling l.cancel here -- not just nilling l.current -- is what makes
// Stop (and Start's own call into this via the superseding path) actually
// abort a poll already in flight for the watch being cleared, rather than
// only ensuring its answer gets discarded once it lands (isCurrent/
// stopIfCurrent in poll.go already did that before q31) -- see Live.cancel.
func (l *Live) clearLocked() {
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.current = nil
}
