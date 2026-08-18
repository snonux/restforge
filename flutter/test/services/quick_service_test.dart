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
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';
import 'dart:convert';

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

/// A shared_preferences store whose writes always throw — for the platform
/// write-failure path through [QuickService.add]/[QuickService.remove]. Reads
/// (getAll) delegate to the in-memory store so load() still works; only
/// [setValue] throws, so a test can assert the store's data is unchanged after
/// a failed write (via [getAll]) — the optimistic SharedPreferences cache is a
/// separate, per-instance concern.
class _FailingWriteStore extends InMemorySharedPreferencesStore {
  _FailingWriteStore([Map<String, Object>? seed])
    : super.withData(seed ?? const <String, Object>{});

  int setValueCalls = 0;

  @override
  Future<bool> setValue(String valueType, String key, Object value) async {
    setValueCalls++;
    throw Exception('disk full: preferences write failed');
  }
}

const a = 'https://a.example/api/';
const b = 'https://b.example/api/';

QuickItem action(String label, String base, String holder, String name) =>
    QuickItem(
      label: label,
      backendName: 'x',
      baseUrl: base,
      kind: QuickKind.action,
      holder: holder,
      name: name,
    );

QuickItem document(String label, String base, String href) => QuickItem(
  label: label,
  backendName: 'x',
  baseUrl: base,
  kind: QuickKind.document,
  href: href,
);

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
      await settings.saveBackends([
        const Backend(name: 'beta renamed', baseUrl: b, secret: 'k'),
      ]);

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
      await settings.saveBackends([
        const Backend(name: 'beta', baseUrl: b, secret: 'k'),
      ]);

      expect(await quick.count(), 1);
    });

    test('it resolves to nothing', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await settings.saveBackends([
        const Backend(name: 'beta', baseUrl: b, secret: 'k'),
      ]);

      final item = await quick.get(0);
      expect(await quick.backendFor(item!), isNull);
    });
  });

  group('backendsFor resolves a list against a supplied backend list', () {
    // backendsFor is the batch, in-memory resolver home_screen uses so N
    // shortcuts cost one backend load, not N+1 secret-storage reads. These
    // pin the matching (by base URL, first match wins, null for gone) and —
    // critically — that it is pure: it resolves against the *passed* list and
    // never reads storage, so the caller's already-loaded list is what counts.
    test('resolves each shortcut to its backend by base URL', () async {
      await quick.add(action('Power off', a, '/api/', 'power-off'));
      await quick.add(document('Status', b, '/api/status'));
      final items = await quick.load();

      final resolved = quick.backendsFor(items, [
        const Backend(name: 'alpha', baseUrl: a, secret: 'k'),
        const Backend(name: 'beta', baseUrl: b, secret: 'k'),
      ]);
      expect(resolved.map((backend) => backend?.name), ['alpha', 'beta']);
    });

    test(
      'returns null per item whose backend is not in the supplied list',
      () async {
        await quick.add(action('one', a, '/api/', 'a1'));
        await quick.add(document('two', b, '/api/status'));
        const c = 'https://c.example/api/';
        await quick.add(document('three', c, '/api/other'));
        final items = await quick.load();

        final resolved = quick.backendsFor(items, [
          const Backend(name: 'alpha', baseUrl: a, secret: 'k'),
          const Backend(name: 'beta', baseUrl: b, secret: 'k'),
        ]);
        expect(resolved.map((backend) => backend?.name), [
          'alpha',
          'beta',
          null,
        ]);
      },
    );

    test(
      'is pure: it resolves against the passed list, never storage',
      () async {
        // Storage has alpha@baseUrl=a (from setUp); pass a list whose a-backend
        // is named differently, and an empty list, and confirm backendsFor uses
        // only what it was handed — never falling back to a storage read.
        await quick.add(action('Power off', a, '/api/', 'power-off'));
        final item = (await quick.load()).single;

        final fromPassed = quick.backendsFor(
          [item],
          [const Backend(name: 'not-from-storage', baseUrl: a, secret: 'x')],
        );
        expect(fromPassed.single?.name, 'not-from-storage');

        final fromEmpty = quick.backendsFor([item], const <Backend>[]);
        expect(fromEmpty.single, isNull);
      },
    );

    test(
      'first backend wins for a duplicate base URL, mirroring backendFor',
      () async {
        await quick.add(action('Power off', a, '/api/', 'power-off'));
        final item = (await quick.load()).single;

        final resolved = quick.backendsFor(
          [item],
          [
            const Backend(name: 'first', baseUrl: a, secret: 'k'),
            const Backend(name: 'second', baseUrl: a, secret: 'k'),
          ],
        );
        expect(resolved.single?.name, 'first');
        // and backendFor agrees, since they share the matching logic.
        expect((await quick.backendFor(item))?.baseUrl, a);
      },
    );
  });

  group('an incomplete shortcut is refused', () {
    test('an action without a holder is refused', () async {
      final result = await quick.add(
        const QuickItem(
          label: 'x',
          baseUrl: a,
          kind: QuickKind.action,
          name: 'power-off',
        ),
      );
      expect(result, isNull);
    });

    test('an action without a name is refused', () async {
      final result = await quick.add(
        const QuickItem(
          label: 'x',
          baseUrl: a,
          kind: QuickKind.action,
          holder: '/api/',
        ),
      );
      expect(result, isNull);
    });

    test('a document without an address is refused', () async {
      final result = await quick.add(document('x', a, ''));
      expect(result, isNull);
    });

    test('nothing was stored', () async {
      await quick.add(
        const QuickItem(
          label: 'x',
          baseUrl: a,
          kind: QuickKind.action,
          name: 'power-off',
        ),
      );
      await quick.add(
        const QuickItem(
          label: 'x',
          baseUrl: a,
          kind: QuickKind.action,
          holder: '/api/',
        ),
      );
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
      final result = await quick.save([
        action('Power off', a, '/api/', 'power-off'),
      ]);
      expect(result, isA<Ok<List<QuickItem>>>());
    });
  });

  group('a platform write failure', () {
    // save() returns Err on a platform-layer write failure (full disk, prefs
    // flush error) — that is save()'s own documented contract. add() and
    // remove() used to ignore the Result and report success anyway (a
    // SnackBar saying "Saved"/"Removed" while nothing was persisted). They
    // now inspect it: add returns null and remove returns false, so a caller
    // never reports a write that did not happen. The store is asserted
    // directly via getAll (bypassing the optimistic SharedPreferences cache,
    // which setString updates before the store write is even attempted).
    test('add returns null, not the item, when the write fails', () async {
      final store = _FailingWriteStore();
      SharedPreferencesStorePlatform.instance = store;

      final result = await quick.add(
        action('Power off', a, '/api/', 'power-off'),
      );

      expect(
        result,
        isNull,
        reason: 'a failed write must not be reported as saved',
      );
      expect(
        store.setValueCalls,
        greaterThan(0),
        reason: 'the add attempted to persist',
      );
      expect(
        await store.getAll(),
        isEmpty,
        reason: 'nothing was written — setValue threw before the store changed',
      );
    });

    test(
      'remove returns false when the write fails, and the entry survives',
      () async {
        final seed = {
          'flutter.restforge.quick': jsonEncode([
            {
              'label': 'one',
              'backendName': 'x',
              'baseUrl': a,
              'kind': 'action',
              'holder': '/api/',
              'name': 'a',
            },
          ]),
        };
        final store = _FailingWriteStore(seed);
        SharedPreferencesStorePlatform.instance = store;
        // Force the cached SharedPreferences to re-read from the failing
        // (seeded) store rather than the empty in-memory one setUp's
        // saveBackends populated — the legacy cache is per-instance, so reload
        // it from the now-current store.
        final prefs = await SharedPreferences.getInstance();
        await prefs.reload();

        final removed = await quick.remove(0);

        expect(
          removed,
          isFalse,
          reason: 'a failed write must not be reported as removed',
        );
        expect(
          store.setValueCalls,
          greaterThan(0),
          reason: 'the remove attempted to persist',
        );
        expect(
          (await store.getAll()).containsKey('flutter.restforge.quick'),
          isTrue,
          reason:
              'the entry is still in storage — setValue threw before the remove persisted',
        );
      },
    );
  });
}
