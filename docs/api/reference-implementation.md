# Reference implementation: f3sctl

[f3sctl](https://github.com/snonux/f3sctl) is the one backend this contract
has ever actually been implemented against. It is a Go CGI program (built as
`internal/httpapi`, served by bozohttpd on two Raspberry Pis, `pi0` and
`pi1`, behind a `relayd` load balancer) that powers a small homelab rack on
and off, checked out locally at `~/git/f3sctl`. Its own client-facing
contract document, `~/git/f3sctl/docs/CLIENT.md`, is normative for f3sctl
specifically and was cross-checked line by line against
`~/git/f3sctl/internal/httpapi/handlers.go` and `siren.go` while writing this
document. Everything below is either quoted from those sources or verified
against them.

**Nothing here is required of a new backend.** f3sctl's specific paths,
`rel`s, action names and property keys are its own vocabulary, not part of
the contract in `hypermedia-contract.md`/`actions-and-jobs.md`. This
document exists to show the contract satisfied end to end by a real,
shipped implementation — a worked example, not a spec to match.

## Topology and auth

- Base URL: `https://f3s.buetow.org/cgi-bin/f3sctl/`
- Auth header: `X-API-Key`, one accepted key per line in a file on each of
  `pi0`/`pi1` (`internal/httpapi/auth.go`); re-read on every request, so
  adding/revoking a key needs no restart. A revoked or wrong key gets `401`;
  the two are indistinguishable by design.
- Two nodes answer behind `relayd`; every response — success or error —
  carries `X-F3sctl-Node: pi0.lan.buetow.org` (or `pi1...`), the diagnostic
  header described generically in `hypermedia-contract.md` §12.
- Response `Content-Type` on every Siren entity: `application/vnd.siren+json`
  (`internal/httpapi/siren.go`'s `sirenMediaType`).

## The root

```http
GET /cgi-bin/f3sctl/ HTTP/1.1
X-API-Key: ...
```

```json
{
  "class": ["f3sctl"],
  "title": "f3s homelab control",
  "properties": { "apiVersion": 1, "version": "v0.1.0", "node": "pi0" },
  "links": [
    { "rel": ["self"],       "href": "/cgi-bin/f3sctl/" },
    { "rel": ["status"],     "href": "/cgi-bin/f3sctl/status" },
    { "rel": ["fans"],       "href": "/cgi-bin/f3sctl/fans" },
    { "rel": ["job"],        "href": "/cgi-bin/f3sctl/job" },
    { "rel": ["monitoring"], "href": "/cgi-bin/f3sctl/monitoring" },
    { "rel": ["describedby"],"href": "/cgi-bin/f3sctl/openapi.json" }
  ],
  "actions": [
    { "name": "power-off", "method": "POST", "href": "/cgi-bin/f3sctl/power/off" },
    { "name": "f3-on",     "method": "POST", "href": "/cgi-bin/f3sctl/power/f3/on" },
    { "name": "fans-off",  "method": "POST", "href": "/cgi-bin/f3sctl/fans/off",
      "type": "application/x-www-form-urlencoded",
      "fields": [ { "name": "force", "type": "checkbox", "value": false, "required": true,
        "title": "Hosts may still be running (f3 still running): the rack fans keep them cool, so switching the plug off now risks overheating. Confirm to proceed." } ] }
  ]
}
```

(Trimmed to three representative actions; with the whole rack up, the live
API returns seven, ~2592 bytes total, measured 2026-08-12 against v0.5.1.)
Note what is present only because it is currently true: no `power-on`
(everything is already up) and no `fans-on` (already on) — the client
renders exactly the array it was given, per `hypermedia-contract.md` §5.

`s.router.Actions(state)` (`handlers.go`) is what decides this array on
every request, freshly, from the live probed state — nothing is cached
across requests.

## `/status`

```json
{
  "class": ["status"], "title": "Host and rack status",
  "properties": { "node": "pi0" },
  "links": [
    { "rel": ["self"], "href": "/cgi-bin/f3sctl/status" },
    { "rel": ["up"],   "href": "/cgi-bin/f3sctl/" }
  ],
  "actions": [ /* same set as the root, freshly computed */ ],
  "entities": [
    { "class": ["host","f"], "rel": ["item"],
      "properties": { "name": "f0", "ip": "192.168.1.120", "ping": true,
                       "pingKnown": true, "ssh": true, "ms": 3 } },
    { "class": ["host","cluster"], "rel": ["item"],
      "properties": { "name": "f1", "ip": "192.168.1.121", "ping": true,
                       "pingKnown": true, "ssh": true, "ms": 4 } },
    { "class": ["fans"], "rel": ["item"],
      "properties": { "on": true, "ip": "192.168.1.130" } },
    { "class": ["job"], "rel": ["item"],
      "properties": { "id": "...", "action": "off", "state": "done", "...": "..." } }
  ]
}
```

`ping`/`ssh`/`pingKnown` are three independent booleans
(`internal/httpapi/handlers.go`'s `hostEntity`), rendered exactly as probed —
none of them are collapsed into a single "up"/"down" flag, which is the
concrete instance of `hypermedia-contract.md`'s "rendering does not
interpret" invariant: `pingKnown: false` means the probe itself could not
run (unmeasured), which is a different fact from `ping: false` (probe ran,
host silent), and the client must show both distinctly. `CLIENT.md` §4 has
the full up/transition/off/unknown state table this feeds.

A `fans` entity that could not be reached carries `"error": "..."` instead
of `"on"` — again, "unknown", never presented as "off".

## Power actions and the job pattern

```http
POST /cgi-bin/f3sctl/power/off HTTP/1.1
X-API-Key: ...
Content-Type: application/x-www-form-urlencoded
Content-Length: 0

```

```
HTTP/1.1 202 Accepted
```
```json
{ "class": ["job"], "rel": ["item"],
  "properties": { "action": "off", "state": "running", "node": "pi0", "rc": null } }
```

`202` plus `properties.state == "running"` — either alone would be enough
per `actions-and-jobs.md` §5, and f3sctl's job-start response gives both.
The origin document (root or `/status`) carries a link with `rel: ["job"]`,
and this response's `class` includes `"job"` — the class/rel match
`actions-and-jobs.md` §5 depends on to find the poll target.

Poll `GET /cgi-bin/f3sctl/job`:

```json
{
  "class": ["job"], "rel": ["item"], "title": "Power operation",
  "properties": {
    "id": "c8fe5f2131c26ed3", "action": "off", "state": "running",
    "node": "pi0", "rc": null,
    "step": "shutting down f2",
    "started": "2026-08-08T19:10:00Z",
    "updated": "2026-08-08T19:12:41Z",
    "staleAfterSeconds": 1800,
    "hosts": {
      "f1": { "phase": "done", "detail": "powered off" },
      "f2": { "phase": "confirming", "detail": "accepted; waiting for it to go silent" },
      "f0": { "phase": "pending" }
    }
  },
  "links": [ { "rel": ["self"], "href": "/cgi-bin/f3sctl/job" } ]
}
```

`staleAfterSeconds` here (`internal/httpapi/handlers.go`'s `jobEntity`) is
derived by the server from its own configured timeouts, not a flat
constant — see `CLIENT.md` §5 for the derivation. `hosts{}.phase` values
(`pending`/`working`/`confirming`/`done`/`failed`) and `step` are entirely
f3sctl's own vocabulary, rendered generically by the client with no
knowledge of what any of those words mean.

When nothing has ever run:

```json
{ "class": ["job"],
  "title": "No power operation has run on either API node",
  "properties": { "state": "none", "node": "pi0" },
  "links": [ { "rel": ["self"], "href": "/cgi-bin/f3sctl/job" },
             { "rel": ["up"],   "href": "/cgi-bin/f3sctl/" } ] }
```

This is exactly the `state: "none"` shape `actions-and-jobs.md` §5 documents
as "not about any job, keep waiting" — note it deliberately carries no `id`,
so it cannot spuriously match (or mismatch) an in-flight watch's id either.

When finished:

```json
{ "class": ["job"],
  "properties": { "id": "c8fe5f2131c26ed3", "action": "off", "state": "done",
                   "rc": 0, "node": "pi0",
                   "started": "...", "finished": "...",
                   "step": "rack fans left ON: f3 still running" } }
```

`state` leaves `"running"` — the client's watch ends here, and per
`actions-and-jobs.md` §6, `"done"` is rendered no differently at the
mechanism level than `"failed"` would be; the difference is entirely in the
verbatim text (here, the `step` explaining the fans were deliberately left
on because `f3` is still up — `power-off` only ever targets `f0`/`f1`/`f2`).

### Two nodes, one job

`GET /job` on whichever node answers asks its peer for its own job first
(`internal/httpapi/handlers.go`'s `currentJob`/`peerJob`) and merges,
reporting the same job regardless of which of `pi0`/`pi1` `relayd` routed
to — but that merge is best-effort (a 3-second-bounded peer request) and
falls back to local-only state if the peer is unreachable. This is the
concrete case `actions-and-jobs.md` §5's `id`-mismatch handling exists for:
a client that skips the `id` check can, and historically did, report a
healthy shutdown as a failure because a poll landed on a node that
momentarily knew about a different, older job of its own.

## `/fans` and the `force` confirmation

```http
GET /cgi-bin/f3sctl/fans HTTP/1.1
```
```json
{ "class": ["fans"], "title": "Rack fan plug",
  "properties": { "on": true, "ip": "192.168.1.130" },
  "links": [ { "rel": ["self"], "href": "/cgi-bin/f3sctl/fans" },
             { "rel": ["up"],   "href": "/cgi-bin/f3sctl/" } ],
  "actions": [ { "name": "fans-off", "method": "POST",
                 "href": "/cgi-bin/f3sctl/fans/off",
                 "fields": [ { "name": "force", "type": "checkbox",
                               "required": true, "value": false,
                               "title": "..." } ] } ] }
```

`internal/httpapi/handlers.go`'s `handleFansOff` is the concrete
implementation of the "judged twice, on two different budgets" case
`actions-and-jobs.md` §4 documents in the abstract: the `force` field's
*presence* comes from a cheap, already-taken snapshot; sending the request
without `force=true` re-checks against a slower, stricter multi-probe
(`rackStillBusy` → `confirmRack`), and can legitimately `409` even though the
snapshot that produced this very response said the field wasn't needed. This
is exactly the situation the client's one bounded `409`-retry-with-the-same-
confirmed-checkbox exists for (`actions-and-jobs.md` §4): a user who ticked
`force` and got `409` anyway should have that same confirmed `force=true`
resent once, automatically, rather than being asked again for a "yes" they
already gave.

`fans-on`/`fans-off` are the one part of this API that answers
**synchronously** — plain `200`, no job, no polling — except that
`fans-off`'s re-probe can take close to the client's 60-second action
timeout (`hypermedia-contract.md` §10), which is exactly why that budget is
60 seconds and not the 20-second read budget.

## `/monitoring`

```json
{ "class": ["monitoring"], "title": "Gogios alerting mute",
  "properties": { "muted": true, "node": "pi0" },
  "entities": [
    { "class": ["gateway"], "rel": ["item"],
      "properties": { "name": "blowfish", "muted": true } },
    { "class": ["gateway"], "rel": ["item"],
      "properties": { "name": "fishfinger", "error": "ssh: connection refused" } }
  ],
  "links": [ { "rel": ["self"], "href": "/cgi-bin/f3sctl/monitoring" },
             { "rel": ["up"],   "href": "/cgi-bin/f3sctl/" } ],
  "actions": [ { "name": "monitoring-unmute", "method": "POST",
                 "href": "/cgi-bin/f3sctl/monitoring/unmute" } ] }
```

Deliberately **not** folded into `/status`: reading it costs an SSH round
trip per gateway, so a status poll running every 30–60 seconds must not drag
it along — it is fetched on demand or on a slow timer instead. A gateway
that could not be reached carries `error` rather than `muted`, the same
"unreachable is not the same as the good answer" pattern as `/fans`.

## Errors

```json
{ "class": ["error"], "properties": { "status": 409, "message": "a power operation is already running on pi1" } }
```

(`internal/httpapi/siren.go`'s `WriteError` — the exact envelope shape
`hypermedia-contract.md` §11 documents generically.)

| Status | f3sctl's meaning | (see `hypermedia-contract.md` §9 for the client's generic handling) |
|---|---|---|
| `401` | Missing or wrong `X-API-Key` | |
| `404` | No such resource — i.e. a client built a URL instead of following an `href` | |
| `405` | Wrong method for that action | |
| `409` | A power operation is already running, or (`fans-off`) the stricter re-probe disagreed with the snapshot | |
| `502` | The fan plug or a host could not be reached | |
| `500` | The API is misconfigured, needs an operator | |

## Discovery document

`describedby` → `GET /cgi-bin/f3sctl/openapi.json` is a machine-readable
OpenAPI description, generated from the same route registry that serves
requests (`internal/httpapi/openapi.go`). Neither RESTForge client reads it
— it documents *what generally exists*, for humans and other tooling; the
live Siren responses describe *what is possible right now*, and per
`CLIENT.md` §11, when the two disagree the Siren response is the one to act
on.

## Full source

- `~/git/f3sctl/docs/CLIENT.md` — normative for f3sctl specifically,
  the document this file draws its worked examples from.
- `~/git/f3sctl/docs/client-reference.js` — a ~100-line, dependency-free
  reference client; the executable proof that `CLIENT.md` matches what
  actually ships (`internal/httpapi/registry_test.go` pins the two against
  each other server-side).
- `~/git/f3sctl/docs/API-KEYS.md` — the operator side of key management
  (`hypermedia-contract.md` §3 only covers the client side).
- `~/git/f3sctl/internal/httpapi/` — the Go source: `handlers.go` (routes),
  `siren.go` (the entity types and the renderer), `registry.go` (which
  actions are advertised when), `auth.go` (the key check).
- Upstream: <https://github.com/snonux/f3sctl>
