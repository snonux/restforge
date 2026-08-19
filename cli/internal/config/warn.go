package config

import "log"

// Warnf is called with a formatted warning whenever this package degrades
// around a problem -- a loosely-permissioned file, unparseable TOML, an
// entry that fails backend.Validate -- rather than failing outright (see
// the package comment's "never throws" invariant). It defaults to the
// standard logger.
//
// This is the package's chosen test hook: tests replace Warnf (saving and
// restoring the previous value, or via t.Cleanup) to assert that a warning
// fired, and with what text, without depending on capturing real stderr --
// which would be fragile under `go test`'s output buffering and parallel
// test runs. Production code should not need to replace it.
var Warnf = func(format string, args ...any) {
	log.Printf("config: "+format, args...)
}
