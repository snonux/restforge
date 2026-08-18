# The RESTForge backend contract

RESTForge (this repository's Pebble watchapp and its Flutter port) is a
**generic [Siren](https://github.com/kevinswiber/siren) hypermedia browser**.
It does not know about any particular backend: `just check`'s genericity grep
fails the build the moment a rel, a class, an action name or a property name
belonging to one particular server appears in either app's source. So there
is no fixed list of routes to document the way a normal REST API reference
would — the "API" both apps speak is a *contract about how any backend must
behave*, not a set of endpoints.

This is the reference for that contract, written so a new person or agent can
implement a **new, from-scratch backend** — with entirely different
functionality from the one this project currently talks to — and have it
work with both apps unmodified. It was produced by reading the client source
directly (`pebble/src/pkjs/`, `flutter/lib/`), not by guessing.

## Why f3sctl is in here

The only backend that exists for this ecosystem today is
**[f3sctl](https://github.com/snonux/f3sctl)** — a Go CGI server that powers a
small homelab rack on and off. It is checked out locally at `~/git/f3sctl`
alongside this repository. f3sctl is not special-cased anywhere in RESTForge
(the genericity grep would fail if it were); it simply happens to be a real,
working implementation of the contract described here, with its own
normative client document at `~/git/f3sctl/docs/CLIENT.md`. Everywhere this
document says "for example, f3sctl does X", that is illustration, not a
requirement — the requirement is stated independently and holds for any
backend.

`pebble/docs/f3sctl-api-handoff.md` in this repository is a narrower,
Pebble-specific hand-off note written while wiring the watchapp up to f3sctl
specifically; it is a good companion read but is not a substitute for this
document, which describes the contract generically.

## How this doc set is organised

| Document | Covers |
|---|---|
| [`api/hypermedia-contract.md`](api/hypermedia-contract.md) | The wire-level contract every backend must speak: transport, auth, the Siren document shape, HTTP methods, status codes, the error envelope, `apiVersion`, timeouts. This is the closest thing to an "endpoint reference" — except the endpoints are discovered, not fixed. |
| [`api/actions-and-jobs.md`](api/actions-and-jobs.md) | The action/outcome model in full: confirmation, field-filling, the one bounded `409` retry, long-running ("live") work and how it is polled, and exactly what a "done: action off" style notice means. |
| [`api/reference-implementation.md`](api/reference-implementation.md) | A worked walkthrough of f3sctl's actual endpoints — concrete request/response pairs for every resource it exposes, as an existence proof of the contract above. |
| [`api/implementing-a-backend.md`](api/implementing-a-backend.md) | A build checklist for a new backend, plus the open questions this investigation could not settle from client source alone. |

## The one-paragraph version

A backend is a base URL plus a secret. The client `GET`s the base URL with
the secret in a configurable request header (`X-API-Key` by default), reads a
JSON document shaped like a Siren entity, and renders exactly what it was
given: properties, embedded sub-entities, links (each with a `rel` and an
`href`) and actions (each with a `name`, `method` and `href`, currently
legal to perform — nothing else). It never builds a path itself. Following a
link is a plain `GET`. Performing an action whose method is outside
`GET`/`HEAD`/`OPTIONS`/`TRACE` asks the user to confirm first, sends the
request as `application/x-www-form-urlencoded`, and always re-fetches
afterwards — never trusting the action's own response as the new state. A
response that says `202` or carries `properties.state == "running"` is work
that outlives the request; the client finds a link on the *origin* document
whose `rel` matches a `class` the response carries, and polls that until the
state stops being `"running"`. A `409` means the client acted on stale state
and must re-fetch and re-render, never blindly retry. A request that never
arrives at all (DNS, TLS, timeout) is reported as "unreachable", never as an
answer about the backend's state.

See [`docs/DESIGN.md`](DESIGN.md) for the invariants this is built to protect
(worded for maintainers of these two clients); this document set describes
the same contract from a backend author's point of view.
