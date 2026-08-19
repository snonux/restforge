// Package version holds the single version string the restforge binary
// reports. It is the Go/CLI client's answer to flutter/pubspec.yaml's
// version field and pebble/package.json's "version" field, kept here as a
// plain text file (VERSION, next to this .go) embedded into the binary, so
// every path that needs the version -- the `restforge version` subcommand,
// the root command's built-in --version flag, and a later User-Agent header
// -- reads one value rather than each hardcoding its own.
//
// The VERSION file is the one source of truth: `just bump-version x.y.z`
// (repo-root Justfile) rewrites it alongside flutter/pubspec.yaml and
// pebble/package.json, and `just version` checks all three agree -- see
// AGENTS.md's "Versioning" section. The Go constant below is the embedded
// contents of that file, not a second place the version is maintained.
package version

import _ "embed"
import "strings"

// versionBytes is the raw contents of VERSION, embedded at build time. The
// //go:embed directive pulls the file next to this .go (cli/internal/version/
// VERSION); go:embed cannot reach above the package directory, so the file
// lives here rather than at the module root.
//
//go:embed VERSION
var versionBytes []byte

// Version is the version restforge reports, trimmed of any surrounding
// whitespace the VERSION file may carry (a trailing newline is the natural
// thing to leave in a one-line text file and must not become part of the
// version string).
var Version = strings.TrimSpace(string(versionBytes))
