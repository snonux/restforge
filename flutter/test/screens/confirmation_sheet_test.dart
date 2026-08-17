// Widget tests for the confirmation sheet: Confirm, Cancel, dismissing
// without answering, the value-prompt path, and the ConfirmQuestion ->
// ValueQuestion transition that must keep the same sheet open rather than
// closing and reopening it. See AGENTS.md section 5 ("Test style"): screens
// get widget tests via testWidgets/pumpWidget, asserting on what's
// rendered, never on a device feature. See confirmation_sheet.dart's module
// comment for the invariants these tests are pinning: "a confirmation that
// does not say which press means yes is not a confirmation" and
// "dismissing without answering must still cancel the pending action".
//
// SessionService is composed over a real HttpService backed by
// package:http's MockClient, mirroring document_screen_test.dart's _Env --
// a route table keyed by "METHOD URL". [ConfirmationSheetHost] is exercised
// directly, driving it through [SessionService.activate] the same way a row
// press on document_screen.dart would, rather than through that whole
// screen -- this file's own scope is the sheet, not the row list around it.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/screens/confirmation_sheet.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/render_service.dart' as render;
import 'package:restforge/services/session.dart';
import 'package:restforge/services/settings_service.dart';

const String _base = 'http://pantry.example/';

final Backend _backend = Backend(name: 'pantry', baseUrl: _base, secret: 'k');

/// The fixture root document: one action with no fields ("sweep", for the
/// plain confirm/cancel/dismiss cases) and one with a required field that
/// has no default and no checkbox to stand in for it ("label", for the
/// value-prompt case and the same-sheet ConfirmQuestion -> ValueQuestion
/// transition -- mirrors action_service_test.dart's "a required field with
/// no default and no checkbox is asked for out loud").
Map<String, dynamic> _rootBody() => {
  'class': ['pantry'],
  'title': 'The pantry',
  'properties': {'kettle': 'cold'},
  'actions': [
    {
      'name': 'sweep',
      'title': 'Sweep the floor',
      'method': 'POST',
      'href': '/sweep',
      'fields': [],
    },
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

const Map<String, String> _jsonHeaders = {'content-type': 'application/json'};

/// Composes a real [SessionService] over a faked backend -- see the header
/// comment. [routeFor] holds one response body per "METHOD URL" key.
class _Env {
  _Env() {
    final client = MockClient((request) async {
      final key = '${request.method} ${request.url}';
      requestLog.add(key);
      final body = routeFor[key];
      return http.Response(
        body == null ? '{"properties":{}}' : jsonEncode(body),
        200,
        headers: _jsonHeaders,
      );
    });
    session = SessionService(
      http: HttpService(client: client, log: (_) {}),
    );
  }

  final Map<String, dynamic> routeFor = {
    'GET $_base': _rootBody(),
    'POST ${_base}sweep': const {
      'properties': {'state': 'done'},
    },
    'POST ${_base}label': const {
      'properties': {'state': 'done'},
    },
  };
  final List<String> requestLog = [];
  late final SessionService session;
}

class _RecordingObserver extends NavigatorObserver {
  int pushCount = 0;
  int popCount = 0;

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
    pushCount++;
  }

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    popCount++;
  }
}

void main() {
  /// Pumps a minimal host: [ConfirmationSheetHost] wrapping an otherwise
  /// empty screen, exactly as document_screen.dart wraps its own build
  /// output -- see that file's module comment.
  Future<_RecordingObserver> pumpHost(
    WidgetTester tester,
    SessionService session,
  ) async {
    final observer = _RecordingObserver();
    await tester.pumpWidget(
      MaterialApp(
        navigatorObservers: [observer],
        home: ConfirmationSheetHost(
          session: session,
          child: const Scaffold(body: SizedBox.shrink()),
        ),
      ),
    );
    await tester.pump();
    return observer;
  }

  testWidgets('Confirm resolves a ConfirmQuestion and closes the sheet', (
    tester,
  ) async {
    final env = _Env();
    await env.session.openBackend(_backend);
    await pumpHost(tester, env.session);

    await env.session.activate(const render.ActionTarget('sweep'));
    await tester.pumpAndSettle();

    expect(env.session.question, isA<ConfirmQuestion>());
    expect(find.byKey(const Key('confirmation-sheet-heading')), findsOneWidget);
    expect(find.text('Sweep the floor'), findsOneWidget);

    await tester.tap(find.byKey(const Key('confirmation-sheet-confirm')));
    await tester.pumpAndSettle();

    expect(env.session.question, isNull);
    expect(
      find.byKey(const Key('confirmation-sheet-heading')),
      findsNothing,
      reason: 'a resolved question leaves no sheet on screen',
    );
    expect(env.requestLog, contains('POST ${_base}sweep'));
    env.session.dispose();
  });

  testWidgets('Cancel explicitly declines and closes the sheet', (
    tester,
  ) async {
    final env = _Env();
    await env.session.openBackend(_backend);
    await pumpHost(tester, env.session);

    await env.session.activate(const render.ActionTarget('sweep'));
    await tester.pumpAndSettle();
    expect(env.session.question, isA<ConfirmQuestion>());

    await tester.tap(find.byKey(const Key('confirmation-sheet-cancel')));
    await tester.pumpAndSettle();

    expect(
      env.session.question,
      isNull,
      reason: 'Cancel is an explicit decline, not just a dismissal',
    );
    expect(
      env.requestLog,
      isNot(contains('POST ${_base}sweep')),
      reason: 'a declined confirmation must never be sent',
    );
    env.session.dispose();
  });

  testWidgets(
    'dismissing without answering (tap outside) still cancels the pending action',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpHost(tester, env.session);

      await env.session.activate(const render.ActionTarget('sweep'));
      await tester.pump();
      expect(env.session.question, isA<ConfirmQuestion>());

      // Tap the modal barrier well above the sheet itself -- one of the
      // three ways Flutter reports "closed with nothing chosen" (see
      // confirmation_sheet.dart's module comment).
      await tester.tapAt(const Offset(20, 20));
      await tester.pumpAndSettle();

      expect(
        env.session.question,
        isNull,
        reason:
            'ConfirmationSheetHost must answer(false) when the sheet closes '
            'with the question still pending, or the action is left '
            'dangling and the idle-refresh clock never resumes',
      );
      expect(
        env.requestLog,
        isNot(contains('POST ${_base}sweep')),
        reason: 'a dismissed confirmation must never be sent',
      );
      env.session.dispose();
    },
  );

  testWidgets(
    'a required field with no default shows the value prompt, and typing '
    '+ Send calls answerValue',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      await pumpHost(tester, env.session);

      await env.session.activate(const render.ActionTarget('label'));
      await tester.pumpAndSettle();
      expect(env.session.question, isA<ConfirmQuestion>());

      await tester.tap(find.byKey(const Key('confirmation-sheet-confirm')));
      await tester.pumpAndSettle();

      expect(env.session.question, isA<ValueQuestion>());
      expect(
        find.byKey(const Key('confirmation-sheet-value-field')),
        findsOneWidget,
      );
      expect(find.text('What should the jar say?'), findsOneWidget);

      await tester.enterText(
        find.byKey(const Key('confirmation-sheet-value-field')),
        'plum jam',
      );
      await tester.tap(find.byKey(const Key('confirmation-sheet-confirm')));
      await tester.pumpAndSettle();

      expect(env.session.question, isNull);
      expect(env.requestLog, contains('POST ${_base}label'));
      env.session.dispose();
    },
  );

  testWidgets(
    'a ConfirmQuestion resolving into a ValueQuestion keeps the same sheet '
    'open rather than closing and reopening it',
    (tester) async {
      final env = _Env();
      await env.session.openBackend(_backend);
      final observer = await pumpHost(tester, env.session);

      await env.session.activate(const render.ActionTarget('label'));
      await tester.pumpAndSettle();
      expect(env.session.question, isA<ConfirmQuestion>());

      final pushesAfterOpening = observer.pushCount;
      expect(observer.popCount, 0);

      await tester.tap(find.byKey(const Key('confirmation-sheet-confirm')));
      await tester.pumpAndSettle();

      // The question moved from ConfirmQuestion to ValueQuestion, but
      // ConfirmationSheetHost's _showing guard must treat this as the same
      // sheet -- no extra route pushed, and nothing popped in between.
      expect(env.session.question, isA<ValueQuestion>());
      expect(observer.pushCount, pushesAfterOpening);
      expect(observer.popCount, 0);

      await tester.tap(find.byKey(const Key('confirmation-sheet-cancel')));
      await tester.pumpAndSettle();

      // Only now -- once the ValueQuestion itself is dismissed -- does the
      // one sheet route finally pop.
      expect(observer.popCount, 1);
      env.session.dispose();
    },
  );
}
