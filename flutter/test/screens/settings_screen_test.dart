// Widget tests, mirroring pebble/tools/test-configpage.js: add and validate,
// reorder keeps unsaved edits, delete removes the right row, base-URL rules,
// cancel leaves storage alone. See AGENTS.md section 5 ("Test style") --
// screens get widget tests, never a mock that needs a device.
//
// Validation wording and bounds are asserted here only to confirm the screen
// *displays* what SettingsService.validate/normalise says -- the rules
// themselves are covered in test/services/settings_service_test.dart and are
// not re-derived here.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/screens/settings_screen.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// In-memory [SecretStore] fake -- same trivial shape as the one in
/// test/services/settings_service_test.dart, kept local rather than shared
/// so this file stays self-contained (see that file's header for why the
/// real plugin can't be used in `flutter test`).
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
  TestWidgetsFlutterBinding.ensureInitialized();

  late _InMemorySecretStore secrets;
  late SettingsService settings;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    secrets = _InMemorySecretStore();
    settings = SettingsService(secretStore: secrets);
  });

  // Each card carries five labelled fields plus their helper text, so two of
  // them together are taller than the default 800x600 test surface -- a real
  // phone would scroll, but a widget test's tap/enterText need their target
  // actually laid out. Growing the surface avoids interacting with an
  // off-screen (and therefore un-hit-testable) row instead of relying on
  // scrollUntilVisible before every interaction below.
  Future<void> pumpScreen(
    WidgetTester tester, {
    SettingsService? service,
  }) async {
    tester.view.physicalSize = const Size(1200, 3600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(home: SettingsScreen(settingsService: service ?? settings)),
    );
    await tester.pumpAndSettle();
  }

  group('empty state and add', () {
    testWidgets('empty state is stated, not blank', (tester) async {
      await pumpScreen(tester);
      expect(find.text('No backends yet.'), findsOneWidget);
    });

    testWidgets('add creates a row with the auth header defaulted', (
      tester,
    ) async {
      await pumpScreen(tester);
      await tester.tap(find.byKey(const Key('add')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('name-0')), findsOneWidget);
      final authHeader = tester.widget<TextField>(
        find.byKey(const Key('authHeader-0')),
      );
      expect(authHeader.controller!.text, SettingsService.defaultAuthHeader);
    });

    testWidgets('add is disabled once the backend cap is reached', (
      tester,
    ) async {
      await settings.saveBackends([
        for (var i = 0; i < SettingsService.maxBackends; i++)
          Backend(name: 'b$i', baseUrl: 'https://h/$i/', secret: 's'),
      ]);
      await pumpScreen(tester);

      final add = tester.widget<OutlinedButton>(find.byKey(const Key('add')));
      expect(add.onPressed, isNull);
    });
  });

  group('save refuses invalid input', () {
    testWidgets('an empty name is refused, and nothing is saved', (
      tester,
    ) async {
      await pumpScreen(tester);
      await tester.tap(find.byKey(const Key('add')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('save')));
      await tester.pumpAndSettle();

      expect(find.textContaining('Name is required'), findsOneWidget);
      expect(await settings.count(), 0);
    });

    testWidgets('a relative base URL is refused', (tester) async {
      await pumpScreen(tester);
      await tester.tap(find.byKey(const Key('add')));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('name-0')), 'homelab');
      await tester.enterText(
        find.byKey(const Key('baseUrl-0')),
        '/cgi-bin/app/',
      );
      await tester.enterText(find.byKey(const Key('secret-0')), 'sekrit');
      await tester.tap(find.byKey(const Key('save')));
      await tester.pumpAndSettle();

      expect(find.textContaining('absolute'), findsOneWidget);
      expect(await settings.count(), 0);
    });

    testWidgets(
      'a missing trailing slash is accepted -- SettingsService adds it',
      (tester) async {
        await pumpScreen(tester);
        await tester.tap(find.byKey(const Key('add')));
        await tester.pumpAndSettle();

        await tester.enterText(find.byKey(const Key('name-0')), 'homelab');
        await tester.enterText(
          find.byKey(const Key('baseUrl-0')),
          'https://host/cgi-bin/app',
        );
        await tester.enterText(find.byKey(const Key('secret-0')), 'sekrit');
        await tester.tap(find.byKey(const Key('save')));
        await tester.pumpAndSettle();

        expect(find.byKey(const Key('error')), findsNothing);
        final saved = await settings.getBackend(0);
        expect(saved?.baseUrl, 'https://host/cgi-bin/app/');
      },
    );
  });

  group('the secret is never pre-filled', () {
    testWidgets('a loaded secret does not appear in the secret field', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(
          name: 'homelab',
          baseUrl: 'https://h/',
          secret: 'existing-secret',
        ),
      ]);
      await pumpScreen(tester);

      final secretField = tester.widget<TextField>(
        find.byKey(const Key('secret-0')),
      );
      expect(secretField.controller!.text, isEmpty);
      expect(secretField.obscureText, isTrue);
    });

    testWidgets(
      'leaving the secret field blank on save keeps the existing secret',
      (tester) async {
        await settings.saveBackends([
          const Backend(
            name: 'homelab',
            baseUrl: 'https://h/',
            secret: 'existing-secret',
          ),
        ]);
        await pumpScreen(tester);

        // Touch an unrelated field so the save path is exercised, but never
        // type into the secret field.
        await tester.enterText(find.byKey(const Key('startRel-0')), 'root');
        await tester.tap(find.byKey(const Key('save')));
        await tester.pumpAndSettle();

        final saved = await settings.getBackend(0);
        expect(saved?.secret, 'existing-secret');
      },
    );

    testWidgets('typing into the secret field replaces the existing secret', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(
          name: 'homelab',
          baseUrl: 'https://h/',
          secret: 'existing-secret',
        ),
      ]);
      await pumpScreen(tester);

      await tester.enterText(
        find.byKey(const Key('secret-0')),
        'replaced-secret',
      );
      await tester.tap(find.byKey(const Key('save')));
      await tester.pumpAndSettle();

      final saved = await settings.getBackend(0);
      expect(saved?.secret, 'replaced-secret');
    });
  });

  group('structural edits keep unsaved input', () {
    testWidgets('reorder moves the card and keeps the untouched one', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(name: 'one', baseUrl: 'https://host/1/', secret: 'a'),
        const Backend(name: 'two', baseUrl: 'https://host/2/', secret: 'b'),
      ]);
      await pumpScreen(tester);

      await tester.enterText(find.byKey(const Key('name-1')), 'two edited');
      await tester.tap(find.byKey(const Key('up-1')));
      await tester.pumpAndSettle();

      final first = tester.widget<TextField>(find.byKey(const Key('name-0')));
      final second = tester.widget<TextField>(find.byKey(const Key('name-1')));
      expect(first.controller!.text, 'two edited');
      expect(second.controller!.text, 'one');
    });

    testWidgets('delete removes the right row', (tester) async {
      await settings.saveBackends([
        const Backend(name: 'one', baseUrl: 'https://host/1/', secret: 'a'),
        const Backend(name: 'two', baseUrl: 'https://host/2/', secret: 'b'),
      ]);
      await pumpScreen(tester);

      await tester.tap(find.byKey(const Key('delete-0')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('name-1')), findsNothing);
      final remaining = tester.widget<TextField>(
        find.byKey(const Key('name-0')),
      );
      expect(remaining.controller!.text, 'two');
    });
  });

  group('delete deletes the secret, once saved', () {
    testWidgets('saving after a delete orphans the removed secret', (
      tester,
    ) async {
      await settings.saveBackends([
        const Backend(
          name: 'one',
          baseUrl: 'https://host/1/',
          secret: 'one-secret',
        ),
        const Backend(
          name: 'two',
          baseUrl: 'https://host/2/',
          secret: 'two-secret',
        ),
      ]);
      await pumpScreen(tester);
      expect(secrets.data.values, containsAll(['one-secret', 'two-secret']));

      await tester.tap(find.byKey(const Key('delete-0')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('save')));
      await tester.pumpAndSettle();

      expect(secrets.data.values, isNot(contains('one-secret')));
      expect(secrets.data.values, contains('two-secret'));
    });
  });

  group('cancel', () {
    testWidgets('cancel leaves storage alone', (tester) async {
      await settings.saveBackends([
        const Backend(name: 'one', baseUrl: 'https://host/1/', secret: 'a'),
      ]);
      await pumpScreen(tester);

      await tester.enterText(
        find.byKey(const Key('name-0')),
        'edited but not saved',
      );
      await tester.tap(find.byKey(const Key('cancel')));
      await tester.pumpAndSettle();

      final stored = await settings.getBackend(0);
      expect(stored?.name, 'one');
    });
  });

  group('a platform failure while saving is shown, not thrown', () {
    testWidgets('an Err from saveBackends is displayed', (tester) async {
      final failing = SettingsService(secretStore: _FailingSecretStore());
      await pumpScreen(tester, service: failing);

      await tester.tap(find.byKey(const Key('add')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('name-0')), 'homelab');
      await tester.enterText(
        find.byKey(const Key('baseUrl-0')),
        'https://host/x/',
      );
      await tester.enterText(find.byKey(const Key('secret-0')), 'sekrit');
      await tester.tap(find.byKey(const Key('save')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('error')), findsOneWidget);
    });
  });
}

/// A [SecretStore] that fails every write -- see
/// test/services/settings_service_test.dart's FailingSecretStore, whose
/// shape this mirrors, to exercise saveBackends' [Err] path without a device.
class _FailingSecretStore implements SecretStore {
  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> write(String key, String value) async =>
      throw Exception('write failed');

  @override
  Future<void> delete(String key) async {}
}
