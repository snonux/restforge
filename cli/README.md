# RESTForge for the terminal

The Go/Charm sibling of [RESTForge](../README.md): a generic
[Siren](https://github.com/kevinswiber/siren) hypermedia browser that knows
Siren and HTTP and nothing whatsoever about the servers it talks to — the
third implementation, alongside [`../pebble`](../pebble) and
[`../flutter`](../flutter).

Point it at an API and it renders whatever that API offers — properties,
sub-entities, links and actions. Follow a link and it fetches it. Invoke an
action and, after confirming, it performs it, then re-reads the document to
see what changed.

One binary, two ways to use it: run `restforge` with no arguments for an
interactive Bubble Tea TUI, or give it a subcommand (`restforge get`,
`restforge act`) for a scriptable one-shot invocation with a process exit
code — the same split tools like `kubectl` or `gh` use. What carries over
from the other two apps is everything that was never about the terminal —
see [AGENTS.md](AGENTS.md) and [../docs/DESIGN.md](../docs/DESIGN.md).

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss) for the TUI,
[Cobra](https://github.com/spf13/cobra) for the CLI subcommands, and TOML
config via [BurntSushi/toml](https://github.com/BurntSushi/toml).

![restforge demo: opening screen, browsing a document, and confirming, watching and re-fetching an action](docs/demo.gif)

Recorded against the shared fixture Siren API with
[VHS](https://github.com/charmbracelet/vhs) — `just record-demo`
(`tools/record-demo.sh` + `tools/demo.tape`) reproduces it. The confirm/watch/
re-fetch moments run through `restforge act`, not the TUI: this recording
predates the TUI's own Confirm and ValuePrompt overlay screens, which are now
implemented (see "What it does today" below) — only the Detail overlay
screen still renders a placeholder. Confirm/ValuePrompt share the same
`internal/session` state machine `restforge act` does, so the recording will
gain a TUI-driven version of that part once it is re-recorded.

## What it does today

- **Renders whatever a document offers** — properties, embedded sub-entities,
  links and actions, in the order the server sent them, with nothing hidden
  or reworded (`internal/render`).
- **Follows links** by the href the server put in the document, resolved
  against the configured base — never builds a URL (`internal/urlresolve`,
  `internal/nav`).
- **Asks before acting, via `restforge act`**: any method outside
  `GET`/`HEAD`/`OPTIONS`/`TRACE` is refused unless `--yes` confirms it, and a
  required field with no default is asked for via `--value` rather than
  invented (`internal/action`).
- **Re-reads after acting**: every action is followed by an unconditional
  re-fetch, so a `409` is re-read and never retried.
- **Watches long-running jobs** without claiming them finished: `restforge
  act` blocks on a watchable (202) response, printing progress to stderr
  until the server says it is done or the wait is given up on, never both at
  once (`internal/live`).
- **Keeps a failed request from replacing the document** — the CLI reports
  the failure kind and message rather than printing an empty result
  (`internal/failure`).
- **Edits configured backends interactively** — the TUI's Settings screen
  (open it with `s` from the opening screen) adds, edits and removes a
  backend's name, base URL, auth header, secret and start rel, saving through
  the same TOML file `restforge get`/`restforge act` read.
- **Saves and removes shortcuts from the TUI** — `s` on a Document row saves
  it (a link or a safe/unsafe action; a property or embedded sub-entity is
  refused, reported inline rather than silently ignored), `d` on Home's
  Quick list removes the one under the cursor, both through
  `internal/quick`, the same TOML file every other write goes through.
- **Navigates with vi keys, in addition to the arrows** — `h`/`j`/`k`/`l`
  stand in for back/down/up/select everywhere a cursor moves or a screen can
  be dismissed, except inside a text field (ValuePrompt, or a Settings
  backend being edited), where a literal `h` or `l` is still just a
  character being typed.
- **Filters the current screen's list** — `/` opens a fuzzy filter (Home's
  two lists, Document's rows, Settings' backend list) matching against
  everything a row shows, not only its title — a property's rendered value,
  a backend's base URL, a shortcut's resolved backend name, and so on.

The TUI's Home (backend/shortcut picker), Document (rendering a fetched
entity), Confirm (yes/no on an unsafe action), ValuePrompt (a required-field
prompt), Detail (a scrollable full-text reading view for a property too long
for its row) and Settings (backend editor) screens are implemented.
`restforge get` prints a document's full text unabridged either way; the
underlying `internal/session` coordinator both the TUI and the one-shot
commands share is what both now render Detail's state through.

## Requirements

- Go 1.26.5+ (see `go.mod`'s `go` directive).
- [`just`](https://github.com/casey/just).
- No emulator, device or browser — a terminal is all `run`, `test` and `check`
  need.

## Development

```sh
just run          # run the TUI from source (go run ./cmd/restforge)
just test         # go test ./..., including the integration suite (below)
just check        # check-secrets + check-code
just check-code   # go vet, gofmt, and the genericity grep, without the secret scan
just build        # build ./bin/restforge
just install      # go install ./cmd/restforge into $GOBIN/$GOPATH/bin
just clean        # remove bin/ and go clean
```

`just check` must print nothing.

`just test` needs no manual setup: `cli/internal/integration`'s suite starts
the shared fixture Siren API itself (as a subprocess, on a free port) and
tears it down when the tests finish, so a single `go test ./...` — what `just
test` runs — is enough, no second terminal required. It drives the full
browse/confirm/act/re-fetch loop both through `internal/session` directly and
through the compiled `restforge get`/`restforge act` binary as a subprocess,
exercising the fixture's awkward cases: a 401, a 409, a non-JSON response, a
request that outruns the read timeout, a roughly-24-second job, and a
required field only a human can fill.

## Pointing it at the shared fixture API

For interactive, hands-on use (rather than `just test`'s self-contained
suite), start the same fixture server the other two apps use:

```sh
just -f ../pebble/Justfile fixture      # :8731, secret "open-sesame"
```

It deliberately shares no vocabulary with anything real, so browsing it
demonstrates the one thing a single real backend cannot: that the client
renders an API it has never seen. It also reproduces the awkward cases on
demand — see the integration-test description above.

Point `restforge` at it by adding a backend to the config file (below) with
`base_url = "http://localhost:8731/"` and `secret = "open-sesame"`, then run
`just run` (TUI) or `restforge get` / `restforge act ...` (one-shot, once
installed or built).

## Configuring backends

`restforge` reads and writes one TOML config file: `$RESTFORGE_CONFIG` if
set, otherwise `--config PATH`, otherwise
`<os.UserConfigDir()>/restforge/config.toml` (honours `$XDG_CONFIG_HOME` on
Linux). Backends live under a top-level `backends` array of tables, and saved
shortcuts under a top-level `quick` array — the same file, the Settings
screen and every one-shot command agree on. A full example:

```toml
[[backends]]
name        = "fixture"
base_url    = "http://localhost:8731/"
auth_header = "X-API-Key"
secret      = "open-sesame"
start_rel   = ""

[[backends]]
name        = "pantry"
base_url    = "https://pantry.example.com/api/"
auth_header = "X-API-Key"
secret      = "put-your-real-key-here"
start_rel   = "inventory"

[[quick]]
label        = "brew a pot"
backend_name = "pantry"
base_url     = "https://pantry.example.com/api/"
kind         = "action"
holder       = "https://pantry.example.com/api/"
name         = "brew"
href         = ""
```

| Backend field | Meaning |
|---|---|
| `name` | What the picker calls it |
| `base_url` | The API root, absolute. A trailing `/` is added if you omit it |
| `auth_header` | Header the secret is sent in. Defaults to `X-API-Key` |
| `secret` | The API key |
| `start_rel` | Optional: a link `rel` to open immediately after the root |

The secret goes into `auth_header` as a request header and nowhere else —
never into a query string, which would put it in the server's access log
and, behind a reverse proxy, the proxy's log too. There is no OS keystore
this client defers to (unlike the Flutter app's `flutter_secure_storage`), so
the config file is the one place a backend and its secret live together;
`internal/config` creates it at file mode `0600` and warns on load if a
looser mode is found. Keep the file itself out of the repo and off shared
storage.

A `quick` entry is either a `link` (`href` set) or an `action` (`kind =
"action"`, `name` set, `href` left empty — the action is looked up by name on
`holder` again each time, never invoked by a remembered href). The Home
screen already runs a saved shortcut (`internal/quick`, `Session.RunQuick`);
saving a new one from the TUI (a long-press-style binding on a link or action
row) is not wired up yet, so add one by hand-editing the config file for now.

## Command-line one-shot usage

```sh
restforge get                                  # the selected backend's root
restforge get https://example.com/api/shelves  # an explicit href
restforge get --rel shelves                    # follow this rel from the root
restforge get --output json                    # raw decoded document, for jq

restforge act brew --yes                          # an unsafe (POST) action, confirmed
restforge act brew --field strength=7 --yes        # ...with a field value supplied
restforge act label-jar --value "cinnamon" --yes   # answer a required field
restforge act cool-down --yes                      # confirm another unsafe action
```

A one-shot subcommand needs a backend to target: with exactly one configured
it is used automatically; with several, pass `--backend NAME`; with none
configured at all, add one first (through the TUI's Settings screen, or by
hand-editing the config file). `--config PATH` overrides the config file
location for one invocation. Exit codes: `0` success, `1` general failure
(the request did not produce a usable answer), `2` usage error (bad flags —
nothing reached the server), `3` an action was refused or withdrawn (a
`409`, or an unsafe action run without `--yes`). `restforge --help` and
`restforge <subcommand> --help` document every flag.

## Release builds

Go's toolchain builds the binary directly for the host — no Docker, cross
compiler or platform SDK needed for a Linux build.

```sh
just build      # ./bin/restforge
just install    # go install ./cmd/restforge, into $GOBIN or $GOPATH/bin
```

Cross-compiling for another OS/architecture is ordinary `GOOS`/`GOARCH`
`go build` — nothing in this module needs cgo.
