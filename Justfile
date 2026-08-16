# =============================================
# RESTForge – a generic Siren hypermedia browser
# Justfile for Fedora Linux + Pebble SDK 4.9+ (2026)
#
# Targets two watches and checks both every time: they differ in shape, not
# just size. See AGENTS.md for the workflow, docs/DESIGN.md for the design.
# =============================================

# Default emulator: gabbro = Pebble Round 2 (new hardware, 260×260 round display)
# Use 'just dev-emery' / 'just debug-dev-emery' for Pebble Time 2 (200×228 rectangular).
emulator := "gabbro"
emulator_alt := "emery"

# Default: show all available commands
default:
    @just --list

# Aliases (shortcuts)
alias b := build
alias i := install
alias d := dev
alias l := logs
alias s := screenshot
alias c := clean
alias k := kill

# ─────────────────────────────────────────────
# Core development commands
# ─────────────────────────────────────────────

# Build the app
build:
    pebble build

# Install to the gabbro emulator (Pebble Round 2)
install:
    pebble install --emulator {{emulator}}

# Build + install on Pebble Round 2 (gabbro) — primary target
# Kills any stale emulator first so install never races against the boot animation.
dev:
    -pebble kill
    pebble build && pebble install --emulator {{emulator}}

# Build + install on Pebble Time 2 (emery), rectangular
dev-emery:
    -pebble kill
    pebble build && pebble install --emulator {{emulator_alt}}

# Show live logs (run this in a **second** terminal)
logs:
    pebble logs --emulator {{emulator}}

# Show live logs for emery emulator
logs-emery:
    pebble logs --emulator {{emulator_alt}}

# Take a screenshot of the emulator
screenshot:
    pebble screenshot --emulator {{emulator}}

# Take a screenshot of the emery emulator
screenshot-emery:
    pebble screenshot --emulator {{emulator_alt}}

# ─────────────────────────────────────────────
# Tests and checks
# ─────────────────────────────────────────────

# Run the PebbleKit JS unit tests (node, no emulator needed)
test:
    node tools/test-url.js
    node tools/test-live.js
    node tools/test-render.js
    node tools/test-nav.js
    node tools/test-actions.js
    node tools/test-session.js
    node tools/test-siren.js
    node tools/test-http.js
    node tools/test-settings.js
    node tools/test-configpage.js
    node tools/test-doc-roundtrip.js

# The two greps that cannot fail loudly on their own.
# ES5: the emulator's PKJS runs a modern V8 and accepts ES6 happily, but the
# build does not transpile and the device is ES5.1 — so the emulator is a
# false green and only this grep catches it.
# Genericity: RESTForge must know Siren, HTTP and AppMessage, and nothing about
# any particular server. Only comments may match.
check:
    @echo "== ES5 =="
    -grep -rnE '=>|\bconst |\blet |`|\.\.\.' src/pkjs/ src/common/ 2>/dev/null
    @echo "== genericity =="
    -grep -rniE 'f3s|power-off|fans|/status|/job|monitoring' src/

# Run the fixture Siren server (a second terminal).
# Its vocabulary is deliberately unfamiliar, so browsing it demonstrates the
# thing f3sctl alone cannot: that the app renders an API it has never seen.
# Add a backend with base URL http://localhost:8731/ and secret open-sesame.
fixture:
    python3 tools/fake-siren-server.py 8731

# ─────────────────────────────────────────────
# Settings page
# ─────────────────────────────────────────────

# Render the settings page to build/config.html from src/pkjs/configpage.js.
# Pass a JSON array of backends as build/config-seed.json to prefill it.
# That file holds secrets and is gitignored — do not commit one.
config-page:
    node tools/gen-config-page.js build/config.html build/config-seed.json

# Open the settings page against the gabbro emulator.
# --file is needed because a desktop browser refuses to navigate to the data:
# URI the phone gets; the page itself is identical either way.
config: config-page
    pebble emu-app-config --emulator {{emulator}} --file build/config.html

# Open the settings page against the emery emulator
config-emery: config-page
    pebble emu-app-config --emulator {{emulator_alt}} --file build/config.html

# ─────────────────────────────────────────────
# Emulator control
# ─────────────────────────────────────────────

# Kill / stop the running emulator
kill:
    pebble kill

# ─────────────────────────────────────────────
# Emulator recovery
# ─────────────────────────────────────────────

# Reset the gabbro SPI flash to factory state.
# Use this when 'just dev' hangs on the Pebble boot screen — it means the
# flash was corrupted (e.g. by killing multiple concurrent qemu processes).
reset-flash:
    -pebble kill
    bunzip2 -k -c ~/.pebble-sdk/SDKs/4.9.148/sdk-core/pebble/gabbro/qemu/qemu_spi_flash.bin.bz2 > ~/.pebble-sdk/4.9.148/gabbro/qemu_spi_flash.bin
    @echo "Flash reset to factory state. Run 'just dev' to reinstall."

# Reset the emery SPI flash to factory state.
reset-flash-emery:
    -pebble kill
    bunzip2 -k -c ~/.pebble-sdk/SDKs/4.9.148/sdk-core/pebble/emery/qemu/qemu_spi_flash.bin.bz2 > ~/.pebble-sdk/4.9.148/emery/qemu_spi_flash.bin
    @echo "Flash reset to factory state. Run 'just dev-emery' to reinstall."

# ─────────────────────────────────────────────
# Maintenance commands
# ─────────────────────────────────────────────

# Clean all build artifacts
clean:
    rm -rf build/ *.pbw *.elf *.bin *.map *.o config.log .lock-waf* .wafpickle*

# Full clean + rebuild + install
rebuild:
    just clean
    just dev

# Quick rebuild + install
quick:
    just build && just install

# Build with DEBUG=1 compile flag
debug-build:
    DEBUG=1 pebble build

# Build + install debug app on Pebble Round 2 (gabbro)
# Kills any stale emulator first (same reason as dev).
debug-dev:
    -pebble kill
    DEBUG=1 pebble build && pebble install --emulator {{emulator}}

# Build + install debug app on Pebble Time 2 (emery), rectangular
debug-dev-emery:
    -pebble kill
    DEBUG=1 pebble build && pebble install --emulator {{emulator_alt}}

# ─────────────────────────────────────────────
# Debug / Testing helpers
# ─────────────────────────────────────────────

# Build, install, and remind you to open logs
dev-with-logs:
    @echo "Building and installing..."
    just dev
    @echo ""
    @echo "Now open a second terminal and run:   just logs"

# Show current SDK version
sdk-version:
    pebble --version

# Help / reminder
help:
    @echo "RESTForge development commands:"
    @echo "  just dev              → build + install on Round 2/gabbro (primary)"
    @echo "  just dev-emery       → build + install on Time 2/emery"
    @echo "  just logs             → live logs for gabbro (second terminal)"
    @echo "  just logs-emery      → live logs for emery (second terminal)"
    @echo "  just config           → open the settings page against gabbro"
    @echo "  just test             → PebbleKit JS unit tests (no emulator)"
    @echo "  just check            → ES5 and genericity greps"
    @echo "  just kill             → stop the emulator"
    @echo "  just clean            → remove build files"
    @echo "  just rebuild          → clean + dev"
    @echo ""
    @echo "Run 'just' to see all available commands."