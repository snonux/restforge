// Pure Dart unit tests: no WidgetTester, no device. See AGENTS.md section 5
// ("Test style"). Ported from pebble/tools/test-settings.js — the cases that
// matter are the degenerate ones: storage written by an older layout,
// hand-edited, or truncated. loadBackends() runs at startup, so anything it
// throws leaves the app with no way to reach the settings screen and fix it.
// Every such case must degrade to "no backends", which is recoverable.
//
// shared_preferences ships its own test double
// (SharedPreferences.setMockInitialValues); flutter_secure_storage does not,
// so this file supplies a trivial in-memory SecretStore fake instead of
// touching the platform channel the real plugin needs.

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/services/settings_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// In-memory [SecretStore] fake. Exposes [data] directly so tests can assert
/// on what did or did not get persisted (and what did or did not get
/// deleted) without going through [SettingsService] itself.
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

/// A [SecretStore] that fails every write, standing in for a real platform
/// failure (a locked Keystore, a full disk) so saveBackends' error path can
/// be exercised without a device.
class FailingSecretStore implements SecretStore {
  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> write(String key, String value) async => throw Exception('write failed');

  @override
  Future<void> delete(String key) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late InMemorySecretStore secrets;
  late SettingsService settings;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    secrets = InMemorySecretStore();
    settings = SettingsService(secretStore: secrets);
  });

  group('no storage', () {
    test('no storage means no backends', () async {
      expect(await settings.loadBackends(), isEmpty);
      expect(await settings.count(), 0);
    });
  });

  group('normalisation', () {
    test('a trailing slash is added', () async {
      // Without it, RFC 3986 resolution drops the last path segment and
      // every href in the document resolves one level too high.
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://host.example.org/cgi-bin/app', secret: 'k'),
      ]);

      final stored = await settings.getBackend(0);
      expect(stored?.baseUrl, 'https://host.example.org/cgi-bin/app/');
    });

    test('the auth header defaults', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://host.example.org/app', secret: 'k'),
      ]);

      final stored = await settings.getBackend(0);
      expect(stored?.authHeader, SettingsService.defaultAuthHeader);
    });

    test('an out-of-range index is null', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://host.example.org/app', secret: 'k'),
      ]);

      expect(await settings.getBackend(5), isNull);
      expect(await settings.getBackend(-1), isNull);
    });
  });

  group('the secret/metadata split', () {
    test('the secret comes back from loadBackends', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'SECRET-VALUE'),
      ]);

      final loaded = await settings.loadBackends();
      expect(loaded.single.secret, 'SECRET-VALUE');
    });

    test('the secret is not in what shared_preferences stores', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'SECRET-MUST-NOT-APPEAR'),
      ]);

      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('restforge.backends');
      expect(raw, isNotNull);
      expect(raw, isNot(contains('SECRET-MUST-NOT-APPEAR')));
    });

    test('the secret is filed under a key derived from the backend', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'k'),
      ]);

      expect(secrets.data.values, contains('k'));
    });

    test('deleting a backend deletes its secret', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'homelab-secret'),
        const Backend(name: 'office', baseUrl: 'https://o/', secret: 'office-secret'),
      ]);
      expect(secrets.data.values, containsAll(['homelab-secret', 'office-secret']));

      // Deletion is expressed the same way settings.js expresses it: saving
      // the list without the removed entry. There is no separate delete().
      await settings.saveBackends([
        const Backend(name: 'office', baseUrl: 'https://o/', secret: 'office-secret'),
      ]);

      expect(secrets.data.values, isNot(contains('homelab-secret')));
      expect(secrets.data.values, contains('office-secret'));
    });

    test('renaming a backend orphans the old secret rather than stranding it', () async {
      await settings.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'k'),
      ]);
      expect(secrets.data.length, 1);

      await settings.saveBackends([
        const Backend(name: 'homelab-renamed', baseUrl: 'https://h/', secret: 'k'),
      ]);

      // Exactly one secret survives: the old key was cleaned up, not left
      // behind holding a secret no backend can reach any more.
      expect(secrets.data.length, 1);
      expect(secrets.data.values, contains('k'));
    });

    test('a platform failure while saving comes back as Err, not a throw', () async {
      final failing = SettingsService(secretStore: FailingSecretStore());

      final result = await failing.saveBackends([
        const Backend(name: 'homelab', baseUrl: 'https://h/', secret: 'k'),
      ]);

      expect(result, isA<Err<List<Backend>>>());
    });
  });

  group('incomplete entries', () {
    test('an unnamed backend is dropped', () async {
      await settings.saveBackends([
        const Backend(name: '', baseUrl: 'https://a/', secret: 'x'),
        const Backend(name: 'good', baseUrl: 'https://b/', secret: 'x'),
      ]);

      final loaded = await settings.loadBackends();
      expect(loaded.length, 1);
      expect(loaded.single.name, 'good');
    });
  });

  group('corrupt storage degrades to no backends', () {
    test('unparseable storage reads as empty', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.backends', 'not json at all');

      expect(await settings.loadBackends(), isEmpty);
    });

    test('a non-array reads as empty', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.backends', '{"backends":1}');

      expect(await settings.loadBackends(), isEmpty);
    });

    test('junk array members are dropped', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('restforge.backends', '[null,3,"x"]');

      expect(await settings.loadBackends(), isEmpty);
    });
  });

  group('validation', () {
    test('a missing secret is reported', () {
      final backend = SettingsService.normalise({'name': 'a', 'baseUrl': 'https://h/'});
      expect(SettingsService.validate(backend), 'Secret is required');
    });

    test('a relative base URL is reported', () {
      final backend = SettingsService.normalise({'name': 'a', 'baseUrl': '/x/', 'secret': 's'});
      expect(SettingsService.validate(backend), contains('absolute'));
    });

    test('a complete backend validates', () {
      final backend = SettingsService.normalise({'name': 'a', 'baseUrl': 'https://h/x', 'secret': 's'});
      expect(SettingsService.validate(backend), isNull);
    });
  });

  group('the backend cap', () {
    test('the backend count is capped at ${SettingsService.maxBackends}', () async {
      final many = [
        for (var i = 0; i < SettingsService.maxBackends + 8; i++)
          Backend(name: 'b$i', baseUrl: 'https://h/$i/', secret: 's'),
      ];

      await settings.saveBackends(many);

      expect(await settings.count(), SettingsService.maxBackends);
    });
  });
}
