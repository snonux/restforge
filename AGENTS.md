# RESTForge - AGENTS.md

This repository holds **two** apps, and almost everything an agent needs is in
the one it is working on. This file exists to route you there and to state the
few things that are true of both.

- What the project is and how the two relate: [README.md](README.md)
- The design and its invariants, written down once for both:
  [docs/DESIGN.md](docs/DESIGN.md)

| Working on | Read |
|---|---|
| The Pebble watchapp | [pebble/AGENTS.md](pebble/AGENTS.md) |
| The Android app | [flutter/AGENTS.md](flutter/AGENTS.md) |

## Layout

```
pebble/     The Pebble watchapp: C on the watch, ES5 PebbleKit JS on the phone.
            Its own Justfile, package.json, wscript, docs/ and tests.
flutter/    The Android app: Dart/Flutter, also runnable on Linux desktop.
            Its own Justfile, pubspec.yaml and tests.
Justfile    Forwards to the two above; owns the repo-wide secret scan.
README.md   What each is, and the contract both keep.
```

## Root commands

```sh
just pebble <recipe>     # e.g. just pebble dev
just flutter <recipe>    # e.g. just flutter run-linux
just test                # both suites; neither needs an emulator or a device
just check               # secret scan (whole tree) + both apps' code checks
```

`just check` must print nothing.

## What is true of both apps

These are contract, not implementation, and they survive the change of
language. The full statement of each, with the reasoning, is in
[docs/DESIGN.md](docs/DESIGN.md); the short form:

- **Never build a URL.** Follow the href the server put in the document,
  resolved against the configured base. No rel, class, action name or property
  name belonging to a particular server may appear in either app's source — the
  genericity grep in each `just check` enforces it, and it matches inside
  comments, so reword the comment rather than loosening the grep.
- **Ask before acting.** Any method outside `GET`, `HEAD`, `OPTIONS`, `TRACE`
  gets a confirmation. RFC 9110's division, not a list of scary-sounding names.
- **A failed request is not an answer.** The last good document stays where it
  is and the reason goes on top of it.
- **Rendering does not interpret**, and hides nothing — including vocabulary
  the app has never seen.
- **Never carry a document across an action.** Re-fetch unconditionally; a
  `409` is re-fetched and re-rendered, never retried.
- **Do not invent a value** for a field a server marked required.
- **Do not claim a job finished.** A reply about a different id, or one saying
  there is no job, is *no news* — not completion.
- **A secret goes in a request header, never a query string**, and never into a
  commit, a command line (where `ps` can read it) or a log. A key belongs in a
  file outside the repo, mode 0600, pasted into the app's settings.

## Versioning

One product, one version number, even though the two apps share no code, no
store and no release process. `flutter/pubspec.yaml`'s `version:` field and
`pebble/package.json`'s `version` field must always carry the same semantic
version, and a release gets exactly **one** git tag — never one per
subproject.

- `just version` — prints both apps' current version and fails loudly if
  their semver portions have drifted apart.
- `just bump-version x.y.z` — sets both files to `x.y.z` in one step. Flutter
  additionally carries a `+N` build-number suffix (Android's `versionCode`,
  which the Play Store requires to strictly increase on every upload); the
  recipe increments that `N` by 1 on every bump. It is unrelated to semver
  and Pebble has no equivalent, since the watchapp is not distributed through
  a store.

To cut a release:

```sh
just bump-version 0.7.0          # edits both version files
just version                     # confirms they now agree
just check && just test          # both apps still pass
git add flutter/pubspec.yaml pebble/package.json
git commit -m "Bump version to 0.7.0"
git tag v0.7.0                   # exactly one tag, at the repo root, not per app
git push && git push --tags
```

Both files change in the **same commit**, and the tag is created **once**,
after that commit, at the repo root — not inside `pebble/` or `flutter/`. A
tag named after only one subproject (`pebble-v0.7.0`, or a tag pushed twice
with two different messages) means the two apps have been allowed to version
independently again, which is the exact thing this section exists to
prevent.

Semantic versioning: increment `z` (patch) for fixes and small changes,
`y` (minor) and reset `z` for new features, and `x` (major) only when
explicitly asked for one — the same rule the `increment-version-and-push`
skill uses elsewhere; the difference here is that "the project" means both
subprojects together, and `bump-version` is the mechanism that keeps them
together.

## Commit policy

Per-app commit rules are in each app's own AGENTS.md. Repo-wide: never commit
anything holding an API key. `just check-secrets` scans the whole working tree,
tracked or not, and is the enforcement.

Last updated: August 18, 2026
Maintained for: RESTForge agents
