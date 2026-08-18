# Building a new backend

A checklist for implementing a backend RESTForge (either the Pebble app or
the Flutter app, unmodified) can talk to, followed by the open questions
this investigation could not settle from client source alone.

This assumes you have already read `hypermedia-contract.md` and
`actions-and-jobs.md` — this document is the "did I actually do it" list,
not a restatement of the rules.

## Checklist

**Transport and discovery**

- [ ] Serve a JSON document shaped like a Siren entity at the URL the user
      will type in as the backend's base URL (§5 of the contract doc).
      Content-Type `application/vnd.siren+json` is recommended, not
      enforced by the client.
- [ ] Every `href` you emit resolves correctly against that base URL under
      RFC 3986 — root-relative (`/foo`) and fully-relative (`foo`) both
      work; do not emit anything the client would have to guess at.
- [ ] `properties.apiVersion` is an integer on the root document, bumped
      only when you repurpose existing vocabulary for a new meaning. Start
      at `1` unless you specifically need to signal that this backend
      speaks a *different, incompatible* dialect from apiVersion 1 — both
      current clients understand up to `1` and will visibly refuse
      anything higher, so shipping `2` on day one gains you nothing.

**Authentication**

- [ ] Every request — reads included — accepts a secret in exactly one
      request header. The header *name* is the user's choice at
      configuration time on the client side, not yours to dictate, though
      `X-API-Key` is the client's own default if the user doesn't change
      it, so it's a reasonable default to design around.
- [ ] `401` or `403` for a missing/invalid key. Consider making the two
      indistinguishable (f3sctl does, deliberately, so a revoked client
      can't fingerprint whether its key still exists).
- [ ] Never require the key anywhere but that header — no query-string
      fallback, no cookie, no session. The client will never send it any
      other way, and per `hypermedia-contract.md` §3 a proxy/access log
      seeing a key in a URL is exactly the failure mode this exists to
      avoid.

**Actions**

- [ ] `actions[]` on any document only ever lists actions that are legal to
      perform *right now*. An action that would currently fail must be
      **omitted**, never included-but-doomed. This is the single most
      important rule in the whole contract — the client has no concept of
      a disabled button.
- [ ] Give every action a stable `name` (matched by clients across
      requests) and a `method` that actually reflects RFC 9110 safety —
      `GET` for read-only, everything else for anything that changes
      state. A `POST` action the client should be able to invoke without
      confirmation does not exist in this contract; use `GET` if you truly
      want that.
- [ ] For an action that needs explicit confirmation beyond the generic
      "are you sure": add a field with `"type": "checkbox", "required":
      true`, and a `title` that reads as a complete sentence — that
      sentence is shown to the user verbatim as the confirmation text.
      Vary the `title` at request time if the underlying reason varies
      (f3sctl's `fans-off` does this).
- [ ] Never require more than one field with neither a default `value` nor
      a checkbox at once — see `actions-and-jobs.md` §3. The client asks
      out loud for exactly one such field and refuses the action outright
      if there's a second.
- [ ] A field's `type` other than `"checkbox"` gets no special client-side
      treatment — it's rendered as a plain text input regardless of what
      you put there (`"number"`, `"date"`, `"hidden"`, ...). Design forms
      accordingly (see the open question below).

**Long-running work**

- [ ] An action that cannot answer synchronously within ~60 seconds
      (`hypermedia-contract.md` §10) returns `202` with a job-shaped entity
      whose `properties.state` is `"running"`.
- [ ] The document that offered the action carries a `link` whose `rel`
      equals one of the `class` values on that job entity. Without this,
      RESTForge will not find anything to poll and will simply not watch
      the job — silently, with only a debug log line, no user-visible
      error. This is the single easiest thing to get wrong when adding new
      long-running work to an existing backend.
- [ ] The polled resource keeps returning `properties.state == "running"`
      while work continues, and something else (any other string) once
      it's done — success or failure both count as "something else"; see
      `actions-and-jobs.md` §6 for why HTTP status is the wrong channel for
      the *eventual outcome* of the work.
- [ ] If more than one process/instance can answer requests for the same
      job (load balancing, multiple replicas), either make every instance
      answer consistently, or put a stable `id` on the job entity so the
      client can tell "not my job" from "my job, finished" — see
      `actions-and-jobs.md` §5.
- [ ] Send `properties.staleAfterSeconds` (a number, seconds) on job
      entities, sized to your own actual worst case. Clients fall back to
      25 minutes when it's absent — safe, but not tuned to your workload.
- [ ] If your backend can validly say "there's nothing to report yet/here"
      for the polled resource, use the literal string `"none"` for
      `properties.state` in that case — clients recognise it specifically
      as "not relevant, keep waiting", distinct from "finished".

**Errors**

- [ ] Error bodies use the same envelope as everything else:
      `{"class": ["error"], "properties": {"status": N, "message":
      "..."}}`. `properties.message` is shown to the user directly when
      present — write it as user-facing prose, not a machine code.
- [ ] Use `409` specifically for "the state you acted on is stale" —
      that's the one status the client re-fetches on automatically. Don't
      use it for anything else a client shouldn't react to that way.
- [ ] If your backend can legitimately re-judge an already-confirmed,
      checkbox-gated action and say `409` on a race (like f3sctl's
      double-probed `fans-off`), design for the client's bounded retry:
      it will resend the identical, already-confirmed field values once,
      within 60 seconds, with no new prompt. Accept that retry the same
      way you'd accept the original request.

**Miscellaneous, but worth doing**

- [ ] A POST with fields but also a fieldless POST (a body-less-in-content
      but Content-Length-bearing empty POST) should both be accepted —
      some HTTP front ends (bozohttpd, in f3sctl's case) reject a POST with
      no `Content-Length` header outright before your handler ever runs.
      Both current clients always send one; a bespoke or unusual HTTP
      front end is the thing to double check.
- [ ] Consider a diagnostic response header present on every reply,
      success or error (f3sctl's `X-F3sctl-Node`) if your backend has more
      than one instance or node that can answer. Clients log every
      response header verbatim with no configuration needed to benefit
      from this.
- [ ] A `describedby` link to a machine-readable description (OpenAPI or
      otherwise) is good practice for humans and tooling but is not read
      by either client — the live Siren responses are authoritative over
      it, per the contract.

## Open questions / gaps

These could not be resolved with confidence from the client source alone,
and are flagged here rather than guessed at silently, per this task's
instructions.

1. **Plain HTTP vs. HTTPS.** Nothing in either client's Siren/HTTP layer
   refuses a `http://` base URL outright — `settings.js`/`settings_service.dart`
   both accept `^https?://` for validation. But `flutter/AGENTS.md`'s
   troubleshooting section documents that Android blocks cleartext HTTP by
   default at the platform level (`AndroidManifest.xml`), independent of
   RESTForge's own code, and its explicit advice is "fix the API rather
   than add an exemption" — so a backend intended for the Flutter app on a
   real Android device should be HTTPS in practice, even though nothing in
   this repository's client code enforces it directly. The Pebble app's
   PebbleKit JS companion runs inside the phone's own JS runtime and app
   sandbox; whether the Rebble companion app enforces an equivalent
   cleartext restriction was not verified — it depends on the companion
   app/OS, not on anything in `pebble/src/pkjs/`.

2. **Pagination / large collections.** `Entity.entities` (both `siren.dart`
   and `siren.js`) is a flat list with no concept of "more pages" —
   there is no documented `rel` convention (a `next` link, say) either
   client looks for, and nothing in either client paginates a request. A
   backend with a genuinely large collection has no verified mechanism in
   this contract to page it; the natural hypermedia answer would be a
   `next`/`prev` rel link on the collection entity, which a client *would*
   render generically as an ordinary followable link — but this is
   inferred from the general design, not something either client's tests
   or source specifically implement or exercise. Treat pagination as
   unspecified rather than unsupported.

3. **Non-checkbox field types.** The client special-cases exactly one Siren
   field `type` value (`"checkbox"`); everything else renders as a plain
   text input with no client-side validation, formatting, or
   type-appropriate widget (no number stepper, no date picker, no radio
   group). A backend designed around richer field types will not get
   richer client-side behaviour from RESTForge as it exists today — only a
   text field and whatever server-side validation you do on submission.

4. **Rate limiting.** No status code in this contract maps specially to
   `429 Too Many Requests` — it would currently fall into the generic
   `FailureKind.client` bucket, shown like any other `4xx`, with no
   built-in backoff or `Retry-After` handling on the client side. f3sctl's
   own `CLIENT.md` doesn't mention `429` either. If a new backend needs
   rate limiting, its behaviour under `429` from RESTForge's client
   perspective is unspecified rather than tested.

5. **A backend's own versioning beyond `apiVersion`.** The contract has
   nothing to say about how a backend versions its own releases (f3sctl
   carries a separate `properties.version` string, e.g. `"v0.1.0"`,
   entirely unread by either client) — only `properties.apiVersion`
   matters to RESTForge, and only as a single monotonically increasing
   integer with no negotiation, range, or "understands 1 through N" story
   beyond "greater than what I support is fatal, otherwise proceed".
