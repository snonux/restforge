# RESTForge

A generic [Siren](https://github.com/kevinswiber/siren) hypermedia browser.
Point it at an API and it renders whatever that API offers — properties,
sub-entities, links and actions. Follow a link and it fetches it. Press an
action and, after confirming, it performs it, then re-reads the document to see
what changed. If the work outlives the request, it keeps watching until the
server says it is done.

Grep the source of either app for the name of any particular API and you will
not find one. That is the point: a client that never builds a URL cannot
contain server-specific code, which is exactly what makes it reusable.

## Two apps, one idea

This repository holds two independent implementations of the same browser.
They share no code — one is C plus ES5 JavaScript, the other is Dart — and they
are not meant to. What they share is the contract, and every invariant that
follows from it.

| Directory | What it is | Toolchain |
|---|---|---|
| [`pebble/`](pebble/) | The Pebble watchapp, for Pebble Time 2 (emery) and Pebble Round 2 (gabbro). Split across watch and phone: the watch renders frames and reports button presses, PebbleKit JS on the phone does everything else. | Rebble Pebble SDK 4.9+, `just`, node, python3 |
| [`flutter/`](flutter/) | The Android app. One device, so no split — but the same rules, the same confirmation policy and the same refusal to know anything about a server. Also runs on Linux desktop for development. | Flutter 3.41+ / Dart 3.11+, `just` |

Start at each directory's own `README.md` for what it does, and its `AGENTS.md`
for how to build, run and check it. The design that both are held to is written
down once, in [`docs/DESIGN.md`](docs/DESIGN.md) — read the
"Invariants worth protecting" section before changing either app, because those
are behavioural requirements rather than implementation notes, and they apply to
both.

## The rule everything else follows from

> Fetch the root. Render what it offers. Never build a URL.

Concretely, in either app:

- Navigation is only ever "follow the href the server put in the document".
  Resolving that href against the configured base is RFC 3986 resolution, not
  path construction: the path came from the server, only the origin is ours.
- No rel, class, action name or property name belonging to any particular
  server appears in the source. A genericity grep enforces it.
- Anything whose HTTP method is not safe (anything but `GET`, `HEAD`,
  `OPTIONS`, `TRACE`) asks before it acts. That division is RFC 9110's, not a
  list of dangerous-sounding names.
- A failed request is not an answer. "I could not ask" never replaces the
  document on screen with an empty one.
- Secrets stay on the device that holds them and go into a request header,
  never a query string.

## Working in the repository

Each app has its own `Justfile`. The root one forwards to them and owns the one
check that has to see both at once:

```sh
just pebble dev          # → cd pebble  && just dev
just flutter run-linux   # → cd flutter && just run-linux

just test                # both test suites; neither needs an emulator
just check               # secret scan (whole tree) + both apps' code checks
```

`just check` must print nothing. It is the check to pay attention to: a leaked
API key, ES5 syntax that only fails on the watch, and server vocabulary that
should not be in a generic client are all mistakes that look fine right up
until they do not.

## Licence

[MIT](LICENSE).
