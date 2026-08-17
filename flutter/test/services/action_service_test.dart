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
// half), testAuthFailureDoesNotRefetch (same), testWithdrawnAction, and
// testDefaultsAreUsedWithoutAsking.
//
// Deliberately NOT ported: testConfirmedActionRetriesOnce,
// testRetryIsNotRepeated and testRetryExpiresAfterTheTTL (the bounded 409
// retry -- its own task, m11) and testRequiredFieldIsAskedFor,
// testNothingHeardSendsNothing and testSeveralMissingFieldsAreRefused (the
// required-field prompt -- its own task, n11; siren.dart's Field does not
// model "required" yet, so there is nothing to drive that logic with here).
//
// Added beyond test-actions.js: direct coverage of isSafeMethod/safeMethods
// (test-actions.js only exercises the split indirectly, through
// brew/peek), confirmationText's fallback sentence, and a field with
// neither a checkbox nor a default being left out of what is sent -- the
// explicit "or refused" behaviour this port stops at, see
// action_service.dart's module comment.

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
              'title': 'The kettle is still hot. Cool it anyway?',
            },
          ],
        },
        {'name': 'peek', 'title': 'Look inside', 'href': '/peek'},
      ],
});

/// One canned reply for [Env]'s [MockClient], keyed by `METHOD URL`.
class Route {
  const Route({this.status = 200, this.body = const {'properties': {}}});
  final int status;
  final Map<String, dynamic> body;
}

/// A fake backend recording every request it received, mirroring
/// test-actions.js's `requested`/`sentBodies` globals plus its route table --
/// as an [Env] object instead of module-level mutable state, since
/// [ActionService] is a plain instance rather than a required module.
class Env {
  Env({Map<String, Route> routes = const {}}) {
    final table = {
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
    final client = MockClient((request) async {
      final key = '${request.method} ${request.url}';
      requested.add(key);
      sentBodies.add(request.body);
      final route = table[key];
      if (route == null) {
        return http.Response(
          '{"properties":{"message":"no such thing here"}}',
          404,
          headers: {'content-type': 'application/json'},
        );
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
    );
  }

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
              'no required checkbox, so nothing to retry (m11 owns retrying)',
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
          'title': 'What should the jar say?',
          'value': 'jam',
        },
      ]);
      await env.service.ask(backend, doc, 'label');

      await env.service.answer(true, backend, doc);

      expect(env.sentBodies, ['text=jam']);
    });

    test('a field with no checkbox and no default is left unfilled', () async {
      // siren.dart's Field does not model "required" yet, so this port has
      // no way to tell such a field apart from an optional one with no
      // value -- both are simply omitted, never guessed at. Asking out loud
      // for a value the server actually requires is n11's job.
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
}
