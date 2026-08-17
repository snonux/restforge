# RESTForge for Android - AGENTS.md

A generic Siren hypermedia browser for Android, in Dart/Flutter.
Fedora Linux + Flutter stable 3.41+ / Dart 3.11+ (August 2026)

This file is the workflow reference for the Flutter app: how to build, run and
check it, and what a change here must not break.

- What both apps are and how they relate: [../README.md](../README.md)
- The rules both are held to: [../AGENTS.md](../AGENTS.md)
- The design and its reasoning, written down once for both:
  [../pebble/docs/DESIGN.md](../pebble/docs/DESIGN.md)

## 1. One-Time Initial Setup

```bash
sudo dnf install -y just gtk3-devel clang cmake ninja-build pkg-config xz-devel
# Flutter stable, unpacked at ~/flutter
export PATH="$HOME/flutter/bin:$PATH"
flutter doctor -v
```

Android builds also need the Android SDK and **JDK 17** — Flutter's Gradle does
not support newer JDKs. On Fedora, GraalVM 17 works:

```bash
flutter config --jdk-dir=/usr/lib/jvm/graalvm-community-openjdk-17.0.9+9.1
```

## 2. After Every Reboot - Restore PATH

```bash
export PATH="$HOME/flutter/bin:$PATH"
flutter --version
```

Or skip it and point the Justfile at the binary instead:
`just FLUTTER=$HOME/flutter/bin/flutter test`.

## 3. Justfile Workflow

- `just pub-get`: resolve dependencies (after any `pubspec.yaml` change)
- `just run-linux`: the fast loop — hot reload, no device or emulator
- `just run-android`: on a connected device
- `just devices`: what Flutter can currently see
- `just test`, `just analyze`, `just format`
- `just check`: analysis, the genericity grep, and the repo-wide secret scan
- `just build-apk`, `just build-apk-arm64`, `just install-apk`, `just build-linux`
- `just clean`, `just doctor`

Daily loop:

1. Restore PATH.
2. `just -f ../pebble/Justfile fixture` in one terminal — the fixture API.
3. `just run-linux` in another.

Linux desktop is a development target, not a product. It exists because a hot
reload against a fixture API in two seconds is worth more than fidelity for
most of this work. Anything touching storage, permissions or the platform
channel has to be checked on a real Android device, because that is where those
behave differently.

### The checks that cannot fail loudly

`just check` must print nothing.

- **Secrets.** The scan lives in the root Justfile and covers the whole working
  tree, both apps at once: they share one repo and one set of keys, so a check
  rooted in one subtree would have a blind spot. It looks for the contents of
  any `~/.*apikey*` file and reports which file leaked, and nothing about the
  key itself. It does not scan history — that is what `git-filter-repo` is for.
- **Genericity.** No path, action name or vocabulary belonging to any
  particular server may appear in `lib/`. RESTForge knows Siren and HTTP and
  nothing else, and a single hardcoded rel would quietly end that. The grep
  matches inside comments too, so prose naming a server trips it. Reword the
  comment rather than loosening the grep — a check with known-good noise in it
  stops being a check.
- **Analysis.** `flutter analyze` against `analysis_options.yaml`, clean.

The watchapp has a third check, an ES5 grep, with no analogue here: its build
does not transpile and its device runs ES5.1, so modern syntax ships broken
while the emulator stays green. Dart is compiled ahead of time and a syntax
error is a build failure. Do not port that check; do not assume its absence
means this app has fewer rules.

## 4. Project Layout

```
pubspec.yaml            dependencies and app metadata
analysis_options.yaml   lints
Justfile                run/build/test/check shortcuts
lib/main.dart           entry point, theme
lib/screens/            the UI: one StatefulWidget/State per screen
lib/services/           HTTP, Siren, navigation, actions, settings, secrets
lib/models/             value types: Siren documents, rendered frames,
                         Result/Failure (see section 5)
test/                   unit and widget tests, mirroring lib/'s subfolders
android/                Android host project (INTERNET permission lives here)
linux/                  Linux desktop host project, for development
```

A file lives under `models/` if it is a value with no behaviour beyond
reading itself (a Siren document, a rendered frame, a `Result`); under
`services/` if it does I/O, holds mutable state, or coordinates other
services (HTTP, navigation, the action pipeline); under `screens/` if it is a
widget a route can push. A type never imports across that grain backwards —
`models/` does not import `services/` or `screens/`, `services/` does not
import `screens/` — so a model stays trivially unit-testable and a service
stays testable without `flutter_test`.

The port follows the watchapp's module split rather than inventing a new one,
because that split is along the concerns the design cares about and the tests
are written per concern. The mapping:

| Concern | Watchapp | Here |
|---|---|---|
| RFC 3986 resolution | `pebble/src/pkjs/url.js` | `lib/services/url_resolver.dart` |
| HTTP, timeouts, error kinds | `http.js` | `lib/services/http_service.dart` |
| Siren lookups, all generic | `siren.js` | `lib/models/siren.dart` |
| Entity → rows | `render.js` | `lib/services/render_service.dart` |
| Navigation stack, fetching, idle refresh | `nav.js` | `lib/services/nav_service.dart` |
| Action policy, confirmation, the 409 retry | `actions.js` | `lib/services/action_service.dart` |
| Work that outlives its request | `live.js` | `lib/services/live_service.dart` |
| Backends and secrets | `settings.js` | `lib/services/settings_service.dart` |
| Saved shortcuts | `quick.js` | `lib/services/quick_service.dart` |
| Coordinator | `session.js` | `lib/services/session.dart` |

What does **not** carry over is the watch/phone split and everything that
served it: AppMessage, frame chunking, the row-index protocol, the four-button
input model, and the fixed-cell layout measured against a 200 px screen. The
watch never learns an href because it cannot be trusted with one and could not
use it anyway; here the same process holds the href, the key and the screen, so
that particular defence is not available and not needed. Do not port
`appmessage.js`, `doc.c`, `comm.c` or the `win_*.c` layout logic — port what
they were carrying, not the pipe.

## 5. App Conventions

These four decisions were made once, before any service existed, so that
nobody has to re-decide them mid-screen. Changing one is a conversation, not
a drive-by in a PR.

### State management

Plain `ChangeNotifier`, exposed to the widget tree with `InheritedNotifier`
(or the `ListenableBuilder`/`AnimatedBuilder` a screen already has), is the
default for anything a screen needs to rebuild against — the current
document, the in-flight request, the current backend. No state-management
package (`provider`, `riverpod`, `bloc`, ...) is added unless a screen's
actual need outgrows what those two SDK classes cover cleanly, and the
reason is written down at the point it's added.

Reasoning: this app is a browser for one document at a time. The state that
changes is "which document is on screen, and what happened to the last
request for it" — a shape `ChangeNotifier` already fits without asking every
screen to learn a second vocabulary for the same idea. Reaching for a
dependency before a documented need exceeds `ChangeNotifier` is exactly the
premature abstraction YAGNI warns against, and it would be a second way to
do the one thing this app does with state, which is a coupling cost with no
buyer yet.

### Error model

A transport failure is a value returned to the caller, never an exception
thrown at it. `lib/models/failure.dart` and `lib/models/result.dart` give
this shape:

- `FailureKind` is the closed set of reasons a request did not produce a
  usable result — `unreachable`, `timeout`, `auth`, `conflict`, `server`,
  `client`, `parse`, `config` — carried over from the vocabulary in
  `pebble/src/pkjs/http.js`. The distinction that matters most is
  `unreachable` against every other kind: a request that never arrived says
  nothing about the state of the thing it asked about, and must not be
  rendered as if the server had answered.
- `Failure` pairs a `FailureKind` with the HTTP status (0 when there wasn't
  one) and a human-readable message.
- `Result<T>` (`Ok<T>` / `Err<T>`) is what a service returns instead of `T`,
  so a caller `switch`es on the outcome instead of wrapping the call in
  try/catch.

Reasoning: `pebble/docs/DESIGN.md` makes "a failed request is not an answer"
an invariant for both apps — the last good document must stay on screen with
the reason on top of it, never replaced by an empty one. An exception is the
wrong vehicle for that: it unwinds past the point where the old document was
still in scope, and a screen that forgets one `catch` silently drops the
distinction the design exists to protect. A `Result` returned as an ordinary
value makes forgetting to look at it a `null`/exhaustiveness question the
analyzer catches, not a runtime surprise. This is also why the two types
live in `lib/models/`: they carry no behaviour beyond being read, exactly
like every other value type there (see section 4).

### Where models, services and screens live

Covered in section 4's layout table and the paragraph under it. The short
version: no behaviour in `models/`, no widgets in `services/`, no direct I/O
in `screens/` — a screen asks a service, a service returns a `Result`
wrapping a model.

### Test style

- **Services get pure Dart unit tests** (`test/services/`, mirroring
  `lib/services/`) — `package:test`/`flutter_test`'s non-widget API, no
  `WidgetTester`, no platform channel. A service that needs a fake HTTP
  backend gets one built in Dart (or points at the fixture Siren server, see
  README.md), never a mock that needs a device.
- **Models get pure Dart unit tests** (`test/models/`) for the same reason —
  they have no widget to pump.
- **Screens get widget tests** (`test/screens/`) via `testWidgets` and
  `tester.pumpWidget`, asserting on what's rendered, never on a device
  feature (`test/screens/home_screen_test.dart` is the existing example).
- **No test may require a physical device or emulator.** `just test` has to
  run in CI and in a fast local loop with neither attached; anything that
  only proves itself on hardware belongs in the manual checks in section 7,
  not in `test/`.

## 6. Commit Policy

Commit everything under `lib/`, `test/`, `android/`, `linux/`, plus
`pubspec.yaml`, `pubspec.lock`, `analysis_options.yaml`, `Justfile`,
`README.md`, `AGENTS.md`, `.metadata` and `.gitignore`.

Never commit:

- `build/`, `.dart_tool/`, `linux/flutter/ephemeral/`, `android/.gradle/`
- `android/local.properties`, `android/key.properties`, `*.jks`, `*.keystore`
- **anything holding an API key** — `just check-secrets` enforces this. A key
  belongs in a file outside the repo, mode 0600, and is typed into the app's
  settings screen. Never into a commit, a command line (where `ps` can read it)
  or a log.

`pubspec.lock` is committed: this is an application, not a library, and a
reproducible dependency set is worth more than automatic minor upgrades.

## 7. Troubleshooting

- `flutter: command not found`: restore PATH from section 2.
- Gradle fails with an unsupported JDK: Flutter needs JDK 17; see section 1.
- `flutter run -d linux` fails to configure: a `-devel` package from section 1
  is missing. `flutter doctor -v` names it.
- No device listed: `adb devices` first — USB debugging is a per-cable, per-host
  authorisation and it lapses.
- The fixture API is unreachable from a physical device: `localhost` is the
  phone, not the workstation. Use the LAN address or
  `adb reverse tcp:8731 tcp:8731`.
- A backend that works on Linux desktop fails on Android: check
  `android/app/src/main/AndroidManifest.xml` still declares `INTERNET`, and
  that the API is HTTPS — Android blocks cleartext HTTP by default, which is a
  reason to fix the API rather than to add an exemption.
- The app says `unauthorized` against a server you know the key works on:
  suspect the key that was *typed*, not the key itself. Verify the stored value
  by hash, never by eye.

Last updated: August 17, 2026
Maintained for: RESTForge agents
