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
lib/screens/            the UI
lib/services/           HTTP, Siren, navigation, actions, settings, secrets
lib/models/             value types for Siren documents and rendered frames
test/                   unit and widget tests
android/                Android host project (INTERNET permission lives here)
linux/                  Linux desktop host project, for development
```

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

## 5. Commit Policy

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

## 6. Troubleshooting

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
