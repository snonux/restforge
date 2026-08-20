package nav

import (
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
	n.generation++
	gen := n.generation
	n.stack = nil
	n.current = be
	n.state = StateLoading
	n.failure = nil
	n.mu.Unlock()

	resp, err := n.http.Get(be, be.BaseURL)
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

	if !n.pushIfCurrent(gen, entity, be.BaseURL, be.Name) {
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
	n.generation++
	fetchGen := n.generation
	n.state = StateLoading
	n.failure = nil
	n.mu.Unlock()

	n.fetchAndApply(be, fetchGen, href, be.StartRel, false)
}

// Fetch follows href and pushes the result on top of the stack -- mirrors
// nav.js's fetch(href, title, false), the case a link row or a saved
// shortcut uses. On failure the stack, and so Document, is untouched; only
// State/Failure change.
func (n *Nav) Fetch(href, title string) {
	n.fetch(href, title, false)
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
	// Bumps generation before resetting the stack so that any fetch still
	// in flight for the backend being switched away from (e.g. a slow
	// followStart from an earlier OpenRoot) finds itself stale once it
	// lands and discards its result instead of pushing onto be's stack --
	// see Nav.generation.
	n.generation++
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
	n.fetch(here.href, here.title, true)
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
	// Bumps generation before pushing: a fetch already in flight when the
	// user opens this embedded entity must not land afterwards and shove
	// its own frame on top of the one just opened -- see Nav.generation.
	n.generation++
	child := entities[index]
	n.pushLocked(child, child.Follow("self"), child.Label())
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
	// Bumped before the pop: this is precisely the scenario the bug this
	// package's generation guard fixes -- a Fetch/Refresh/OpenRoot started
	// before the user pressed Back must not land afterwards and silently
	// override the Back the user actually asked for -- see Nav.generation.
	n.generation++
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

// fetch performs one fetch against whatever backend is current right now,
// and applies its outcome to the stack. Shared by Fetch (push) and Refresh
// (replace) -- mirrors nav.js's single fetch(href, title, replace).
// followStart does not go through here -- see its own doc comment for why
// it needs a fixed backend rather than n.current.
//
// Known boundary of the staleness guard: href is chosen by the caller (a
// row the user pressed, captured from whatever document was on screen at
// that moment) but current is read fresh, right here, not carried in from
// the caller. Fetch/Refresh have no backend parameter of their own to pin
// the way followStart pins be -- see Session.Activate/Session.Refresh,
// which call them with only an href, never a backend.Backend, because
// render.FetchTarget (what a pressed row's target actually is) carries no
// backend either. In the extremely narrow case where a second call already
// switched n.current to a different backend by the time this fetch()'s own
// lock/bump runs -- even though that second call was issued strictly later
// by the user -- href (meant for the backend on screen when it was
// captured) would be sent to that different backend, and the generation
// guard cannot catch it, since this call's own bump is the newest one at
// that point. Closing this fully would mean threading backend.Backend
// through render.FetchTarget and Session's own API, not just this
// package's guard; out of scope here -- see i31 for the guard this
// function does provide, and its own annotations for why this residual
// case was tracked as a follow-up instead of folded in.
//
// A failure never touches the stack: Document keeps reading whatever was
// there before this call, which is precisely the invariant this package
// exists to protect -- see the package comment.
func (n *Nav) fetch(href, title string, replace bool) {
	n.mu.Lock()
	// Bumping generation here, not just in Back/OpenRoot/Adopt/OpenEmbedded,
	// is what makes two concurrent fetches (e.g. a second Fetch fired
	// before the first returns) resolve to "the one that started last
	// wins": whichever fetch's HTTP round trip lands first will find its
	// captured gen no longer equal to n.generation once the other one has
	// started, and discard its result -- see Nav.generation.
	n.generation++
	gen := n.generation
	n.state = StateLoading
	n.failure = nil
	current := n.current
	n.mu.Unlock()

	n.fetchAndApply(current, gen, href, title, replace)
}

// fetchAndApply performs the GET against be and applies the result to the
// stack, but only if gen is still the live generation once the round trip
// returns -- see Nav.generation. Caller must have already bumped
// n.generation to gen (and set State/Failure for the loading phase) before
// calling this; it exists only to share the GET-then-guarded-apply shape
// between fetch() (which reads be from n.current) and followStart (which
// cannot: see its own doc comment).
func (n *Nav) fetchAndApply(be backend.Backend, gen uint64, href, title string, replace bool) {
	// The HTTP round trip runs with no lock held -- see the package's mu
	// doc comment -- so a concurrent View render keeps reading whatever
	// State/Document the caller committed before calling this (StateLoading,
	// and the last-good Document beneath it) instead of blocking for the
	// duration of the request.
	resp, err := n.http.Get(be, href)
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
	if replace && len(n.stack) > 0 {
		n.stack[len(n.stack)-1] = frame{entity: entity, href: href, title: title}
	} else {
		n.stack = append(n.stack, frame{entity: entity, href: href, title: title})
	}
	n.state = StateOK
	n.failure = nil
}

// pushLocked appends entity onto the stack and marks it the new current
// document. Caller must hold n.mu.
func (n *Nav) pushLocked(entity siren.Entity, href, title string) {
	n.stack = append(n.stack, frame{entity: entity, href: href, title: title})
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
func (n *Nav) pushIfCurrent(gen uint64, entity siren.Entity, href, title string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.generation != gen {
		return false
	}
	n.pushLocked(entity, href, title)
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
	n.applyFailureLocked(err)
}

// applyFailureLocked records err as the reason the last fetch did not land,
// mapping its Kind onto a DocumentState via StateFor. Coerces err through
// the shared failure.From rather than hand-rolling the type assertion:
// httpGetter.Get always returns a *failure.Failure on error (see
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
