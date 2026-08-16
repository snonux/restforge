# Sideloading onto a real Pebble

How to get a build onto real hardware over USB, and the traps that cost an hour
the first time. Everything here was established against a **Pebble Time 2**
(emery) paired to a **Pixel 7 Pro** running the **Core Devices** companion app,
on 2026-08-16.

## The companion app is not called "Pebble"

```sh
adb shell pm list packages | grep -i pebble    # finds nothing
```

The app's package is **`coredevices.coreapp`**. Grepping for `pebble`,
`rebble` or `cobble` finds nothing and looks exactly like "no companion app is
installed", which is the wrong conclusion. Check for the real name:

```sh
adb shell pm list packages | grep -i core
adb shell dumpsys window | grep mCurrentFocus     # what is on screen now
```

## Three switches, not one

The developer connection needs **all** of these. Two of them are in different
places and neither alone opens the port:

| Where | What |
|---|---|
| Settings → Connectivity | **Use LAN developer connection** |
| Devices → the watch → **⋮** | **Dev Connection** |
| (reportedly) a GitHub sign-in | was *not* required in practice |

Only the second one actually starts the listener. Confirm it worked by looking
for port 9000 rather than trusting the UI:

```sh
adb shell 'cat /proc/net/tcp /proc/net/tcp6' \
  | awk 'NR>1 && $4=="0A" { split($2,a,":"); print strtonum("0x" a[2]) }' | sort -u
```

**Dev Connection switches itself off again** — after a while, and seemingly when
the settings webview opens. If an install starts failing with
`[Errno 111] Connection refused`, re-enable it before debugging anything else.

## Installing over USB

No WiFi and no phone IP needed; tunnel the port instead.

```sh
adb forward tcp:9000 tcp:9000
pebble install --phone localhost build/restforge.pbw
pebble install --phone localhost --logs build/restforge.pbw   # launch + JS logs
```

`--logs` both launches the app and shows PebbleKit JS output. A separately
attached `pebble logs --phone localhost` works too, but needs the tunnel to
still be up — check `adb forward --list` first.

## Driving the phone UI from the shell

Useful for the settings page, which is a WebView. Its contents *are* exposed to
uiautomator, so coordinates can be read rather than guessed:

```sh
adb shell uiautomator dump /sdcard/ui.xml && adb pull /sdcard/ui.xml
```

Then parse `bounds="[x1,y1][x2,y2]"` and tap the centre. Two traps:

- **Screenshot coordinates are not device coordinates.** A thumbnail scaled for
  viewing is not what `adb shell input tap` wants. Take coordinates from the
  UI dump, which is in device pixels.
- **A tap on a label is not a tap on its checkbox.** Look for the node with
  `checkable="true"`, not the one with the matching `text`.
- The soft keyboard covers the bottom of the page. `adb shell input keyevent 4`
  dismisses it before tapping Save.

## Entering an API key — read this before typing one

**`adb shell input text` corrupts long strings.** A 64-character key arrived as
66 characters, with the first divergence at position 8. The app then reported
`Auth / unauthorized`, which looks exactly like a wrong key, a wrong header, or
a server problem — and is none of them.

Type it in chunks and verify before saving:

```sh
# 8-character chunks with a pause; </dev/null on every adb call, because
# `adb shell` swallows the loop's stdin and it silently runs once.
for chunk in $(fold -w8 ~/.f3sctl-apikey-pebble); do
    adb shell input text "$chunk" </dev/null >/dev/null
    sleep 0.7
done
```

Then confirm by hash against the file — the settings page has a **Show** toggle
that reveals the field, and its value appears in the UI dump.

**Print only derived facts about a secret: its length, a hash, or match/no-match.
Never its value.** A debug line that printed a field's contents "just to check
it was cleared" put twenty characters of a live key into a session transcript
and forced a rotation. Length and hash would have answered the same question.

The key belongs in a file outside the repo at mode 0600, read into a variable,
never echoed and never passed where it lands in shell history. `just
check-secrets` enforces the repo half of that.

## What real hardware confirmed

Worth knowing, because the emulator cannot answer any of it:

- The **`data:` URI settings page works** in the Android WebView — a 14 KB page
  rendered with its form intact. The fallback of serving it over HTTP was never
  needed.
- **`capabilities: ["configurable"]`** produces the Settings button, as
  documented.
- The **`pebblejs://close#…`** close path works; the page dismisses itself and
  the companion stores the config.
- The negotiated AppMessage inbox is **8200 bytes** — the real app advertises
  the protocol capability, which only the phone can decide.
- `VERSION 0.6 (sideloaded)` appears on the app's page in the companion, which
  is a quick way to confirm which build is actually on the watch.
