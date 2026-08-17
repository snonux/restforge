# RESTForge watchapp design

This is the watchapp's structural map: how the watch/phone split draws the
work, the layout on two screens, what bit, and a per-file guide. The
contract *both* apps keep — "The rule everything else follows from" and
"Invariants worth protecting" — lives once at
[`../../docs/DESIGN.md`](../../docs/DESIGN.md), so it is not stated twice and
does not drift; read that before changing anything here, because those are
behavioural requirements rather than implementation notes. This file is the
watchapp-specific half.

## The split

The watch renders documents and reports button presses. Everything else — every
HTTP request, every Siren document, the navigation stack, and all the secrets —
lives in PebbleKit JS on the phone.

That is not a performance decision. The watch has no IP stack, and a Siren
document is several kilobytes of JSON with no business in 128 KB of app RAM.
But the reason it is drawn *here* rather than somewhere convenient is a
security one, and it holds by construction:

> **The watch never learns an href.**

Everything the watch can say is "the user did X to row N". Everything the phone
says back is one rendered frame: a title, a list of `label \t sublabel` rows, a
state, and optionally an overlay. A compromised watch app cannot leak an API
key, because it never had one, and it cannot reach an endpoint it was not
offered, because it cannot name one.

```
   watch                             phone (PebbleKit JS)
   ─────                             ────────────────────
   win_list      ── row N pressed ──▶  session.js (coordinator)
   win_detail    ◀── one frame ──────      ├─ nav.js     ──▶ http.js ──▶ the server
   win_prompt                              └─ actions.js ◀── siren.js ◀──
   doc.c / comm.c                      render.js, live.js, settings.js (the only
                                        module that touches a secret)
```

## The rule and the invariants

The rule everything else follows from, and the invariants worth protecting,
are the contract both apps keep and are stated once, at
[`../../docs/DESIGN.md`](../../docs/DESIGN.md). They are not repeated here.

## Things that bit, and would again

Getting a build onto real hardware has its own set, kept separately in
[SIDELOADING.md](SIDELOADING.md). The short version: the companion app is not
called "Pebble", the developer connection needs two switches and turns itself
off, and `adb shell input text` corrupts a long API key into something that
presents as an auth failure.


- **The emulator is a false green for syntax.** Its JavaScript runtime is
  modern; the watch's is ES5.1, and the build does not transpile. Only the ES5
  grep catches this.
- **`onerror` never fires** on the failures you would expect it to. A DNS
  failure, TLS error or refused connection sets `status` to 0 and fires only
  `readystatechange`. Unreachability is detected as `readyState === 4 &&
  status === 0`; a client waiting for `onerror` simply hangs.
- **A POST must carry `Content-Length`**, which means `send('')` and never
  `send()`.
- **There is no `URL` constructor**, no `fetch` and no promises. Hence
  `url.js`.
- **The AppMessage inbox size is negotiated with the phone**, not a property of
  the watch model. The watch reports what it actually got and the phone sizes
  its chunks to fit. Assuming the maximum works on a developer's phone and
  fails on a user's.
- **An oversized inbox reports `APP_MSG_BUSY`**, not `APP_MSG_BUFFER_OVERFLOW`.
  The SDK docs are stale on this point.
- **`MESSAGE_KEY_Foo` is a variable, not a constant**, so it cannot appear in a
  `switch` label. Dispatch with `dict_find`.
- **The SDK truncates a font at 256 glyphs**, silently, by breaking out of its
  walk of the typeface's character map. A larger subset loses everything with a
  higher codepoint — which is why the font resources set `"extended": true`.
  This app renders whatever a server sends, so the subset has to cover more
  than ASCII; anything outside it draws as a hollow box rather than an error.
- **A round display clips what a text layout thinks fits.** Clipping happens in
  the frame buffer, long after layout decided the line was fine. Widths are
  derived from the chord (`layout.c`), and text is insetted so it ellipsises
  visibly rather than vanishing into the bezel.

## Layout on two screens

Type is as large as legibly fits, because a watch is read at arm's length and
not studied. But "as large as possible" and "readable" are the same
requirement, and they pull apart at exactly one point: when the type has grown
large enough that the words stop fitting. Past that point the larger face is
the *less* legible one, and the layout gives way rather than truncating.

- **The focused row is the reading surface.** 34 px, wrapped over up to four
  lines — dropping to 26 px if the label needs more than that, so a long title
  is shown whole rather than large and cut off (`layout_label_font`).
- **Every other row is an index entry.** 22 px over up to two lines. Smaller
  than the focused row on purpose: at 26 px on a 200 px screen a row holds
  about thirteen characters, so `Power off every f-host (f0-f3)` arrived as
  `Power off ev…`. Two smaller lines show the whole thing.
- **The second line yields to the first.** An unfocused row whose label already
  wrapped drops its sublabel; the label is what the row is for.
- Anything that still does not fit opens full-screen in a scrolling view at
  28 px.

No row height is a constant. They are measured from the actual font at runtime
(`layout_cell_height`), because a label being readable only holds if the box it
is drawn into was sized around it. Two traps live here:

- **`graphics_draw_text` clips to the graphics context, not to the box it was
  handed.** With `GTextOverflowModeWordWrap` a long label draws straight past
  the bottom of its cell and over the next row. `GTextOverflowModeTrailingEllipsis`
  wraps *within* the box and stops at its edge, which is what the measured
  height assumed.
- **`menu_layer_set_center_focused(true)` on both shapes**, not just the round
  one. It is the mode the SDK supports a taller focused row in; with it off,
  the layout is computed from cell heights that go stale the moment the
  selection moves, and a row measured as an index entry but drawn as the
  reading surface spills over its neighbours.

## Where to look

| Concern | File |
|---|---|
| Startup, window stack, overlay routing | `src/c/restforge.c` |
| AppMessage, chunk reassembly, inbox negotiation | `src/c/comm.{h,c}` |
| The document on screen | `src/c/doc.{h,c}` |
| Fonts, geometry, measurement, state colours | `src/c/layout.{h,c}` |
| The list window (every screen) | `src/c/win_list.{h,c}` |
| Full-screen reading | `src/c/win_detail.{h,c}` |
| Confirming, and asking for a value | `src/c/win_prompt.{h,c}` |
| Event wiring, and nothing else | `src/pkjs/index.js` |
| Coordinator: wires nav.js and actions.js, the public API | `src/pkjs/session.js` |
| Navigation stack, frame assembly, fetching, idle refresh | `src/pkjs/nav.js` |
| Siren action field-filling, confirmation, the 409 retry | `src/pkjs/actions.js` |
| Following work that outlives its request | `src/pkjs/live.js` |
| Entity → rows | `src/pkjs/render.js` |
| Siren lookups, all generic | `src/pkjs/siren.js` |
| HTTP, timeouts, error kinds | `src/pkjs/http.js` |
| RFC 3986 resolution | `src/pkjs/url.js` |
| Backends and secrets | `src/pkjs/settings.js` |
| The phone settings page | `src/pkjs/configpage.js` |
| Frame encoding and chunking | `src/pkjs/appmessage.js` |
