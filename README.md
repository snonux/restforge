# RESTForge

<p align="center">
  <img src="docs/logo.svg" width="120" alt="RESTForge logo">
</p>

A generic [Siren](https://github.com/kevinswiber/siren) hypermedia browser.
Point it at an API and it renders whatever that API offers — properties,
sub-entities, links and actions. Follow a link and it fetches it. Press an
action and, after confirming, it performs it, then re-reads the document to see
what changed. If the work outlives the request, it keeps watching until the
server says it is done.

Grep the source of either app for the name of any particular API and you will
not find one. That is the point: a client that never builds a URL cannot
contain server-specific code, which is exactly what makes it reusable.

## Two sister projects, one idea

This repository holds two independent implementations of the same browser.
They share no code — one is C plus ES5 JavaScript, the other is Dart — and they
are not meant to. What they share is the contract, and every invariant that
follows from it.

| Directory | What it is | Toolchain |
|---|---|---|
| [`pebble/`](pebble/) | The Pebble watchapp, for Pebble Time 2 (emery) and Pebble Round 2 (gabbro). Split across watch and phone: the watch renders frames and reports button presses, PebbleKit JS on the phone does everything else. | Rebble Pebble SDK 4.9+, `just`, node, python3 |
| [`flutter/`](flutter/) | The Android app. One device, so no split — but the same rules, the same confirmation policy and the same refusal to know anything about a server. Also runs on Linux desktop for development. | Flutter 3.41+ / Dart 3.11+, `just` |

For what each one does and how to use, build, run and check it, read its own
README and its `AGENTS.md`:

- [`pebble/README.md`](pebble/README.md) — the Pebble watchapp
- [`flutter/README.md`](flutter/README.md) — the Android app

The design both are held to — the rule everything else follows from, and the
invariants worth protecting — is written down **once**, in
[`docs/DESIGN.md`](docs/DESIGN.md); read it before changing either app, because
those are behavioural requirements rather than implementation notes, and they
apply to both.

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