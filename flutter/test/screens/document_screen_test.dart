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
// property (t11), the confirmation sheet / value prompt for an action
// (u11), and live-job progress (v11). Where those matter, a test below only
// checks that SessionService.activate was reached and reacted the way
// session.dart's own tests already pin (e.g. an unsafe action asks for
// confirmation before doing anything) -- not that this screen renders that
// reaction, which it does not yet.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/screens/document_screen.dart';
import 'package:restforge/services/http_service.dart';
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

/// Composes a real [SessionService] over a faked backend -- see the header
/// comment. [routeFor] holds one response body per "METHOD URL" key,
/// mutable so a test can change what the *next* fetch to the same address
/// returns (a refresh finding different data, or starting to fail).
/// [throwing] simulates a connection that never answers at all
/// ([FailureKind.unreachable]) rather than an HTTP error status; [pause]
/// lets one test hold a response open to observe a fetch mid-flight.
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
    session = SessionService(http: HttpService(client: client, log: (_) {}));
  }

  final Map<String, dynamic> routeFor = {'GET $_base': _rootBody()};
  final Map<String, int> statusFor = {};
  final List<String> requestLog = [];
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
}
