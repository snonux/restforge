# RESTForge - AGENTS.md
Pebble Watchapp Skeleton Development Guide
Fedora Linux + Rebble Pebble SDK 4.9+ (August 2026)

This file is the primary workflow reference for RESTForge contributors.

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

Available commands:

- `just`: list commands
- `just dev`: build + install to emulator
- `just logs`: live logs (second terminal)
- `just kill`: stop emulator
- `just clean`: remove build artifacts
- `just rebuild`: clean + full rebuild + install
- `just quick`: quick rebuild + install
- `just screenshot`: screenshot of emulator
- `just sdk-version`: print Pebble SDK/tool version

Daily loop after reboot:

1. Restore PATH (`export PATH="$HOME/.local/bin:$PATH"`).
2. `cd /path/to/restforge`
3. `just dev`
4. In a second terminal run `just logs`

## 4. Project Layout

```
package.json   Pebble app manifest (name, UUID, target platforms)
wscript        Waf build rules (pebble_sdk)
Justfile        dev/build/install/emulator shortcuts
src/c/         C sources for the on-watch app
src/pkjs/       PebbleKit JS companion (stub)
```

- `src/c/restforge.c` — main entry point, single window + live clock.
- `src/pkjs/index.js` — companion stub (on-watch only; nothing sent to phone).

## 5. Commit Policy

Commit these project files when changed:

- `README.md`
- `AGENTS.md`
- `Justfile`
- `package.json`
- `wscript`
- `src/c/restforge.c`
- `src/pkjs/index.js`
- `store/icon.svg`, `store/icon-144.png`, `store/icon-80.png`
- `screenshots/*.png`
- `.gitignore`

Do not commit generated artifacts:

- `build/`
- `*.pbw`, `*.elf`, `*.bin`, `*.o`, `*.map`
- `.lock-waf*`, `.wafpickle*`, `config.log`

## 6. Troubleshooting

- `pebble: command not found`: restore PATH from section 2.
- Emulator does not start: run `just kill` then `just dev`.
- Build fails: run `just clean` then `just rebuild`.
- Change app version: update `package.json`, then run `just dev`.
- `Waiting for the firmware to boot` forever: reset the emulator flash as
  described in the Justfile (`just reset-flash` / `just reset-flash-basalt`).

Last updated: August 8, 2026
Maintained for: RESTForge agents