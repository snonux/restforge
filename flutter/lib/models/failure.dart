/// Why a request did not produce a usable result.
///
/// This is the Dart side of the vocabulary [pebble/src/pkjs/http.js] keeps:
/// the distinction that matters most is [FailureKind.unreachable] against
/// everything else. A request that never arrived says nothing about the
/// state of the thing it asked about, and must never be rendered as if the
/// server had answered — see AGENTS.md section 5 ("Error model") for the
/// reasoning this type exists to make concrete.
library;

/// The kind of failure, independent of the human-readable [Failure.message].
///
/// Kept as a flat enum rather than an HTTP status code because callers
/// branch on the kind ("was this even reachable?"), not the exact status —
/// mirrors the constants at the top of `http.js`.
enum FailureKind {
  /// No answer came back at all: DNS, TLS, a refused connection, or a read
  /// that ran past its timeout budget. The server may be perfectly healthy;
  /// this only says the question never arrived or the answer never did.
  unreachable,

  /// The request was sent but no response arrived within budget.
  timeout,

  /// The server rejected the credentials (401/403).
  auth,

  /// The server rejected the request because the state it acted on was
  /// stale (409). Never retried automatically — see the re-fetch invariant
  /// in pebble/docs/DESIGN.md.
  conflict,

  /// The server reported its own failure (5xx).
  server,

  /// The request was malformed in a way this app is responsible for (4xx,
  /// excluding auth and conflict).
  client,

  /// A response arrived with a 2xx status but the body was not the JSON
  /// this app expected.
  parse,

  /// The request could not even be built — e.g. a header name the user
  /// typed was rejected by the platform's HTTP stack. Not the server's
  /// fault and not a network problem: a local configuration problem.
  config,
}

/// A failed attempt to ask a server something.
///
/// [Failure] is a plain value, not an exception — see AGENTS.md section 5.
/// [status] is the HTTP status code when one was received, or 0 when the
/// failure happened before or without one (unreachable, timeout, config).
class Failure {
  final FailureKind kind;
  final int status;
  final String message;

  const Failure({required this.kind, this.status = 0, required this.message});

  @override
  String toString() => 'Failure($kind, status: $status, "$message")';
}
