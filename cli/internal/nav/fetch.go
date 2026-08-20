package nav

import (
	"context"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/siren"
)

// OpenRoot opens be at its base URL, replacing whatever backend and stack
// were previously open, then follows be.StartRel if it has one. Mirrors
// openBackend + fetchRoot + followStart in nav.js and openRoot in
// nav_service.dart.
//
// Nothing is carried over from the previous backend: keeping its document
// on screen while this one loads would be showing one server's state under
// another server's name -- mirrors the same reasoning in nav.js's
// openBackend.
func (n *Nav) OpenRoot(be backend.Backend) {
	n.mu.Lock()
	gen, ctx := n.beginFetchLocked()
	n.stack = nil
	n.current = be
	n.state = StateLoading
	n.failure = nil
	n.mu.Unlock()

	resp, err := n.http.GetContext(ctx, be, be.BaseURL)
	if err != nil {
		// A second OpenRoot/Adopt/Back/Fetch may have run while this GET
		// was in flight -- see Nav.generation. If so, this failure is not
		// about the backend a caller is looking at any more; applying it
		// would show an error for the wrong server.
		n.applyFailureIfCurrent(gen, err)
		return
	}

	entity := siren.EntityFromJSON(resp.Entity)
	// Checked before anything is pushed: a server speaking a version this
	// app was not written against may have changed the meaning of
	// something it would otherwise display confidently and wrongly --
	// mirrors the siren.versionProblem check in fetchRoot.
	if problem := entity.VersionProblem(); problem != "" {
		n.applyFailureIfCurrent(gen, &failure.Failure{Kind: failure.Client, Message: problem})
		return
	}

	if !n.pushIfCurrent(gen, entity, be.BaseURL, be.Name, be) {
		// Superseded while the GET was in flight: some other call already
		// reset n.stack/n.current out from under this one (see
		// Nav.generation). be's root must not be pushed on top of
		// whatever that call left behind, and followStart below must not
		// run at all -- see followStart's own doc comment for why gen is
		// threaded through to it rather than letting it read n.current.
		return
	}
	n.followStart(be, entity, gen)
}

// followStart follows the link with rel be.StartRel on the freshly-fetched
// root, if be configured one and the root actually offers it. The rel is
// the only server-specific string anywhere in this app, and the user typed
// it into the settings screen themselves -- the one exception
// docs/DESIGN.md carves out -- mirrors followStart in nav.js. A missing rel
// is not a failure: the server may simply not offer it right now, which is
// a legitimate answer, not something to route around.
//
// gen is the generation OpenRoot's own push was still current under. It is
// re-checked here, under lock, before this call's own GET is even issued --
// closing the gap between pushIfCurrent's unlock and this call that a bare
// "call fetch() and let it read n.current" would leave open: without this
// check, a second OpenRoot/Adopt for a different backend landing in that
// gap would make fetch() send be.StartRel's href to whatever backend is
// current *now* (wrong BaseURL for a relative href, wrong auth secret),
// not the be this root came from. Passing be explicitly through to
// fetchAndApply (rather than fetch(), which reads n.current) keeps this
// call pinned to be for its own GET even though n.current may move on to a
// different backend while that GET is still in flight -- the generation
// check on the way back in still discards the result if that happens, the
// same as any other fetch.
func (n *Nav) followStart(be backend.Backend, root siren.Entity, gen uint64) {
	if be.StartRel == "" {
		return
	}
	href := root.Follow(be.StartRel)
	if href == "" {
		return
	}

	n.mu.Lock()
	if n.generation != gen {
		n.mu.Unlock()
		return
	}
	fetchGen, ctx := n.beginFetchLocked()
	n.state = StateLoading
	n.failure = nil
	n.mu.Unlock()

	n.fetchAndApply(ctx, be, fetchGen, href, be.StartRel, false)
}

// Fetch follows href and pushes the result on top of the stack -- mirrors
// nav.js's fetch(href, title, false), the case a link row or a saved
// shortcut uses. On failure the stack, and so Document, is untouched; only
// State/Failure change.
//
// be is the backend href actually belongs to -- render.FetchTarget.Backend
// for a pressed row, or whatever backend a caller just Adopt-ed for a saved
// shortcut (see Session.Activate and RunQuick). Pinning the GET to be,
// rather than re-reading whatever Nav.current happens to be once this
// call's own lock section runs, is what closes p31: without it, a second,
// later-dispatched OpenRoot/Adopt for a different backend landing first
// could send href to that other backend's server before this call's own
// generation bump even runs, since bumping-and-reading n.current used to
// happen in the same critical section here.
func (n *Nav) Fetch(be backend.Backend, href, title string) {
	n.fetch(be, href, title, false)
}

// Adopt switches to be without fetching its root -- mirrors adopt in
// nav.js. A saved shortcut jumps straight to somewhere inside a backend, so
// paying for the root fetch OpenRoot always makes would be a wasted request
// and a screen nobody asked for. The stack starts empty exactly as OpenRoot
// leaves it, so CanGoBack is false and a subsequent Back falls through to
// whatever screen opened this one, rather than into a history nobody
// walked.
func (n *Nav) Adopt(be backend.Backend) {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Supersedes before resetting the stack so that any fetch still in
	// flight for the backend being switched away from (e.g. a slow
	// followStart from an earlier OpenRoot) is both cancelled outright and
	// -- as a backstop, for a round trip already past cancelling -- finds
	// itself stale once it lands and discards its result instead of
	// pushing onto be's stack -- see Nav.generation and Nav.cancel.
	n.supersedeLocked()
	n.stack = nil
	n.current = be
	n.state = StateOK
	n.failure = nil
}

// Refresh re-fetches the document on top of the stack and replaces it in
// place -- mirrors refresh in nav.js. An embedded document has no address
// of its own to re-fetch from -- re-rendering what is already in hand is
// the honest option there, same as nav.js's comment on the same case: it is
// not a failure, so it does not touch State/Failure either, it is simply a
// no-op past clearing whatever error was already on screen.
//
// The backend to re-fetch against comes from here.be -- the backend this
// exact frame was originally fetched from -- captured in the same lock
// section as here.href/here.title below, rather than read separately from
// Nav.current the way this method did before p31. Nav.current and the top
// frame's own backend agree at every ordinary moment (both only ever change
// together, under OpenRoot/Adopt), but only reading here.be closes the same
// gap Fetch's own be parameter closes for a caller-supplied href: a second,
// later-dispatched OpenRoot/Adopt for a different backend landing between
// this read and fetch()'s own generation bump must not be able to make this
// call's GET go to that other backend instead of the one here.href actually
// belongs to.
func (n *Nav) Refresh() {
	n.mu.Lock()
	if len(n.stack) == 0 {
		n.mu.Unlock()
		return
	}
	here := n.stack[len(n.stack)-1]
	if here.href == "" {
		n.state = StateOK
		n.failure = nil
		n.mu.Unlock()
		return
	}
	n.mu.Unlock()
	n.fetch(here.be, here.href, here.title, true)
}

// OpenEmbedded opens a sub-entity that arrived embedded inside the document
// already on screen. Nothing is fetched: it is already in hand, and asking
// the server for it again could legitimately return something different --
// mirrors openEmbedded in nav.js. Out-of-range or before anything is open,
// index is simply ignored: there is nothing there to open.
func (n *Nav) OpenEmbedded(index int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.stack) == 0 {
		return
	}
	entities := n.stack[len(n.stack)-1].entity.Entities
	if index < 0 || index >= len(entities) {
		return
	}
	// Supersedes before pushing: a fetch already in flight when the user
	// opens this embedded entity must not land afterwards and shove its own
	// frame on top of the one just opened -- see Nav.generation and
	// Nav.cancel.
	n.supersedeLocked()
	child := entities[index]
	// n.current, not a stale copy: this whole method runs under n.mu, so it
	// is exactly the backend the entity already on screen (and so child,
	// embedded inside it) was fetched from.
	n.pushLocked(child, child.Follow("self"), child.Label(), n.current)
}

// Back pops one document. A no-op at the backend's root -- see CanGoBack.
// Mirrors the stack-popping half of back in nav.js; the half that falls
// through to the picker below the root does not carry over here (out of
// scope -- see the package comment).
func (n *Nav) Back() {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Inlined rather than calling CanGoBack: that accessor takes its own
	// read lock, which would deadlock against the write lock already held
	// here (sync.RWMutex is not reentrant).
	if len(n.stack) <= 1 {
		return
	}
	// Superseded before the pop: this is precisely the scenario the bug
	// this package's generation guard fixes -- a Fetch/Refresh/OpenRoot
	// started before the user pressed Back must not land afterwards and
	// silently override the Back the user actually asked for -- see
	// Nav.generation and Nav.cancel.
	n.supersedeLocked()
	last := len(n.stack) - 1
	// Zeroed before the re-slice: shrinking a slice by re-slicing alone
	// leaves the dropped element's frame (including its siren.Entity,
	// which can carry an arbitrarily large Properties/Entities of its
	// own) reachable through the backing array until a future append
	// happens to overwrite that slot. Clearing it here lets the GC
	// reclaim it immediately instead of leaking it for however long this
	// Nav (and so this backing array) stays alive.
	n.stack[last] = frame{}
	n.stack = n.stack[:last]
	n.state = StateOK
	n.failure = nil
}

// fetch performs one fetch against be and applies its outcome to the stack.
// Shared by Fetch (push) and Refresh (replace) -- mirrors nav.js's single
// fetch(href, title, replace). followStart does not go through here -- see
// its own doc comment for why it needs a fixed backend rather than
// n.current.
//
// be is pinned by the caller (Fetch's own be parameter, or Refresh's
// here.be) rather than read fresh from n.current the way this method did
// before p31 -- see Fetch's and Refresh's own doc comments for the gap that
// closes. It is threaded straight through to fetchAndApply, the same shape
// followStart already used for the same reason (see fetchAndApply's own
// doc comment).
//
// A failure never touches the stack: Document keeps reading whatever was
// there before this call, which is precisely the invariant this package
// exists to protect -- see the package comment.
func (n *Nav) fetch(be backend.Backend, href, title string, replace bool) {
	n.mu.Lock()
	// Superseding here, not just in Back/OpenRoot/Adopt/OpenEmbedded, is
	// what makes two concurrent fetches (e.g. a second Fetch fired before
	// the first returns) resolve to "the one that started last wins":
	// whichever fetch's HTTP round trip lands first will find its captured
	// gen no longer equal to n.generation once the other one has started
	// (and, since n31, will already have had its own context cancelled by
	// that later call's beginFetchLocked) -- see Nav.generation and
	// Nav.cancel.
	gen, ctx := n.beginFetchLocked()
	n.state = StateLoading
	n.failure = nil
	n.mu.Unlock()

	n.fetchAndApply(ctx, be, gen, href, title, replace)
}

// fetchAndApply performs the GET against be and applies the result to the
// stack, but only if gen is still the live generation once the round trip
// returns -- see Nav.generation. Caller must have already called
// beginFetchLocked to obtain gen and ctx (and set State/Failure for the
// loading phase) before calling this; it exists only to share the
// GET-then-guarded-apply shape between fetch() and followStart, both of
// which (since p31) pin be explicitly from the caller rather than reading
// Nav.current here.
func (n *Nav) fetchAndApply(ctx context.Context, be backend.Backend, gen uint64, href, title string, replace bool) {
	// The HTTP round trip runs with no lock held -- see the package's mu
	// doc comment -- so a concurrent View render keeps reading whatever
	// State/Document the caller committed before calling this (StateLoading,
	// and the last-good Document beneath it) instead of blocking for the
	// duration of the request. ctx is beginFetchLocked's own cancellable
	// context for this fetch -- cancelled by a later supersedeLocked, so
	// this call may return early with a context.Canceled-flavoured error
	// rather than running to completion; either way the gen check below
	// discards the result the same as any other stale response.
	resp, err := n.http.GetContext(ctx, be, href)
	if err != nil {
		// Superseded while the GET was in flight (a Back, a second
		// Fetch/Refresh/followStart, or an OpenRoot/Adopt for a different
		// backend) -- see Nav.generation. The failure is not about
		// whatever is on screen now, so it must not be applied to it.
		n.applyFailureIfCurrent(gen, err)
		return
	}

	entity := siren.EntityFromJSON(resp.Entity)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.generation != gen {
		// Same guard as above, for the success path: n.stack may have
		// been reset or popped by something else while this GET was in
		// flight, and appending/replacing onto it now would silently
		// override whatever that something else did -- exactly the bug
		// this generation counter exists to prevent.
		return
	}
	n.releaseCancelLocked()
	// be, the backend this GET was actually pinned to and sent against, is
	// recorded on the frame itself -- not re-read from n.current -- so a
	// later Refresh or rendered FetchTarget can pin its own fetch the same
	// way (see frame.be and p31).
	if replace && len(n.stack) > 0 {
		n.stack[len(n.stack)-1] = frame{entity: entity, be: be, href: href, title: title}
	} else {
		n.stack = append(n.stack, frame{entity: entity, be: be, href: href, title: title})
	}
	n.state = StateOK
	n.failure = nil
}

// supersedeLocked cancels whatever fetch is currently in flight, if any,
// and bumps generation, returning its new value. n.mu must be held.
//
// Every call that may invalidate an in-flight fetch goes through this --
// OpenRoot and followStart via beginFetchLocked below, and Adopt,
// OpenEmbedded, Back and fetch() directly or via beginFetchLocked -- so
// that a fetch this call supersedes is not just made stale (the generation
// check every caller of Get used to rely on alone, pre-n31) but has its
// underlying HTTP round trip actually cancelled: see Nav.cancel and n31.
// Calling an already-fired or already-nil cancel func is a safe no-op
// (context.CancelFunc's contract), so there is no need to track whether
// the fetch it belonged to had already finished on its own.
func (n *Nav) supersedeLocked() uint64 {
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	n.generation++
	return n.generation
}

// beginFetchLocked supersedes whatever fetch is in flight (see
// supersedeLocked) and opens a fresh, cancellable context for a new one,
// remembering its cancel func so a later supersedeLocked can abort it in
// turn. n.mu must be held by the caller; the returned ctx is for the
// caller's own HTTP round trip, to be run after releasing the lock.
func (n *Nav) beginFetchLocked() (uint64, context.Context) {
	gen := n.supersedeLocked()
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	return gen, ctx
}

// releaseCancelLocked cancels and forgets n.cancel, if set. Called by every
// site that applies a fetch's outcome (pushIfCurrent, applyFailureIfCurrent,
// fetchAndApply's own success tail) once it has confirmed, under lock, that
// gen still matches n.generation -- i.e. this fetch landed on its own,
// neither superseded nor invalidated in the meantime.
//
// Without this, n.cancel would go on pointing at a finished fetch's now-
// pointless cancel func until some unrelated later call happened to
// supersede it -- contradicting Nav.cancel's own "a fetch is genuinely in
// flight" invariant the moment this fetch lands, and leaving a cancel func
// uncalled indefinitely, which every context.CancelFunc's contract expects
// its last use to do. Calling it here has no observable effect today (the
// context this cancels is always rooted at context.Background(), so there
// is no parent watcher goroutine to release), but keeps that contract
// honestly satisfied rather than accidentally satisfied only because of
// how beginFetchLocked happens to build its context today. n.mu must be
// held. Safe to call when n.cancel is already nil.
func (n *Nav) releaseCancelLocked() {
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
}

// pushLocked appends entity onto the stack and marks it the new current
// document. be is the backend entity was fetched from (or, for
// OpenEmbedded, the backend the entity it was embedded in already belongs
// to) -- recorded on the frame so a later Refresh or rendered FetchTarget
// can pin its own fetch to it -- see frame.be and p31. Caller must hold
// n.mu.
func (n *Nav) pushLocked(entity siren.Entity, href, title string, be backend.Backend) {
	n.stack = append(n.stack, frame{entity: entity, be: be, href: href, title: title})
	n.state = StateOK
	n.failure = nil
}

// pushIfCurrent pushes entity onto the stack and reports true, but only if
// gen still matches n.generation -- guarding OpenRoot's push against a
// second OpenRoot/Adopt/Back/Fetch that ran while its HTTP round trip was
// in flight, the same way fetch() guards its own push. Returns false when
// superseded, in which case the caller must not treat entity as
// authoritative for anything further -- see OpenRoot's use of this before
// followStart.
func (n *Nav) pushIfCurrent(gen uint64, entity siren.Entity, href, title string, be backend.Backend) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.generation != gen {
		return false
	}
	n.releaseCancelLocked()
	n.pushLocked(entity, href, title, be)
	return true
}

// applyFailureIfCurrent applies err as the reason a fetch did not land, but
// only if gen still matches n.generation -- see Nav.generation. Shared by
// OpenRoot's two failure branches (a transport error, and a version
// problem the entity itself reports) and fetchAndApply's, so the
// lock/check/apply shape is not repeated inline at each call site.
func (n *Nav) applyFailureIfCurrent(gen uint64, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.generation != gen {
		return
	}
	n.releaseCancelLocked()
	n.applyFailureLocked(err)
}

// applyFailureLocked records err as the reason the last fetch did not land,
// mapping its Kind onto a DocumentState via StateFor. Coerces err through
// the shared failure.From rather than hand-rolling the type assertion:
// httpGetter.GetContext always returns a *failure.Failure on error (see
// httpclient's package comment), so the Kind: Config fallback exists only
// so a hand-rolled test double that returns a plain error still degrades
// to something sensible rather than panicking -- the same fallback action
// and live use for their own equivalent seams, see failure.From's doc
// comment for why that is one conscious choice rather than three
// independent ones. Caller must hold n.mu.
func (n *Nav) applyFailureLocked(err error) {
	f := failure.From(err, failure.Config)
	n.state = StateFor(f.Kind)
	n.failure = f
}
