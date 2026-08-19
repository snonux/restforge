// Package version holds the single version string the restforge binary
// reports. It is the Go/CLI client's answer to flutter/pubspec.yaml's
// version field and pebble/package.json's "version" field, kept here as a
// constant so every path that needs the version -- the `restforge version`
// subcommand today, build metadata and User-Agent headers later -- reads
// one value rather than each hardcoding its own.
//
// The value is a placeholder for now: a later versioning task replaces it
// with the real semver, the same one `just bump-version` writes into the
// sibling apps' files (see the repo-root AGENTS.md's "Versioning" section).
// Until that task lands, "0.0.0-dev" makes clear that the binary was built
// from a tree that has not been versioned yet.
package version

// Version is the version restforge reports. See the package comment for
// why this is a placeholder and which task replaces it.
const Version = "0.0.0-dev"
