// Pure Dart unit tests: no WidgetTester, no device. See AGENTS.md section 5
// ("Test style") — these two types are what the error-model decision looks
// like in code, so the tests exist to keep that decision honest.

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/result.dart';

void main() {
  group('Result', () {
    test('Ok carries the value through unchanged', () {
      const result = Ok<int>(42);

      expect(result, isA<Result<int>>());
      expect(result.value, 42);
    });

    test('Err carries the failure, not an exception', () {
      const failure = Failure(kind: FailureKind.unreachable, message: 'no answer from example.com');
      const Result<int> result = Err<int>(failure);

      expect(result, isA<Err<int>>());
      expect((result as Err<int>).failure.kind, FailureKind.unreachable);
    });

    test('switch distinguishes Ok from Err without a thrown exception', () {
      Result<int> attempt(bool succeed) {
        if (succeed) {
          return const Ok(1);
        }
        return const Err(Failure(kind: FailureKind.timeout, message: 'timed out'));
      }

      final outcomes = [attempt(true), attempt(false)].map((result) => switch (result) {
            Ok(:final value) => 'ok:$value',
            Err(:final failure) => 'err:${failure.kind.name}',
          });

      expect(outcomes, ['ok:1', 'err:timeout']);
    });
  });

  group('Failure', () {
    test('unreachable is distinct from every other kind', () {
      // The distinction that matters most, per pebble/src/pkjs/http.js: a
      // request that never arrived says nothing about the server's answer.
      const unreachable = Failure(kind: FailureKind.unreachable, message: 'no answer');
      const auth = Failure(kind: FailureKind.auth, status: 401, message: 'auth rejected');

      expect(unreachable.kind, isNot(auth.kind));
      expect(unreachable.status, 0);
      expect(auth.status, 401);
    });
  });
}
