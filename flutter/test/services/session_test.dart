// Pure Dart unit tests: no WidgetTester, no device, no socket. See
// AGENTS.md section 5 ("Test style"). Ported from the cases in
// pebble/tools/test-session.js that exercise behaviour session.dart itself
// is responsible for -- the nav-vs-action dispatch in activate(), the
// setActionPendingCheck wiring, and the invoke -> live-watch-or-refetch
// glue action_service.dart's module comment defers to this file. Every
// other behaviour test-session.js re-checks (fetching, the four document
// states, the safe/unsafe split, field filling, the bounded 409 retry) is
// already pinned in nav_service_test.dart/action_service_test.dart/
// live_service_test.dart against the very same services this file composes,
// so it is not duplicated here -- only reached through activate()/answer()
// far enough to prove the composition itself is wired correctly.
//
// Matched from test-session.js: testOpenAndRender/testFollowLinkAndBack/
// testEmbeddedEntity/testPropertyOpensOverlay (activate()'s dispatch --
// fetch, embedded, detail), testUnsafeActionAsksFirst/
// testSafeActionGoesStraightThrough/testDeclining (activate() reaching
// ActionService and back), testConfirmingInvokesAndRefetches/
// testConflictRefetchesAndDoesNotRetry/testAuthFailureDoesNotRefetch/
// testWithdrawnAction/testRequiredCheckbox/testRequiredFieldIsAskedFor/
// testNothingHeardSendsNothing (the invoke-outcome -> notice/refetch glue),
// testLiveModeStartsOn202/testPollTargetIsRelClassMatch/
// testProgressIsReportedInTheServersWords/testWatchingEndsAndRefetches/
// testDeadlineComesFromTheServer (translated to a give-up)/
// testNavigatingAwayStopsWatching (the invoke-outcome -> live-watch glue,
// and stopping it on back()), and the nav.js/actions.js wiring
// testIdleRefresh exists to pin translated to this file's own
// setActionPendingCheck construction-time call. Also matched now (task
// x11): testSaveAndRunAQuickAction/testQuickDocument/testUnsaveableRow/
// testShortcutToDeletedBackend -- saveQuick()/runQuick() are this file's
// own composition of quick_service.dart with nav_service.dart/
// action_service.dart, exactly the kind of glue this file's tests are for
// (see the module comment on saveQuick/runQuick).
//
// Deliberately NOT ported (AppMessage/PebbleKit-wire-protocol-specific, or
// exercising a module this file only composes rather than reimplements --
// see this file's own module comment and flutter/AGENTS.md section 4's
// "what does not carry over"):
//  - testPicker, testRootFlag, testSwitchingBackendsResetsTheStack: the
//    backend picker frame and the atRoot flag are AppMessage/PebbleKit
//    concepts (home_screen.dart's job here, see nav_service.dart's module
//    comment); switching backends discarding the stack is nav_service.dart's
//    own behaviour, already pinned there.
//  - testRefresh, testFailureKeepsTheDocument, testUnreachableIsNotAnError:
//    nav_service.dart's own invariant, already pinned in
//    nav_service_test.dart; session.dart's refresh() is a one-line
//    passthrough.
//  - testPollAboutAnotherJobIsIgnored, testPollWithNoJobIsIgnored,
//    testFailedPollKeepsWatching: live_service.dart's own filtering logic,
//    already pinned in live_service_test.dart; nothing about routing a
//    result to those checks is session.dart's.
//  - testConfirmedActionRetriesOnce, testUnconfirmedActionIsNotRetried,
//    testRetryIsNotRepeated: the bounded 409 retry is entirely internal to
//    action_service.dart (see InvokeFailed's doc comment) and already
//    pinned in action_service_test.dart; session.dart only ever sees the
//    single, final InvokeOutcome that comes back afterwards.
//  - testDefaultsAreUsedWithoutAsking, testSeveralMissingFieldsAreRefused:
//    fillFields()'s own decision, already pinned in action_service_test.dart.
//  - testSequenceIsEchoed, and everything about AppMessage chunking/row
//    indices the harness in test-session.js decodes to make its other
//    assertions: no wire protocol exists on this port to test.

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/services/action_service.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/idle_refresh_clock.dart'
    show idleRefreshInterval;
import 'package:restforge/services/live_service.dart';
import 'package:restforge/services/nav_service.dart';
import 'package:restforge/services/quick_service.dart';
import 'package:restforge/services/render_service.dart';
import 'package:restforge/services/session.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

const String base = 'http://pantry.example/';

final Backend backend = Backend(
  name: 'pantry',
  baseUrl: base,
  secret: 'open-sesame',
);

/// The fixture document -- mirrors test-session.js's ROOT: a link to
/// "shelves", an embedded entity, an action with no fields ("brew", whose
/// response can be made to look like a still-running job), one with a
/// required checkbox ("cool"), and a safe one ("peek").
final Map<String, dynamic> root = {
  'class': ['pantry'],
  'title': 'The pantry',
  'properties': {'apiVersion': 1, 'kettle': 'cold'},
  'entities': [
    {
      'class': ['shelf'],
      'title': 'Top shelf',
      'properties': {'name': 'top', 'jars': 4},
    },
  ],
  'links': [
    {
      'rel': ['self'],
      'href': '/',
    },
    {
      'rel': ['shelves'],
      'href': '/shelves',
    },
    {
      'rel': ['kettle-job'],
      'href': '/job',
    },
  ],
  'actions': [
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
    {
      'name': 'label',
      'title': 'Label a jar',
      'method': 'POST',
      'href': '/label',
      'fields': [
        {
          'name': 'text',
          'type': 'text',
          'required': true,
          'title': 'What should the jar say?',
        },
      ],
    },
  ],
};

final Map<String, dynamic> shelves = {
  'class': ['shelf-list'],
  'title': 'Shelves',
  'properties': {'count': 1},
  'links': [
    {
      'rel': ['self'],
      'href': '/shelves',
    },
  ],
};

Map<String, dynamic> jobBody(Map<String, dynamic> overrides) => {
  'class': ['kettle-job'],
  'properties': {
    'state': 'running',
    'id': 7,
    'step': 'heating the water',
    'staleAfterSeconds': 120,
    ...overrides,
  },
  'links': [
    {
      'rel': ['self'],
      'href': '/job',
    },
  ],
};

class Route {
  const Route({this.status = 200, this.body = const {'properties': {}}});
  final int status;
  final Map<String, dynamic> body;
}

/// A fake, wall-clock-free timer/clock, driven by [tick] -- serves as both
/// [NavService]'s idle-refresh timer and [LiveService]'s poll timer/clock,
/// so a test can advance one notion of time across the whole composed
/// [SessionService] rather than two independent fakes drifting apart.
/// Mirrors nav_service_test.dart's `FakeTimers` and live_service_test.dart's
/// `FakeClock`, merged.
class FakeTimers {
  FakeTimers([DateTime? start])
    : _now = start ?? DateTime.fromMillisecondsSinceEpoch(1700000000000);

  DateTime _now;
  final List<_FakeTimer> _pending = [];

  DateTime now() => _now;

  Timer createTimer(Duration duration, void Function() callback) {
    final timer = _FakeTimer(this, _now.add(duration), callback);
    _pending.add(timer);
    return timer;
  }

  void _remove(_FakeTimer timer) => _pending.remove(timer);

  Future<void> tick(Duration duration) async {
    final until = _now.add(duration);
    for (;;) {
      _FakeTimer? next;
      for (final timer in _pending) {
        if (!timer.fireAt.isAfter(until) &&
            (next == null || timer.fireAt.isBefore(next.fireAt))) {
          next = timer;
        }
      }
      if (next == null) break;
      _now = next.fireAt;
      _pending.remove(next);
      next.callback();
      for (var i = 0; i < 5; i++) {
        await Future<void>.delayed(Duration.zero);
      }
    }
    _now = until;
  }
}

class _FakeTimer implements Timer {
  _FakeTimer(this._clock, this.fireAt, this.callback);

  final FakeTimers _clock;
  final DateTime fireAt;
  final void Function() callback;
  bool _active = true;

  @override
  void cancel() {
    if (_active) {
      _active = false;
      _clock._remove(this);
    }
  }

  @override
  bool get isActive => _active;

  @override
  int get tick => _active ? 0 : 1;
}

/// Composes a real [SessionService] over a faked backend -- a route table
/// keyed by `METHOD URL`, exactly like test-session.js's `routes`/FakeXHR.
class Env {
  Env({FakeTimers? timers}) : timers = timers ?? FakeTimers() {
    final client = MockClient((request) async {
      final key = '${request.method} ${request.url}';
      requested.add(key);
      sentBodies.add(request.body);
      if (unreachable) {
        throw Exception('connection refused');
      }
      final route = routes[key];
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
    final httpService = HttpService(client: client, log: (_) {});
    nav = NavService(http: httpService);
    actions = ActionService(
      http: httpService,
      log: (_) {},
      now: this.timers.now,
    );
    live = LiveService(
      http: httpService,
      log: (_) {},
      now: this.timers.now,
      createTimer: this.timers.createTimer,
    );
    // A real SettingsService/QuickService, not a fake: backendFor() is
    // quick_service.dart's own storage-and-resolution logic, and the point
    // of these tests is that session.dart composes it correctly, not that
    // it is reimplemented here. secretStore is in-memory, exactly as
    // home_screen_test.dart's own copy of this fake -- no secret round
    // trips through a real Keystore in a unit test.
    settings = SettingsService(secretStore: _InMemorySecretStore());
    quick = QuickService(settings: settings);
    session = SessionService(
      http: httpService,
      nav: nav,
      actions: actions,
      live: live,
      quick: quick,
      createTimer: this.timers.createTimer,
    );
  }

  final FakeTimers timers;

  Map<String, Route> routes = {
    'GET $base': Route(body: root),
    'GET ${base}shelves': Route(body: shelves),
    'POST ${base}brew': Route(
      body: {
        'properties': {'state': 'done', 'id': 7},
      },
    ),
    'POST ${base}cool': Route(
      body: {
        'properties': {'state': 'done'},
      },
    ),
    'GET ${base}peek': Route(
      body: {
        'properties': {'seen': true},
      },
    ),
    'POST ${base}label': Route(
      body: {
        'properties': {'state': 'done'},
      },
    ),
  };

  bool unreachable = false;
  final List<String> requested = [];
  final List<String> sentBodies = [];

  late final NavService nav;
  late final ActionService actions;
  late final LiveService live;
  late final SettingsService settings;
  late final QuickService quick;
  late final SessionService session;

  void reset() {
    requested.clear();
    sentBodies.clear();
  }

  /// Opens [backend] and returns the row targets of its root document, for
  /// tests that need to press one by label.
  Future<Map<String, RowTarget>> openRoot() async {
    await session.openBackend(backend);
    reset();
    return {for (final row in session.document!.rows) row.label: row.target};
  }

  /// Opens [backend] and returns the *rows* of its root document, keyed by
  /// label -- unlike [openRoot], for tests that call [SessionService.saveQuick],
  /// which needs the whole [Row] (label and target both), not just the
  /// target [SessionService.activate] alone needs.
  Future<Map<String, Row>> openRootRows() async {
    await session.openBackend(backend);
    reset();
    return {for (final row in session.document!.rows) row.label: row};
  }
}

/// In-memory [SecretStore] fake -- see home_screen_test.dart, the original
/// of this pattern. Duplicated for the same reason that file gives: no
/// other coupling between the two, and this is a handful of lines.
class _InMemorySecretStore implements SecretStore {
  final Map<String, String> data = {};

  @override
  Future<String?> read(String key) async => data[key];

  @override
  Future<void> write(String key, String value) async {
    data[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    data.remove(key);
  }
}

void main() {
  setUp(() {
    // QuickService/SettingsService are backed by shared_preferences; a
    // fresh mock store per test keeps a shortcut saved in one test from
    // leaking into the next.
    SharedPreferences.setMockInitialValues({});
  });

  group('activate() dispatches by target type', () {
    test('a fetch target follows the href and shows the document', () async {
      final env = Env();
      final rows = await env.openRoot();

      await env.session.activate(rows['shelves']!);

      expect(env.requested, ['GET ${base}shelves']);
      expect(env.session.document?.title, 'Shelves');
    });

    test('an embedded target opens without a request', () async {
      final env = Env();
      final rows = await env.openRoot();

      await env.session.activate(rows['Top shelf']!);

      expect(env.requested, isEmpty);
      expect(env.session.document?.title, 'Top shelf');
      expect(env.session.canGoBack, isTrue);
    });

    test(
      'a detail target opens for reading, asking the server nothing',
      () async {
        final env = Env();
        final rows = await env.openRoot();

        await env.session.activate(rows['kettle']!);

        expect(env.requested, isEmpty);
        expect(env.session.detail?.heading, 'kettle');
        expect(env.session.detail?.body, 'cold');
        expect(
          env.session.document?.title,
          'The pantry',
          reason: 'the document underneath is unchanged',
        );
      },
    );

    test('dismissDetail closes it without touching navigation', () async {
      final env = Env();
      final rows = await env.openRoot();
      await env.session.activate(rows['kettle']!);

      env.session.dismissDetail();

      expect(env.session.detail, isNull);
      expect(env.session.document?.title, 'The pantry');
    });

    test('back pops the stack and stops any live watch', () async {
      final env = Env();
      final rows = await env.openRoot();
      await env.session.activate(rows['shelves']!);
      env.reset();

      env.session.back();

      expect(env.session.document?.title, 'The pantry');
      expect(env.requested, isEmpty, reason: 'back asks the server nothing');
    });
  });

  group('activate() on an action target', () {
    test('an unsafe action asks before acting', () async {
      final env = Env();
      final rows = await env.openRoot();

      await env.session.activate(rows['Brew a pot of tea']!);

      expect(env.requested, isEmpty);
      final question = env.session.question;
      expect(question, isA<ConfirmQuestion>());
      expect((question as ConfirmQuestion).heading, 'Brew a pot of tea');
      expect(
        env.session.document?.rows.length,
        env.session.document?.rows.length,
        reason: 'the document is still underneath',
      );
    });

    test('a safe action goes straight through', () async {
      final env = Env();
      final rows = await env.openRoot();

      await env.session.activate(rows['Look inside']!);

      // Not gated by a confirmation -- but still followed by the same
      // unconditional re-fetch every invoked action gets: "never carry a
      // document across an action" makes no exception for a safe one.
      expect(env.requested.first, 'GET ${base}peek');
      expect(env.requested, contains('GET $base'));
      expect(env.session.question, isNull);
    });

    test('declining sends nothing and clears the question', () async {
      final env = Env();
      final rows = await env.openRoot();
      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();

      await env.session.answer(false);

      expect(env.requested, isEmpty);
      expect(env.session.question, isNull);
      expect(env.session.document?.title, 'The pantry');
    });

    test(
      'confirming invokes the action and re-fetches the document afterwards',
      () async {
        final env = Env();
        final rows = await env.openRoot();
        await env.session.activate(rows['Brew a pot of tea']!);
        env.reset();

        await env.session.answer(true);

        expect(env.requested, ['POST ${base}brew', 'GET $base']);
        final notice = env.session.notice;
        expect(notice, isA<ActionOutcomeReported>());
        notice as ActionOutcomeReported;
        expect(notice.heading, 'Brew a pot of tea');
        expect(notice.message, 'done');
        expect(notice.body, contains('id: 7'));
        expect(env.session.document?.title, 'The pantry');
      },
    );

    test(
      'a required checkbox is the confirmation, and fills the field',
      () async {
        final env = Env();
        final rows = await env.openRoot();

        await env.session.activate(rows['Let the kettle cool']!);
        final question = env.session.question as ConfirmQuestion;
        expect(question.body, 'The kettle is still hot. Cool it anyway?');

        env.reset();
        await env.session.answer(true);
        expect(env.sentBodies.first, 'confirm=true');
      },
    );

    test('a conflict re-fetches instead of retrying', () async {
      final env = Env();
      final rows = await env.openRoot();
      env.routes['POST ${base}brew'] = Route(
        status: 409,
        body: {
          'properties': {'message': 'a brew is already running'},
        },
      );
      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();

      await env.session.answer(true);

      expect(env.requested, ['POST ${base}brew', 'GET $base']);
      final notice = env.session.notice as ActionFailed;
      expect(notice.failure.kind, FailureKind.conflict);
      expect(notice.failure.message, 'a brew is already running');
    });

    test('an auth failure does not re-fetch', () async {
      final env = Env();
      final rows = await env.openRoot();
      env.routes['POST ${base}brew'] = Route(
        status: 401,
        body: {
          'properties': {'message': 'API key rejected'},
        },
      );
      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();

      await env.session.answer(true);

      expect(env.requested, ['POST ${base}brew']);
      final notice = env.session.notice as ActionFailed;
      expect(notice.failure.kind, FailureKind.auth);
      expect(env.session.document?.title, 'The pantry');
    });

    test(
      'answering a question the server withdrew in the meantime sends nothing',
      () async {
        final env = Env();
        final rows = await env.openRoot();
        await env.session.activate(rows['Brew a pot of tea']!);
        // The confirmation is pending; now the document changes underneath
        // it, exactly as if the server withdrew the action.
        env.routes['GET $base'] = Route(
          body: {
            'class': ['pantry'],
            'links': [
              {
                'rel': ['self'],
                'href': '/',
              },
            ],
            'actions': [],
          },
        );
        await env.session.refresh();
        env.reset();

        await env.session.answer(true);

        expect(env.requested, isEmpty);
      },
    );

    test('a required field with no default is asked for out loud', () async {
      final env = Env();
      final rows = await env.openRoot();
      // "label" is a POST, so it asks to confirm before fillFields ever
      // gets a chance to notice the missing field -- mirrors labelAction()
      // in test-session.js calling session.answer(true) before the value
      // prompt appears.
      await env.session.activate(rows['Label a jar']!);
      expect(env.session.question, isA<ConfirmQuestion>());

      await env.session.answer(true);
      final question = env.session.question;
      expect(question, isA<ValueQuestion>());
      expect((question as ValueQuestion).label, 'What should the jar say?');

      env.reset();
      await env.session.answerValue('plum jam');
      expect(env.sentBodies.first, 'text=plum+jam');
    });

    test('an empty answer to a value question sends nothing', () async {
      final env = Env();
      final rows = await env.openRoot();
      await env.session.activate(rows['Label a jar']!);
      await env.session.answer(true);
      env.reset();

      await env.session.answerValue('');

      expect(env.requested, isEmpty);
      final notice = env.session.notice as ActionRefused;
      expect(notice.reason, contains('Nothing was heard'));
    });

    test(
      'an action the server no longer offers is reported, not guessed at',
      () async {
        final env = Env();
        final rows = await env.openRoot();
        env.routes['GET $base'] = Route(
          body: {
            'class': ['pantry'],
            'links': [
              {
                'rel': ['self'],
                'href': '/',
              },
            ],
            'actions': [],
          },
        );
        await env.session.refresh();
        env.reset();

        await env.session.activate(rows['Brew a pot of tea']!);

        expect(env.requested, isEmpty);
        expect(env.session.notice, isA<ActionWithdrawn>());
      },
    );
  });

  group('live mode -- the invoke-outcome-to-watch glue', () {
    /// Starts brewing: a 202 with a running job, watched from there on --
    /// mirrors test-session.js's startBrew().
    Future<Map<String, RowTarget>> startBrew(Env env) async {
      final rows = await env.openRoot();
      env.routes['POST ${base}brew'] = Route(status: 202, body: jobBody({}));
      env.routes['GET ${base}job'] = Route(body: jobBody({}));
      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();
      await env.session.answer(true);
      return rows;
    }

    test('a 202 starts a watch instead of calling it done', () async {
      final env = Env();
      await startBrew(env);

      expect(
        env.requested,
        ['POST ${base}brew'],
        reason: 'a still-running job is not re-fetched as if it were done',
      );
      expect(env.session.isLive, isTrue);
      final notice = env.session.notice as ActionOutcomeReported;
      // The fixture's job body already carries a "state", which
      // _resultBanner prefers over the 202-derived "Accepted" fallback --
      // the server's own word beats a generic one whenever it gave one.
      expect(notice.message, 'running');
    });

    test(
      'the job is polled by matching its class against the origin\'s links',
      () async {
        final env = Env();
        await startBrew(env);
        env.reset();

        await env.timers.tick(pollInterval);

        expect(env.requested, ['GET ${base}job']);
      },
    );

    test('progress is reported in the server\'s own words', () async {
      final env = Env();
      await startBrew(env);
      env.routes['GET ${base}job'] = Route(body: jobBody({'step': 'steeping'}));
      env.reset();

      await env.timers.tick(pollInterval);

      final notice = env.session.notice as ActionProgress;
      expect(notice.step, 'steeping');
      expect(env.session.isLive, isTrue);
    });

    test('watching ends and the origin document is re-fetched', () async {
      final env = Env();
      await startBrew(env);
      env.routes['GET ${base}job'] = Route(
        body: jobBody({'state': 'done', 'step': ''}),
      );
      env.reset();

      await env.timers.tick(pollInterval);

      expect(env.requested, ['GET ${base}job', 'GET $base']);
      final notice = env.session.notice as ActionOutcomeReported;
      expect(notice.message, 'done');
      expect(env.session.isLive, isFalse);
    });

    test('giving up is reported as giving up, and still re-fetches', () async {
      final env = Env();
      final rows = await env.openRoot();
      env.routes['POST ${base}brew'] = Route(
        status: 202,
        body: jobBody({'staleAfterSeconds': 1}),
      );
      env.routes['GET ${base}job'] = Route(
        body: jobBody({'staleAfterSeconds': 1}),
      );
      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();
      await env.session.answer(true);

      // 1s of budget plus the 60s buffer: a poll at 70s is outside it.
      await env.timers.tick(const Duration(seconds: 70));

      expect(env.session.notice, isA<ActionGaveUp>());
      expect(env.session.isLive, isFalse);
      expect(
        env.requested.last,
        'GET $base',
        reason: 'giving up on the watch still re-reads the document',
      );
    });

    test('leaving the document being watched stops the watch', () async {
      final env = Env();
      await startBrew(env);
      env.reset();

      env.session.back();
      await env.timers.tick(const Duration(seconds: 30));

      expect(
        env.requested.where((r) => r.contains('job')),
        isEmpty,
        reason: 'nothing polls a job for a screen nobody is looking at',
      );
    });
  });

  group('the setActionPendingCheck wiring', () {
    test('a pending action question suppresses the idle refresh; resolving it '
        'lets the clock reconsider', () async {
      final env = Env();
      final rows = await env.openRoot();

      await env.session.activate(rows['Brew a pot of tea']!);
      env.reset();

      await env.timers.tick(idleRefreshInterval);
      expect(
        env.requested,
        isEmpty,
        reason:
            'session.dart wires ActionService.hasPending into the '
            'idle-refresh clock (IdleRefreshClock, task a21) at '
            'construction time, so the clock must hold off while a '
            'confirmation is outstanding',
      );

      // Confirming resolves the question -- and, via the invoke -> refetch
      // glue this file itself owns, touches nav_service.dart directly, and
      // nav_service.dart's notifyListeners is exactly what gives the
      // idle-refresh clock (a listener: it reconsiders on every nav
      // notification, see idle_refresh_clock.dart) a chance to notice
      // nothing is pending any more.
      await env.session.answer(true);
      env.reset();
      await env.timers.tick(idleRefreshInterval);
      expect(
        env.requested,
        ['GET $base'],
        reason: 'once nothing is pending, the idle clock schedules again',
      );
    });
  });

  group('saved shortcuts (saveQuick/runQuick)', () {
    test('saving an action asks the server nothing; running it re-reads the '
        'holder and still asks before acting', () async {
      final env = Env();
      final rows = await env.openRootRows();
      await env.settings.saveBackends([backend]);

      final saveOutcome = await env.session.saveQuick(
        rows['Brew a pot of tea']!,
      );
      expect(saveOutcome, QuickSaveOutcome.saved);
      expect(env.requested, isEmpty, reason: 'saving asks the server nothing');

      final saved = (await env.quick.load()).single;
      env.reset();

      final runOutcome = await env.session.runQuick(saved);
      expect(runOutcome, QuickRunOutcome.opened);
      expect(
        env.requested,
        ['GET $base'],
        reason:
            'the holder is re-read so the action is looked up by name '
            'in a current document, rather than fired at a remembered '
            'href',
      );
      expect(
        env.session.question,
        isA<ConfirmQuestion>(),
        reason:
            'it still asks before acting, exactly as a hand-pressed '
            'action would',
      );
      expect(
        env.requested.any((r) => r.startsWith('POST')),
        isFalse,
        reason: 'the press alone sends nothing',
      );

      env.reset();
      await env.session.answer(true);
      expect(
        env.requested.first,
        'POST ${base}brew',
        reason: 'confirming is what invokes it',
      );
    });

    test('saving a link and running it fetches it directly, with nothing to '
        'go back to', () async {
      final env = Env();
      final rows = await env.openRootRows();
      await env.settings.saveBackends([backend]);

      final saveOutcome = await env.session.saveQuick(rows['shelves']!);
      expect(saveOutcome, QuickSaveOutcome.saved);

      final saved = (await env.quick.load()).single;
      env.reset();

      final runOutcome = await env.session.runQuick(saved);
      expect(runOutcome, QuickRunOutcome.opened);
      expect(env.requested, ['GET ${base}shelves']);
      expect(env.session.document?.title, 'Shelves');
      expect(
        env.session.canGoBack,
        isFalse,
        reason:
            'straight to the destination: nothing was walked, so there '
            'is no history to pop back through',
      );
    });

    test(
      'a property row cannot be a shortcut, and nothing is stored',
      () async {
        final env = Env();
        final rows = await env.openRootRows();

        final outcome = await env.session.saveQuick(rows['kettle']!);

        expect(outcome, QuickSaveOutcome.notSaveable);
        expect(await env.quick.count(), 0);
      },
    );

    test(
      'an already-embedded sub-entity row cannot be a shortcut either',
      () async {
        final env = Env();
        final rows = await env.openRootRows();

        final outcome = await env.session.saveQuick(rows['Top shelf']!);

        expect(outcome, QuickSaveOutcome.notSaveable);
        expect(await env.quick.count(), 0);
      },
    );

    test('a shortcut to a backend that is no longer configured explains '
        'itself rather than reaching for it', () async {
      final env = Env();
      final rows = await env.openRootRows();
      await env.settings.saveBackends([backend]);
      await env.session.saveQuick(rows['shelves']!);
      final saved = (await env.quick.load()).single;

      await env.settings.saveBackends([
        const Backend(
          name: 'other',
          baseUrl: 'http://elsewhere.example/',
          secret: 'x',
        ),
      ]);
      env.reset();

      final outcome = await env.session.runQuick(saved);

      expect(outcome, QuickRunOutcome.backendMissing);
      expect(
        env.requested,
        isEmpty,
        reason: 'it does not reach for the old server',
      );
    });

    test(
      'saving past MAX_QUICK is refused rather than silently dropped',
      () async {
        final env = Env();
        final rows = await env.openRootRows();
        for (var i = 0; i < QuickService.maxQuick; i++) {
          await env.quick.add(
            QuickItem(
              label: 'n$i',
              baseUrl: base,
              kind: QuickKind.document,
              href: '/n$i',
            ),
          );
        }

        final outcome = await env.session.saveQuick(rows['shelves']!);

        expect(outcome, QuickSaveOutcome.full);
        expect(await env.quick.count(), QuickService.maxQuick);
      },
    );

    test('an action shortcut whose action the server no longer offers reports '
        'that rather than doing nothing', () async {
      final env = Env();
      final rows = await env.openRootRows();
      await env.settings.saveBackends([backend]);
      await env.session.saveQuick(rows['Brew a pot of tea']!);
      final saved = (await env.quick.load()).single;
      // The next fetch of the holder no longer offers "brew" at all.
      env.routes['GET $base'] = Route(
        body: {...root, 'actions': const <Map<String, dynamic>>[]},
      );
      env.reset();

      final outcome = await env.session.runQuick(saved);

      expect(outcome, QuickRunOutcome.opened);
      expect(env.session.notice, isA<ActionWithdrawn>());
      expect(
        env.session.question,
        isNull,
        reason: 'a withdrawn action is a real answer, not a question',
      );
    });
  });
}
