// Widget tests for the document screen: the rendered rows in specification
// order, the four document states, pull-to-refresh, and back navigation
// through SessionService's own stack rather than the platform Navigator.
// See AGENTS.md section 5 ("Test style"): screens get widget tests via
// testWidgets/pumpWidget, asserting on what's rendered, never on a device
// feature.
//
// SessionService is composed over a real HttpService backed by
// package:http's MockClient, mirroring test/services/session_test.dart's
// Env -- a route table keyed by "METHOD URL", plus a controllable completer
// for the one test that needs to observe a fetch mid-flight. This is a
// self-contained fixture rather than a shared harness: the two files
// exercise different layers (session.dart's own composition vs. what a
// screen renders from it), and duplicating a dozen lines of MockClient
// wiring is cheaper than coupling them.
//
// Deliberately not covered here, each a separate task depending on this one
// -- see document_screen.dart's module comment: the full reading view for a
// property (t11) and the confirmation sheet / value prompt for an action
// (u11, covered in confirmation_sheet_test.dart). Where those matter, a test
// below only checks that SessionService.activate was reached and reacted the
// way session.dart's own tests already pin (e.g. an unsafe action asks for
// confirmation before doing anything) -- not that this screen renders that
// reaction.
//
// Live-job progress (v11, this task) gets its own group below, built on a
// second fixture document (_liveRootBody/_jobBody) rather than _rootBody --
// see that group's header comment for why.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/screens/document_screen.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/live_service.dart';
import 'package:restforge/services/nav_service.dart';
import 'package:restforge/services/session.dart';
import 'package:restforge/services/settings_service.dart';

const String _base = 'http://bench.example/';

final Backend _backend = Backend(name: 'bench', baseUrl: _base, secret: 'k');

/// The fixture root document: two properties (their order is asserted), one
/// embedded sub-entity (so a "back" test needs no second HTTP round trip),
/// one link, and one unsafe (POST) action.
Map<String, dynamic> _rootBody({String temp = 'cool'}) => {
  'class': ['workshop'],
  'title': 'The workshop',
  'properties': {'status': 'idle', 'temp': temp},
  'entities': [
    {
      'class': ['tool'],
      'title': 'Lathe',
      'properties': {'oiled': true, 'hours': 12},
    },
  ],
  'links': [
    {
      'rel': ['catalogue'],
      'href': '/catalogue',
      'title': 'Catalogue',
    },
  ],
  'actions': [
    {
      'name': 'sweep',
      'title': 'Sweep the floor',
      'method': 'POST',
      'href': '/sweep',
      'fields': [],
    },
  ],
};

const Map<String, String> _jsonHeaders = {'content-type': 'application/json'};

/// A second root, used only by the live-notice group below: a `GET` action
/// ("fire") -- safe, so `action_service.dart` invokes it straight through
/// with no confirmation sheet (u11's concern, not this file's) -- and a link
/// whose rel matches the job entity's class, so `LiveService.pollTarget`
/// finds it. Kept separate from [_rootBody] rather than added onto it, so
/// the row-count/icon-count assertions in the tests above stay untouched.
/// Mirrors session_test.dart's pantry fixture ('brew'/'kettle-job').
Map<String, dynamic> _liveRootBody() => {
  'class': ['workshop'],
  'title': 'The workshop',
  'properties': {'status': 'idle'},
  'links': [
    {
      'rel': ['kiln-job'],
      'href': '/job',
    },
  ],
  'actions': [
    {'name': 'fire', 'title': 'Fire the kiln', 'href': '/fire', 'fields': []},
  ],
};

/// A job resource body -- mirrors session_test.dart's `jobBody()`.
Map<String, dynamic> _jobBody(Map<String, dynamic> overrides) => {
  'class': ['kiln-job'],
  'properties': {
    'state': 'running',
    'id': 9,
    'step': 'heating',
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

/// A manually-advanced clock for [LiveService]'s budget check alone --
/// [LiveService]'s poll `Timer` keeps the real `Timer` factory, since
/// `flutter_test`'s own fake clock (what `tester.pump(duration)` elapses)
/// already fires those correctly inside a `testWidgets` body (proved by the
/// tests above this group, well before this task). What `pump(duration)`
/// does *not* do is make `DateTime.now()` itself return a later time, so a
/// [LiveService] budget compared against the real clock never appears to
/// expire no matter how much virtual time `pump()` elapses. [advance] is
/// called in lockstep with `tester.pump(duration)` by the one test that
/// needs a budget to actually run out (see the "gave up" test below); every
/// other test in this group leaves it untouched, which is indistinguishable
/// from real time not having moved -- exactly what a still-running watch
/// needs to keep watching.
class _FakeClock {
  DateTime _now = DateTime.fromMillisecondsSinceEpoch(1700000000000);
  DateTime now() => _now;
  void advance(Duration duration) => _now = _now.add(duration);
}

/// Composes a real [SessionService] over a faked backend -- see the header
/// comment. [routeFor] holds one response body per "METHOD URL" key,
/// mutable so a test can change what the *next* fetch to the same address
/// returns (a refresh finding different data, or starting to fail).
/// [throwing] simulates a connection that never answers at all
/// ([FailureKind.unreachable]) rather than an HTTP error status; [pause]
/// lets one test hold a response open to observe a fetch mid-flight.
/// [clock] feeds [LiveService]'s budget check alone -- see [_FakeClock].
class _Env {
  _Env() {
    final client = MockClient((request) async {
      final key = '${request.method} ${request.url}';
      requestLog.add(key);
      if (pause != null) {
        await pause!.future;
      }
      if (throwing) {
        throw Exception('connection refused');
      }
      final status = statusFor[key] ?? 200;
      final body = routeFor[key];
      return http.Response(
        body == null ? '{"properties":{}}' : jsonEncode(body),
        status,
        headers: _jsonHeaders,
      );
    });
    final httpService = HttpService(client: client, log: (_) {});
    final live = LiveService(http: httpService, log: (_) {}, now: clock.now);
    session = SessionService(http: httpService, live: live);
  }

  final Map<String, dynamic> routeFor = {'GET $_base': _rootBody()};
  final Map<String, int> statusFor = {};
  final List<String> requestLog = [];
  final _FakeClock clock = _FakeClock();
  bool throwing = false;
  Completer<void>? pause;
  late final SessionService session;
}

class _RecordingObserver extends NavigatorObserver {
  int popCount = 0;

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    popCount++;
  }
}

void main() {
  Future<void> pumpDocument(WidgetTester tester, SessionService session) async {
    await tester.pumpWidget(MaterialApp(home: DocumentScreen(session: session)));
    await tester.pump();
  }

  /// Opens [_liveRootBody] and presses "Fire the kiln" -- a safe `GET`, so
  /// `action_service.dart` invokes it straight through with no confirmation
  /// sheet -- landing on a reply whose `state: running` starts a watch.
  /// Mirrors session_test.dart's `startBrew()`.
  Future<_Env> startWatch(WidgetTester tester) async {
    final env = _Env();
    env.routeFor['GET $_base'] = _liveRootBody();
    await env.session.openBackend(_backend);
    await pumpDocument(tester, env.session);

    env.routeFor['GET ${_base}fire'] = _jobBody({});
    env.routeFor['GET ${_base}job'] = _jobBody({});

    await tester.tap(find.text('Fire the kiln'));
    await tester.pump();
    return env;
  }

  testWidgets(
    'rows render in specification order -- properties, sub-entities, links, actions',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);

      expect(find.text('status'), findsOneWidget);
      expect(find.text('temp'), findsOneWidget);
      expect(find.text('Lathe'), findsOneWidget);
      expect(find.text('Catalogue'), findsOneWidget);
      expect(find.text('Sweep the floor'), findsOneWidget);

      double top(Finder finder) => tester.getTopLeft(finder).dy;
      expect(top(find.text('status')), lessThan(top(find.text('temp'))));
      expect(top(find.text('temp')), lessThan(top(find.text('Lathe'))));
      expect(top(find.text('Lathe')), lessThan(top(find.text('Catalogue'))));
      expect(
        top(find.text('Catalogue')),
        lessThan(top(find.text('Sweep the floor'))),
      );

      // Each kind carries its own icon -- the visual half of "never look
      // like the same gesture".
      expect(find.byIcon(Icons.label_outline), findsNWidgets(2));
      expect(find.byIcon(Icons.account_tree_outlined), findsOneWidget);
      expect(find.byIcon(Icons.link), findsOneWidget);
      expect(find.byIcon(Icons.bolt), findsOneWidget);
      env.session.dispose();
    },
  );

  testWidgets('the action row is a button-shaped tile, not a plain list row', (
    tester,
  ) async {
    final env = _Env();
    await env.session.openBackend(_backend);
    await pumpDocument(tester, env.session);

    expect(
      find.ancestor(
        of: find.text('Sweep the floor'),
        matching: find.byType(ListTile),
      ),
      findsNothing,
    );
    expect(
      find.ancestor(
        of: find.text('Sweep the floor'),
        matching: find.byType(InkWell),
      ),
      findsOneWidget,
    );
    // A property/entity/link row, in contrast, IS a plain ListTile.
    expect(
      find.ancestor(of: find.text('status'), matching: find.byType(ListTile)),
      findsOneWidget,
    );
    env.session.dispose();
  });

  testWidgets(
    'tapping a property row opens it for reading without touching the document',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);

      await tester.tap(find.text('status'));
      await tester.pump();

      final detail = env.session.detail;
      expect(detail, isNotNull);
      expect(detail!.heading, 'status');
      expect(detail.body, 'idle');
      expect(env.session.document?.title, 'The workshop');
      expect(env.session.state, DocumentState.ok);
      env.session.dispose();
    },
  );

  testWidgets(
    'tapping a sub-entity row opens the embedded entity without a network call',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);
      final before = env.requestLog.length;

      await tester.tap(find.text('Lathe'));
      await tester.pump();

      expect(env.session.document?.title, 'Lathe');
      expect(env.session.canGoBack, isTrue);
      expect(env.requestLog.length, before);
      env.session.dispose();
    },
  );

  testWidgets(
    'tapping an action row asks for confirmation before doing anything',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);
      final before = env.requestLog.length;

      await tester.tap(find.text('Sweep the floor'));
      await tester.pump();

      // pebble/docs/DESIGN.md, "Ask before acting": an unsafe method (POST
      // here) is never sent before it is confirmed -- rendering that
      // confirmation is task u11's job, not this screen's; this only proves
      // the row press reached SessionService.activate and it dispatched to
      // the action pipeline rather than treating the row as a property.
      expect(env.session.question, isA<ConfirmQuestion>());
      expect(env.requestLog.length, before);
      env.session.dispose();
    },
  );

  testWidgets(
    "back pops SessionService's own stack, not the platform route, whenever there is somewhere to pop to",
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      final observer = _RecordingObserver();

      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [observer],
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () => Navigator.push(
                    context,
                    MaterialPageRoute(
                      builder: (_) => DocumentScreen(session: env.session),
                    ),
                  ),
                  child: const Text('open'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Lathe'));
      await tester.pump();
      expect(env.session.document?.title, 'Lathe');
      expect(env.session.canGoBack, isTrue);

      final popsBefore = observer.popCount;
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();

      // Popped SessionService's stack, not the route: the document changed
      // back to root, but this screen is still the one on top.
      expect(env.session.document?.title, 'The workshop');
      expect(observer.popCount, popsBefore);
      expect(find.byType(DocumentScreen), findsOneWidget);

      // At the root document there is nowhere left in SessionService's own
      // stack to pop to, so the next back is let through to the platform,
      // returning to whatever pushed this screen.
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();

      expect(observer.popCount, popsBefore + 1);
      expect(find.byType(DocumentScreen), findsNothing);
      env.session.dispose();
    },
  );

  testWidgets('pull-to-refresh re-reads the current document', (
    tester,
  ) async {
    final env = _Env();
    await env.session.openBackend(_backend);
    await pumpDocument(tester, env.session);
    expect(find.text('cool'), findsOneWidget);

    env.routeFor['GET $_base'] = _rootBody(temp: 'hot');

    await tester.fling(find.byType(ListView), const Offset(0, 300), 1000);
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(find.text('hot'), findsOneWidget);
    expect(find.text('cool'), findsNothing);
    env.session.dispose();
  });

  testWidgets(
    'a document not yet fetched shows the loading state, not a blank screen',
    (tester) async {
      final env = _Env()..pause = Completer<void>();
      final opened = env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);

      expect(find.byKey(const Key('initial-loading')), findsOneWidget);
      expect(env.session.document, isNull);

      env.pause!.complete();
      await opened;
      await tester.pump();

      expect(find.byKey(const Key('initial-loading')), findsNothing);
      expect(find.text('status'), findsOneWidget);
      env.session.dispose();
    },
  );

  testWidgets(
    'a failed refresh keeps the last good document on screen with the '
    'failure banner layered over it (unreachable)',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);

      // The last good document is on screen with nothing over it yet.
      expect(find.text('status'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);

      env.throwing = true;
      await env.session.refresh();
      await tester.pump();

      // The invariant this file exists to protect
      // (pebble/docs/DESIGN.md, "A failed request is not an answer"): the
      // rows from before are still exactly there --
      expect(find.text('status'), findsOneWidget);
      expect(find.text('idle'), findsOneWidget);
      expect(find.text('Lathe'), findsOneWidget);
      // -- with the failure laid over them, never instead of them.
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.textContaining('Unreachable'), findsOneWidget);
      expect(env.session.state, DocumentState.unreachable);
      env.session.dispose();
    },
  );

  testWidgets(
    'a failed refresh keeps the last good document on screen with the '
    'failure banner layered over it (server error)',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpDocument(tester, env.session);

      env.statusFor['GET $_base'] = 500;
      env.routeFor['GET $_base'] = {
        'properties': {'message': 'kiln overheated'},
      };
      await env.session.refresh();
      await tester.pump();

      expect(find.text('status'), findsOneWidget);
      expect(find.text('Lathe'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.textContaining('Request failed'), findsOneWidget);
      expect(find.textContaining('kiln overheated'), findsOneWidget);
      expect(env.session.state, DocumentState.error);
      env.session.dispose();
    },
  );

  group('the live-job notice banner (v11)', () {
    // Four distinct things, shown as four distinct things -- see
    // document_screen.dart's module comment and live_service.dart's own,
    // which spends most of its length on exactly this failure mode. Built on
    // startWatch()/_liveRootBody()/_jobBody() rather than _rootBody(), whose
    // single POST action and lack of a job-shaped link give nothing for
    // LiveService.pollTarget to follow.

    testWidgets(
      'progress: a running job shows the watching banner (spinner, current '
      'text), not dismissible',
      (tester) async {
        final env = await startWatch(tester);

        expect(env.session.isLive, isTrue);
        // Before the first poll lands, the banner shows the invoke's own
        // ActionOutcomeReported message -- see _watchingBanner's doc comment
        // -- which is the job's own "state": "running".
        expect(find.byKey(const Key('live-watching-banner')), findsOneWidget);
        expect(find.text('running'), findsOneWidget);
        expect(find.byIcon(Icons.close), findsNothing);
        expect(find.byKey(const Key('notice-dismiss')), findsNothing);

        // The first poll lands with a new step: still ActionProgress, still
        // the same watching banner, now showing the step instead.
        env.routeFor['GET ${_base}job'] = _jobBody({'step': 'glazing'});
        await tester.pump(pollInterval);

        expect(env.session.isLive, isTrue);
        expect(find.byKey(const Key('live-watching-banner')), findsOneWidget);
        expect(find.text('glazing'), findsOneWidget);
        expect(find.byKey(const Key('notice-dismiss')), findsNothing);
        env.session.dispose();
      },
    );

    testWidgets(
      'no news: a poll about a different job leaves the watching banner '
      'exactly as it was',
      (tester) async {
        // Reasoned about in document_screen.dart's module comment: a poll
        // that comes back about a different job calls no LiveHandlers
        // callback (live_service.dart's _relevant), so SessionService never
        // notifies and this screen never rebuilds for it. What is already on
        // screen -- unchanged -- IS what "no news" looks like, held over the
        // frame the irrelevant poll landed on.
        final env = await startWatch(tester);
        expect(find.text('running'), findsOneWidget);

        env.routeFor['GET ${_base}job'] = _jobBody({
          'id': 999,
          'step': 'a different job entirely',
        });
        await tester.pump(pollInterval);

        expect(env.session.isLive, isTrue);
        expect(find.byKey(const Key('live-watching-banner')), findsOneWidget);
        // Still the pre-poll text -- the irrelevant reply changed nothing.
        expect(find.text('running'), findsOneWidget);
        expect(find.text('a different job entirely'), findsNothing);
        env.session.dispose();
      },
    );

    testWidgets(
      'done: a finished job shows a distinct, dismissible banner, and '
      'dismissing it calls SessionService.dismissNotice',
      (tester) async {
        final env = await startWatch(tester);

        env.routeFor['GET ${_base}job'] = _jobBody({
          'state': 'done',
          'step': '',
        });
        await tester.pump(pollInterval);

        expect(env.session.isLive, isFalse);
        expect(find.byKey(const Key('live-watching-banner')), findsNothing);
        expect(find.byKey(const Key('live-done-banner')), findsOneWidget);
        expect(find.textContaining('done'), findsWidgets);

        await tester.tap(find.byKey(const Key('notice-dismiss')));
        await tester.pump();

        expect(env.session.notice, isNull);
        expect(find.byKey(const Key('live-done-banner')), findsNothing);
        env.session.dispose();
      },
    );

    testWidgets(
      'gave up: abandoning the watch shows a banner distinct from both '
      'progress and done, and is dismissible',
      (tester) async {
        final env = _Env();
        env.routeFor['GET $_base'] = _liveRootBody();
        await env.session.openBackend(_backend);
        await pumpDocument(tester, env.session);

        // A one-second budget (plus live_service.dart's 60s buffer) so a
        // poll past 61s gives up -- mirrors session_test.dart's give-up test.
        env.routeFor['GET ${_base}fire'] = _jobBody({
          'staleAfterSeconds': 1,
        });
        env.routeFor['GET ${_base}job'] = _jobBody({'staleAfterSeconds': 1});
        await tester.tap(find.text('Fire the kiln'));
        await tester.pump();
        expect(env.session.isLive, isTrue);

        // Advance the fake clock (the budget check) and the real/test-zone
        // Timer (the poll itself) together -- see _FakeClock's doc comment.
        env.clock.advance(const Duration(seconds: 70));
        await tester.pump(const Duration(seconds: 70));

        expect(env.session.isLive, isFalse);
        expect(find.byKey(const Key('live-watching-banner')), findsNothing);
        expect(find.byKey(const Key('live-done-banner')), findsNothing);
        expect(find.byKey(const Key('live-giveup-banner')), findsOneWidget);
        expect(find.byKey(const Key('notice-dismiss')), findsOneWidget);

        await tester.tap(find.byKey(const Key('notice-dismiss')));
        await tester.pump();

        expect(env.session.notice, isNull);
        env.session.dispose();
      },
    );

    testWidgets(
      'no notice at all renders nothing -- same as before this task',
      (tester) async {
        final env = _Env();
        await env.session.openBackend(_backend);
        await pumpDocument(tester, env.session);

        expect(env.session.isLive, isFalse);
        expect(env.session.notice, isNull);
        expect(find.byKey(const Key('live-watching-banner')), findsNothing);
        expect(find.byKey(const Key('live-done-banner')), findsNothing);
        expect(find.byKey(const Key('live-giveup-banner')), findsNothing);
        expect(find.byKey(const Key('notice-dismiss')), findsNothing);
        env.session.dispose();
      },
    );
  });
}
