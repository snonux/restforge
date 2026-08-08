# =============================================
# RESTForge – Pebble app skeleton
# Justfile for Fedora Linux + Pebble SDK 4.9+ (2026)
# =============================================

# Default emulator: gabbro = Pebble Round 2 (new hardware, 260×260 round display)
# Use 'just dev-basalt' / 'just debug-dev-basalt' for the older rectangular display.
emulator := "gabbro"
emulator_old := "basalt"

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

# Build + install on old rectangular display (basalt)
dev-basalt:
    -pebble kill
    pebble build && pebble install --emulator {{emulator_old}}

# Show live logs (run this in a **second** terminal)
logs:
    pebble logs --emulator {{emulator}}

# Show live logs for basalt emulator
logs-basalt:
    pebble logs --emulator {{emulator_old}}

# Take a screenshot of the emulator
screenshot:
    pebble screenshot --emulator {{emulator}}

# Take a screenshot of the basalt emulator
screenshot-basalt:
    pebble screenshot --emulator {{emulator_old}}

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

# Reset the basalt SPI flash to factory state.
reset-flash-basalt:
    -pebble kill
    bunzip2 -k -c ~/.pebble-sdk/SDKs/4.9.148/sdk-core/pebble/basalt/qemu/qemu_spi_flash.bin.bz2 > ~/.pebble-sdk/4.9.148/basalt/qemu_spi_flash.bin
    @echo "Flash reset to factory state. Run 'just dev-basalt' to reinstall."

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

# Build + install debug app on old rectangular display (basalt)
debug-dev-basalt:
    -pebble kill
    DEBUG=1 pebble build && pebble install --emulator {{emulator_old}}

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
    @echo "  just dev-basalt       → build + install on old basalt display"
    @echo "  just logs             → live logs for gabbro (second terminal)"
    @echo "  just logs-basalt      → live logs for basalt (second terminal)"
    @echo "  just kill             → stop the emulator"
    @echo "  just clean            → remove build files"
    @echo "  just rebuild          → clean + dev"
    @echo ""
    @echo "Run 'just' to see all available commands."