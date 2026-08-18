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

# Print each app's current version, and flag it if they have drifted apart.
#
# The two apps ship on release cadences that have nothing to do with each
# other technically — a Pebble build and an Android build share no code,
# no store, no review process — but they are one product with one version
# number, so the semver portion (the part before Flutter's "+buildNumber")
# must always read the same in both files. This is the read-only half of
# that check; `bump-version` is the half that keeps it true.
version:
    @flutter_version=$(grep '^version:' flutter/pubspec.yaml | sed -E 's/^version:[[:space:]]*//'); \
    pebble_version=$(grep '"version"' pebble/package.json | sed -E 's/.*"version":[[:space:]]*"([^"]*)".*/\1/'); \
    flutter_semver=$(echo "$flutter_version" | sed -E 's/\+.*//'); \
    echo "flutter: $flutter_version  (pubspec.yaml)"; \
    echo "pebble:  $pebble_version  (package.json)"; \
    if [ "$flutter_semver" != "$pebble_version" ]; then \
        echo "MISMATCH: flutter's semver ($flutter_semver) != pebble's version ($pebble_version)"; \
        echo "Run 'just bump-version <x.y.z>' to bring both back in sync."; \
        exit 1; \
    fi

# Bump BOTH apps to the same semantic version — this is the one place that
# changes either app's version string, so the two can never drift apart.
#
#   just bump-version 0.7.0
#
# Sets flutter/pubspec.yaml's version to x.y.z+N and pebble/package.json's
# version to plain x.y.z. The "+N" is Flutter's Android versionCode: a
# store-facing integer that must strictly increase on every Play Store
# upload, unrelated to semver, so it is incremented by 1 here rather than
# derived from x.y.z. Pebble has no store and no equivalent counter, so its
# package.json carries only the semver string.
#
# This recipe edits files only — it does not commit or tag. See AGENTS.md's
# "Versioning" section for the full release procedure, which ends in exactly
# ONE git tag for the release (never one per subproject).
bump-version version:
    #!/usr/bin/env bash
    set -euo pipefail
    old_line=$(grep '^version:' flutter/pubspec.yaml)
    old_build=$(echo "$old_line" | sed -E 's/.*\+([0-9]+)[[:space:]]*$/\1/')
    new_build=$((old_build + 1))
    sed -i "s/^version:.*/version: {{ version }}+${new_build}/" flutter/pubspec.yaml
    sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"{{ version }}\"/" pebble/package.json
    echo "flutter/pubspec.yaml -> $(grep '^version:' flutter/pubspec.yaml)"
    echo "pebble/package.json  -> $(grep '"version"' pebble/package.json)"
    echo ""
    echo "Now: review the diff, commit both files in ONE commit, then:"
    echo "  git tag v{{ version }}"
    echo "  git push && git push --tags"

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
