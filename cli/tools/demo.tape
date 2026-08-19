# demo.tape -- VHS (https://github.com/charmbracelet/vhs) script for
# cli/docs/demo.gif, the terminal counterpart to flutter/screenshots'
# pantry_0{1..5}.png. Run via `just record-demo` (record-demo.sh), never by
# hand with a bare `vhs tools/demo.tape`: it needs the fixture Siren server
# already running on 127.0.0.1:8731 and RESTFORGE_CONFIG/PATH pointed at a
# scratch config and a freshly built restforge binary -- record-demo.sh sets
# all three up and tears them down again, this file only drives the terminal
# once they exist.
#
# Five moments, matching flutter/README.md's screenshot captions:
#
#   1. the opening screen: a configured backend and a saved shortcut
#   2. a document: properties, links and actions (the root), then a second
#      document with an embedded and a referenced sub-entity (/shelves) --
#      split across two screens because no single document in the fixture
#      carries all four row kinds at once (root has no sub-entity; /shelves
#      has no actions) -- see fake-siren-server.py's root()/shelves().
#   3, 4, 5. confirming an unsafe action, watching it run, and the
#      re-fetched result -- done through `restforge act`, not the TUI: as of
#      this recording the TUI's Confirm/ValuePrompt/Detail overlay screens
#      only render a placeholder (internal/tui's package doc comment, task
#      631/731 not yet landed). `restforge act` shares the exact same
#      internal/session state machine the TUI screens will drive once they
#      exist, so this is the same confirm/watch/re-fetch logic, just fronted
#      by the one-shot command instead of an interactive overlay -- swap
#      this section for a TUI walkthrough once those screens land.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 780
Set Padding 20
Set TypingSpeed 60ms

Hide
Type "clear"
Enter
Show

# === 1. Home: a configured backend and a saved shortcut ====================
Type "restforge"
Enter
Sleep 1s

# === 2a. The root document: properties, links and actions ==================
Enter
Sleep 1500ms

# Follow the "shelves" link (row 7: apiVersion, brews, kettle, unicode,
# version, self, shelves) to a document with a sub-entity.
Down@150ms 6
Enter
Sleep 1500ms

# === 2b. /shelves: properties, an embedded sub-entity and a referenced one =
Sleep 1s
Type "q"
Sleep 500ms

# === 3/4/5: confirm, watch, re-fetch -- via `restforge act`, see above =====
Type "echo '--- restforge act brew        (no --yes: unsafe action refused)'"
Enter
Sleep 500ms
Type "restforge act brew"
Enter
Sleep 1s
Type 'echo "exit code: $?"'
Enter
Sleep 1500ms

Type "echo '--- restforge act brew --yes  (confirmed, watched, re-fetched)'"
Enter
Sleep 500ms
Type "restforge act brew --yes"
Enter
# internal/live polls every PollInterval (10s -- internal/live/live.go) and
# the fixture's brew job runs for BREW_SECONDS=24 (fake-siren-server.py), so
# "done" is only detected on the 4th poll, at ~30s, not 24s -- sleep past
# that plus a margin for the final re-fetch and print, then hold on the
# result a moment before the recording ends.
Sleep 34s

Sleep 3s
