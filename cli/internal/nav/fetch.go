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
	n.stack = nil
	n.current = be
	n.state = StateLoading
	n.failure = nil

	resp, err := n.http.Get(be, be.BaseURL)
	if err != nil {
		n.applyFailure(err)
		return
	}

	entity := siren.EntityFromJSON(resp.Entity)
	// Checked before anything is pushed: a server speaking a version this
	// app was not written against may have changed the meaning of
	// something it would otherwise display confidently and wrongly --
	// mirrors the siren.versionProblem check in fetchRoot.
	if problem := entity.VersionProblem(); problem != "" {
		n.state = StateError
		n.failure = &failure.Failure{Kind: failure.Client, Message: problem}
		return
	}

	n.push(entity, be.BaseURL, be.Name)
	n.followStart(be, entity)
}

// followStart follows the link with rel be.StartRel on the freshly-fetched
// root, if be configured one and the root actually offers it. The rel is
// the only server-specific string anywhere in this app, and the user typed
// it into the settings screen themselves -- the one exception
// docs/DESIGN.md carves out -- mirrors followStart in nav.js. A missing rel
// is not a failure: the server may simply not offer it right now, which is
// a legitimate answer, not something to route around.
func (n *Nav) followStart(be backend.Backend, root siren.Entity) {
	if be.StartRel == "" {
		return
	}
	href := root.Follow(be.StartRel)
	if href == "" {
		return
	}
	n.fetch(href, be.StartRel, false)
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
	if len(n.stack) == 0 {
		return
	}
	here := n.stack[len(n.stack)-1]
	if here.href == "" {
		n.state = StateOK
		n.failure = nil
		return
	}
	n.fetch(here.href, here.title, true)
}

// OpenEmbedded opens a sub-entity that arrived embedded inside the document
// already on screen. Nothing is fetched: it is already in hand, and asking
// the server for it again could legitimately return something different --
// mirrors openEmbedded in nav.js. Out-of-range or before anything is open,
// index is simply ignored: there is nothing there to open.
func (n *Nav) OpenEmbedded(index int) {
	if len(n.stack) == 0 {
		return
	}
	entities := n.stack[len(n.stack)-1].entity.Entities
	if index < 0 || index >= len(entities) {
		return
	}
	child := entities[index]
	n.push(child, child.Follow("self"), child.Label())
}

// Back pops one document. A no-op at the backend's root -- see CanGoBack.
// Mirrors the stack-popping half of back in nav.js; the half that falls
// through to the picker below the root does not carry over here (out of
// scope -- see the package comment).
func (n *Nav) Back() {
	if !n.CanGoBack() {
		return
	}
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

// fetch performs one fetch and applies its outcome to the stack. Shared by
// Fetch (push), Refresh (replace) and followStart (push) -- mirrors
// nav.js's single fetch(href, title, replace).
//
// A failure never touches the stack: Document keeps reading whatever was
// there before this call, which is precisely the invariant this package
// exists to protect -- see the package comment.
func (n *Nav) fetch(href, title string, replace bool) {
	n.state = StateLoading
	n.failure = nil

	resp, err := n.http.Get(n.current, href)
	if err != nil {
		n.applyFailure(err)
		return
	}

	entity := siren.EntityFromJSON(resp.Entity)
	if replace && len(n.stack) > 0 {
		n.stack[len(n.stack)-1] = frame{entity: entity, href: href, title: title}
	} else {
		n.stack = append(n.stack, frame{entity: entity, href: href, title: title})
	}
	n.state = StateOK
	n.failure = nil
}

// push appends entity onto the stack and marks it the new current document.
func (n *Nav) push(entity siren.Entity, href, title string) {
	n.stack = append(n.stack, frame{entity: entity, href: href, title: title})
	n.state = StateOK
	n.failure = nil
}

// applyFailure records err as the reason the last fetch did not land,
// mapping its Kind onto a DocumentState via StateFor. httpGetter.Get always
// returns a *failure.Failure on error (see httpclient's package comment);
// the type-assertion fallback exists only so a hand-rolled test double that
// returns a plain error still degrades to something sensible rather than
// panicking.
func (n *Nav) applyFailure(err error) {
	f, ok := err.(*failure.Failure)
	if !ok {
		f = &failure.Failure{Kind: failure.Client, Message: err.Error()}
	}
	n.state = StateFor(f.Kind)
	n.failure = f
}
