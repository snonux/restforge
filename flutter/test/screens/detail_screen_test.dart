// Widget tests for the full-screen reading view (task t11) — the port of
// pebble/src/c/win_detail.c. See AGENTS.md section 5 ("Test style"):
// screens get widget tests via testWidgets/pumpWidget, asserting on what's
// rendered, never on a device feature.
//
// SessionService is composed over a real HttpService backed by
// package:http's MockClient, the same self-contained fixture shape
// test/screens/document_screen_test.dart uses — a route table keyed by
// "METHOD URL". The point is to prove the reading view renders the whole
// value (wrapping, scrollable, selectable) and dismisses back to the
// document, not to re-prove session.dart's detail dispatch (already pinned
// in session_test.dart and document_screen_test.dart).

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/screens/detail_screen.dart';
import 'package:restforge/screens/document_screen.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/live_service.dart';
import 'package:restforge/services/nav_service.dart';
import 'package:restforge/services/session.dart';
import 'package:restforge/services/settings_service.dart';

const String _base = 'http://workshop.example/';

final Backend _backend = Backend(name: 'workshop', baseUrl: _base, secret: 'k');

/// A property whose value is long enough that it would not fit on one row
/// and must wrap, and long enough that the SingleChildScrollView has
/// something to scroll past the bottom of the screen — the case the reading
/// view exists for.
const String _longNote =
    'A long note that would not fit on one row and must wrap and scroll so '
    'the whole value is legible rather than silently cut off. It keeps going '
    'to ensure the reading view has something to scroll past the bottom of '
    'the screen, because "nothing is silently cut off" is the property this '
    'view exists to hold.';

Map<String, dynamic> _rootBody() => {
  'class': ['workshop'],
  'title': 'The workshop',
  'properties': {'status': 'idle', 'notes': _longNote},
};

/// Composes a real [SessionService] over a faked backend — see the header
/// comment. One route: the root document.
class _Env {
  _Env() {
    final client = MockClient((request) async {
      return http.Response(
        jsonEncode(_rootBody()),
        200,
        headers: const {'content-type': 'application/json'},
      );
    });
    final httpService = HttpService(client: client, log: (_) {});
    final live = LiveService(http: httpService, log: (_) {});
    session = SessionService(http: httpService, live: live);
  }

  late final SessionService session;
}

Future<void> _openDocument(WidgetTester tester, SessionService session) async {
  await session.openBackend(_backend);
  await tester.pumpWidget(MaterialApp(home: DocumentScreen(session: session)));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'tapping a property opens the reading view with its heading and full '
    'body',
    (tester) async {
      final env = _Env();
      await _openDocument(tester, env.session);

      await tester.tap(find.text('status'));
      await tester.pumpAndSettle();

      // The reading route is on top, titled with the property's name and
      // showing its full value. Scoped to DetailScreen so the property row
      // underneath (same label/sublabel) does not muddle the assertion.
      expect(find.byType(DetailScreen), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(DetailScreen),
          matching: find.text('status'),
        ),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byType(DetailScreen),
          matching: find.text('idle'),
        ),
        findsOneWidget,
      );
      env.session.dispose();
    },
  );

  testWidgets('the body is selectable so a value can be copied out', (
    tester,
  ) async {
    final env = _Env();
    await _openDocument(tester, env.session);

    await tester.tap(find.text('status'));
    await tester.pumpAndSettle();

    // SelectableText, not plain Text: a value like a URL or an id can be
    // copied rather than read off the screen.
    expect(find.byType(SelectableText), findsOneWidget);
    expect(
      find.descendant(
        of: find.byType(SelectableText),
        matching: find.text('idle'),
      ),
      findsOneWidget,
    );
    env.session.dispose();
  });

  testWidgets('a long value is shown in full, not truncated to an ellipsis', (
    tester,
  ) async {
    final env = _Env();
    await _openDocument(tester, env.session);

    await tester.tap(find.text('notes'));
    await tester.pumpAndSettle();

    // The whole value is present in the SelectableText — nothing was
    // silently cut off, which is the property win_detail.c existed to
    // hold on the watch and this view holds here.
    expect(find.textContaining(_longNote), findsOneWidget);
    // And it lives inside a scroll view, so overflow past the screen
    // scrolls rather than clips.
    expect(
      find.ancestor(
        of: find.byType(SelectableText),
        matching: find.byType(SingleChildScrollView),
      ),
      findsOneWidget,
    );
    env.session.dispose();
  });

  testWidgets('the reading view does not replace the document underneath it', (
    tester,
  ) async {
    // pebble/docs/DESIGN.md is silent on this one because the watch's
    // detail window was its own window, but the invariant the reading
    // view inherits is the same as a failure's: opening a value must not
    // throw away the document it came from, since the reader will go back
    // to it.
    final env = _Env();
    await _openDocument(tester, env.session);
    final beforeDoc = env.session.document?.title;

    await tester.tap(find.text('notes'));
    await tester.pumpAndSettle();

    // The document is still the workshop underneath the reading route.
    expect(env.session.document?.title, beforeDoc);
    expect(env.session.state, DocumentState.ok);
    env.session.dispose();
  });

  testWidgets('back dismisses the reading view and clears the detail', (
    tester,
  ) async {
    final env = _Env();
    await _openDocument(tester, env.session);

    await tester.tap(find.text('status'));
    await tester.pumpAndSettle();
    expect(env.session.detail, isNotNull);

    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();

    expect(env.session.detail, isNull);
    // Back under the reading route is the document, not a blank screen.
    expect(find.text('The workshop'), findsWidgets);
    env.session.dispose();
  });

  testWidgets('the close action dismisses too', (tester) async {
    final env = _Env();
    await _openDocument(tester, env.session);

    await tester.tap(find.text('status'));
    await tester.pumpAndSettle();

    await tester.tap(find.byType(CloseButton));
    await tester.pumpAndSettle();

    expect(env.session.detail, isNull);
    env.session.dispose();
  });
}
