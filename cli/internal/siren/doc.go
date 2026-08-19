// Package siren is the Siren hypermedia document model and the generic
// lookups a screen uses to read one.
//
// This is the Go port of flutter/lib/models/siren.dart, itself a port of
// pebble/src/pkjs/siren.js; see that file's header and
// docs/DESIGN.md ("The rule everything else follows from") for the
// reasoning. Everything here is generic by construction: there is no rel,
// no class, no action name and no property name written down in this
// package, and there must never be one -- a client that knows a server's
// vocabulary has to be updated when the server changes, which is the
// failure hypermedia exists to avoid. just check's genericity grep
// enforces this on every commit.
//
// The contract these types exist to keep is that a caller locates things
// by rel and by name, uses the href and method the server offered, and
// treats an unknown class, rel, name or field type as ordinary -- not as
// an error. So every lookup below answers "not offered" (a zero value, a
// nil pointer or an empty slice) rather than panicking, and every JSON
// constructor tolerates a missing or wrongly-typed member: a document
// missing an optional part is not malformed, and a malformed one never
// crashes the caller. See ParseDocument for the one place actual JSON
// bytes are decoded -- the only case where "not usable" is reported as a
// *failure.Failure rather than as an empty field.
//
// Optional string members (Title, Type, a Link's or Entity's Href, and so
// on) use "" as the "the server did not send this" sentinel throughout,
// rather than a pointer -- unlike Dart's nullable String?, Go's zero value
// for string is already unambiguous here because an empty title, href or
// type is never meaningfully different from an absent one anywhere this
// package or its callers read one.
package siren
