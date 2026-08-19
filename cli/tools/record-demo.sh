#!/usr/bin/env bash
# Record cli/docs/demo.gif by driving the *real* restforge binary against
# the fixture Siren API, the terminal counterpart to
# flutter/tools/capture-screenshots.sh. What this does, in order:
#
#   1. build restforge into a scratch dir and put that dir on $PATH, so the
#      tape file can just `Type "restforge"` and find it;
#   2. start pebble/tools/fake-siren-server.py on the host (port 8731);
#   3. seed a scratch config (RESTFORGE_CONFIG) with the fixture backend and
#      one saved shortcut -- never the real user's config, which this never
#      touches or even reads;
#   4. run `vhs tools/demo.tape`, which types the walkthrough documented at
#      the top of that file;
#   5. clean up (fixture process, scratch dir) even on failure.
#
# Not part of `just check` or `just test` (it launches a GUI-less terminal
# recorder and takes the better part of a minute, most of it the fixture's
# own ~24s brew job); run by hand:
#   just record-demo
#
# Requires `vhs` (https://github.com/charmbracelet/vhs) on $PATH.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cli_dir="$(cd "$script_dir/.." && pwd)"
fixture="$cli_dir/../pebble/tools/fake-siren-server.py"

if [ ! -f "$fixture" ]; then
  printf 'could not find fixture at %s\n' "$fixture" >&2
  exit 1
fi
if ! command -v vhs >/dev/null 2>&1; then
  printf 'vhs not found on PATH -- install it from https://github.com/charmbracelet/vhs\n' >&2
  exit 1
fi

port=8731
workdir="$(mktemp -d -t restforge-demo.XXXXXX)"
config_path="$workdir/config.toml"
fixture_log="$workdir/fixture.log"

# Cleanup runs on any exit (success, failure, or an interrupt) so a failed
# recording never leaves the fixture server running or a scratch config
# lying around -- the same trap-on-EXIT shape capture-screenshots.sh uses.
fixture_pid=""
cleanup() {
  if [ -n "$fixture_pid" ]; then
    kill "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

printf 'building restforge into %s\n' "$workdir/restforge"
(cd "$cli_dir" && go build -o "$workdir/restforge" ./cmd/restforge)

printf 'starting fixture on :%s\n' "$port"
python3 "$fixture" "$port" >"$fixture_log" 2>&1 &
fixture_pid=$!

# Wait for the fixture's own ready banner -- a poll, not a fixed sleep, so a
# slow Python startup does not race the first fetch. Mirrors
# capture-screenshots.sh's own wait loop.
ready=""
for _ in $(seq 1 100); do
  if grep -q 'fixture Siren server on' "$fixture_log" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.1
done
if [ -z "$ready" ]; then
  printf 'fixture did not start; log:\n' >&2
  cat "$fixture_log" >&2
  exit 1
fi

# A scratch config with the fixture backend and one saved document shortcut
# -- the "saved shortcut" the Home screen shows in moment 1. Written
# directly as TOML (the shape cli/README.md's "Configuring backends"
# section documents) rather than through internal/config, since this is a
# throwaway shell script, not Go code with that package already imported.
# 0600 up front, matching what internal/config itself would create, so
# nothing here ever triggers its insecure-mode warning.
cat >"$config_path" <<EOF
[[backends]]
name        = "fixture"
base_url    = "http://127.0.0.1:$port/"
auth_header = "X-API-Key"
secret      = "open-sesame"
start_rel   = ""

[[quick]]
label        = "browse shelves"
backend_name = "fixture"
base_url     = "http://127.0.0.1:$port/"
kind         = "document"
holder       = ""
name         = ""
href         = "http://127.0.0.1:$port/shelves"
EOF
chmod 600 "$config_path"

export RESTFORGE_CONFIG="$config_path"
export PATH="$workdir:$PATH"

mkdir -p "$cli_dir/docs"
printf 'recording demo.tape (this takes about a minute, most of it the fixture'"'"'s brew job)...\n'
(cd "$cli_dir" && vhs tools/demo.tape)

printf 'wrote %s/docs/demo.gif\n' "$cli_dir"
