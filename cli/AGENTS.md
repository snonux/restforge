# RESTForge for the terminal - AGENTS.md

A generic Siren hypermedia browser for the terminal, in Go — the third
RESTForge implementation, alongside `../pebble` and `../flutter`.
Fedora Linux + Go 1.26.5+ (August 2026)

This file is the workflow reference for the **Go/Charm client**: how to
build, run and check it, and what a change here must not break.

- What all three apps are and how they relate: [../README.md](../README.md)
- The rules all three are held to: [../AGENTS.md](../AGENTS.md)
- The design and its reasoning, written down once for all three:
  [../docs/DESIGN.md](../docs/DESIGN.md)
- What it does today and how to configure and run it:
  [README.md](README.md)

## 1. One-Time Initial Setup

```bash
sudo dnf install -y golang just
go version   # confirm it satisfies go.mod's `go 1.26.5` directive
```

No SDK, emulator, device or browser is needed — a Go toolchain and `just` are
the whole dependency list.

## 2. Justfile Workflow

```bash
just run          # go run ./cmd/restforge -- the interactive TUI from source
just test         # go test ./..., including the self-contained integration suite
just check        # check-secrets + check-code
just check-code   # go vet, gofmt, and the genericity grep
just build         # go build -o bin/restforge ./cmd/restforge
just install       # go install ./cmd/restforge into $GOBIN/$GOPATH/bin
just clean         # rm -rf bin/ && go clean
```

Daily loop:

1. `just -f ../pebble/Justfile fixture` in one terminal, if you want a live
   backend to point the TUI or a one-shot command at by hand — `just test`
   does not need this, it starts and stops its own copy.
2. `just run` (TUI) or `go run ./cmd/restforge get ...` (one-shot) in
   another.

`just test` and `just check` are both single commands with no manual setup:
neither needs a second terminal, an emulator or a device — see README.md's
"Development" section for what the integration suite covers.

### The checks that cannot fail loudly

`just check` must print nothing.

- **Secrets.** The scan lives in the root Justfile and covers the whole
  working tree, all three apps at once: they share one repo and one set of
  keys, so a check rooted in one subtree would have a blind spot. It looks
  for the contents of any `~/.*apikey*` file and reports which file leaked,
  and nothing about the key itself. It does not scan history — that is what
  `git-filter-repo` is for.
- **Genericity.** `tools/check-genericity.sh` greps `internal/` and `cmd/`
  (production `*.go`, `_test.go` excluded) for the fixture Siren server's
  vocabulary — the same rel/class/path list `pebble/Justfile` and
  `flutter/Justfile` already grep for, kept in sync by hand across the three
  scripts. RESTForge knows Siren and HTTP and nothing else, and a single
  hardcoded rel would quietly end that. The grep matches inside comments
  too, so prose naming a server trips it — reword the comment rather than
  loosening the grep, since a check with known-good noise in it stops being
  a check. `_test.go` files are excluded because the integration suite and
  package tests legitimately talk to the fixture server by name; `.md` files
  are out of scope by construction (the script only scans `*.go`), because
  docs may name the fixture server deliberately, as README.md does.
- **`go vet` and `gofmt`.** `go vet ./...` and a `gofmt -l .` that must list
  no files — the closest Go equivalent to `flutter analyze`, run before the
  genericity grep in `check-code` so a syntax or vet problem is reported
  before a slower text scan runs.

Two checks the other two apps have that this one does not, on purpose:
- No ES5 grep — Go is compiled, not transpiled, so a syntax error is a build
  failure, not a false-green emulator. Do not port that check; its absence
  here is not a gap.
- No UI-analysis-only equivalent beyond `go vet`/`gofmt` — there is no
  separate lint config (`analysis_options.yaml`'s equivalent) yet; if one is
  added later, wire it into `check-code` alongside `go vet`.

## 3. Testing

`just test` runs `go test ./...`, which includes `internal/integration`'s
suite: it starts `pebble/tools/fake-siren-server.py` itself as a subprocess
(on a free port, torn down at the end of the run) and drives the full
browse → confirm → act → re-fetch loop against it — both directly through
`internal/session` (fastest, no process overhead) and through the compiled
`restforge` binary invoked as a subprocess (`restforge get`, `restforge act`),
so the actual argument parsing and output formatting are exercised too, not
just the library code. It covers the fixture's known awkward cases: a 401, a
409, a non-JSON response, a request that outruns the read timeout, a
roughly-24-second job, and a required field only a human can fill. No test
in this module requires a terminal, an emulator or a device — `just test`
has to run unattended in CI and in a fast local loop alike.

## 4. Project Layout

```
go.mod, go.sum          module and dependency lock
Justfile                run/test/check/build/install/clean shortcuts
tools/check-genericity.sh   the genericity grep (see section 2)
cmd/restforge/main.go   entry point: hands os.Args to internal/cli
internal/               all application code; see the table below
```

The Go client ports the same module split the watchapp and the Flutter app
already settled on, because that split is along the concerns the design
cares about and each concern's tests are written against it. The mapping,
extending [`../flutter/AGENTS.md`](../flutter/AGENTS.md) section 4's table
with a third column:

| Concern | Watchapp | Flutter | Here (Go) |
|---|---|---|---|
| RFC 3986 resolution | `pebble/src/pkjs/url.js` | `lib/services/url_resolver.dart` | `internal/urlresolve` |
| HTTP, timeouts, error kinds | `http.js` | `lib/services/http_service.dart` | `internal/httpclient` |
| Siren lookups, all generic | `siren.js` | `lib/models/siren.dart` | `internal/siren` |
| Entity → rows | `render.js` | `lib/services/render_service.dart` | `internal/render` |
| Navigation stack, fetching | `nav.js` | `lib/services/nav_service.dart` | `internal/nav` |
| Action policy, confirmation, the 409 retry | `actions.js` | `lib/services/action_service.dart` | `internal/action` |
| Work that outlives its request | `live.js` | `lib/services/live_service.dart` | `internal/live` |
| Backend value type + validation | `settings.js` (mixed with I/O) | `lib/services/settings_service.dart` (mixed with I/O) | `internal/backend` (pure value type only) |
| Backends and secrets, persisted | `settings.js` | `lib/services/settings_service.dart` | `internal/config` (TOML file I/O) |
| Saved shortcuts | `quick.js` | `lib/services/quick_service.dart` | `internal/quick` |
| Coordinator | `session.js` | `lib/services/session.dart` | `internal/session` |
| A failure as a value, not an exception | (return values in `http.js`) | `lib/models/failure.dart`, `lib/models/result.dart` | `internal/failure` |
| Command tree, one-shot dispatch, exit codes | — (no CLI-mode analogue) | — | `internal/cli` |
| Interactive front end | on-watch C (`src/c/`) + PebbleKit JS | `lib/screens/`, `lib/main.dart` | `internal/tui` |
| Version reported by the client | `package.json`'s `version` | `pubspec.yaml`'s `version` | `internal/version` (embeds `VERSION` via `go:embed`) |

`internal/backend` is split from `internal/config` the way neither JS nor
Dart splits it: this Go port keeps the pure `Backend` value type and its
`Normalise`/`Validate` free of file I/O, so `internal/nav`, `internal/action`
and `internal/live` can depend on the type without pulling in TOML or
filesystem concerns — see `internal/backend`'s package comment.

`internal/http` and `internal/url` are empty placeholder packages left over
from the initial scaffold (each holds nothing but a "scaffolding, no code
yet" doc comment) — the real HTTP and URL-resolution code landed in
`internal/httpclient` and `internal/urlresolve` instead, named to avoid
colliding with the standard library's own `net/http` and `net/url`. Do not
add code to `internal/http` or `internal/url`; if they are still empty when
you notice them, that is expected, not a bug to fix in passing.

`internal/tui`'s own package comment (`internal/tui/doc.go`) has the
file-by-file map for that package specifically — home/document/settings
screens, the shell, the async command pattern — since that is one level of
detail below what this table tracks.

## 5. Commit Policy

Commit `go.mod`, `go.sum`, every `cli/internal/**/*.go` and
`cli/cmd/**/*.go` file, `Justfile`, `tools/check-genericity.sh`, `README.md`,
`AGENTS.md`, `.gitignore`, and `internal/version/VERSION`.

Never commit:

- `bin/` — the build output directory `just build` writes into.
- A stray `restforge` binary built by hand outside `bin/` (e.g. from
  `go build ./cmd/restforge` run at the module root without `-o`).
- **anything holding an API key** — `just check-secrets` enforces this. A
  key belongs in a config file outside the repo, mode `0600`
  (`internal/config` creates one at that mode and warns on load if a looser
  one is found), typed into the TUI's Settings screen or hand-edited into
  that file. Never into a commit, a command line (where `ps` can read it) or
  a log.

## 6. Troubleshooting

- `go: command not found`: install Go (section 1) and confirm `go version`
  reports at least what `go.mod`'s `go` directive names.
- A fresh `go build`/`go test` hangs or fails resolving a module: check
  `GOPROXY`/`GOFLAGS` and the module cache (`go env GOMODCACHE`); `go clean
  -modcache` is the last resort if the cache itself is corrupted.
- `restforge get`/`restforge act` says no backends are configured: add one
  through the TUI's Settings screen (`s` from the opening screen), or add a
  `[[backends]]` table by hand to the config file — see README.md's
  "Configuring backends" section for the schema.
- Testing against a scratch config without touching a real one:
  `RESTFORGE_CONFIG=/tmp/scratch-restforge.toml restforge ...`, or the
  equivalent `--config` flag — `internal/config.Path` honours the env var
  first, which is also what the test suite uses to point at a `t.TempDir()`
  file instead of a real home directory.
- The fixture API is unreachable: confirm `just -f ../pebble/Justfile
  fixture` is actually running (port 8731) and that the configured
  `base_url` matches it exactly, trailing slash included.
- `restforge` reports `unauthorized` against a server you know the key
  works on: suspect the key that was *typed* into the config, not the key
  itself — re-check the `secret` value in the config file rather than the
  server side first.
- Change the client's version: do not edit `internal/version/VERSION` by
  hand — that desyncs it from the Pebble and Flutter apps' versions. Run
  `just bump-version x.y.z` at the repo root (see the root `AGENTS.md`'s
  "Versioning" section), which rewrites all three files together.

Last updated: August 19, 2026
Maintained for: RESTForge agents
