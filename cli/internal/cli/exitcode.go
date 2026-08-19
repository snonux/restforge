// Package cli is the one-binary, mode-by-invocation command tree for
// restforge: a Cobra root command that dispatches by invocation, the way
// kubectl or gh blend interactive and scriptable use into one binary.
//
// With no subcommand the root launches the interactive Bubble Tea TUI
// (internal/tui); with any subcommand it runs one-shot and exits with a
// process exit code reflecting the outcome. The TUI loads its own
// backends when it opens the backend picker (a later task); a subcommand
// that needs backends loads them at startup through internal/config.
//
// # Exit codes
//
// The binary keeps a small, closed set of exit codes, documented here so
// callers (scripts, CI, a shell pipeline) can rely on them. They map
// loosely onto internal/failure's closed Kind set -- loosely, because
// failure.Kind distinguishes why a request failed, while an exit code
// distinguishes what the process should do next, and those are not the
// same question:
//
//   - 0 success -- the command completed and produced its result.
//   - 1 general failure -- the command ran but did not succeed. This is
//     the default for any error not otherwise classified, and covers the
//     failure kinds that mean "the request did not produce a usable
//     answer": Unreachable, Timeout, Auth, Server, Client, Parse and
//     Config. A failed request is not an answer (../../AGENTS.md), so the
//     process says so and lets the caller decide.
//   - 2 usage error -- the command was invoked wrong: an unknown
//     subcommand, a bad flag, a missing required argument. The user
//     corrects the invocation and tries again; nothing reached the
//     server, so no failure.Kind applies.
//   - 3 action refused or withdrawn -- an action the user started did
//     not happen because it was refused (the server returned 409
//     Conflict: the state it acted on was stale, never retried
//     automatically -- see docs/DESIGN.md) or withdrawn (the user
//     declined the confirmation an unsafe method requires, per
//     ../../AGENTS.md's "ask before acting"). Distinct from 1 because
//     "the user changed their mind" or "the server said no, re-fetch" is
//     not a failure of the request itself.
//
// Subcommands signal a non-default code by returning an [*ExitError]
// from their RunE; anything else maps through [ExitCode] to its code.
package cli

import "errors"

// Exit codes used across the cli package. See the package comment for the
// full convention and the loose mapping onto internal/failure.Kind.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitRefused = 3
)

// ExitError carries a process exit code alongside an error, so a
// subcommand's RunE can ask the dispatch layer to exit with a specific
// code rather than the default general-failure code. It implements the
// error interface so it passes through Cobra unchanged, and Unwrap so
// errors.Is/errors.As still reach the underlying cause.
type ExitError struct {
	Code int
	Err  error
}

// Error returns the wrapped error's message.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is/errors.As.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode maps a Cobra result to a process exit code: nil is OK, an
// [*ExitError] is its own code, and anything else is a general failure.
// See the package comment for the convention and the loose mapping onto
// internal/failure.Kind.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ex *ExitError
	if errors.As(err, &ex) {
		return ex.Code
	}
	return ExitFailure
}
