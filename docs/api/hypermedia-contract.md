# The hypermedia contract

This is the wire-level contract both RESTForge clients (`pebble/src/pkjs/`
and `flutter/lib/`) enforce identically. Both are ports of the same design —
the Pebble app in ES5 JavaScript (`http.js`, `siren.js`, `actions.js`,
`live.js`, `settings.js`), the Flutter app in Dart
(`http_service.dart`, `siren.dart`, `action_service.dart`,
`live_service.dart`, `settings_service.dart`) — and every rule below was
verified against both, plus cross-checked against
[f3sctl](https://github.com/snonux/f3sctl)'s server-side implementation
(`~/git/f3sctl/internal/httpapi/`) as a concrete, running example.

## 1. Transport

- HTTP or HTTPS. Nothing in the client source refuses plain HTTP, but see
  the open question in `implementing-a-backend.md` about Android's cleartext
  policy — in practice, ship HTTPS.
- One **base URL** per backend, absolute, and it must end in `/`. The
  settings screen appends a trailing `/` automatically if the user forgot
  one (`flutter/lib/services/settings_service.dart`'s `normalise`,
  `pebble/src/pkjs/settings.js`'s equivalent) — required because RFC 3986
  resolution drops the last path segment of a base that doesn't end in `/`.
- Every `href` a server sends — in a `link`, an `action`, or a job's `self`
  link — is resolved against that base URL per **RFC 3986 §5.2**
  (`flutter/lib/services/url_resolver.dart`, `pebble/src/pkjs/url.js`). It
  may be root-relative (`/api/status`), fully relative (`status`), or
  absolute (`https://other-host/status`); the client never inspects or
  constructs a path itself — only the scheme and authority are supplied by
  the client's own configuration. **This is the single rule everything else
  in this contract follows from.**

## 2. Discovery

There is exactly one URL a client is configured with: the base URL. From
there:

1. `GET {baseUrl}` with the auth header.
2. Read `properties.apiVersion` (see §6).
3. Render `links` and `actions` generically.
4. Optionally follow one link automatically: `Backend.startRel` is a rel
   name the *user* types into the settings screen (the one server-specific
   string either app's source is allowed to hold, because a human supplied
   it at runtime, not because it is hardcoded) — if present, the client
   looks it up by rel on the freshly-fetched root and fetches it
   immediately, pushing it onto the navigation stack above the root.

A backend defines **no other fixed entry point**. Every other resource is
reached by a `rel` on some document the client already has.

## 3. Authentication

- One secret per configured backend, sent in **one HTTP header per
  request**, on every request, including safe (`GET`) ones.
- The **header name is configurable per backend**, not fixed to any one
  name — `Backend.authHeader`, defaulting to `X-API-Key`
  (`SettingsService.defaultAuthHeader` / `DEFAULT_AUTH_HEADER` in
  `settings.js`). A user can point RESTForge at a backend that expects
  `Authorization: Bearer ...` or any other header by typing that header name
  into the settings screen; the client validates only that the name is a
  legal HTTP token (RFC 7230's `token` grammar) before using it — an illegal
  name is refused locally as `FailureKind.config`, never sent.
- The secret is **never** sent as a query parameter, form field, or in any
  other place — only that one header.
- **There is no login flow, no session, no token refresh.** The header is
  attached to every request from the moment a backend is configured until
  its secret is changed. A `401`/`403` is fatal and final for that request;
  the client does not attempt to re-authenticate or retry.
- A configured backend is one unit: `{name, baseUrl, authHeader, secret,
  startRel}`. Multiple backends can be configured side by side (the Flutter
  settings screen caps this at 12); **the contract in this document applies
  identically to every one of them** — nothing in either client special-cases
  one configured backend differently from another. A new backend does not
  need to match f3sctl's header name, path layout, or vocabulary; it only
  needs to speak this contract.

## 4. Content negotiation

- Every request carries `Accept: application/vnd.siren+json, application/json`.
- A response's `Content-Type` is not inspected by either client — a body is
  simply parsed as JSON regardless of what content type it was served
  with. (f3sctl still does the correct thing and serves
  `application/vnd.siren+json` on every entity response; a well-behaved
  backend should too, both for interoperability with other Siren tooling and
  because it costs nothing.)
- The response body, if present, must decode as a single JSON value whose
  top level is a JSON **object**. Anything else — invalid JSON syntax, a
  top-level array or scalar — is `FailureKind.parse` and is treated as a
  failure, not as an empty/absent document.
- An empty response body is treated as "no entity" rather than a parse
  failure — relevant for actions whose response carries nothing informative
  beyond the status code.

## 5. The Siren document shape

Every response body a client actually reads is expected to look like a
[Siren](https://github.com/kevinswiber/siren) entity. Every member below is
optional from the client's point of view — a missing or wrongly-typed member
degrades to "not offered" (`null`, or an empty list), never to a crash or a
rejected document:

```
{
  "class":      ["..."],          // string or [string]; identifies the entity's type
  "title":      "...",            // human-readable title, preferred wording for a label
  "properties": { ... },          // arbitrary key/value data
  "entities":   [ ... ],          // sub-entities: embedded, or bare references (see below)
  "links":      [ { "rel": [...], "href": "...", "title": "...", "class": [...], "type": "..." } ],
  "actions":    [ {
    "name":   "...",              // stable identifier; matched by name, never by href
    "title":  "...",              // human-readable label, preferred over name
    "method": "GET|POST|...",     // defaults to GET if the server omits it
    "href":   "...",              // resolved against the base URL
    "class":  [...],
    "type":   "application/x-www-form-urlencoded",
    "fields": [ {
      "name":     "...",
      "type":     "checkbox|...", // "checkbox" is the one type the client treats specially
      "value":    ...,            // the default, sent as-is if the client doesn't override it
      "title":    "...",          // preferred label — see actions-and-jobs.md
      "required": true|false      // a server extension; absent/false means "not required"
    } ]
  } ]
}
```

Two details worth being explicit about, because they are easy to get wrong
as a backend author:

- **A sub-entity may be embedded or a bare reference.** An entity inside
  `entities` counts as *embedded* the moment it carries its own `properties`
  object or its own `entities` array — even an empty one. Otherwise (only a
  `class`, a `rel` to its parent, and an `href`) it counts as a *reference*:
  the client shows it as a row that must be fetched (a `GET`) before it can
  be read, rather than something already in hand. Choose deliberately: an
  embedded array that is genuinely empty (`"entities": []`) is a meaningful
  answer ("there is nothing here right now"), different from a reference
  that has to be followed to find out.
- **An action is either offered or not offered — there is no third,
  disabled-but-visible state.** If an action would currently fail (a job is
  already running, the target is already in the desired state, a
  precondition is not met), *omit it from `actions` entirely*. The client
  renders exactly the array it is given; it does not gray out, disable, or
  otherwise second-guess an action's availability. This is the load-bearing
  idea behind the whole design: which actions exist *is* the server's
  expression of what is currently legal.

`required` on a field is not part of the Siren specification proper — it is
a de facto extension both this client and f3sctl's server treat as a plain
boolean member on a field object. A field with `required` absent, `false`,
or any non-`true` JSON value is treated as optional.

## 6. `apiVersion`

- The root document's `properties.apiVersion`, if present, is checked
  against the highest version the client understands
  (`supportedApiVersion = 1` today, in both `siren.dart` and the
  corresponding constant in `siren.js`).
- A missing or non-numeric `apiVersion` is **not** an error — the client
  proceeds normally. A server that never declares one is not making a claim
  the client can act on.
- A numeric `apiVersion` **greater than** what the client understands stops
  the client cold: the root is not pushed onto the navigation stack, and the
  document state becomes an error ("server speaks apiVersion N, this app
  understands 1") rather than rendering a document that might mean something
  the client would silently misread.
- **Only bump `apiVersion` on an actual breaking change** — repurposing an
  existing `rel`, class, action `name`, or property key for a different
  meaning. Adding new links, actions, entity classes, or properties is not
  breaking (see §8) and does not need a version bump; existing clients
  render unrecognised vocabulary generically rather than erroring on it.

## 7. HTTP methods and the safe/unsafe split

The client asks for confirmation before sending any request whose method is
outside RFC 9110's **safe** set — `GET`, `HEAD`, `OPTIONS`, `TRACE`. This is
a division by HTTP semantics, not a list of scary-sounding action names: an
action named `detonate` sent over `GET` is still invoked with no
confirmation, and one named `refresh` sent over `POST` still asks. A
backend's action `method` value is what decides this, so choose method
values that actually reflect RFC 9110 semantics (`GET` for anything that
only reads, everything that changes state as `POST`/`PUT`/`PATCH`/`DELETE`).

An action with no `method` given defaults to Siren's own default, `GET`.

## 8. Sending a request

- A safe request (`GET`) carries no body.
- An unsafe request (an invoked action) is always sent with
  `Content-Type: application/x-www-form-urlencoded`, and **always carries a
  body — even an empty one** — when the action has no fields to fill. This
  matters concretely against a CGI backend behind bozohttpd (f3sctl's
  deployment): bozohttpd rejects a POST with no `Content-Length` header with
  a bare `400`, *before the CGI process ever runs* — which looks
  indistinguishable from the API refusing the action. Both clients always
  attach a `Content-Length`, sending an explicit empty body rather than no
  body at all, to avoid exactly that. A backend author using a similarly
  strict HTTP front end should verify a field-less POST with a
  zero-length, but present, body is accepted.
- Field values are percent-encoded per `application/x-www-form-urlencoded`
  (the space-as-`+` convention, not bare percent-encoding).

## 9. Status codes

| Status | Client-side meaning | Notes |
|---|---|---|
| `200`–`299` | Success | `202` is a normal success meaning "accepted, not finished yet" — see `actions-and-jobs.md`. |
| `401`, `403` | `FailureKind.auth` | Treated identically. The client does not retry and does not attempt to distinguish "missing key" from "wrong key" — nor is a backend expected to (f3sctl deliberately makes them indistinguishable). |
| `409` | `FailureKind.conflict` | The state the client acted on is stale. **Never retried automatically**, with exactly one narrow exception — see `actions-and-jobs.md` §5. The client's standard response is to re-fetch and re-render, never to resend the same request. |
| Any other `4xx` | `FailureKind.client` | The client is at fault (bad request, wrong method, unknown route, ...). |
| `5xx` | `FailureKind.server` | The backend is at fault; shown to the user with the server's own message when one is present. |
| No response at all (DNS failure, TLS error, connection refused, or the read timeout — see §10 — elapses) | `FailureKind.unreachable` (or `FailureKind.timeout` for the elapsed-budget case; both are treated as the same *document state*, `unreachable`, by the UI layer) | **The single most important distinction in this contract.** A request that never arrived says nothing about the backend's actual state and must never be rendered as if it had answered — see §11 and `pebble/docs/DESIGN.md`/`docs/DESIGN.md` ("A failed request is not an answer"). |
| `2xx` with a body that is not parsable JSON | `FailureKind.parse` | |

Both clients keep the same `unreachable`/`timeout` vs. everything-else split
downstream too: `NavService.stateFor` in Flutter (`nav_service.dart`) maps
every `FailureKind` except `unreachable`/`timeout` onto a generic `error`
document state, and those two onto a distinct `unreachable` state the UI
renders differently — e.g. never as "the thing you asked about is off".

## 10. Timeouts

Both clients enforce **two separate client-side budgets**, not one:

- **Reads** (`GET`/`HEAD`): **20 seconds** (`getTimeout` /
  `GET_TIMEOUT_MS`).
- **Everything else** (an invoked action): **60 seconds**
  (`actionTimeout` / `ACTION_TIMEOUT_MS`).

The longer action budget exists because a state-changing request can
legitimately take a long time to *answer* (not to finish — a slow-to-finish
job should return `202` promptly, see `actions-and-jobs.md`), and a client
giving up early on a request that is still being processed cannot tell that
apart from one that actually failed. f3sctl's `fans-off` route is the
concrete example this budget was sized around: it re-probes the rack before
switching the plug, which can legitimately take most of a minute even when
it is about to succeed. **A backend action that can take close to a minute
to answer synchronously must fit inside that 60-second budget, or return
`202` and a pollable job instead.**

These are client-enforced ceilings, not something a backend declares or
negotiates — there is no request-level timeout header or query parameter in
this contract.

## 11. The error envelope

An error response's body — when a backend chooses to send one, which is
recommended — is expected to be a Siren entity whose `properties.message` is
a human-readable string:

```json
{ "class": ["error"], "properties": { "status": 409, "message": "..." } }
```

Both clients dig for exactly `properties.message` (a string) inside a JSON
object body and prefer it over anything they would otherwise say themselves.
When it's absent, or the body doesn't parse, or there's no body at all, each
`FailureKind` has a generic client-side fallback (`"auth rejected"`,
`"state changed"` for a conflict, or a bare `"HTTP <status>"`). A backend
should therefore treat `properties.message` as directly user-facing prose,
not a machine-readable error code — the server's own wording is always
preferred to anything a generic client could invent, because only the
server knows *why* it said no.

`class: ["error"]` itself is not actually inspected by either client — only
the shape (`properties.message`) matters. Using it is still good practice,
both as documentation and because a future client version, or another Siren
tool, may key off it.

## 12. Response headers

Neither client hardcodes the name of any response header. Every header on
every response is logged verbatim (`http_service.dart`'s `_logResponse`,
`http.js`'s `logResponse`) — deliberately, so that a deployment-specific
diagnostic header (f3sctl's `X-F3sctl-Node`, naming which of its two
load-balanced nodes answered) shows up in client-side logs "for free"
without either client needing to know it exists in advance. A backend that
sits behind a load balancer, a proxy cache, or multiple replicas should
consider adding a header like this — it costs nothing on the client side and
is the single most useful thing for debugging "why did two consecutive polls
disagree".

Response headers a client itself *sent* (chiefly the auth header) are never
logged, on either side, regardless of the header's name.

## 13. The genericity rule, restated for a backend author

Nothing above names a path, a `rel`, a `class`, an action `name`, or a
property key belonging to any particular backend — because nothing in
either client's source is allowed to. Concretely, this means:

- **A backend cannot assume the client already knows its vocabulary.**
  Every resource must be reachable by following a `rel` from somewhere the
  client already has (starting at the root), and every legal action must
  actually appear in `actions` at the moment it is legal.
- **A backend is free to invent its own `rel`, `class`, action `name` and
  property vocabulary** — that is the entire point of a hypermedia contract.
  f3sctl's vocabulary (`status`, `fans`, `job`, `power-off`, `ping`, ...) is
  one example set, not a reserved or required one.
- **New vocabulary is always safe to add.** A client renders an unrecognised
  `class`, `rel`, action, or property generically rather than hiding it or
  erroring on it (`pebble/docs/DESIGN.md` / `docs/DESIGN.md`, "Rendering
  does not interpret"). Only repurposing *existing* vocabulary for a
  different meaning is a breaking change (see §6).
