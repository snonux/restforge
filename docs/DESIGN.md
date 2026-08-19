# RESTForge design — the contract all three apps keep

This repository holds three apps — a Pebble watchapp ([`pebble/`](../pebble)), an
Android port ([`flutter/`](../flutter)) and a Go/Charm terminal client
([`cli/`](../cli)) — and the contract below is the part they all share: what a
hypermedia client owes the person using it, written down once so it does not
drift by being stated three times. It survived the change of language from
ES5 to Dart to Go and it survives every change to any of the three apps; a
behaviour here is a requirement, not an implementation note.

Each app keeps its own structural map — the watchapp's watch/phone split,
two-screen layout and file map in
[`pebble/docs/DESIGN.md`](../pebble/docs/DESIGN.md), the Flutter port's module
mapping and conventions in [`flutter/AGENTS.md`](../flutter/AGENTS.md) (sections 4
and 5), and the Go port's module mapping in [`cli/AGENTS.md`](../cli/AGENTS.md)
(section 4). This file is only the contract.

## The rule everything else follows from

> Fetch the root. Render what it offers. Never build a URL.

A client that obeys that cannot contain server-specific code — which is what
makes it reusable. Concretely:

- The Siren lookup module contains no rel, no class, no action name and no
  property name, and must not gain one.
- Navigation is only ever "follow the href the server put in the document".
  The URL resolver resolves it against the configured base — that is RFC 3986
  resolution, not path construction: the path came from the server, only the
  origin is ours.
- `just check`'s genericity grep enforces this. It must print nothing.

The single exception is `startRel`, which the user types into the settings page
themselves. Even then the app locates the link *by rel* and uses the href it
finds.

## Invariants worth protecting

Each of these has a test, and each exists because the obvious implementation
gets it wrong.

**A failed request is not an answer.** "I could not ask" and "the answer was
no" are different things, and only one of them is about the server. A failure
never replaces the document on screen with an empty one — the last good
document stays exactly where it was and the reason goes on top of it. Reporting
a healthy service as down because the phone lost signal is the failure this
design makes unrepresentable.

**Rendering does not interpret.** A value is shown as the server sent it, so
three states never collapse into two. A client that renders `ping: false` as
"off" is asserting something the server did not say. Nothing is hidden either,
including vocabulary the app has never seen: "I do not recognise this" is not
a reason to withhold it from the person using it.

**Ask before acting.** Any method outside `GET`, `HEAD`, `OPTIONS` and `TRACE`
gets a confirmation screen. That division is RFC 9110's, not a list of
dangerous-sounding action names. On any device, the row that opens a property
and the row that changes the world must not act the same.

**Never carry a document across an action.** Every action is followed by an
unconditional re-fetch. A `409` is re-fetched and re-rendered, never retried:
it means the state we acted on was stale, and repeating a request the server
just judged wrong cannot fix that. The one bounded exception is a required
checkbox the user actually ticked, re-sent once, within a minute.

**Do not invent a value.** A required field with no default and no confirmation
to stand in for it is asked for out loud, or refused. A value nobody supplied,
for a field a server marked required, is how a client does something nobody
asked for.

**Do not claim a job finished.** While something is still running, a poll can
be routed to a machine that never saw it. A reply about a different `id`, or
one saying there is no job, is *no news* — not completion. A failed poll is
news about the network. And giving up is reported as giving up. See the
live-work module, which spends most of its comments on exactly this.