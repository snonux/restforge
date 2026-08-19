#!/usr/bin/env bash
#
# check-genericity.sh -- fail if any fixture-server vocabulary appears in
# the restforge cli's production source, mirroring the genericity grep
# pebble/Justfile and flutter/Justfile already run (see their AGENTS.md
# files). The repo-root AGENTS.md states the invariant this enforces: "Never
# build a URL ... No rel, class, action name or property name belonging to a
# particular server may appear in either app's source." A single hardcoded
# rel would quietly end the cli client's genericity, the way it would the
# Pebble or Flutter one.
#
# The pattern is the fake-siren fixture server's vocabulary -- the same list
# pebble and flutter grep for -- so all three apps enforce the same check
# against the same server-specific strings. It matches inside comments too,
# so prose mentioning a server's name will trip it: reword the comment
# rather than loosening the check (a check with known-good noise in it
# stops being a check).
#
# _test.go files are excluded: tests legitimately reference the shared
# fixture Siren API's vocabulary. Documentation (.md) is out of scope by
# virtue of only scanning *.go: docs may name the fixture server
# deliberately.
#
# Prints nothing and exits 0 on a clean tree; prints the offending matches
# and a short explanation to stderr and exits 1 otherwise. Run from the
# cli/ directory (as the Justfile recipe does): scans ./internal and ./cmd.
set -euo pipefail

# The fixture server's vocabulary: rels, classes, paths it exposes. Kept in
# sync with pebble/Justfile and flutter/Justfile's genericity grep.
pattern='f3s|power-off|fans|/status|/job|monitoring'

# Scan Go production source under internal/ and cmd/, skipping _test.go.
hits=$(grep -rniE "$pattern" \
    --include='*.go' --exclude='*_test.go' \
    internal cmd 2>/dev/null) || true

if [ -n "$hits" ]; then
    {
        echo "restforge cli: genericity violation -- fixture-server vocabulary found in production source:"
        echo "$hits"
        echo ""
        echo "No rel, class, action name or property name belonging to a particular server"
        echo "may appear in cli/internal or cli/cmd. Reword the reference or move it to a"
        echo "test rather than loosening this check. See ../AGENTS.md and AGENTS.md."
    } >&2
    exit 1
fi