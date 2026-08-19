// Package failure is the closed vocabulary for why a request did not
// produce a usable result.
//
// This is the Go side of the vocabulary flutter/lib/models/failure.dart
// keeps (which in turn carries over pebble/src/pkjs/http.js's constants):
// the distinction that matters most is [Unreachable] against every other
// kind. A request that never arrived says nothing about the state of the
// thing it asked about, and must never be rendered as if the server had
// answered — see flutter/AGENTS.md section 5 ("Error model") for the
// reasoning this type exists to make concrete.
//
// Unlike the Dart side, there is no generic Result[T] wrapper here. Go's
// built-in (value, error) multi-return already gives every caller the same
// never-silently-drop-a-failure guarantee that Result/Ok/Err was built to
// provide in a language with exceptions: a caller that ignores the error
// return has to do so explicitly (`_ = err`), the same way a Dart caller
// would have to explicitly ignore an Err. Every later package (httpclient,
// nav, action, live, config) returns (T, error) and type-asserts the error
// to *Failure when it needs the Kind.
package failure

import "fmt"

// Kind is the reason a request did not produce a usable result, independent
// of the human-readable message. Kept as a closed enum rather than an HTTP
// status code because callers branch on the kind ("was this even
// reachable?"), not the exact status.
type Kind int

const (
	// Unreachable means no answer came back at all: DNS, TLS, a refused
	// connection, or a read that ran past its timeout budget. The server
	// may be perfectly healthy; this only says the question never arrived
	// or the answer never did.
	Unreachable Kind = iota

	// Timeout means the request was sent but no response arrived within
	// budget.
	Timeout

	// Auth means the server rejected the credentials (401/403).
	Auth

	// Conflict means the server rejected the request because the state it
	// acted on was stale (409). Never retried automatically — see the
	// re-fetch invariant in docs/DESIGN.md.
	Conflict

	// Server means the server reported its own failure (5xx).
	Server

	// Client means the request was malformed in a way this app is
	// responsible for (4xx, excluding Auth and Conflict).
	Client

	// Parse means a response arrived with a 2xx status but the body was
	// not what this app expected.
	Parse

	// Config means the request could not even be built — e.g. a header
	// name the user typed was rejected by the platform's HTTP stack. Not
	// the server's fault and not a network problem: a local configuration
	// problem.
	Config
)

// String names a Kind for logs and error messages. It is also how tests
// enumerate the closed set: a Kind outside [Unreachable, Config] has no
// name and falls through to the "unknown" case.
func (k Kind) String() string {
	switch k {
	case Unreachable:
		return "unreachable"
	case Timeout:
		return "timeout"
	case Auth:
		return "auth"
	case Conflict:
		return "conflict"
	case Server:
		return "server"
	case Client:
		return "client"
	case Parse:
		return "parse"
	case Config:
		return "config"
	default:
		return fmt.Sprintf("unknown(%d)", int(k))
	}
}

// Failure is a failed attempt to ask a server something. It implements the
// error interface, so a caller that only needs "did this fail" can treat it
// as an ordinary error, while a caller that needs to branch on why type-
// asserts it to *Failure to read Kind.
//
// Status is the HTTP status code when one was received, or 0 when the
// failure happened before or without one (Unreachable, Timeout, Config).
type Failure struct {
	Kind    Kind
	Status  int
	Message string
}

// Error formats the Failure for display in logs and generic error paths.
// Callers that need to branch on Kind should type-assert to *Failure
// instead of parsing this string.
func (f *Failure) Error() string {
	return fmt.Sprintf("%s (status %d): %s", f.Kind, f.Status, f.Message)
}
