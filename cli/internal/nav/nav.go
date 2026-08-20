package nav

import (
	"context"
	"sync"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// httpGetter is the minimal seam Nav needs against httpclient.Client --
// just GetContext, the only method any method in this package calls.
// *httpclient.Client already satisfies this structurally (Go's implicit
// interface satisfaction), so production code passes one straight to New
// with no adapter; a test substitutes a fake that implements just this one
// method, sidestepping the need to run every case over a real (or
// httptest) socket the way httpclient_test.go does -- that package is
// testing the transport itself, this one is not.
//
// Takes a context (n31): every fetch this package starts is cancellable,
// and the context Nav passes in is the one it cancels itself when a later
// call supersedes that fetch -- see beginFetchLocked and
// Nav.supersedeLocked. That is what lets a superseded fetch's underlying
// HTTP round trip actually be aborted, not just have its result discarded
// once it lands (which the generation guard already did before n31, and
// still does as a backstop for a round trip that was already past
// cancellation when it was superseded).
type httpGetter interface {
	GetContext(ctx context.Context, be backend.Backend, href string) (httpclient.HTTPResponse, error)
}

// Nav owns the navigation stack and every fetch that changes it -- see the
// package comment. Constructed with an httpGetter (in production, a
// *httpclient.Client); every method mutates Nav's own fields in place and
// returns once the fetch it performs has landed, deliberately with no
// notification of its own -- see the package comment on why no observer
// pattern is needed here.
type Nav struct {
	http httpGetter

	// mu guards every field below. It exists because a caller's cmd
	// goroutine (see internal/tui/cmd.go's sessionCmd) mutates these fields
	// while that same caller's render goroutine may be reading them through
	// the accessors below at the same time -- see the package comment's "No
	// observer pattern" section for why that is a real, not theoretical,
	// concurrent access.
	mu      sync.RWMutex
	stack   []frame
	current backend.Backend
	state   DocumentState
	failure *failure.Failure

	// generation is bumped by every call that may invalidate an in-flight
	// fetch: fetch() (Fetch/Refresh), followStart and OpenRoot all bump it
	// themselves for their own GET, and OpenRoot, Adopt, Back and
	// OpenEmbedded also bump it as the first thing they do under the lock
	// even when they perform no I/O of their own, purely to invalidate
	// whatever fetch might already be in flight. fetch() and followStart
	// (through the fetchAndApply helper both funnel into) capture the
	// value their own bump produced before releasing the lock for their
	// HTTP round trip, then compare it against the live value once the
	// round trip returns; a mismatch means some other call ran in the
	// meantime (a second Fetch/OpenRoot, or a synchronous Back/Adopt/
	// OpenEmbedded that reset the stack), so this response is stale and
	// must be discarded rather than applied on top of state it no longer
	// corresponds to. Mirrors internal/live's
	// isCurrent/stopIfCurrent/scheduleIfCurrent pattern, using a counter
	// instead of pointer identity since Nav has no single per-call object
	// (like live's *watch) to compare against -- every mutating call
	// shares the same n.stack/n.current instead.
	generation uint64

	// cancel, when non-nil, cancels the context the fetch currently in
	// flight was issued under -- see beginFetchLocked and supersedeLocked.
	// Bumping generation alone (which every superseding call already did
	// before n31) only makes a stale response get discarded once it lands;
	// storing and calling this is what actually aborts that response's HTTP
	// round trip instead of leaving its goroutine and socket running for up
	// to httpclient.GetTimeout/ActionTimeout for nothing -- see n31.
	//
	// Set only by beginFetchLocked (paired 1:1 with the generation it
	// returns), and cleared in exactly two ways: supersedeLocked calling and
	// nilling it when a later call invalidates the fetch it belongs to, or
	// releaseCancelLocked calling and nilling it once that same fetch lands
	// on its own -- still current, neither superseded nor invalidated. Every
	// site that applies a fetch's outcome (pushIfCurrent, applyFailureIfCurrent,
	// fetchAndApply's own success tail) calls releaseCancelLocked once it has
	// confirmed gen is still current, so this never sits non-nil pointing at
	// a fetch that has already finished -- non-nil always means "a fetch is
	// genuinely in flight right now."
	cancel context.CancelFunc
}

// New builds a Nav that performs its fetches through client. Starts with no
// backend open and Document/Entity/Href all reporting "nothing yet" -- see
// their own doc comments.
func New(client httpGetter) *Nav {
	return &Nav{http: client, state: StateOK}
}

// Backend is the backend currently open, or the zero value before OpenRoot
// or Adopt has ever been called. A live accessor rather than a value handed
// out once -- mirrors currentBackend in nav.js, kept for the same reason: a
// future caller (the action pipeline) needs whichever backend is current at
// the moment it sends a request, not whichever one was current when it
// first asked.
func (n *Nav) Backend() backend.Backend {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.current
}

// State is what is on screen right now. See the package comment for why
// this never changes for a loading fetch or a failed one -- only State and
// Failure do.
func (n *Nav) State() DocumentState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// Failure is why State is StateError or StateUnreachable. nil whenever
// State is StateOK or StateLoading.
func (n *Nav) Failure() *failure.Failure {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.failure
}

// Document is the document to render: the last entity that was actually
// fetched or opened, turned into rows. nil only before the first document
// has ever arrived, or right after OpenRoot has reset the stack for a new
// backend and before its root has landed -- there is nothing to keep
// showing at that point because nothing has been shown yet.
func (n *Nav) Document() *render.RenderedDocument {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.stack) == 0 {
		return nil
	}
	top := n.stack[len(n.stack)-1]
	doc := render.Document(&top.entity, top.title, top.be)
	return &doc
}

// Entity is the raw document on top of the stack, before Document turns it
// into rows -- what internal/action needs to look an action up by name and
// what internal/live needs to match a poll target against. nil under the
// same conditions Document is.
func (n *Nav) Entity() *siren.Entity {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.stack) == 0 {
		return nil
	}
	e := n.stack[len(n.stack)-1].entity
	return &e
}

// Href is the href Entity can be re-fetched from, or "" for a sub-entity
// that arrived embedded rather than linked, or before anything is open --
// same nullability as frame.href, and added for the same reason as Entity.
func (n *Nav) Href() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.stack) == 0 {
		return ""
	}
	return n.stack[len(n.stack)-1].href
}

// CanGoBack is true once there is a document below the one on screen to pop
// back to with Back. False for the backend's root document -- what "back"
// means from there (closing this backend, say) is a decision for whatever
// caller holds this Nav, not this package; mirrors nav.js's back() falling
// through to the picker frame at that point, minus the picker itself (out
// of scope here -- see the package comment).
func (n *Nav) CanGoBack() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.stack) > 1
}
