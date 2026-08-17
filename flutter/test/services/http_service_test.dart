// Pure Dart unit tests: no WidgetTester, no device, no socket. See
// AGENTS.md section 5 ("Test style"). Ported from
// pebble/tools/test-http.js, which drives http.js against a stub XHR built
// to reproduce the PebbleKit runtime's actual failure behaviour. The stub
// here is package:http's MockClient instead: HttpService is never handed a
// real http.Client in this file, so nothing here touches a socket.
//
// The cases worth keeping are the same ones test-http.js keeps: not the
// happy path so much as what the caller can rely on when the network is not
// cooperating -- an unreachable host must resolve as `unreachable`, not
// silently or as some other kind; a timeout is its own kind, distinct from
// unreachable, because "no answer within budget" and "no answer ever" are
// different facts about the request; and nothing sent to the injected `log`
// callback ever contains the secret, no matter which of those happens.
//
// What is deliberately NOT ported: test-http.js's FakeXHR reproduces two
// PebbleKit-only XHR quirks (status 0 with no onerror on a connection
// failure; a POST needing send('') for Content-Length). package:http has
// neither quirk -- a connection failure throws before any Response exists,
// and its request body is sent exactly as given -- so there is nothing
// quirky here to pin down. The *behaviour* those quirks made hard to get
// right in JS (unreachable resolves as unreachable; a field-less POST still
// carries a body) is still asserted below, because it is still a
// requirement -- just one package:http satisfies structurally rather than
// by workaround.

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/settings_service.dart';

const secret = 'this-is-the-secret-value';

final backend = Backend(
  name: 'test',
  baseUrl: 'https://host.example.org/cgi-bin/app/',
  authHeader: 'X-API-Key',
  secret: secret,
);

/// Builds an [HttpService] whose [http.Client] is a [MockClient] driven by
/// [handler], and whose log output is captured into [logs] instead of
/// going to `debugPrint` -- mirrors test-http.js's `console.log` capture.
HttpService buildService({
  required Future<http.Response> Function(http.Request request) handler,
  List<String>? logs,
  Duration? getTimeoutOverride,
  Duration? actionTimeoutOverride,
}) {
  return HttpService(
    client: MockClient(handler),
    log: (message) => logs?.add(message),
    getTimeoutOverride: getTimeoutOverride,
    actionTimeoutOverride: actionTimeoutOverride,
  );
}

http.Response jsonResponse(int status, String body, {Map<String, String>? headers}) =>
    http.Response(body, status, headers: headers ?? {'content-type': 'application/json'});

void main() {
  group('a successful GET', () {
    test('yields the parsed document', () async {
      http.Request? seen;
      final service = buildService(
        handler: (request) async {
          seen = request;
          return jsonResponse(200, '{"class":["status"],"properties":{"apiVersion":1}}');
        },
      );

      final result = await service.get(backend, '/cgi-bin/app/status');

      expect(result, isA<Ok<HttpResponse>>());
      final ok = result as Ok<HttpResponse>;
      expect(ok.value.status, 200);
      expect(ok.value.entity['properties']['apiVersion'], 1);
      expect(seen!.url.toString(), 'https://host.example.org/cgi-bin/app/status');
      expect(seen!.headers['X-API-Key'], secret);
      expect(seen!.url.toString().contains(secret), isFalse, reason: 'the secret must never be in the URL');
      expect(seen!.body, isEmpty, reason: 'a GET sends no body');
    });
  });

  group('an action POST', () {
    test('a 202 is a success, not a failure', () async {
      http.Request? seen;
      final service = buildService(
        handler: (request) async {
          seen = request;
          return jsonResponse(202, '{"properties":{"state":"running"}}');
        },
      );

      final result = await service.request(backend, '/cgi-bin/app/act', 'POST', {'force': true});

      expect(result, isA<Ok<HttpResponse>>());
      expect((result as Ok<HttpResponse>).value.status, 202);
      expect(seen!.headers['Content-Type'], 'application/x-www-form-urlencoded');
      expect(seen!.body, 'force=true', reason: 'the fields are form-encoded');
    });

    test('a field-less POST still sends an empty body, not nothing', () async {
      // No send()-vs-send('') distinction here -- package:http always sends
      // the body it is given -- but the *shape* carries over: an action
      // with no fields still declares Content-Type and sends an empty
      // string, because some servers reject a POST with neither.
      http.Request? seen;
      final service = buildService(
        handler: (request) async {
          seen = request;
          return jsonResponse(200, '{}');
        },
      );

      await service.request(backend, '/cgi-bin/app/act', 'POST', null);

      expect(seen!.body, isEmpty);
      expect(seen!.headers['Content-Type'], 'application/x-www-form-urlencoded');
    });
  });

  group('unreachable', () {
    test('a connection failure resolves as unreachable, not silently', () async {
      final service = buildService(
        handler: (request) async => throw const SocketFailure('connection refused'),
      );

      final result = await service.get(backend, '/status');

      expect(result, isA<Err<HttpResponse>>());
      expect((result as Err<HttpResponse>).failure.kind, FailureKind.unreachable);
    });

    test('unreachable names the host, not the whole target', () async {
      final service = buildService(handler: (request) async => throw const SocketFailure('refused'));

      final result = await service.get(backend, '/status') as Err<HttpResponse>;

      expect(result.failure.message, contains('https://host.example.org'));
    });
  });

  group('timeout', () {
    test('a request that outruns its budget is its own kind, not unreachable', () async {
      final service = buildService(
        handler: (request) async {
          await Future.delayed(const Duration(milliseconds: 50));
          return jsonResponse(200, '{}');
        },
        getTimeoutOverride: const Duration(milliseconds: 5),
      );

      final result = await service.get(backend, '/status');

      expect(result, isA<Err<HttpResponse>>());
      expect((result as Err<HttpResponse>).failure.kind, FailureKind.timeout);
    });

    test('a GET gets the short budget and an action gets the long one', () async {
      // The two budgets are injected here so the test does not wait 20 real
      // seconds to prove they differ -- production always uses the
      // top-level getTimeout/actionTimeout constants (asserted below).
      Future<http.Response> slowHandler(http.Request request) async {
        await Future.delayed(const Duration(milliseconds: 30));
        return jsonResponse(200, '{}');
      }

      final service = buildService(
        handler: slowHandler,
        getTimeoutOverride: const Duration(milliseconds: 5),
        actionTimeoutOverride: const Duration(milliseconds: 200),
      );

      final getResult = await service.get(backend, '/status');
      expect((getResult as Err<HttpResponse>).failure.kind, FailureKind.timeout,
          reason: 'a GET must not get the long, action-sized budget');

      final actionResult = await service.request(backend, '/act', 'POST', null);
      expect(actionResult, isA<Ok<HttpResponse>>(),
          reason: 'an action must not get the short, GET-sized budget');
    });

    test('the production budgets: GET 20s, action 60s, and they differ', () {
      expect(getTimeout, const Duration(seconds: 20));
      expect(actionTimeout, const Duration(seconds: 60));
      expect(getTimeout, isNot(actionTimeout));
    });
  });

  group('status mapping', () {
    final cases = <(int, FailureKind)>[
      (401, FailureKind.auth),
      (403, FailureKind.auth),
      (409, FailureKind.conflict),
      (404, FailureKind.client),
      (400, FailureKind.client),
      (500, FailureKind.server),
      (503, FailureKind.server),
    ];

    for (final (status, kind) in cases) {
      test('$status maps to $kind', () async {
        final service = buildService(handler: (request) async => jsonResponse(status, '{}'));

        final result = await service.get(backend, '/x') as Err<HttpResponse>;

        expect(result.failure.kind, kind);
        expect(result.failure.status, status);
      });
    }
  });

  group('the server\'s own wording', () {
    test('is used when the error body carries one', () async {
      final service = buildService(
        handler: (request) async =>
            jsonResponse(409, '{"properties":{"message":"a job is already running"}}'),
      );

      final result = await service.get(backend, '/x') as Err<HttpResponse>;

      expect(result.failure.message, 'a job is already running');
    });
  });

  group('an unparseable body', () {
    test('a 200 that is not JSON is a parse error', () async {
      final service = buildService(
        handler: (request) async => http.Response('<html>not siren at all</html>', 200),
      );

      final result = await service.get(backend, '/x') as Err<HttpResponse>;

      expect(result.failure.kind, FailureKind.parse);
    });

    test('a 200 with an empty body is also a parse error', () async {
      final service = buildService(handler: (request) async => http.Response('', 200));

      final result = await service.get(backend, '/x') as Err<HttpResponse>;

      expect(result.failure.kind, FailureKind.parse);
    });
  });

  group('a bad auth header name', () {
    test('is a config problem, and no request is ever sent', () async {
      var calls = 0;
      final broken = Backend(
        name: 'broken',
        baseUrl: backend.baseUrl,
        authHeader: 'X API Key',
        secret: secret,
      );
      final service = buildService(
        handler: (request) async {
          calls++;
          return jsonResponse(200, '{}');
        },
      );

      final result = await service.get(broken, '/x') as Err<HttpResponse>;

      expect(result.failure.kind, FailureKind.config);
      expect(calls, 0, reason: 'a config problem must not reach the network');
    });
  });

  group('what reaches the log', () {
    test('the secret never reaches it', () async {
      final logs = <String>[];
      final service = buildService(
        handler: (request) async => jsonResponse(200, '{}'),
        logs: logs,
      );

      await service.get(backend, '/status');

      expect(logs.join('\n').contains(secret), isFalse);
    });

    test('response headers are logged in full', () async {
      final logs = <String>[];
      final service = buildService(
        handler: (request) async => jsonResponse(200, '{}'),
        logs: logs,
      );

      await service.get(backend, '/status');

      expect(logs.join('\n'), contains('content-type: application/json'));
    });

    test('the status and elapsed time are logged', () async {
      final logs = <String>[];
      final service = buildService(
        handler: (request) async => jsonResponse(200, '{}'),
        logs: logs,
      );

      await service.get(backend, '/status');

      expect(logs.any((line) => RegExp(r'-> 200 \(\d+ms\)').hasMatch(line)), isTrue);
    });

    test('the secret never reaches it even when the request fails', () async {
      final logs = <String>[];
      final service = buildService(
        handler: (request) async => throw const SocketFailure('refused'),
        logs: logs,
      );

      await service.get(backend, '/status');

      expect(logs.join('\n').contains(secret), isFalse);
    });
  });
}

/// A stand-in for the connection failures package:http's IO client actually
/// throws (`SocketException`, `HandshakeException`, ...). Its own type
/// carries no meaning to [HttpService] -- the `catch` in
/// [HttpService.request] treats anything thrown before a response exists as
/// [FailureKind.unreachable], regardless of the exception's concrete type,
/// so a plain stand-in is exactly as good a test double as the real thing.
class SocketFailure implements Exception {
  final String message;
  const SocketFailure(this.message);

  @override
  String toString() => 'SocketFailure: $message';
}
