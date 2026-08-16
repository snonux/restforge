# RESTForge - AGENTS.md
A generic Siren hypermedia browser for Pebble Time 2 and Pebble Round 2
Fedora Linux + Rebble Pebble SDK 4.9+ (August 2026)

This file is the workflow reference: how to build, run and check RESTForge.
For what it is and how the pieces fit, read [docs/DESIGN.md](docs/DESIGN.md).

## 1. One-Time Initial Setup

```bash
sudo dnf update
sudo dnf install -y python3-pip nodejs SDL-devel dtc uv just
uv tool install pebble-tool --python 3.13
export PATH="$HOME/.local/bin:$PATH"
pebble sdk install latest
```

## 2. After Every Reboot - Restore uv Tool PATH

```bash
export PATH="$HOME/.local/bin:$PATH"
pebble --version
```

Optional permanent fix:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

## 3. Justfile Workflow

RESTForge targets two watches, and both are checked every time: they differ in
shape, not just size, and a layout that works on one can be clipped on the
other.

- `just dev` / `just dev-emery`: build + install to gabbro / emery
- `just logs` / `just logs-emery`: live logs (second terminal)
- `just screenshot` / `just screenshot-emery`
- `just config` / `just config-emery`: open the phone settings page
- `just fixture`: the fixture Siren API on :8731 (second terminal)
- `just test`: the JavaScript unit tests — no emulator needed
- `just check`: the two greps described below
- `just kill`, `just clean`, `just rebuild`, `just quick`, `just sdk-version`

Daily loop after reboot:

1. Restore PATH (`export PATH="$HOME/.local/bin:$PATH"`).
2. `cd /path/to/restforge`
3. `just fixture` in one terminal, `just logs` in another
4. `just dev`

### The two checks that cannot fail loudly

`just check` must print nothing. Both of its greps guard against a mistake that
otherwise looks fine right up until it does not:

- **ES5.** The emulator's JavaScript runtime is modern, the watch's is ES5.1,
  and the build does not transpile. Arrow functions, `const`, `let` and
  template literals all work perfectly in the emulator and ship broken. The
  emulator is a false green; this grep is the only check.
- **Genericity.** No path, action name or vocabulary belonging to any
  particular server may appear in `src/`. RESTForge knows Siren, HTTP and
  AppMessage and nothing else, and a single hardcoded rel would quietly end
  that.

Both greps also match inside comments, so prose mentioning `const` or a
server's name will trip them. Reword the comment rather than loosening the
grep — a check with known-good noise in it stops being a check.

### Verifying a change

`just test` is fast, needs no emulator, and covers the JavaScript. It does not
cover the watch, and screenshots are not decoration: legibility is an
acceptance criterion here, and regressions in it are invisible in logs. Look at
both screens after any layout change.

The fixture API deliberately shares no vocabulary with anything real, and
reproduces the awkward cases on demand: a 401, a 409, a non-JSON response, a
request that outruns the read timeout, a job that takes 24 seconds, and a
required field only a human can fill.

## 4. Project Layout

```
package.json    Pebble manifest: platforms, message keys, font resources
wscript         Waf build rules (globs src/c and src/pkjs, so new files
                need no build change)
Justfile        dev/build/install/emulator/test shortcuts
src/c/          the on-watch app: rendering and buttons
src/pkjs/       PebbleKit JS companion: HTTP, Siren, navigation, secrets
resources/      the fonts, vendored and subsetted at build time
tools/          the fixture API and the JavaScript unit tests
docs/           DESIGN.md and the f3sctl API handoff notes
```

The companion is **not** a stub, and the watch is **not** self-contained. Every
HTTP request, every Siren document, the navigation stack and all the secrets
live in `src/pkjs/`; the watch renders frames and reports button presses. See
[docs/DESIGN.md](docs/DESIGN.md) for why, and for the invariants a change must
not break — chief among them that the watch never learns an href.

A per-file map is at the end of DESIGN.md. Every module also carries its own
rationale at the top of the file; read that before changing it, because most of
the non-obvious code is non-obvious for a reason that is written down.

## 5. Commit Policy

Commit everything under `src/`, `tools/`, `docs/`, `resources/`, `store/` and
`screenshots/`, plus `README.md`, `AGENTS.md`, `Justfile`, `package.json`,
`wscript` and `.gitignore`.

Never commit:

- `build/`, `*.pbw`, `*.elf`, `*.bin`, `*.o`, `*.map`
- `.lock-waf*`, `.wafpickle*`, `config.log`
- **anything holding an API key.** `build/config-seed.json` is the one that
  exists on purpose — it prefills the settings page for testing and is
  gitignored twice over. A key belongs in a file outside the repo, mode 0600,
  and is pasted into the settings page. Never into a commit, a command line
  (where `ps` can read it) or a log.

## 6. Troubleshooting

- `pebble: command not found`: restore PATH from section 2.
- Emulator does not start: run `just kill` then `just dev`.
- Build fails: run `just clean` then `just rebuild`.
- Change app version: update `package.json`, then run `just dev`.
- `Waiting for the firmware to boot` forever: reset the emulator flash as
  described in the Justfile (`just reset-flash` / `just reset-flash-emery`).
  This is usually caused by two qemu processes sharing one flash image, so
  avoid running two emulators against the same platform.
- `pebble emu-button` errors with `empty group`: the action comes first, e.g.
  `pebble emu-button --emulator emery click select`. The bare form fails while
  argparse formats its usage message, which looks like a hang.
- The watch shows hollow boxes instead of characters: the font subset in
  `package.json` does not cover them. See the font notes in DESIGN.md.

Last updated: August 16, 2026
Maintained for: RESTForge agents