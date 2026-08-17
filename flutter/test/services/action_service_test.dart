// Pure Dart unit tests: no WidgetTester, no device, no socket. See
// AGENTS.md section 5 ("Test style"). Ported from the cases in
// pebble/tools/test-actions.js that belong to action_service.dart's scope --
// see that file's module comment for what does not carry over.
//
// Matched from test-actions.js: testHasPendingTracksAskAction,
// testUnsafeActionAsksFirst, testSafeActionGoesStraightThrough,
// testDeclining, testConfirmingInvokesAndRefetches (minus the re-fetch --
// that is a coordinator's job here, not this service's, see the module
// comment), testRequiredCheckboxFillsFromTheConfirmation,
// testConflictRefetchesAndDoesNotRetryWithoutConfirmation (minus the
// re-fetch, same reason -- what is checked here is the "does not retry"
// half), testAuthFailureDoesNotRefetch (same), testWithdrawnAction,
// testDefaultsAreUsedWithoutAsking, testRequiredFieldIsAskedFor,
// testNothingHeardSendsNothing and testSeveralMissingFieldsAreRefused
// (n11 -- now that siren.dart's Field models "required", fillFields can
// tell a required field with no default apart from an optional one, and
// ActionService.answerValue is the reply to the "say a value" prompt that
// distinction drives); and testConfirmedActionRetriesOnce,
// testRetryIsNotRepeated and testRetryExpiresAfterTheTTL (m11 -- the
// bounded 409 retry, in the "the bounded 409 retry" group below, using an
// injected `now` the same way live_service_test.dart's FakeClock controls
// time without a real minute passing).
//
// Added beyond test-actions.js: direct coverage of isSafeMethod/safeMethods
// (test-actions.js only exercises the split indirectly, through
// brew/peek), confirmationText's fallback sentence, and a field with
// neither a checkbox nor a default being left out of what is sent when it
// is not required, mirroring the same "nothing to safely guess with" branch
// of fieldValues() -- required is what tips that into a question or a
// refusal instead.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/siren.dart';
import 'package:restforge/services/action_service.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/settings_service.dart';

const String base = 'http://pantry.example/';

final Backend backend = Backend(
  name: 'pantry',
  baseUrl: base,
  secret: 'open-sesame',
);

/// The fixture document -- mirrors test-actions.js's ROOT: an action with no
/// fields ("brew"), one with a checkbox that carries the confirmation
/// ("cool"), and a safe one ("peek", defaulting to GET).
Entity root({List<Map<String, dynamic>>? actions}) => Entity.fromJson({
  'class': ['pantry'],
  'title': 'The pantry',
  'properties': {'apiVersion': 1, 'kettle': 'cold'},
  'links': [
    {
      'rel': ['self'],
      'href': '/',
    },
  ],
  'actions':
      actions ??
      [
        {
          'name': 'brew',
          'title': 'Brew a pot of tea',
          'method': 'POST',
          'href': '/brew',
          'fields': [],
        },
        {
          'name': 'cool',
          'title': 'Let the kettle cool',
          'method': 'POST',
          'href': '/cool',
          'fields': [
            {
              'name': 'confirm',
              'type': 'checkbox',
              'required': true,
              'title': 'The kettle is still hot. Cool it anyway?',
            },
          ],
        },
        {'name': 'peek', 'title': 'Look inside', 'href': '/peek'},
      ],
});

/// One canned reply for [Env]'s [MockClient], keyed by `METHOD URL`.
///
/// [advanceClockBy], when set, moves [Env.clock] forward by that much
/// *while this route is being served* -- after the request is recorded but
/// before the response is returned -- so a test can place the moment
/// [ActionService._retryable] checks the confirmation's age on either side
/// of [confirmationRetryTtl], the same way a slow server's response arriving
/// late would. Used only by the bounded-retry group below; every other test
/// leaves it null and the clock never moves.
class Route {
  const Route({
    this.status = 200,
    this.body = const {'properties': {}},
    this.advanceClockBy,
  });
  final int status;
  final Map<String, dynamic> body;
  final Duration? advanceClockBy;
}

/// A fake backend recording every request it received, mirroring
/// test-actions.js's `requested`/`sentBodies` globals plus its route table --
/// as an [Env] object instead of module-level mutable state, since
/// [ActionService] is a plain instance rather than a required module.
///
/// [sequences], when a key is present, serves successive [Route]s to
/// successive requests for that key (the last one repeating once
/// exhausted) -- mirrors test-actions.js's
/// `testConfirmedActionRetriesOnce` reaching into `FakeXHR.prototype.send`
/// to make a conflict's *retry* succeed, without this file needing the
/// same kind of prototype patching.
class Env {
  Env({
    Map<String, Route> routes = const {},
    Map<String, List<Route>> sequences = const {},
  }) {
    this.routes = {
      'POST ${base}brew': const Route(
        body: {
          'properties': {'state': 'done', 'id': 7},
        },
      ),
      'POST ${base}cool': const Route(
        body: {
          'properties': {'state': 'done'},
        },
      ),
      'GET ${base}peek': const Route(
        body: {
          'properties': {'seen': true},
        },
      ),
      ...routes,
    };
    this.sequences = Map.of(sequences);
    final client = MockClient((request) async {
      final key = '${request.method} ${request.url}';
      requested.add(key);
      sentBodies.add(request.body);
      final route = _routeFor(key);
      if (route == null) {
        return http.Response(
          '{"properties":{"message":"no such thing here"}}',
          404,
          headers: {'content-type': 'application/json'},
        );
      }
      if (route.advanceClockBy != null) {
        clock = clock.add(route.advanceClockBy!);
      }
      return http.Response(
        jsonEncode(route.body),
        route.status,
        headers: {'content-type': 'application/json'},
      );
    });
    service = ActionService(
      http: HttpService(client: client, log: (_) {}),
      log: (_) {},
      now: () => clock,
    );
  }

  /// Picks [key]'s next [Route]: from [sequences] if one was given for it
  /// (advancing [_sequenceCalls]'s count for that key, clamped to the last
  /// entry once exhausted), otherwise the fixed entry in [routes].
  Route? _routeFor(String key) {
    final sequence = sequences[key];
    if (sequence == null || sequence.isEmpty) {
      return routes[key];
    }
    final index = _sequenceCalls.update(key, (v) => v + 1, ifAbsent: () => 0);
    return sequence[index < sequence.length ? index : sequence.length - 1];
  }

  final Map<String, int> _sequenceCalls = {};
  late final Map<String, Route> routes;
  late final Map<String, List<Route>> sequences;

  /// The time [ActionService]'s injected clock reports -- see [Route]'s
  /// doc comment for how a route moves it forward. Never touched outside a
  /// route's [Route.advanceClockBy], so an ordinary test's assertions about
  /// elapsed time (there are none) stay meaningless by construction: only
  /// the bounded-retry group below cares what this holds.
  DateTime clock = DateTime.fromMillisecondsSinceEpoch(1700000000000);

  final List<String> requested = [];
  final List<String> sentBodies = [];
  late final ActionService service;
}

void main() {
  group('the safe/unsafe split', () {
    test('GET, HEAD, OPTIONS and TRACE are safe', () {
      for (final method in ['GET', 'HEAD', 'OPTIONS', 'TRACE']) {
        expect(isSafeMethod(method), isTrue, reason: method);
      }
    });

    test('POST, PUT, PATCH and DELETE are not', () {
      for (final method in ['POST', 'PUT', 'PATCH', 'DELETE']) {
        expect(isSafeMethod(method), isFalse, reason: method);
      }
    });

    test('the check is case-insensitive', () {
      expect(isSafeMethod('get'), isTrue);
      expect(isSafeMethod('post'), isFalse);
    });

    test(
      'safeMethods is exactly RFC 9110\'s division, not a name blocklist',
      () {
        expect(safeMethods, {'GET', 'HEAD', 'OPTIONS', 'TRACE'});
      },
    );
  });

  group('hasPending', () {
    test('tracks a question raised by ask', () async {
      final env = Env();
      expect(env.service.hasPending, isFalse);

      final outcome = await env.service.ask(backend, root(), 'brew');
      expect(outcome, isA<ConfirmationRequired>());
      expect(env.service.hasPending, isTrue);

      env.service.cancelPending();
      expect(env.service.hasPending, isFalse);
      expect(
        env.requested,
        isEmpty,
        reason: 'nothing was sent by asking then cancelling',
      );
    });
  });

  group('asking about an action', () {
    test('an unsafe action asks before acting', () async {
      final env = Env();
      final outcome = await env.service.ask(backend, root(), 'brew');

      expect(env.requested, isEmpty);
      expect(outcome, isA<ConfirmationRequired>());
      final confirm = outcome as ConfirmationRequired;
      expect(
        confirm.heading,
        'Brew a pot of tea',
        reason: 'headed by the server\'s wording',
      );
    });

    test('a safe action goes straight through', () async {
      final env = Env();
      final outcome = await env.service.ask(backend, root(), 'peek');

      expect(env.requested, ['GET ${base}peek']);
      expect(outcome, isA<ActionInvoked>());
      expect((outcome as ActionInvoked).outcome, isA<InvokeSucceeded>());
      expect(
        env.service.hasPending,
        isFalse,
        reason: 'nothing is left pending once it went straight through',
      );
    });

    test(
      'an action the server no longer offers is reported, not invented',
      () async {
        final env = Env();
        final outcome = await env.service.ask(
          backend,
          root(actions: []),
          'brew',
        );

        expect(env.requested, isEmpty);
        expect(outcome, isA<ActionNotOffered>());
        expect((outcome as ActionNotOffered).name, 'brew');
        expect(env.service.hasPending, isFalse);
      },
    );
  });

  group('answering', () {
    test('declining sends nothing and clears the question', () async {
      final env = Env();
      await env.service.ask(backend, root(), 'brew');

      final outcome = await env.service.answer(false, backend, root());

      expect(outcome, isNull);
      expect(env.requested, isEmpty);
      expect(env.service.hasPending, isFalse);
    });

    test(
      'confirming posts to the href the server gave, with its fields',
      () async {
        final env = Env();
        await env.service.ask(backend, root(), 'brew');

        final outcome = await env.service.answer(true, backend, root());

        expect(env.requested, ['POST ${base}brew']);
        expect(outcome, isA<InvokeSucceeded>());
        final response = (outcome as InvokeSucceeded).response;
        expect(response.status, 200);
        expect(env.service.hasPending, isFalse);
      },
    );

    test(
      'a required checkbox fills from the confirmation the user gave',
      () async {
        final env = Env();
        final ask = await env.service.ask(backend, root(), 'cool');
        expect(
          (ask as ConfirmationRequired).body,
          'The kettle is still hot. Cool it anyway?',
          reason: 'the checkbox title is the question asked',
        );

        await env.service.answer(true, backend, root());

        expect(env.sentBodies, ['confirm=true']);
      },
    );

    test('answering with nothing pending sends nothing', () async {
      final env = Env();
      final outcome = await env.service.answer(true, backend, root());

      expect(outcome, isNull);
      expect(env.requested, isEmpty);
    });

    test(
      'answering against a document that withdrew the action refuses, not invents',
      () async {
        final env = Env();
        await env.service.ask(backend, root(), 'brew');

        // The document underneath the question moved on -- e.g. an idle
        // refresh landed while the confirmation was on screen -- and no
        // longer offers "brew".
        final outcome = await env.service.answer(
          true,
          backend,
          root(actions: []),
        );

        expect(outcome, isA<InvokeRefused>());
        expect(env.requested, isEmpty);
        expect(env.service.hasPending, isFalse);
      },
    );
  });

  group('failures', () {
    test(
      'a conflict is reported like any other failure, and is not retried',
      () async {
        final env = Env(
          routes: {
            'POST ${base}brew': const Route(
              status: 409,
              body: {
                'properties': {'message': 'a brew is already running'},
              },
            ),
          },
        );
        await env.service.ask(backend, root(), 'brew');

        final outcome = await env.service.answer(true, backend, root());

        final posts = env.requested.where((r) => r.startsWith('POST')).length;
        expect(
          posts,
          1,
          reason:
              '"brew" has no required checkbox, so nothing was confirmed in '
              'the sense the bounded retry covers -- see the "the bounded '
              '409 retry" group below for when a retry does happen',
        );
        expect(outcome, isA<InvokeFailed>());
        expect((outcome as InvokeFailed).failure.kind, FailureKind.conflict);
      },
    );

    test('an auth failure is reported', () async {
      final env = Env(
        routes: {
          'POST ${base}brew': const Route(
            status: 401,
            body: {
              'properties': {'message': 'API key rejected'},
            },
          ),
        },
      );
      await env.service.ask(backend, root(), 'brew');

      final outcome = await env.service.answer(true, backend, root());

      expect(env.requested.length, 1);
      expect(outcome, isA<InvokeFailed>());
      expect((outcome as InvokeFailed).failure.kind, FailureKind.auth);
    });
  });

  group('field-filling', () {
    Entity labelDoc(List<Map<String, dynamic>> fields) => Entity.fromJson({
      'class': ['pantry'],
      'title': 'The pantry',
      'links': [
        {
          'rel': ['self'],
          'href': '/',
        },
      ],
      'actions': [
        {
          'name': 'label',
          'title': 'Label a jar',
          'method': 'POST',
          'href': '/label',
          'fields': fields,
        },
      ],
    });

    test('a server-supplied default is used without asking', () async {
      final env = Env(
        routes: {
          'POST ${base}label': const Route(
            body: {
              'properties': {'state': 'done'},
            },
          ),
        },
      );
      final doc = labelDoc([
        {
          'name': 'text',
          'type': 'text',
          'required': true,
          'title': 'What should the jar say?',
          'value': 'jam',
        },
      ]);
      await env.service.ask(backend, doc, 'label');

      final outcome = await env.service.answer(true, backend, doc);

      expect(
        outcome,
        isA<InvokeSucceeded>(),
        reason: 'required is satisfied by the server-supplied default',
      );
      expect(env.sentBodies, ['text=jam']);
    });

    test(
      'a field with no checkbox, no default, and not required is left unfilled',
      () async {
        final env = Env(
          routes: {
            'POST ${base}label': const Route(
              body: {
                'properties': {'state': 'done'},
              },
            ),
          },
        );
        final doc = labelDoc([
          {'name': 'text', 'type': 'text', 'title': 'What should the jar say?'},
        ]);
        await env.service.ask(backend, doc, 'label');

        await env.service.answer(true, backend, doc);

        expect(env.sentBodies, ['']);
      },
    );

    test(
      'a required field with no default and no checkbox is asked for out loud',
      () async {
        final env = Env(
          routes: {
            'POST ${base}label': const Route(
              body: {
                'properties': {'state': 'done'},
              },
            ),
          },
        );
        final doc = labelDoc([
          {
            'name': 'text',
            'type': 'text',
            'required': true,
            'title': 'What should the jar say?',
          },
        ]);
        await env.service.ask(backend, doc, 'label');

        final outcome = await env.service.answer(true, backend, doc);

        expect(
          env.requested,
          isEmpty,
          reason: 'nothing is sent while a value is missing',
        );
        expect(outcome, isA<InvokeNeedsValue>());
        final needsValue = outcome as InvokeNeedsValue;
        expect(needsValue.fieldName, 'text');
        expect(
          needsValue.label,
          'What should the jar say?',
          reason: 'the question uses the server\'s wording',
        );

        final answered = await env.service.answerValue(
          'plum jam',
          backend,
          doc,
        );

        expect(
          answered,
          isA<InvokeSucceeded>(),
          reason: 'what was said is what is sent',
        );
        expect(env.sentBodies, ['text=plum+jam']);
      },
    );

    test('an empty spoken value sends nothing, and says so', () async {
      final env = Env();
      final doc = labelDoc([
        {'name': 'text', 'type': 'text', 'required': true},
      ]);
      await env.service.ask(backend, doc, 'label');
      await env.service.answer(true, backend, doc);

      final outcome = await env.service.answerValue('', backend, doc);

      expect(
        env.requested,
        isEmpty,
        reason: 'an empty transcription sends nothing',
      );
      expect(outcome, isA<InvokeRefused>());
      expect((outcome as InvokeRefused).reason, contains('Nothing was heard'));
      expect(
        env.service.hasPending,
        isFalse,
        reason: 'an unanswerable question is not left pending forever',
      );
    });

    test(
      'more than one missing required field is refused, not dictated one at a time',
      () async {
        final env = Env();
        final doc = labelDoc([
          {'name': 'text', 'type': 'text', 'required': true},
          {'name': 'colour', 'type': 'text', 'required': true},
        ]);
        await env.service.ask(backend, doc, 'label');

        final outcome = await env.service.answer(true, backend, doc);

        expect(
          env.requested,
          isEmpty,
          reason: 'more than one missing value is refused, not dictated',
        );
        expect(outcome, isA<InvokeRefused>());
        expect(
          env.service.hasPending,
          isFalse,
          reason: 'a refused action does not stay pending',
        );
      },
    );

    test('answerValue with nothing awaiting a value sends nothing', () async {
      final env = Env();
      final outcome = await env.service.answerValue(
        'anything',
        backend,
        root(),
      );

      expect(outcome, isNull);
      expect(env.requested, isEmpty);
    });
  });

  group('confirmationText', () {
    test('a checkbox with a title is the question asked', () {
      final action = root().actionByName('cool')!;
      expect(
        confirmationText(action),
        'The kettle is still hot. Cool it anyway?',
      );
    });

    test('without one, the fallback names the action and its method', () {
      final action = root().actionByName('brew')!;
      expect(
        confirmationText(action),
        'Brew a pot of tea?  POST to this server.',
      );
    });
  });

  group('fieldValues', () {
    test('a checkbox carries the confirmation, not a server default', () {
      final action = root().actionByName('cool')!;
      expect(fieldValues(action, confirmed: true), {'confirm': 'true'});
      expect(fieldValues(action, confirmed: false), {'confirm': 'false'});
    });

    test('an action with no fields fills nothing', () {
      final action = root().actionByName('brew')!;
      expect(fieldValues(action, confirmed: true), <String, String>{});
    });
  });

  // --- the bounded 409 retry ------------------------------------------
  //
  // The one case where repeating a request is right: the user ticked a
  // required checkbox, and the server judged the same request twice on
  // budgets that changed in between. Mirrors test-actions.js's own
  // "the bounded retry" section (testConfirmedActionRetriesOnce,
  // testRetryIsNotRepeated, testRetryExpiresAfterTheTTL) -- "cool" is the
  // fixture action with the required checkbox, same as
  // testRequiredCheckboxFillsFromTheConfirmation above.
  group('the bounded 409 retry', () {
    test('a confirmed required checkbox is retried once, and the retry '
        'succeeding is what is reported', () async {
      final env = Env(
        sequences: {
          'POST ${base}cool': [
            const Route(
              status: 409,
              body: {
                'properties': {'message': 'needs confirmation'},
              },
            ),
            const Route(
              body: {
                'properties': {'state': 'done'},
              },
            ),
          ],
        },
      );
      await env.service.ask(backend, root(), 'cool');

      final outcome = await env.service.answer(true, backend, root());

      final posts = env.requested.where((r) => r.startsWith('POST')).length;
      expect(posts, 2, reason: 'a confirmed action is retried exactly once');
      expect(env.sentBodies, [
        'confirm=true',
        'confirm=true',
      ], reason: 'both attempts carried the confirmation');
      expect(
        outcome,
        isA<InvokeSucceeded>(),
        reason: 'the retry succeeding is what is reported',
      );
    });

    test('a retry that also conflicts is not retried again', () async {
      final env = Env(
        routes: {
          'POST ${base}cool': const Route(
            status: 409,
            body: {
              'properties': {'message': 'still no'},
            },
          ),
        },
      );
      await env.service.ask(backend, root(), 'cool');

      final outcome = await env.service.answer(true, backend, root());

      final posts = env.requested.where((r) => r.startsWith('POST')).length;
      expect(
        posts,
        2,
        reason: 'the retry itself is never retried, however it comes back',
      );
      expect(outcome, isA<InvokeFailed>());
      expect((outcome as InvokeFailed).failure.kind, FailureKind.conflict);
    });

    test('a conflict that comes back after the confirmation is a minute old is '
        'not retried', () async {
      final env = Env(
        routes: {
          'POST ${base}cool': const Route(
            status: 409,
            body: {
              'properties': {'message': 'needs confirmation'},
            },
            // The response itself is what carries the clock forward, so
            // the confirmation is already stale by the time the 409 is
            // in hand -- exactly the case the TTL exists to reject.
            advanceClockBy: Duration(minutes: 1),
          ),
        },
      );
      await env.service.ask(backend, root(), 'cool');

      final outcome = await env.service.answer(true, backend, root());

      final posts = env.requested.where((r) => r.startsWith('POST')).length;
      expect(
        posts,
        1,
        reason: 'a confirmation older than the TTL is not spent on a retry',
      );
      expect(outcome, isA<InvokeFailed>());
      expect((outcome as InvokeFailed).failure.kind, FailureKind.conflict);
    });

    test('a conflict that comes back just inside the confirmation TTL is still '
        'retried once', () async {
      final env = Env(
        sequences: {
          'POST ${base}cool': [
            const Route(
              status: 409,
              body: {
                'properties': {'message': 'needs confirmation'},
              },
              advanceClockBy: Duration(seconds: 59),
            ),
            const Route(
              body: {
                'properties': {'state': 'done'},
              },
            ),
          ],
        },
      );
      await env.service.ask(backend, root(), 'cool');

      final outcome = await env.service.answer(true, backend, root());

      final posts = env.requested.where((r) => r.startsWith('POST')).length;
      expect(
        posts,
        2,
        reason: 'a minute has not yet passed, so the retry is still taken',
      );
      expect(outcome, isA<InvokeSucceeded>());
    });

    test('an action with no required checkbox is never retried even on a '
        'confirmed unsafe action', () async {
      // "brew" has no checkbox at all: confirming it ticks nothing, so
      // there is no confirmation for the retry exception to apply to.
      final env = Env(
        routes: {
          'POST ${base}brew': const Route(
            status: 409,
            body: {
              'properties': {'message': 'a brew is already running'},
            },
          ),
        },
      );
      await env.service.ask(backend, root(), 'brew');

      await env.service.answer(true, backend, root());

      final posts = env.requested.where((r) => r.startsWith('POST')).length;
      expect(posts, 1);
    });
  });
}
