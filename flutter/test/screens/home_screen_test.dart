// Widget tests for the backend picker. See AGENTS.md section 5 ("Test
// style"): screens get widget tests via testWidgets/pumpWidget, asserting on
// what's rendered, never on a device feature.
//
// SettingsService here is a real one backed by fakes — a mocked
// shared_preferences and an in-memory SecretStore, the same pattern
// test/services/settings_service_test.dart uses — rather than a hand-rolled
// double for SettingsService itself, so what the picker reads matches what
// the real service would normalise and cap.
//
// This replaces the placeholder-screen test that used to live here: the
// screen is no longer a static "skeleton" page, so "the app starts on the
// backend picker" is now several narrower assertions about what the picker
// does with an empty vs. a populated configuration, rather than one check
// of fixed text.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/screens/document_screen.dart';
import 'package:restforge/screens/home_screen.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/quick_service.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// In-memory [SecretStore] fake — see settings_service_test.dart, the
/// original of this pattern. Duplicated rather than shared because the two
/// test files have no other coupling and this is a handful of lines.
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

/// Records pushes without caring what got pushed. The backend editor
/// (`settings_screen.dart`) is built by a parallel task; asserting through a
/// [NavigatorObserver] proves a route into it exists without this file
/// needing to know its type or constructor.
class _RecordingNavigatorObserver extends NavigatorObserver {
  int pushCount = 0;

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
    pushCount++;
  }
}

void main() {
  late SettingsService settings;
  late _RecordingNavigatorObserver observer;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    settings = SettingsService(secretStore: _InMemorySecretStore());
    observer = _RecordingNavigatorObserver();
  });

  Future<void> pumpHome(WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        navigatorObservers: [observer],
        home: HomeScreen(settingsService: settings),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('an empty configuration shows the empty state', (tester) async {
    await pumpHome(tester);

    expect(find.text('RESTForge'), findsOneWidget);
    expect(find.text('No backends configured'), findsOneWidget);
    expect(find.text('Add a backend'), findsOneWidget);
  });

  testWidgets('a configured backend is listed by name and base URL', (
    tester,
  ) async {
    await settings.saveBackends([
      const Backend(
        name: 'test lab',
        baseUrl: 'https://example.test/',
        secret: 'k',
      ),
    ]);

    await pumpHome(tester);

    expect(find.text('No backends configured'), findsNothing);
    expect(find.text('test lab'), findsOneWidget);
    expect(find.text('https://example.test/'), findsOneWidget);
  });

  testWidgets('several configured backends are all listed', (tester) async {
    await settings.saveBackends([
      const Backend(name: 'alpha', baseUrl: 'https://alpha.test/', secret: 'k'),
      const Backend(name: 'beta', baseUrl: 'https://beta.test/', secret: 'k'),
    ]);

    await pumpHome(tester);

    expect(find.text('alpha'), findsOneWidget);
    expect(find.text('beta'), findsOneWidget);
  });

  testWidgets('the empty state leads to the editor', (tester) async {
    await pumpHome(tester);
    final before = observer.pushCount;

    await tester.tap(find.text('Add a backend'));
    await tester.pump();

    expect(observer.pushCount, before + 1);
  });

  testWidgets('the settings action leads to the editor', (tester) async {
    await pumpHome(tester);
    final before = observer.pushCount;

    await tester.tap(find.byIcon(Icons.settings));
    await tester.pump();

    expect(observer.pushCount, before + 1);
  });

  testWidgets('tapping a configured backend opens it for browsing', (
    tester,
  ) async {
    // Tapping a backend now opens its document, not the editor — see
    // home_screen.dart's _openBackend. The editor stays reachable from the
    // AppBar action and the empty state (asserted above), never from a row
    // tap, so a tap is always "browse this".
    await settings.saveBackends([
      const Backend(
        name: 'test lab',
        baseUrl: 'https://example.test/',
        secret: 'k',
      ),
    ]);
    final client = MockClient((request) async {
      expect(request.method, 'GET');
      expect(request.url.toString(), 'https://example.test/');
      return http.Response(
        jsonEncode({
          'class': ['pantry'],
          'title': 'The pantry',
          'properties': {'kettle': 'cold'},
        }),
        200,
        headers: const {'content-type': 'application/json'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        navigatorObservers: [observer],
        home: HomeScreen(
          settingsService: settings,
          httpService: HttpService(client: client, log: (_) {}),
        ),
      ),
    );
    await tester.pumpAndSettle();
    final before = observer.pushCount;

    await tester.tap(find.text('test lab'));
    await tester.pumpAndSettle();

    expect(observer.pushCount, before + 1);
    expect(find.byType(DocumentScreen), findsOneWidget);
    // A property only the fetched document carries.
    expect(find.text('kettle'), findsOneWidget);

    // Pop so _openBackend's awaited push completes and the session is
    // disposed (NavService's idle timer would otherwise leak).
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
  });

  group('saved shortcuts (x11)', () {
    // Task x11's "list them, run one, remove one" half, at the widget
    // layer. The storage and the runQuick composition underneath are
    // already pinned in quick_service_test.dart and session_test.dart; this
    // proves the opening screen renders the saved rows, hands a run to
    // SessionService.runQuick (navigating on success, reporting a gone
    // backend instead), and removes one with a report -- never silent on
    // any of the three, per this task's own rule.
    //
    // A real [SettingsService] and [QuickService] over the mocked
    // preferences and the in-memory secret store, so what the screen reads
    // matches the real normalise/cap behaviour -- the same pattern the
    // tests above use for the backend list.

    const String base = 'http://bench.example/';

    /// A Siren document the run test fetches when it follows a saved
    /// document shortcut's href. Distinct properties so the assertion can
    /// target what only DocumentScreen would show.
    Map<String, dynamic> catalogueBody() => {
      'class': ['catalogue'],
      'title': 'Catalogue',
      'properties': {'itemCount': 42},
    };

    Future<void> pumpHomeWith(
      WidgetTester tester, {
      required QuickService quick,
      HttpService? http,
    }) async {
      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [observer],
          home: HomeScreen(
            settingsService: settings,
            quickService: quick,
            httpService: http,
          ),
        ),
      );
      await tester.pumpAndSettle();
    }

    testWidgets('a saved shortcut is listed under a Shortcuts header', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(name: 'bench', baseUrl: base, secret: 'k'),
      ]);
      final quick = QuickService(settings: settings);
      await quick.add(
        const QuickItem(
          label: 'Catalogue',
          baseUrl: base,
          kind: QuickKind.document,
          href: '/catalogue',
        ),
      );

      await pumpHomeWith(tester, quick: quick);

      expect(find.text('Shortcuts'), findsOneWidget);
      // The shortcut tile shows its label...
      expect(find.text('Catalogue'), findsOneWidget);
      // ...and the backend's *current* name as the subtitle, resolved by
      // base URL -- scoped to the shortcut tile so the backend list's own
      // 'bench' row (which is also legitimately present) does not muddle
      // the assertion.
      final shortcutTile = find.ancestor(
        of: find.text('Catalogue'),
        matching: find.byType(ListTile),
      );
      expect(
        find.descendant(of: shortcutTile, matching: find.text('bench')),
        findsOneWidget,
      );
    });

    testWidgets(
      'a shortcut whose backend has been removed is shown as gone, not '
      'dropped',
      (tester) async {
        // Kept and marked, not silently dropped -- mirrors quick.js's rows()
        // and quick_service.dart's module comment: something the user saved
        // going missing without explanation is worse than a row that says so.
        final quick = QuickService(settings: settings);
        await quick.add(
          const QuickItem(
            label: 'Orphan',
            baseUrl: base,
            kind: QuickKind.document,
            href: '/catalogue',
          ),
        );
        // No backend configured for `base`.

        await pumpHomeWith(tester, quick: quick);

        expect(find.text('Shortcuts'), findsOneWidget);
        expect(find.text('Orphan'), findsOneWidget);
        expect(find.text('Backend removed'), findsOneWidget);
      },
    );

    testWidgets(
      'running a document shortcut fetches its href and opens the document',
      (tester) async {
        await settings.saveBackends([
          const Backend(name: 'bench', baseUrl: base, secret: 'k'),
        ]);
        final quick = QuickService(settings: settings);
        await quick.add(
          const QuickItem(
            label: 'Catalogue',
            baseUrl: base,
            kind: QuickKind.document,
            href: '/catalogue',
          ),
        );

        final client = MockClient((request) async {
          expect(request.method, 'GET');
          expect(request.url.toString(), '${base}catalogue');
          return http.Response(
            jsonEncode(catalogueBody()),
            200,
            headers: const {'content-type': 'application/json'},
          );
        });
        final httpService = HttpService(client: client, log: (_) {});

        await pumpHomeWith(tester, quick: quick, http: httpService);
        final before = observer.pushCount;

        await tester.tap(find.text('Catalogue'));
        await tester.pumpAndSettle();

        // runQuick opened the backend, fetched the href, and the screen
        // pushed DocumentScreen to render it.
        expect(observer.pushCount, before + 1);
        expect(find.byType(DocumentScreen), findsOneWidget);
        // A property only the fetched document carries -- the shortcut tile
        // behind it does not.
        expect(find.text('itemCount'), findsOneWidget);

        // Pop the document so _runShortcut's awaited push completes and the
        // session it built is disposed -- otherwise NavService's idle-refresh
        // timer stays pending and the test framework rejects the leak.
        await tester.tap(find.byType(BackButton));
        await tester.pumpAndSettle();
      },
    );

    testWidgets(
      'running a shortcut whose backend is gone reports it and does not '
      'navigate',
      (tester) async {
        final quick = QuickService(settings: settings);
        await quick.add(
          const QuickItem(
            label: 'Orphan',
            baseUrl: base,
            kind: QuickKind.document,
            href: '/catalogue',
          ),
        );
        // No backend for `base`, and an HTTP client that fails the test if
        // anything is fetched -- runQuick must report before fetching.
        final client = MockClient((request) async {
          fail('runQuick should not fetch when the backend is missing');
        });
        final httpService = HttpService(client: client, log: (_) {});

        await pumpHomeWith(tester, quick: quick, http: httpService);
        final before = observer.pushCount;

        await tester.tap(find.text('Orphan'));
        await tester.pumpAndSettle();

        expect(observer.pushCount, before);
        expect(find.byType(DocumentScreen), findsNothing);
        expect(find.textContaining('no longer configured'), findsOneWidget);
      },
    );

    testWidgets('the delete button removes a shortcut and reports it', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(name: 'bench', baseUrl: base, secret: 'k'),
      ]);
      final quick = QuickService(settings: settings);
      await quick.add(
        const QuickItem(
          label: 'Catalogue',
          baseUrl: base,
          kind: QuickKind.document,
          href: '/catalogue',
        ),
      );
      await pumpHomeWith(tester, quick: quick);

      await tester.tap(find.byIcon(Icons.delete_outline));
      await tester.pumpAndSettle();

      expect(find.text('Removed shortcut'), findsOneWidget);
      // The row is gone from the listing.
      expect(find.text('Catalogue'), findsNothing);
      expect(await quick.count(), 0);
    });

    testWidgets('removing the last shortcut hides the Shortcuts section', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(name: 'bench', baseUrl: base, secret: 'k'),
      ]);
      final quick = QuickService(settings: settings);
      await quick.add(
        const QuickItem(
          label: 'Catalogue',
          baseUrl: base,
          kind: QuickKind.document,
          href: '/catalogue',
        ),
      );
      await pumpHomeWith(tester, quick: quick);

      await tester.tap(find.byIcon(Icons.delete_outline));
      await tester.pumpAndSettle();

      expect(find.text('Shortcuts'), findsNothing);
    });
  });
}
