// End to end through the app's own screens: create every kind of setting the
// way a user does (the backend editor for backends, a long-press in a
// document for both kinds of shortcut), export from the home screen, wipe
// storage the way an uninstall does, import on a fresh home screen, and check
// that everything came back — down to the secret actually being sent in the
// restored auth header.
//
// Only two things are faked, both at the edge: the platform file dialog
// (BackupFiles, which needs a platform channel `flutter test` does not have)
// and the network (a MockClient behind HttpService). Storage is the same
// mocked shared_preferences plus in-memory SecretStore every other test uses.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/screens/document_screen.dart';
import 'package:restforge/screens/home_screen.dart';
import 'package:restforge/screens/settings_screen.dart';
import 'package:restforge/services/backup_files.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/quick_service.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

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

/// Stands in for the platform file dialog: [save] keeps what it was given,
/// [open] hands back [toOpen].
class _FakeBackupFiles implements BackupFiles {
  String? savedName;
  String? saved;
  int saveCalls = 0;
  int openCalls = 0;
  bool cancelSave = false;
  Result<String?> toOpen = const Ok(null);

  @override
  Future<Result<String?>> save(String fileName, String contents) async {
    saveCalls++;
    if (cancelSave) {
      return const Ok(null);
    }
    savedName = fileName;
    saved = contents;
    return Ok('/storage/emulated/0/Download/$fileName');
  }

  @override
  Future<Result<String?>> open() async {
    openCalls++;
    return toOpen;
  }
}

const _alphaBase = 'https://alpha.test/v1/';
const _betaBase = 'https://beta.test/api/';

const Map<String, String> _jsonHeaders = {'content-type': 'application/json'};

/// Beta's root: one link and one unsafe action, so both kinds of shortcut
/// can be saved from it by a long-press.
Map<String, dynamic> _betaRoot() => {
  'class': ['bench'],
  'title': 'The bench',
  'properties': {'status': 'idle'},
  'links': [
    {
      'rel': ['catalogue'],
      'href': '/api/catalogue',
      'title': 'Catalogue',
    },
  ],
  'actions': [
    {
      'name': 'sweep',
      'title': 'Sweep the floor',
      'method': 'POST',
      'href': '/api/sweep',
      'fields': [],
    },
  ],
};

void main() {
  late List<http.Request> requests;
  late HttpService httpService;
  late _FakeBackupFiles files;

  setUp(() {
    requests = [];
    files = _FakeBackupFiles();
    httpService = HttpService(
      client: MockClient((request) async {
        requests.add(request);
        return http.Response(
          jsonEncode(
            request.url.toString() == _betaBase
                ? _betaRoot()
                : {'properties': {}},
          ),
          200,
          headers: _jsonHeaders,
        );
      }),
      log: (_) {},
    );
  });

  /// A fresh install: nothing in shared_preferences, nothing in secure
  /// storage, and a new home screen reading from both.
  Future<({SettingsService settings, _InMemorySecretStore secrets})> install(
    WidgetTester tester,
    int generation,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final secrets = _InMemorySecretStore();
    final settings = SettingsService(secretStore: secrets);
    await tester.pumpWidget(
      MaterialApp(
        key: ValueKey('app-$generation'),
        home: HomeScreen(
          settingsService: settings,
          httpService: httpService,
          backupFiles: files,
        ),
      ),
    );
    await tester.pumpAndSettle();
    return (settings: settings, secrets: secrets);
  }

  void bigSurface(WidgetTester tester) {
    // Two backend cards are taller than the default 800x600 surface; see
    // settings_screen_test.dart for the same reasoning.
    tester.view.physicalSize = const Size(1200, 3600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
  }

  Future<void> fillBackend(
    WidgetTester tester,
    int index, {
    required String name,
    required String baseUrl,
    required String authHeader,
    required String secret,
    required String startRel,
  }) async {
    await tester.tap(find.byKey(const Key('add')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(ValueKey('name-$index')), name);
    await tester.enterText(find.byKey(ValueKey('baseUrl-$index')), baseUrl);
    await tester.enterText(
      find.byKey(ValueKey('authHeader-$index')),
      authHeader,
    );
    await tester.enterText(find.byKey(ValueKey('secret-$index')), secret);
    await tester.enterText(find.byKey(ValueKey('startRel-$index')), startRel);
  }

  Future<void> clearSnackBars(WidgetTester tester) async {
    tester
        .state<ScaffoldMessengerState>(find.byType(ScaffoldMessenger))
        .clearSnackBars();
    await tester.pumpAndSettle();
  }

  Future<void> openMenu(WidgetTester tester, String entry) async {
    await tester.tap(find.byKey(const Key('settings-menu')));
    await tester.pumpAndSettle();
    await tester.tap(find.text(entry));
    await tester.pumpAndSettle();
  }

  testWidgets('every setting survives export, a wiped install and import', (
    tester,
  ) async {
    bigSurface(tester);
    final first = await install(tester, 1);
    expect(find.text('No backends configured'), findsOneWidget);

    // Backends, through the editor, every field at a non-default value.
    await tester.tap(find.byTooltip('Backends'));
    await tester.pumpAndSettle();
    expect(find.byType(SettingsScreen), findsOneWidget);
    await fillBackend(
      tester,
      0,
      name: 'Alpha',
      baseUrl: 'https://alpha.test/v1',
      authHeader: 'Authorization',
      secret: 'Bearer alpha-secret',
      startRel: 'dashboard',
    );
    await fillBackend(
      tester,
      1,
      name: 'Beta',
      baseUrl: _betaBase,
      authHeader: 'X-Token',
      secret: 'beta-secret',
      startRel: '',
    );
    await tester.tap(find.byKey(const Key('save')));
    await tester.pumpAndSettle();
    expect(find.text('Alpha'), findsOneWidget);
    expect(find.text('Beta'), findsOneWidget);

    // Both kinds of shortcut, through a long-press in a document.
    await tester.tap(find.text('Beta'));
    await tester.pumpAndSettle();
    expect(find.byType(DocumentScreen), findsOneWidget);
    await tester.longPress(find.text('Catalogue'));
    await tester.pumpAndSettle();
    await tester.longPress(find.text('Sweep the floor'));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
    expect(find.text('Shortcuts'), findsOneWidget);
    // The two "Saved as a shortcut" snackbars would otherwise queue ahead of
    // the export's own report.
    await clearSnackBars(tester);

    final backendsBefore = await first.settings.loadBackends();
    final shortcutsBefore = await QuickService(settings: first.settings).load();
    expect(backendsBefore, [
      const Backend(
        name: 'Alpha',
        baseUrl: _alphaBase,
        authHeader: 'Authorization',
        secret: 'Bearer alpha-secret',
        startRel: 'dashboard',
      ),
      const Backend(
        name: 'Beta',
        baseUrl: _betaBase,
        authHeader: 'X-Token',
        secret: 'beta-secret',
      ),
    ]);
    expect(shortcutsBefore.map((s) => s.kind), [
      QuickKind.document,
      QuickKind.action,
    ]);

    // Export: warned about secrets first, then written.
    await openMenu(tester, 'Export settings');
    expect(find.textContaining('in plain text'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-export')));
    await tester.pumpAndSettle();
    expect(find.text('Exported 2 backends and 2 shortcuts'), findsOneWidget);
    expect(files.savedName, startsWith('restforge-settings-'));
    expect(files.saved, contains('beta-secret'));
    expect(files.saved, contains('Bearer alpha-secret'));
    final exported = files.saved!;

    // The uninstall.
    final second = await install(tester, 2);
    expect(find.text('No backends configured'), findsOneWidget);
    expect(await second.settings.loadBackends(), isEmpty);
    expect(second.secrets.data, isEmpty);

    // Import: asked first, naming what goes and what comes.
    files.toOpen = Ok(exported);
    await openMenu(tester, 'Import settings');
    expect(find.text('Replace all settings?'), findsOneWidget);
    expect(
      find.textContaining('Your 0 backends and 0 shortcuts will be replaced '),
      findsOneWidget,
    );
    expect(
      find.textContaining('the 2 backends and 2 shortcuts'),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('confirm-import')));
    await tester.pumpAndSettle();

    // Reflected on screen straight away...
    expect(find.text('Imported 2 backends and 2 shortcuts'), findsOneWidget);
    expect(find.text('Alpha'), findsOneWidget);
    expect(find.text('Beta'), findsNWidgets(3)); // backend + 2 shortcut rows
    expect(find.text('Catalogue'), findsOneWidget);
    expect(find.text('Sweep the floor'), findsOneWidget);
    expect(find.text('No backends configured'), findsNothing);

    // ...identical in storage, secrets included...
    expect(await second.settings.loadBackends(), backendsBefore);
    expect(
      await QuickService(settings: second.settings).load(),
      shortcutsBefore,
    );
    expect(second.secrets.data.values.toSet(), {
      'Bearer alpha-secret',
      'beta-secret',
    });

    // ...in the editor...
    await clearSnackBars(tester);
    await tester.tap(find.byTooltip('Backends'));
    await tester.pumpAndSettle();
    String field(String key) =>
        tester.widget<TextField>(find.byKey(ValueKey(key))).controller!.text;
    expect(field('authHeader-0'), 'Authorization');
    expect(field('startRel-0'), 'dashboard');
    expect(field('baseUrl-1'), _betaBase);
    await tester.tap(find.byKey(const Key('cancel')));
    await tester.pumpAndSettle();

    // ...and on the wire: the restored secret goes out in the restored header.
    requests.clear();
    await tester.tap(find.text('Beta').first);
    await tester.pumpAndSettle();
    expect(requests.single.url.toString(), _betaBase);
    expect(requests.single.headers['X-Token'], 'beta-secret');
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
  });

  group('asking first and refusing clearly', () {
    Future<SettingsService> seeded(WidgetTester tester) async {
      final env = await install(tester, 1);
      await env.settings.saveBackends([
        const Backend(name: 'Keep me', baseUrl: _betaBase, secret: 'k'),
      ]);
      await tester.pumpWidget(
        MaterialApp(
          home: HomeScreen(
            key: const ValueKey('reloaded'),
            settingsService: env.settings,
            httpService: httpService,
            backupFiles: files,
          ),
        ),
      );
      await tester.pumpAndSettle();
      return env.settings;
    }

    testWidgets('cancelling the export warning writes nothing', (tester) async {
      await seeded(tester);
      await openMenu(tester, 'Export settings');
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(files.saveCalls, 0);
    });

    testWidgets('cancelling the save dialog reports nothing', (tester) async {
      await seeded(tester);
      files.cancelSave = true;
      await openMenu(tester, 'Export settings');
      await tester.tap(find.byKey(const Key('confirm-export')));
      await tester.pumpAndSettle();
      expect(files.saveCalls, 1);
      expect(find.textContaining('Exported'), findsNothing);
    });

    testWidgets('a file from another app is refused with the reason', (
      tester,
    ) async {
      final settings = await seeded(tester);
      files.toOpen = Ok(
        jsonEncode({
          'app': 'com.example.other',
          'format': 'restforge-settings',
          'formatVersion': 1,
          'backends': [],
          'shortcuts': [],
        }),
      );
      await openMenu(tester, 'Import settings');
      expect(find.text('Cannot import'), findsOneWidget);
      expect(find.textContaining('another app'), findsOneWidget);
      expect(find.text('Replace all settings?'), findsNothing);
      await tester.tap(find.text('OK'));
      await tester.pumpAndSettle();
      expect((await settings.loadBackends()).single.name, 'Keep me');
    });

    testWidgets('a file that cannot be read is refused with the reason', (
      tester,
    ) async {
      await seeded(tester);
      files.toOpen = const Err(
        Failure(kind: FailureKind.parse, message: 'it is not text.'),
      );
      await openMenu(tester, 'Import settings');
      expect(find.text('Cannot import'), findsOneWidget);
      expect(find.text('it is not text.'), findsOneWidget);
    });

    testWidgets('cancelling the file dialog does nothing', (tester) async {
      await seeded(tester);
      await openMenu(tester, 'Import settings');
      expect(files.openCalls, 1);
      expect(find.byType(AlertDialog), findsNothing);
    });

    testWidgets('declining the replace question keeps everything', (
      tester,
    ) async {
      final settings = await seeded(tester);
      files.toOpen = Ok(
        jsonEncode({
          'app': 'org.buetow.restforge',
          'format': 'restforge-settings',
          'formatVersion': 1,
          'backends': [],
          'shortcuts': [],
        }),
      );
      await openMenu(tester, 'Import settings');
      expect(
        find.textContaining('Your 1 backend and 0 shortcuts'),
        findsOneWidget,
      );
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(find.text('Keep me'), findsOneWidget);
      expect((await settings.loadBackends()).single.name, 'Keep me');
    });
  });
}
