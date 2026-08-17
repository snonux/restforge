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

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/screens/home_screen.dart';
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

  testWidgets('tapping a configured backend leads to the editor', (
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
    final before = observer.pushCount;

    await tester.tap(find.text('test lab'));
    await tester.pump();

    expect(observer.pushCount, before + 1);
  });
}
