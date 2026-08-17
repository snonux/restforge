# Hand-off: driving the f3sctl API from RESTForge

You are implementing a Pebble watchapp that controls the f3s homelab — powering
the rack on and off, showing host state, switching the rack fans — through
f3sctl's HTTP API.

This document is the bridge between two things that already exist:

| What | Where | Status |
|---|---|---|
| The API contract | `~/git/f3sctl/docs/CLIENT.md` | **Normative. Read it first, in full.** |
| A working 100-line client | `~/git/f3sctl/docs/client-reference.js` | Dependency-free JS, adaptable to PebbleKit JS |
| This document | you are here | Pebble-specific layer only |

**Everything about the protocol is in `CLIENT.md`, and it is normative — where
this document and `CLIENT.md` disagree, `CLIENT.md` wins.** Nothing here
repeats its rules; this covers only what changes because the client is a
Pebble watch, plus the facts I verified against the live API on 2026-08-12
running f3sctl v0.5.1.

---

## 1. The one rule you must not design around

> **Fetch the root. Render what it offers. Never build a URL.**

The API is hypermedia (Siren). Which actions exist *is* the state: no
`power-on` when the rack is already up, no action at all while a job runs, a
confirmation field on `fans-off` only while a host is still drawing power. The
server owns every one of those rules.

The tempting Pebble shortcut — hardcode the six paths, save a request, save
memory — produces an app that shows buttons which only ever return errors, and
that breaks the first time a path changes. Do not do it. The action list is
*small*: with the rack up you get seven actions, `2592` bytes of root
document. Parse it, keep `name` + `href` + `method`, throw the rest away.

Concretely, from the live API with the rack up:

```
power-off  POST /cgi-bin/f3sctl/power/off        (f0,f1,f2 — the cluster)
all-off    POST /cgi-bin/f3sctl/power/all/off    (every f-host, f3 included)
f0-off     POST /cgi-bin/f3sctl/power/f0/off     (also f1-off, f2-off, f3-off)
fans-off   POST /cgi-bin/f3sctl/fans/off         (+ a `force` field, see §6)
```

With the rack down you get the `-on` mirror instead. Your UI should be a list
built from that array, not a fixed menu.

---

## 2. Where the HTTP actually happens

A Pebble watch has no IP stack of its own. All networking runs in
**PebbleKit JS** (`src/pkjs/index.js`) on the phone; the C app talks to it over
**AppMessage**. So:

```
  src/c/restforge.c            src/pkjs/index.js              pi0/pi1
  ┌───────────────┐  AppMessage  ┌──────────────┐  HTTPS   ┌──────────────┐
  │ UI, menus,    │ ◀──────────▶ │ HTTP, Siren  │ ◀──────▶ │ f3sctl CGI   │
  │ button events │   key/value  │ parse, poll  │ X-API-Key│ (bozohttpd)  │
  └───────────────┘              └──────────────┘          └──────────────┘
```

This split decides the whole design:

- **JSON never reaches the watch.** `/status` is `5311` bytes; the root is
  `2592`. AppMessage inboxes are a few KB at best and vary by platform, and
  you have no JSON parser in C worth having. JS must reduce each response to a
  handful of scalars before sending.
- **The JS runtime is ES5 with `XMLHttpRequest`.** No `fetch`, no promises, no
  arrow functions, no `let`/`const`. `client-reference.js` uses modern syntax
  and Node APIs — port its *logic*, not its text.
- **`localStorage` is available** in the JS layer and is where the API key and
  base URL live (see §3).
- In the emulator, the JS runs on your Fedora box, so it reaches
  `f3s.buetow.org` directly — which makes `just dev` + `just logs` a complete
  test loop with no phone involved.

### Suggested message keys

Add these to `package.json` → `pebble.messageKeys` (currently `[]`). Keep the
payload flat and scalar; that is all AppMessage is good at.

| Key | Direction | Type | Meaning |
|---|---|---|---|
| `CMD` | watch → JS | int | 0 = refresh status, 1 = invoke action |
| `ACTION_IDX` | watch → JS | int | index into the last action list sent up |
| `CONFIRM` | watch → JS | int | 1 = user confirmed a `force`-style field |
| `HOSTS_UP` / `HOSTS_TOTAL` | JS → watch | int | for a "3/4 up" summary line |
| `FANS` | JS → watch | int | 0 off, 1 on, 2 unknown (**see §5**) |
| `JOB_STATE` | JS → watch | int | 0 none, 1 running, 2 done, 3 failed |
| `JOB_STEP` | JS → watch | string | prose, display verbatim, truncate to fit |
| `ACTIONS` | JS → watch | string | `\n`-separated titles, index-aligned with `ACTION_IDX` |
| `ERROR` | JS → watch | string | human-readable; empty when fine |

`ACTION_IDX` referring to *the list JS last sent* is deliberate: the watch
never learns a URL, and JS keeps the `href` privately. That is the hypermedia
rule surviving the AppMessage boundary intact.

---

## 3. The two constants, and where to put them

```js
var BASE = 'https://f3s.buetow.org/cgi-bin/f3sctl/';
var KEY  = '...';                       // 41-byte random string
```

The key goes in the **`X-API-Key` header on every request** — never the query
string, because bozohttpd logs request URIs to syslog and relayd logs
connections, so a key in a URL is written to two logs on three machines.

Ship a **Clay configuration page** so the key is entered rather than committed,
and keep it in `localStorage`. There is no login, no session, no refresh.

To get a key for development, read it from a machine that already has one —
e.g. `~/.f3sctl-apikey` on earth. Do not paste it into git.

---

## 4. Polling, and what it costs a battery

Two different loops, and conflating them is the classic mistake:

**Idle (a screen is open, nothing happening):** poll `/status` every **30–60 s**.
Nothing in the rack changes faster than that on its own.

**While a job runs:** poll the `job` link every **5–15 s**. `CLIENT.md` §5 is
normative here; three points matter enough to repeat:

- **Match `properties.id` against the job you started.** relayd load-balances
  pi0 and pi1, so a poll routinely lands on the node that did *not* run your
  job, and that node holds a *different* job — quite possibly an old failed
  one. Anything that is not your id is "no news", not a result. Clients that
  skipped this reported healthy shutdowns as failures; it has happened twice.
- **Derive your deadline from `properties.staleAfterSeconds`** (live value
  today: `1800`), not from a constant. Fall back to 25 minutes when it is
  absent.
- **The reliable completion signal is host state, not the job.** After
  `all-off` the f-hosts stop answering; after `power-on` they start. Use the
  job for *why* something went wrong.

How long a shutdown actually takes, measured on this rack (v0.5.1, 2026-08-12):
**about 3 minutes** for `all-off` — one real run was 3 m 3 s, another 3 m 11 s.
It used to be ~10 minutes before the hosts were parallelised and the guests'
NFS teardown was fixed. Do not hardcode either number; the point is that a
progress screen has minutes to fill, so showing `step` is worth the effort.

Steps you will see, in order, so you can size a label: `pre-flight checks`,
`checking the zusb backup pool`, `muting Gogios monitoring`, `stopping the CARP
failover daemons`, `shutting down f1, f2, f3 together`, `shutting down f0`,
`confirming the hosts actually powered down`, `switching the rack fans off`.
They are prose — **display them, never parse them**.

---

## 5. Three states, not two

The single highest-value thing this app can get right, and the easiest to get
wrong:

| Situation | Wrong | Right |
|---|---|---|
| Phone offline / API unreachable | "rack is off" | **"unreachable"** |
| `pingKnown: false` on a host | "off" | **"unknown"** |
| `fans` entity carries `error` | "fans off" | **"unknown"** |

If a request fails you have learned nothing about the rack — only that you
could not ask. Reporting the cluster as down because the phone lost signal is
the most likely wrong thing this app will do, which is why `FANS` above has a
third value and your host summary needs one too.

There is a fourth: a host with `ping: false` may be **off, or hung in
single-user mode** — powered on, no network, and *not* wakeable by
Wake-on-LAN. They are indistinguishable from here. If a `power-on` job
completes and a host is still silent minutes later, say it may need a physical
power cycle rather than letting the user press the button forever.

---

## 6. `fans-off` is the one action with a confirmation

Live, with hosts running, the action arrives as:

```json
{ "name": "fans-off", "method": "POST", "href": "/cgi-bin/f3sctl/fans/off",
  "type": "application/x-www-form-urlencoded",
  "fields": [{ "name": "force", "type": "checkbox", "value": false, "required": true,
    "title": "Hosts may still be running (f0, f1, f2, f3 still running): the rack fans keep them cool, so switching the plug off now risks overheating. Confirm to proceed." }] }
```

Render fields generically: `checkbox` → a confirmation dialog, anything else →
text input, and **use the field's `title` as the label**. Do not write your own
wording — the title states the *current* reason, and that reason varies (`f3
still running` is normal; `f3 could not be probed, so assumed running` means
the server is refusing to guess). When the rack is cold the same action arrives
with **no fields at all** and you show a plain button, with no code in your app
that knows the word "force".

Two Pebble-specific traps:

- **Always send a `Content-Length`, even for a body-less POST.** bozohttpd
  rejects a POST without it with a 400 *before the CGI runs*, so it looks like
  the API refusing your action. `XMLHttpRequest.send('')` gets this right.
- **Allow 60 s for `fans-off`.** It re-probes the rack before cutting cooling,
  and proving a silent host is really off takes several pings ten seconds
  apart. Every other route answers in a few seconds. Set your XHR timeout
  accordingly, per-request.

---

## 7. Errors → what the user sees

| Status | Show | Then |
|---|---|---|
| 401 | "API key rejected" | Stop. Do not retry; it will not start working. |
| 409 | *(nothing)* | **Re-fetch and re-render.** Never retry blindly, never special-case it. |
| 502 | the message | It is about the homelab, not your request. |
| 500 | the message | Needs an operator. |
| network error | "unreachable" | **Not** "rack is off". See §5. |

A 409 means you acted on stale state — the correct response is always to
re-fetch, and the new response will show the running job and offer no power
actions, which is exactly what the user should see. Two watches and a laptop
can act at once; the server serialises with a lock and does not queue.

Log the **`X-F3sctl-Node`** response header (`pi0.lan.buetow.org` /
`pi1.lan.buetow.org`) on every request. It is present on every reply including
errors, and it is the single most useful thing for debugging "why did that poll
say something different" — which is nearly always relayd having sent you to the
other node.

---

## 8. Suggested build order

1. **JS only.** `just dev` + `just logs`, no UI: fetch the root, log
   `apiVersion`, the node header, and the action names. You have a working
   client the moment that prints.
2. **Status → watch.** Send `HOSTS_UP`/`HOSTS_TOTAL`/`FANS` up; render one
   summary line. Get the three-state rule (§5) right here, before anything can
   act.
3. **Action list.** Render `ACTIONS` as a menu; send `ACTION_IDX` back; JS
   POSTs the matching `href`. Re-fetch the root after every action.
4. **Job progress.** Poll on `202`, show `step`, handle the id match and the
   `state: "none"` shape.
5. **Confirmation fields.** Only now add the `force` dialog (§6).
6. **Clay config** for base URL + key.

Check your work against `client-reference.js`, which is also the executable
proof that `CLIENT.md` describes the API that actually shipped:

```sh
F3SCTL_URL=https://f3s.buetow.org/cgi-bin/f3sctl/ F3SCTL_KEY=... \
  node ~/git/f3sctl/docs/client-reference.js status
```

A machine-readable surface description lives at the `describedby` link
(`/openapi.json`). It says what exists in general; the Siren responses say what
is possible *now*. When they disagree, the Siren response is the one to act on.

---

## 9. Facts verified against the live API

Checked 2026-08-12 against `https://f3s.buetow.org/cgi-bin/f3sctl/`, f3sctl
v0.5.1, so you can trust these without re-deriving them:

- `apiVersion` is `1`. Refuse anything you do not understand rather than guess.
- Response sizes: root `2592` B, `/status` `5311` B, `/fans` `896` B, `/job`
  `927` B, `/monitoring` `937` B.
- `links` rels: `self`, `status`, `fans`, `job`, `monitoring`, `describedby`.
- `/status` carries one entity per host (`class: ["host","f"]` or
  `["host","cluster"]`) with `name`, `ip`, `ping`, `pingKnown`, `ssh`, `ms`,
  plus one `class: ["fans"]` entity with `on` and `ip`.
- `job` properties: `id`, `action`, `state`, `started`, `finished`, `rc`,
  `node`, `step`, `updated`, `staleAfterSeconds` (1800), `hosts{}` with
  per-host `phase` (`pending`/`working`/`confirming`/`done`/`failed`) and
  `detail`.
- `/monitoring` is **not** folded into `/status` on purpose: reading it costs
  an SSH round trip to each gateway. Fetch it on user request or a slow timer,
  never in the status poll.
