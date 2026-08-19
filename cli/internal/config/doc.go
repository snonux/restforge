// Package config persists the backends configured in restforge to a single
// TOML file on disk -- the Go/CLI client's answer to
// flutter/lib/services/settings_service.dart's storage half, and to
// pebble/src/pkjs/settings.js's original design before that. See
// internal/backend's package comment for the reasoning behind the value
// type stored here (why a secret only ever goes in a request header); this
// package's job is only the I/O around it.
//
// Unlike flutter's split between shared_preferences (metadata) and
// flutter_secure_storage (secrets, Android-Keystore backed), the CLI has no
// OS keystore to defer to. A backend and its secret therefore live together
// in one TOML file, the same shape pebble/src/pkjs/settings.js used before
// the Flutter app introduced the split. What both predecessors still
// enforce applies here too -- a secret belongs in a file outside the repo,
// mode 0600 (../../docs/DESIGN.md) -- and this package enforces that mode
// rather than merely relying on it: new files are created at 0600 (see
// writeAtomic in store.go), and every load stats the file and warns, via
// Warnf (warn.go), when group or other has any permission bit set, so a
// misconfigured umask cannot silently leak a key.
//
// Never throws at startup: LoadBackends degrades a missing file,
// unparseable TOML, a loosely-permissioned file, or an entry that fails
// backend.Validate down to "no backends" (or fewer backends), reporting the
// problem through Warnf rather than returning an error the caller must
// crash on -- the same "never throws" contract settings_service.dart's
// module comment documents, and for the same reason: a hard failure at
// startup would leave the user with no way to reach a settings screen and
// fix the problem. The one error LoadBackends can return is reserved for
// something no amount of config-file cleverness fixes: the config path
// itself could not be resolved (os.UserConfigDir failing because
// RESTFORGE_CONFIG is unset and neither $XDG_CONFIG_HOME nor $HOME is
// defined).
package config
