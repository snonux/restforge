/// The outcome of an operation that can fail without throwing.
///
/// A [Result] is how this app tells "I could not ask" from "the answer was
/// no" without letting an exception reach the UI — see AGENTS.md section 5
/// ("Error model"). A service returns `Result<T>` instead of `T`, and the
/// caller pattern-matches on the two variants rather than wrapping the call
/// in try/catch. The last good value a screen was showing is never
/// discarded just because the next request failed: the caller decides that,
/// looking at the [Err] it got back, not at a thrown exception that already
/// unwound past the point where the old value was still in scope.
library;

import 'failure.dart';

sealed class Result<T> {
  const Result();
}

/// The request succeeded; [value] is the answer.
final class Ok<T> extends Result<T> {
  final T value;
  const Ok(this.value);
}

/// The request did not succeed; [failure] says why.
final class Err<T> extends Result<T> {
  final Failure failure;
  const Err(this.failure);
}
