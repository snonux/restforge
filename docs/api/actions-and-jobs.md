# The action, confirmation, and job-polling model

This document covers everything that happens after a user presses a row
that is an offered Siren `action`: confirming it, filling its fields,
sending it, interpreting what comes back, and — if the work outlives the
request — watching it to completion. It is the Dart/JS-verified behaviour of
`action_service.dart` + `live_service.dart` + `session.dart` (Flutter) and
`actions.js` + `live.js` + `session.js` (Pebble), which are ports of each
other and were checked to agree.

## 1. Asking: safe methods go straight through, unsafe ones confirm first

An action's `method` (§7 of `hypermedia-contract.md`) decides everything
here:

- **Safe** (`GET`, `HEAD`, `OPTIONS`, `TRACE`): invoked immediately, no
  confirmation shown.
- **Anything else**: the client shows a confirmation screen before sending
  anything. What it asks:
  - **Heading**: the action's own label (its `title`, or its `name` if no
    title was given).
  - **Body**: the `title` of the action's first field that is both
    `type: "checkbox"` **and** `required: true` — this is deliberately
    server-authored: the sentence explains the *current* reason a
    confirmation is needed, and that reason can vary run to run (f3sctl's
    `fans-off` field title differs between "a host is still running" and "a
    host could not be probed, so assumed running" — same field, different
    prose, because the situation is different). If there is no such field,
    the client falls back to a generic sentence:
    `"{action.label}?  {METHOD} to this server."`

If the named action is not currently present in the document's `actions`
array at all (withdrawn since it was rendered, or never offered), the client
reports this as a real, final answer — "not offered right now" — never as a
bug to route around by inventing a request.

## 2. Filling fields

Given the action the user confirmed (or a safe one invoked directly), each
of its `fields` is filled generically, with no field-name-specific logic
anywhere in the client:

1. **`type: "checkbox"`** — filled from the user's yes/no answer to the
   confirmation itself: `"true"` if they confirmed, `"false"` otherwise.
   *A required checkbox field is, by design, how a backend gets an explicit
   confirmation flag into the request* — see f3sctl's `force` field.
2. **A value the user was separately asked to supply** (see §3) — matched
   by field name.
3. **Otherwise, the field's own `value`** (its server-supplied default), if
   present — sent as-is, stringified.
4. A field matching none of the above is simply **left out of the request
   body**.

## 3. Do not invent a value: the one field the client will ask about

After the above, the client checks whether any field marked `required` still
has no value:

- **Zero** such fields: send the request with whatever was filled.
- **Exactly one**: the client asks the user for it out loud (a text prompt
  showing the field's `title`, or its `name` if there is no title), then
  resumes with the answer plugged in by name. An empty answer is treated as
  "nothing was heard" and the whole action is refused — never sent with an
  empty string standing in for a required value.
- **More than one**: the whole action is refused outright, with no prompt at
  all. Asking for one missing value out loud is acceptable; dictating a form
  one field at a time is worse than plainly declining. **A backend that
  wants to be usable through this contract should never require more than
  one field with no server-supplied default at once.**

This is the client-side expression of `docs/DESIGN.md`'s "do not invent a
value": a required field with nothing to fill it is asked for, or the action
is refused — it is never silently defaulted, omitted, or guessed at.

## 4. Sending, and what comes back

The filled request is sent per §8 of `hypermedia-contract.md` (form-encoded,
Content-Length always present). What happens to the response:

### Success (any `2xx`, `202` included)

- A notice is shown: **`"{message}: {body}"`**.
  - `message` is the response entity's `properties.state` if it has a
    string one, verbatim — otherwise `"Accepted"` for a `202`, or `"Done"`
    for any other `2xx`.
  - `body` is a **generic, order-preserving dump of every property** on the
    response entity: `key1: value1   key2: value2   ...` (wide-space
    joined; nested objects/arrays are rendered as their own JSON). If the
    response carried no properties at all, `body` falls back to
    `"HTTP {status}"`.
  - This is exactly the `"done: action off ..."`-shaped text a user sees: it
    is not a fixed vocabulary the client understands, it is the server's own
    `state` word plus a literal dump of whatever else the server put in the
    response. **The client attaches no success/failure semantics to the
    word itself** — `"done"` and `"failed"` are rendered exactly the same
    way (see §7).
- **The response is then discarded as a source of truth about the
  document.** The client never treats an action's own response body as the
  new state of the resource it acted on (`docs/DESIGN.md`, "never carry a
  document across an action"). Instead:
  - If the response looks like it needs watching (§6), the client starts a
    poll loop and defers the re-fetch until that loop concludes.
  - Otherwise, the client immediately re-fetches (a plain `GET`) the
    document the action was invoked *from*, and that re-fetched document —
    not the action's response — becomes what's on screen next.

### Conflict (`409`)

Handled per §9 of `hypermedia-contract.md`: this always means the client
acted on stale state. The client's default response is to **re-fetch and
re-render**, never to resend the exact same request — with exactly one
bounded exception:

> **If (and only if) the action being invoked had a required checkbox field
> that the user actually, explicitly confirmed** (§2's checkbox case), the
> client remembers those exact field values for **60 seconds**
> (`confirmationRetryTtl` / `CONFIRMATION_TTL_MS`) and, on a `409` for that
> same action within that window, **resends the identical request exactly
> once**, with no further prompt. Whatever that retry returns — success,
> the same `409` again, or anything else — is final; the confirmation is
> consumed either way and there is no second retry.

This exists because a backend may legitimately judge the same action twice
on different evidence and reasonably say no the first time — f3sctl's
`fans-off` is the concrete case this was built for: the field's presence is
decided from a cheap snapshot, but the actual switch is guarded by a slower,
stricter re-probe that can disagree with the snapshot. A user who explicitly
ticked "yes, force it" gave real consent; re-asking for a second confirmation
on a race the server itself introduced would be worse than replaying the
consent the user already gave. **A backend relying on this must accept the
identical field values on the immediate retry, with no new confirmation
required.**

Any `409` *not* eligible for this (no confirmed required checkbox on that
action, the 60-second window has passed, or the one retry already happened)
is reported as a failure like any other, and the client still re-fetches —
conflict is the one failure kind that always triggers a re-fetch, because it
specifically means "the state you looked at is stale", which a re-fetch
directly answers.

### Any other failure

Reported to the user as a failure notice (`"{action label} failed:
{message}"`). **The client does not re-fetch** on a non-conflict failure —
an auth rejection, an unreachable backend, a malformed response, or a plain
`4xx`/`5xx` says nothing that a re-fetch would usefully resolve, and
re-fetching on every failure would risk papering over a real, still-current
problem.

## 5. Long-running work: the "live" / job-polling model

A response is treated as **still in progress** — "shouldWatch" — when
**either**:

- its HTTP status is exactly `202`, **or**
- its entity carries `properties.state == "running"` (a string, exact
  match).

Either signal alone is sufficient; a backend can use whichever fits its
transport (a synchronous CGI process most naturally returns `202` for
"started, not done"; a resource a client re-`GET`s naturally reports its own
`state`).

### Finding what to poll

The client does **not** re-poll the action's own response entity, and does
**not** fall back to re-fetching the document the action came from. Instead:

1. Take every `class` string on the action's **response** entity.
2. On the **origin** document — the one the action was invoked from, still
   held from before the action was sent — look for a `link` whose `rel`
   contains one of those class strings.
3. The first match's `href` is the thing to poll.
4. **If nothing matches, the client refuses to watch at all** (logged, not
   surfaced as an error) — it does not fall back to polling the origin
   document itself, because the origin's own re-fetch would say nothing
   about whether the job is done; treating its ordinary content as "no job
   running, must be finished" would report completion within seconds of
   starting.

**Consequence for a backend:** an action that can return `202` or a
`state: "running"` entity MUST also ensure the document that offered the
action carries a link whose `rel` equals a `class` on that response entity.
f3sctl's convention (from `CLIENT.md`/`internal/httpapi/handlers.go`) is
exactly this: the root and `/status` both carry a link with
`rel: ["job"]`, and every job entity carries `class: ["job"]` — so the
class-to-rel match always succeeds. Get this wrong and RESTForge will
silently not watch the job at all; there is no error shown for it, only a
log line.

### The poll loop

- Interval: a fixed **10 seconds** (`pollInterval` / `POLL_MS`) between
  polls, regardless of what the server reports.
- Each poll is a plain `GET` on the target found above.
- **A failed poll is not news about the job — only about the network.** The
  client keeps polling on the same schedule until its deadline (below)
  elapses; a transport failure never ends the watch by itself.
- A poll response is filtered before being acted on:
  - `properties.state == "none"` (the literal string) means "there is
    nothing here for me" — treated as irrelevant, kept waiting. This is the
    documented shape a backend should use for "no job has ever run / there
    is nothing to report" (f3sctl's `GET /job` with nothing recorded).
  - If the watch started with an `id` (from `properties.id` on the action's
    original response) **and** the poll response also carries an `id`
    **and** the two differ, the response is **irrelevant** — treated the
    same as "no news", never as completion. This matters specifically for a
    load-balanced or multi-instance backend, where consecutive requests can
    land on an instance that knows nothing about — or knows about a
    *different*, older — job than the one the client started. **A backend
    with more than one instance answering requests MUST either make every
    instance answer consistently for the same job id, or accept that
    without an `id` on the job entity, the client cannot tell an unrelated
    reply from its own job's.**
  - If the response has **no `state` property at all**, it is judged
    "not judgeable" — the resource being polled apparently doesn't report
    progress at all, so its silence must not be read as completion. The
    watch stops and reports "gave up" (never "done", never "failed" — see
    §7).
  - If `state` is present and is exactly `"running"`, the client reports
    progress (see §7) and keeps polling.
  - If `state` is present and is anything else (and the response passed the
    `id`/`"none"` filters above) — the watch is **finished**. Any string
    other than `"running"` or `"none"` counts as terminal; the client does
    not maintain a fixed enum of "success" vs "failure" state words (see
    §7).

### The deadline

- Derived from the response's own `properties.staleAfterSeconds` (a number,
  seconds) plus **60 seconds of buffer** (`budgetBuffer`), *re-derived from
  every poll response that carries one* — not fixed at the moment watching
  started. This lets a backend change its own staleness ceiling mid-watch
  and have the client pick it up.
- If no response has ever carried `staleAfterSeconds`, the deadline falls
  back to **25 minutes** (`fallbackBudget`). This is deliberately generous:
  it can only make the client wait longer than a backend's own budget would
  require, never give up on a job that is genuinely still running.
- On expiry, the client stops polling and reports "gave up" — **not**
  failure, **not** success, a third outcome entirely — then still re-fetches
  the document the action was invoked from, because whatever the action
  changed before the client gave up watching is still worth showing.

**Recommendation for a backend:** send `staleAfterSeconds` on every job-like
entity, derived from your own actual worst-case duration for that kind of
work (plus slack for the client's own poll interval and round trip) rather
than letting every client fall back to the generous, one-size-fits-all
default.

## 6. Rendering states generically — no fixed vocabulary beyond "running"/"none"

The **only** two state strings either client attaches any behaviour to are:

- `"running"` — keep polling.
- `"none"` — this specific poll response is not about any job; keep
  polling, waiting for something relevant.

Every other value of `properties.state` — `"done"`, `"failed"`, or anything
else a backend chooses — is displayed **verbatim** and treated identically
by the client: it is simply "the watch is over", with the state word shown
as the notice's headline text and every other property dumped generically
underneath it (§4). **The client does not colour, sort, or otherwise treat
a `"failed"` job differently from a `"done"` one at the state-machine
level** — this follows directly from `docs/DESIGN.md`'s "rendering does not
interpret": a value is shown as the server sent it, and three states never
collapse into two.

Practical implication for a backend: **do not use an HTTP error status to
signal that a job's *eventual outcome* was a failure.** The action's own
response (`202`, or a synchronous `2xx`) reports only that the *request* was
accepted; the terminal state of the work it started belongs in
`properties.state` on the polled resource, read at the client's leisure,
with whatever additional properties (an `rc`, an `error` string, per-item
progress — f3sctl's `hosts{}` map is one shape for this) explain *why* it
ended the way it did. Those properties are exactly what shows up in the
`{body}` half of the `"{message}: {body}"` notice.

## 7. What the user actually sees, end to end

Putting §§4–6 together, a single action produces exactly one of these
outcomes on screen (Flutter's `SessionNotice` hierarchy in `session.dart`
names each explicitly; the Pebble app renders the same distinctions without
naming a type for them):

| Outcome | Shown as | When |
|---|---|---|
| Not offered | `"{name}" is no longer offered` | The named action isn't in the current document. |
| Refused | `{heading}: {reason}` | More than one required field had nothing to fill it, or an out-loud answer came back empty. |
| Needs a value | A prompt for one field | Exactly one required field had nothing to fill it. |
| Sent, answered, not watched | `{message}: {body}` | Success, no `202`/`running` signal, or nothing on the origin document to poll against. |
| Sent, answered, being watched | The latest `step` (or `state`) while running | `shouldWatch` was true and a poll target was found. |
| Watch finished | `{message}: {body}` | The polled resource's `state` left `"running"` (and wasn't filtered as irrelevant). |
| Watch gave up | `Gave up waiting for "{heading}" to finish` | The deadline (§5) elapsed, or the polled resource stopped reporting `state` at all. Document is still re-fetched. |
| Failed | `{heading} failed: {message}` | Any non-conflict failure, or a conflict that exhausted the one allowed retry. |

Nothing in this table is backend-specific vocabulary — it is entirely the
generic machinery described above, applied to whatever `state`/`step`/other
properties a given backend actually sends.
