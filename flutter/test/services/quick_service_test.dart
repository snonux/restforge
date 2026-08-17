// Pure Dart unit tests: no WidgetTester, no device. See AGENTS.md section 5
// ("Test style"). Ported from pebble/tools/test-quick.js — the cases that
// matter are the ways a shortcut can go wrong, and they are all versions of
// pointing somewhere other than where the user meant: at a reordered
// backend, at an action the server has since withdrawn, or at an address
// that was never the server's to give.
//
// shared_preferences ships its own test double
// (SharedPreferences.setMockInitialValues); SettingsService's secret half
// uses an in-memory SecretStore fake instead of the platform channel the
// real plugin needs, same as settings_service_test.dart.

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/services/quick_service.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// In-memory [SecretStore] fake — see settings_service_test.dart's copy for
/// the full reasoning. [QuickService] never touches secrets itself; this is
/// only here so its injected [SettingsService] can resolve backends without
/// a platform channel.
class InMemorySecretStore implements SecretStore {
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

const a = 'https://a.example/api/';
const b = 'https://b.example/api/';

QuickItem action(String label, String base, String holder, String name) => QuickItem(
  label: label,
  backendName: 'x',
  baseUrl: base,
  kind: QuickKind.action,
  holder: holder,
  name: name,
);

QuickItem document(String label, String base, String href) =>
    QuickItem(label: label, backendName: 'x', baseUrl: base, kind: QuickKind.document, href: href);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late SettingsService settings;
  late QuickService quick;

  /// Mirrors test-quick.js's setUp(): a fresh preferences store on every
  /// test, with two backends configured by default so [QuickService.backendFor]
  /// has something to resolve against.
  Future<void> setUpWith(List<Backend> backends) async {
    SharedPreferences.setMockInitialValues({});
    settings = SettingsService(secretStore: InMemorySecretStore());
    quick = QuickService(settings: settings);
    await settings.saveBackends(backends);
  }

  setUp(() async {
    await setUpWith([
      const Backend(name: 'alpha', baseUrl: a, secret: 'k'),
      const Backend(name: 'beta', baseUrl: b, secret: 'k'),
    ]);
  });

  group('add and list', () {
    test('shortcuts are kept in order', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await quick.add(document('Status', a, '/api/status'));

      final loaded = await quick.load();
      expect(loaded.map((i) => i.label).toList(), ['Power off', 'Status']);
    });

    test('backendFor names the backend a shortcut belongs to', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));

      final item = await quick.get(0);
      final backend = await quick.backendFor(item!);
      expect(backend?.name, 'alpha');
    });
  });

  group('add is idempotent', () {
    // Saving the same row twice is a natural thing to do.
    test('saving the same target twice does not duplicate it', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await quick.add(action('Power off (renamed)', a, '/api/', 'power-off'));

      expect(await quick.count(), 1);
    });

    test('and the newer label wins', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await quick.add(action('Power off (renamed)', a, '/api/', 'power-off'));

      final loaded = await quick.load();
      expect(loaded.single.label, 'Power off (renamed)');
    });
  });

  group('a backend is resolved by URL, not position', () {
    // The failure this guards: reordering backends must not re-point a
    // shortcut at a different server.
    test('resolves to the backend it was saved from', () async {
      await quick.add(action('Power off', b, '/api/', 'power-off'));

      final item = await quick.get(0);
      final backend = await quick.backendFor(item!);
      expect(backend?.name, 'beta');
    });

    test('still resolves after the list is reordered', () async {
      await quick.add(action('Power off', b, '/api/', 'power-off'));
      await settings.saveBackends([
        const Backend(name: 'beta', baseUrl: b, secret: 'k'),
        const Backend(name: 'alpha', baseUrl: a, secret: 'k'),
      ]);

      final item = await quick.get(0);
      final backend = await quick.backendFor(item!);
      expect(backend?.baseUrl, b);
    });

    test('still resolves after the backend is renamed', () async {
      await quick.add(action('Power off', b, '/api/', 'power-off'));
      await settings.saveBackends([const Backend(name: 'beta renamed', baseUrl: b, secret: 'k')]);

      final item = await quick.get(0);
      final backend = await quick.backendFor(item!);
      expect(backend?.baseUrl, b);
    });
  });

  group('a missing backend is shown, not dropped', () {
    // Kept and marked, not silently dropped: something the user saved going
    // missing without explanation is worse than a row that says so — see
    // quick_service.dart's module comment on why rows()/the "backend
    // removed" label are not ported here; this only checks backendFor's
    // half of that contract.
    test('a shortcut to a deleted backend survives', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await settings.saveBackends([const Backend(name: 'beta', baseUrl: b, secret: 'k')]);

      expect(await quick.count(), 1);
    });

    test('it resolves to nothing', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await settings.saveBackends([const Backend(name: 'beta', baseUrl: b, secret: 'k')]);

      final item = await quick.get(0);
      expect(await quick.backendFor(item!), isNull);
    });
  });

  group('an incomplete shortcut is refused', () {
    test('an action without a holder is refused', () async {
      final result = await quick.add(
        const QuickItem(label: 'x', baseUrl: a, kind: QuickKind.action, name: 'power-off'),
      );
      expect(result, isNull);
    });

    test('an action without a name is refused', () async {
      final result = await quick.add(
        const QuickItem(label: 'x', baseUrl: a, kind: QuickKind.action, holder: '/api/'),
      );
      expect(result, isNull);
    });

    test('a document without an address is refused', () async {
      final result = await quick.add(document('x', a, ''));
      expect(result, isNull);
    });

    test('nothing was stored', () async {
      await quick.add(const QuickItem(label: 'x', baseUrl: a, kind: QuickKind.action, name: 'power-off'));
      await quick.add(const QuickItem(label: 'x', baseUrl: a, kind: QuickKind.action, holder: '/api/'));
      await quick.add(document('x', a, ''));

      expect(await quick.count(), 0);
    });
  });

  group('remove', () {
    test('remove takes the named one', () async {
      await quick.add(action('one', a, '/api/', 'a'));
      await quick.add(action('two', a, '/api/', 'b'));

      expect(await quick.remove(0), true);
    });

    test('the other survives', () async {
      await quick.add(action('one', a, '/api/', 'a'));
      await quick.add(action('two', a, '/api/', 'b'));
      await quick.remove(0);

      final loaded = await quick.load();
      expect(loaded.single.label, 'two');
    });

    test('removing past the end is refused', () async {
      await quick.add(action('one', a, '/api/', 'a'));

      expect(await quick.remove(9), false);
    });
  });

  group('the shortcut cap', () {
    test('the list is capped at ${QuickService.maxQuick}', () async {
      for (var i = 0; i < QuickService.maxQuick + 5; i++) {
        await quick.add(action('n$i', a, '/api/', 'a$i'));
      }

      expect(await quick.count(), QuickService.maxQuick);
    });
  });

  group('corrupt storage degrades to no shortcuts', () {
    test('unparseable storage reads as empty', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.quick', 'not json');

      expect(await quick.load(), isEmpty);
    });

    test('a non-array reads as empty', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.quick', '{"a":1}');

      expect(await quick.load(), isEmpty);
    });

    test('unusable members are dropped', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.quick', '[null,3,{"label":"x"}]');

      expect(await quick.load(), isEmpty);
    });
  });

  group('an action is stored by name, never by its own href', () {
    // The server withdraws actions as its state changes, and a remembered
    // href would outlive that.
    test('the action name is what is kept', () async {
      await quick.add(
        const QuickItem(
          label: 'Power off',
          baseUrl: a,
          kind: QuickKind.action,
          holder: '/api/',
          name: 'power-off',
        ),
      );

      final stored = await quick.get(0);
      expect(stored?.name, 'power-off');
      expect(stored?.holder, '/api/');
    });
  });

  group('save', () {
    test('returns the persisted list on success', () async {
      final result = await quick.save([action('Power off', a, '/api/', 'power-off')]);
      expect(result, isA<Ok<List<QuickItem>>>());
    });
  });
}
