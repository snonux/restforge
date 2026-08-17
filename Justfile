# =============================================
# RESTForge — root Justfile
#
# Two apps, one repository: pebble/ (the Pebble watchapp) and flutter/ (the
# Android app). Each owns its own Justfile and its own toolchain; this one
# forwards to them and owns the one check that has to see both at once.
#
#   just pebble dev        →  cd pebble  && just dev
#   just flutter test      →  cd flutter && just test
#
# See README.md for what is what.
# =============================================

default:
    @just --list

# Forward a recipe to the Pebble watchapp, e.g. `just pebble dev-emery`
pebble *args:
    @just -f pebble/Justfile {{args}}

# Forward a recipe to the Android app, e.g. `just flutter run-android`
flutter *args:
    @just -f flutter/Justfile {{args}}

# Everything both apps have to pass before a commit. The secret scan runs once,
# here, because it already covers the whole working tree; each app then runs
# only the checks that are about its own code.
check: check-secrets
    @just -f pebble/Justfile check-code
    @just -f flutter/Justfile check-code

# Both test suites. Neither needs an emulator.
test:
    @just -f pebble/Justfile test
    @just -f flutter/Justfile test

# Fail if key material has leaked into the repo.
#
# An API key belongs in a file outside the repo, mode 0600, pasted into the
# app's settings screen — never in a commit, a command line (where ps can read
# it) or a log. This checks the one thing that assertion can be checked
# against: the key files themselves. It scans every file in the working tree,
# tracked or not, in both apps, and says nothing about the key beyond which
# file leaked it.
#
# It lives here rather than in either app because the two share one set of
# keys, and a scan rooted in one subtree would not see a leak into the other.
#
# It does not scan history; that was verified once by hand and is what
# git-filter-repo is for if it ever fails.
check-secrets:
    @echo "== secrets =="
    @found=0; \
    for f in "$HOME"/.*apikey*; do \
        [ -s "$f" ] || continue; \
        hits=$(grep -rlIF -f "$f" . --exclude-dir=.git 2>/dev/null); \
        if [ -n "$hits" ]; then \
            echo "  LEAK: material from $f appears in:"; \
            echo "$hits" | sed 's/^/    /'; \
            found=1; \
        fi; \
    done; \
    [ $found -eq 0 ] || exit 1
