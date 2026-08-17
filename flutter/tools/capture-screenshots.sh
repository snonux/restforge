#!/usr/bin/env bash
# Capture README screenshots by driving the *real* app against the fixture
# Siren API on a connected Android device or emulator.
#
# This is the host-side orchestrator for
# integration_test/screenshot_test.dart, which runs on the device and emits
# each PNG as a tagged base64 line on stdout (see that file's module comment
# for why the bytes come back that way rather than through adb pull or the
# integration_test report JSON). What this script does, in order:
#
#   1. find an Android device (or take one given as $1);
#   2. start pebble/tools/fake-siren-server.py on the host (port 8731);
#   3. `adb reverse` so the device's 127.0.0.1:8731 reaches the host fixture;
#   4. run the integration test, capturing its stdout;
#   5. decode the tagged lines into PNGs under screenshots/;
#   6. stop the fixture.
#
# Not part of `just test` (no device) or `just check`; run by hand:
#   just screenshots            # auto-selects the Android device
#   just screenshots emulator-5554
#
# Exits non-zero if the test fails or no PNGs were captured.

set -euo pipefail

device="${1:-}"
flutter_bin="${FLUTTER:-flutter}"

# Locate the fixture server two levels up (pebble/tools/, a sibling of this
# app's tree). Resolved relative to this script so it works from anywhere.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture="${script_dir}/../../pebble/tools/fake-siren-server.py"
if [ ! -f "$fixture" ]; then
  printf 'could not find fixture at %s\n' "$fixture" >&2
  exit 1
fi

# Pick the first Android device if none was given.
if [ -z "$device" ]; then
  device="$("$flutter_bin" devices --machine \
    | python3 -c 'import sys,json; d=[x for x in json.load(sys.stdin) if x.get("platformType")=="android" or x.get("targetPlatform","").startswith("android")]; print(d[0]["id"] if d else "")' \
    || true)"
fi
if [ -z "$device" ]; then
  printf 'no Android device or emulator connected\n' >&2
  printf 'start one with: flutter emulators --launch <id>\n' >&2
  exit 1
fi
printf 'using device: %s\n' "$device"

port=8731
fixture_log="$(mktemp -t restforge-fixture.XXXXXX.out)"

# Start the fixture and tear it down on exit (or error, thanks to set -e).
python3 "$fixture" "$port" >"$fixture_log" 2>&1 &
fixture_pid=$!
out_log="$(mktemp -t restforge-screenshots.XXXXXX.out)"
trap 'kill "$fixture_pid" 2>/dev/null || true; rm -f "$fixture_log" "$out_log"' EXIT

# Wait for the fixture's own ready banner — a poll, not a fixed sleep, so a
# slow Python startup does not race the first fetch.
ready=""
for _ in $(seq 1 100); do
  if grep -q 'fixture Siren server on' "$fixture_log" 2>/dev/null; then
    ready=1; break
  fi
  sleep 0.1
done
if [ -z "$ready" ]; then
  printf 'fixture did not start; log:\n' >&2
  cat "$fixture_log" >&2
  exit 1
fi

# Forward the device's localhost:port to the host fixture — the app talks to
# http://127.0.0.1:8731/ and this makes that address reach the host.
adb -s "$device" reverse tcp:"$port" tcp:"$port"

printf 'running screenshot test (this takes ~40s for the 24s job)...\n'
if ! "$flutter_bin" test integration_test/screenshot_test.dart -d "$device" \
      >"$out_log" 2>&1; then
  printf 'screenshot test failed; tail of output:\n' >&2
  tail -40 "$out_log" >&2
  exit 1
fi

mkdir -p screenshots
count="$(python3 - "$out_log" <<'PY'
import base64, sys
count = 0
for line in open(sys.argv[1]):
    if line.startswith('RESTFORGE_SCREENSHOT\t'):
        _, name, b64 = line.rstrip('\n').split('\t')
        with open(f'screenshots/{name}.png', 'wb') as f:
            f.write(base64.b64decode(b64))
        count += 1
print(count)
PY
)"
# out_log is removed by the EXIT trap; no explicit rm here.

if [ "$count" -eq 0 ]; then
  printf 'no screenshots were captured\n' >&2
  exit 1
fi
printf 'wrote %s screenshots to screenshots/\n' "$count"