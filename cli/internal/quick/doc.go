// Package quick manages saved shortcuts to somewhere the user goes often --
// the Go port of flutter/lib/services/quick_service.dart, itself a port of
// pebble/src/pkjs/quick.js; see those files' module comments for the full
// reasoning, which carries over unchanged. Browsing to a deep action costs
// a lot of keystrokes; a shortcut remembers a destination so it can be
// reached from the opening screen instead. What it remembers is
// deliberately not the whole answer:
//
//   - An action is stored by name, with the address of the document that
//     offered it -- never the action's own href. Actions come and go as
//     server state changes, and their hrefs are the server's business.
//     Running one means re-fetching the holder and looking the name up
//     again, exactly as if the user had walked there by hand; if it is no
//     longer offered, that is a real answer, reported rather than hidden.
//   - A backend is stored by base URL, not by position -- see BackendFor
//     and BackendsFor. Reordering or renaming the configured backend list
//     (internal/config) must not silently re-point a saved shortcut at a
//     different server.
//
// A shortcut is a shortcut through the navigation, not through the
// deciding. This package only stores and resolves shortcuts -- it never
// fetches a document or invokes an action itself, exactly as quick.js has
// no runQuick of its own and quick_service.dart states the same boundary
// in its module comment: that composition belongs one level up, in
// internal/session (a later task). Running an action shortcut, when that
// wiring exists, means re-fetching QuickItem.Holder and asking
// QuickItem.Name of it through the ordinary confirmation flow every other
// action goes through -- never a shortcut that skips the confirmation a
// hand-reached action would get.
//
// Storage extends internal/config's schema: a top-level TOML array of
// tables named "quick" in the same config file internal/config already
// reads and writes for backends, via config.LoadQuick/config.SaveQuick,
// rather than a second file -- unlike flutter's split between
// shared_preferences (backends) and a separate shared_preferences key
// (quick items), this Go client has one config file, not two storage
// backends to split across. Normalise/usable stay per-type here, same as
// the Dart and JS originals; the decode-tolerate-cap shape (Load/Save
// filtering to usable and capping at MaxQuick) is duplicated from
// internal/config's own filterBackends rather than shared through a
// helper, the same Rule-of-Three-not-met call quick_service.dart's module
// comment documents for its own duplication from settings_service.dart:
// two callers, not three, and extracting a generic helper now would couple
// two bounded value types (backend.Backend vs QuickItem) before a third
// case exists.
//
// Unlike the Dart port, there is no Result[T] wrapper: Save, Add and
// Remove return a plain (T, error), the same idiom internal/config and
// internal/failure use -- see internal/failure's package comment for why.
// A nil error with a nil/false result (Add refusing an incomplete or
// already-full shortcut list, Remove refusing an out-of-range index, Get
// resolving to nothing, BackendFor resolving to a since-deleted backend)
// is a real, reportable answer, not a bug; a non-nil error means storage
// itself failed (a full disk, or the config path could not be resolved).
package quick
