// Pure Dart unit tests for the settings backup: no WidgetTester, no device.
// See AGENTS.md section 5 ("Test style"). The cases that matter are the ones
// where a restore could quietly not restore: a field that does not survive
// the round trip, a file that is accepted but is not ours, and an import
// that fails half way and leaves the app half replaced.
//
// shared_preferences ships its own test double; secrets use the same
// in-memory SecretStore fake the other service tests use.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/services/backup_service.dart';
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

/// A [SecretStore] whose writes fail — a locked Keystore — so the backend
/// half of an import fails after the metadata half has been written.
class _FailingWriteSecretStore extends _InMemorySecretStore {
  bool failWrites = false;

  /// Reads throw while set — a Keystore that is only temporarily unreadable.
  bool failReads = false;

  @override
  Future<String?> read(String key) async {
    if (failReads) {
      throw Exception('keystore busy');
    }
    return super.read(key);
  }

  @override
  Future<void> write(String key, String value) async {
    if (failWrites) {
      throw Exception('keystore locked');
    }
    await super.write(key, value);
  }
}

/// A [QuickService] whose save fails once armed, standing in for a full
/// disk on the second half of an import.
class _FailingQuickService extends QuickService {
  _FailingQuickService({super.settings});

  bool failSaves = false;

  /// Fails only this many saves, then works again — so the import's write
  /// fails but the rollback's succeeds.
  int failNextSaves = 0;

  @override
  Future<Result<List<QuickItem>>> save(List<QuickItem> list) async {
    if (failSaves || failNextSaves > 0) {
      if (failNextSaves > 0) {
        failNextSaves--;
      }
      return const Err(Failure(kind: FailureKind.config, message: 'disk full'));
    }
    return super.save(list);
  }
}

/// Every field of every kind of setting at a non-default value: a
/// non-default auth header, a start rel, a secret with characters JSON has
/// to escape, both kinds of shortcut, and a shortcut whose backend is not in
/// the list (kept, and shown as "backend removed", not dropped).
final _backends = [
  const Backend(
    name: 'Production',
    baseUrl: 'https://api.example.test/v1/',
    authHeader: 'Authorization',
    secret: r'Bearer s3cr"et\with/ünïcode',
    startRel: 'dashboard',
  ),
  const Backend(
    name: 'Staging',
    baseUrl: 'http://10.0.0.2:8731/',
    authHeader: 'X-Token',
    secret: 'stage-key',
    startRel: '',
  ),
];

final _shortcuts = [
  const QuickItem(
    label: 'Restart worker',
    backendName: 'Production',
    baseUrl: 'https://api.example.test/v1/',
    kind: QuickKind.action,
    holder: 'https://api.example.test/v1/workers/7',
    name: 'restart',
  ),
  const QuickItem(
    label: 'Queue',
    backendName: 'Staging',
    baseUrl: 'http://10.0.0.2:8731/',
    kind: QuickKind.document,
    href: 'http://10.0.0.2:8731/queue?page=2',
  ),
  const QuickItem(
    label: 'Orphan',
    backendName: 'Gone',
    baseUrl: 'https://gone.example.test/',
    kind: QuickKind.document,
    href: 'https://gone.example.test/x',
  ),
];

Map<String, dynamic> _validFile() =>
    jsonDecode(
          BackupService.encode(
            SettingsBackup(backends: _backends, shortcuts: _shortcuts),
          ),
        )
        as Map<String, dynamic>;

String _failureOf(Result<SettingsBackup> result) => switch (result) {
  Ok() => fail('expected the file to be refused'),
  Err(:final failure) => failure.message,
};

String _failureOfApply(Result<SettingsBackup> result) => switch (result) {
  Ok() => fail('expected the import to fail'),
  Err(:final failure) => failure.message,
};

SettingsBackup _valueOf(Result<SettingsBackup> result) => switch (result) {
  Ok(:final value) => value,
  Err(:final failure) => fail('expected success, got: ${failure.message}'),
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FailingWriteSecretStore secrets;
  late SettingsService settings;
  late _FailingQuickService quick;
  late BackupService backup;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    secrets = _FailingWriteSecretStore();
    settings = SettingsService(secretStore: secrets);
    quick = _FailingQuickService(settings: settings);
    backup = BackupService(settings: settings, quick: quick);
  });

  Future<void> seed() async {
    expect(await settings.saveBackends(_backends), isA<Ok<List<Backend>>>());
    expect(await quick.save(_shortcuts), isA<Ok<List<QuickItem>>>());
  }

  /// A fresh install: empty preferences and an empty secret store.
  void wipe() {
    SharedPreferences.setMockInitialValues({});
    secrets = _FailingWriteSecretStore();
    settings = SettingsService(secretStore: secrets);
    quick = _FailingQuickService(settings: settings);
    backup = BackupService(settings: settings, quick: quick);
  }

  group('export', () {
    test('writes the header, every field and every secret', () async {
      await seed();
      final text = await backup.exportJson(now: DateTime.utc(2026, 9, 26, 8));
      final json = jsonDecode(text) as Map<String, dynamic>;

      expect(json['app'], 'org.buetow.restforge');
      expect(json['format'], 'restforge-settings');
      expect(json['formatVersion'], 1);
      expect(json['exportedAt'], '2026-09-26T08:00:00.000Z');
      expect(json['backends'], [
        {
          'name': 'Production',
          'baseUrl': 'https://api.example.test/v1/',
          'authHeader': 'Authorization',
          'secret': r'Bearer s3cr"et\with/ünïcode',
          'startRel': 'dashboard',
        },
        {
          'name': 'Staging',
          'baseUrl': 'http://10.0.0.2:8731/',
          'authHeader': 'X-Token',
          'secret': 'stage-key',
          'startRel': '',
        },
      ]);
      expect(json['shortcuts'], [for (final s in _shortcuts) s.toJson()]);
      expect((json['shortcuts'] as List).first['kind'], 'action');
    });

    test('an empty app exports two empty lists', () async {
      final json = jsonDecode(await backup.exportJson()) as Map;
      expect(json['backends'], isEmpty);
      expect(json['shortcuts'], isEmpty);
      expect(_valueOf(BackupService.parse(jsonEncode(json))).backends, isEmpty);
    });

    test('the string form of a backup never carries a secret', () {
      final value = SettingsBackup(backends: _backends, shortcuts: _shortcuts);
      expect(value.toString(), isNot(contains('stage-key')));
      expect(value.toString(), contains('2 backend(s), 3 shortcut(s)'));
    });

    test('suggested file name is dated and ends in .json', () {
      expect(
        BackupService.suggestedFileName(DateTime(2026, 1, 2, 3, 4)),
        'restforge-settings-20260102-0304.json',
      );
    });
  });

  group('round trip', () {
    test('export, wipe, import restores every setting exactly', () async {
      await seed();
      final text = await backup.exportJson();

      wipe();
      expect(await settings.loadBackends(), isEmpty);
      expect(await quick.load(), isEmpty);

      final parsed = _valueOf(BackupService.parse(text));
      expect(await backup.apply(parsed), isA<Ok<SettingsBackup>>());

      expect(await settings.loadBackends(), _backends);
      expect(await quick.load(), _shortcuts);
      expect(secrets.data.values.toSet(), {
        r'Bearer s3cr"et\with/ünïcode',
        'stage-key',
      });
    });

    test('the maximum number of each survives', () async {
      final many = [
        for (var i = 0; i < SettingsService.maxBackends; i++)
          Backend(name: 'b$i', baseUrl: 'https://h$i.test/', secret: 's$i'),
      ];
      final shortcuts = [
        for (var i = 0; i < QuickService.maxQuick; i++)
          QuickItem(
            label: 'q$i',
            baseUrl: 'https://h0.test/',
            kind: QuickKind.document,
            href: 'https://h0.test/$i',
          ),
      ];
      final text = BackupService.encode(
        SettingsBackup(backends: many, shortcuts: shortcuts),
      );
      final parsed = _valueOf(BackupService.parse(text));
      await backup.apply(parsed);
      expect(await settings.loadBackends(), many);
      expect(await quick.load(), shortcuts);
    });

    test('a backend with no secret imports, with the secret empty', () async {
      final file = _validFile();
      (file['backends'] as List).first['secret'] = '';
      final parsed = _valueOf(BackupService.parse(jsonEncode(file)));
      expect(parsed.backends.first.secret, '');
    });
  });

  group('import replaces', () {
    test('what was there before is gone, secrets included', () async {
      await settings.saveBackends([
        const Backend(
          name: 'Old',
          baseUrl: 'https://old.test/',
          secret: 'old-key',
        ),
      ]);
      await quick.save([
        const QuickItem(
          label: 'Old shortcut',
          baseUrl: 'https://old.test/',
          kind: QuickKind.document,
          href: 'https://old.test/a',
        ),
      ]);

      final parsed = _valueOf(BackupService.parse(jsonEncode(_validFile())));
      await backup.apply(parsed);

      expect(await settings.loadBackends(), _backends);
      expect(await quick.load(), _shortcuts);
      expect(secrets.data.values, isNot(contains('old-key')));
    });

    test('an empty backup empties the app', () async {
      await seed();
      final parsed = _valueOf(
        BackupService.parse(
          BackupService.encode(
            const SettingsBackup(backends: [], shortcuts: []),
          ),
        ),
      );
      await backup.apply(parsed);
      expect(await settings.loadBackends(), isEmpty);
      expect(await quick.load(), isEmpty);
      expect(secrets.data, isEmpty);
    });

    test('a failed shortcut write rolls the backends back', () async {
      await settings.saveBackends([
        const Backend(name: 'Old', baseUrl: 'https://old.test/', secret: 'k'),
      ]);
      quick.failSaves = true;

      final parsed = _valueOf(BackupService.parse(jsonEncode(_validFile())));
      final result = await backup.apply(parsed);

      expect(result, isA<Err<SettingsBackup>>());
      expect(await settings.loadBackends(), [
        const Backend(name: 'Old', baseUrl: 'https://old.test/', secret: 'k'),
      ]);
    });

    test('a rolled-back import says the settings are unchanged', () async {
      await seed();
      quick.failNextSaves = 1;
      final result = await backup.apply(
        const SettingsBackup(backends: [], shortcuts: []),
      );
      expect(
        _failureOfApply(result),
        allOf(contains('your settings are unchanged'), contains('disk full')),
      );
      expect(await settings.loadBackends(), _backends);
      expect(await quick.load(), _shortcuts);
    });

    test('a rollback that fails too is reported, not hidden', () async {
      await seed();
      quick.failSaves = true;
      final result = await backup.apply(
        const SettingsBackup(backends: [], shortcuts: []),
      );
      expect(
        _failureOfApply(result),
        allOf(
          contains('putting the previous settings back failed too'),
          contains('Check your backends and shortcuts'),
        ),
      );
    });

    test('a rollback never blanks a secret that was only unreadable', () async {
      await seed();
      // The snapshot taken before the import cannot read any secret, so it
      // holds '' for each; restoring it must not write those over the keys.
      secrets.failReads = true;
      quick.failNextSaves = 1;
      final result = await backup.apply(
        const SettingsBackup(
          backends: [
            Backend(name: 'New', baseUrl: 'https://new.test/', secret: 'n'),
          ],
          shortcuts: [],
        ),
      );
      expect(result, isA<Err<SettingsBackup>>());
      secrets.failReads = false;
      expect(await settings.loadBackends(), _backends);
      expect(secrets.data.values, isNot(contains('n')));
    });

    test('an empty secret in the file keeps the stored one', () async {
      await seed();
      final file = _validFile();
      for (final entry in file['backends'] as List) {
        entry['secret'] = '';
      }
      final parsed = _valueOf(BackupService.parse(jsonEncode(file)));
      expect(await backup.apply(parsed), isA<Ok<SettingsBackup>>());
      expect(await settings.loadBackends(), _backends);
    });

    test('an empty secret for a new backend stores no secret', () async {
      await backup.apply(
        const SettingsBackup(
          backends: [Backend(name: 'Keyless', baseUrl: 'https://k.test/')],
          shortcuts: [],
        ),
      );
      expect(secrets.data, isEmpty);
      expect((await settings.loadBackends()).single.secret, '');
    });

    test('a failed secret write leaves the backend list as it was', () async {
      await seed();
      secrets.failWrites = true;
      final result = await backup.apply(
        const SettingsBackup(
          backends: [
            Backend(name: 'New', baseUrl: 'https://new.test/', secret: 'n'),
          ],
          shortcuts: [],
        ),
      );
      expect(result, isA<Err<SettingsBackup>>());
      secrets.failWrites = false;
      expect(await settings.loadBackends(), _backends);
      expect(await quick.load(), _shortcuts);
    });
  });

  group('validation', () {
    test('not JSON', () {
      expect(_failureOf(BackupService.parse('hello')), contains('not JSON'));
    });

    test('JSON that is not an object', () {
      expect(
        _failureOf(BackupService.parse('[]')),
        'This is not a RESTForge settings backup.',
      );
    });

    test('another app', () {
      final file = _validFile()..['app'] = 'com.example.other';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        contains('another app ("com.example.other")'),
      );
    });

    test('no app id at all', () {
      final file = _validFile()..remove('app');
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'This is not a RESTForge settings backup.',
      );
    });

    test('an unknown format name', () {
      final file = _validFile()..['format'] = 'something-else';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'This is not a RESTForge settings backup.',
      );
    });

    for (final version in <Object?>[null, '1', 0, -3, 1.5]) {
      test('an unknown format version: $version', () {
        final file = _validFile()..['formatVersion'] = version;
        expect(
          _failureOf(BackupService.parse(jsonEncode(file))),
          contains('unknown format version'),
        );
      });
    }

    test('a newer format version names both versions', () {
      final file = _validFile()..['formatVersion'] = 2;
      final message = _failureOf(BackupService.parse(jsonEncode(file)));
      expect(message, contains('newer RESTForge'));
      expect(message, contains('format 2'));
      expect(message, contains('up to 1'));
    });

    for (final key in ['backends', 'shortcuts']) {
      test('a missing $key list', () {
        final file = _validFile()..remove(key);
        expect(
          _failureOf(BackupService.parse(jsonEncode(file))),
          contains('incomplete'),
        );
      });

      test('a $key value that is not a list', () {
        final file = _validFile()..[key] = {'a': 1};
        expect(
          _failureOf(BackupService.parse(jsonEncode(file))),
          contains('incomplete'),
        );
      });
    }

    test('more backends than the app holds', () {
      final file = _validFile();
      file['backends'] = [
        for (var i = 0; i <= SettingsService.maxBackends; i++)
          {'name': 'b$i', 'baseUrl': 'https://h/', 'secret': 's'},
      ];
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        contains('at most ${SettingsService.maxBackends}'),
      );
    });

    test('more shortcuts than the app holds', () {
      final file = _validFile();
      file['shortcuts'] = [
        for (var i = 0; i <= QuickService.maxQuick; i++)
          {
            'label': 'q$i',
            'baseUrl': 'https://h/',
            'kind': 'document',
            'href': 'https://h/$i',
          },
      ];
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        contains('at most ${QuickService.maxQuick}'),
      );
    });

    test('a backend that is not an object', () {
      final file = _validFile();
      (file['backends'] as List).add('nope');
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'Backend 3 in this backup is not a backend.',
      );
    });

    test('a backend with no name', () {
      final file = _validFile();
      (file['backends'] as List).first['name'] = '  ';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'Backend 1 in this backup: Name is required.',
      );
    });

    test('a backend with a relative base URL', () {
      final file = _validFile();
      (file['backends'] as List)[1]['baseUrl'] = 'api/v1';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        contains('Backend 2 in this backup: Base URL must be absolute'),
      );
    });

    test('two backends with the same name and base URL', () {
      final file = _validFile();
      final backends = file['backends'] as List;
      backends.add({...backends.first as Map, 'secret': 'other'});
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'Backends 1 and 3 in this backup have the same name and base URL.',
      );
    });

    test('the same name at two base URLs is two backends', () {
      final file = _validFile();
      final backends = file['backends'] as List;
      backends.add({...backends.first as Map, 'baseUrl': 'https://b.test/'});
      expect(
        _valueOf(BackupService.parse(jsonEncode(file))).backends,
        hasLength(3),
      );
    });

    test('a shortcut of an unknown kind', () {
      final file = _validFile();
      (file['shortcuts'] as List)[1]['kind'] = 'teleport';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'Shortcut 2 in this backup is not a shortcut.',
      );
    });

    test('an action shortcut with no action name', () {
      final file = _validFile();
      (file['shortcuts'] as List).first['name'] = '';
      expect(
        _failureOf(BackupService.parse(jsonEncode(file))),
        'Shortcut 1 in this backup is incomplete.',
      );
    });

    test('a file too large to be a backup', () {
      expect(
        _failureOf(BackupService.parse(' ' * (BackupService.maxFileBytes + 1))),
        contains('too large'),
      );
    });

    test('a refused file changes nothing', () async {
      await seed();
      final file = _validFile()..['app'] = 'x';
      expect(BackupService.parse(jsonEncode(file)), isA<Err<SettingsBackup>>());
      expect(await settings.loadBackends(), _backends);
      expect(await quick.load(), _shortcuts);
    });
  });

  group('unknown keys', () {
    test('are ignored at the top level and inside every entry', () {
      final file = _validFile();
      file['futureSetting'] = {'theme': 'dark'};
      file['exportedBy'] = 'someone';
      for (final entry in file['backends'] as List) {
        entry['colour'] = 'blue';
      }
      for (final entry in file['shortcuts'] as List) {
        entry['pinned'] = true;
      }
      final parsed = _valueOf(BackupService.parse(jsonEncode(file)));
      expect(parsed.backends, _backends);
      expect(parsed.shortcuts, _shortcuts);
    });

    test('a missing exportedAt is not required', () {
      final file = _validFile()..remove('exportedAt');
      expect(
        _valueOf(BackupService.parse(jsonEncode(file))).backends,
        _backends,
      );
    });

    test('missing optional fields take their defaults', () {
      final file = _validFile();
      file['backends'] = [
        {'name': 'Bare', 'baseUrl': 'https://bare.test'},
      ];
      final parsed = _valueOf(BackupService.parse(jsonEncode(file)));
      expect(
        parsed.backends.single,
        const Backend(name: 'Bare', baseUrl: 'https://bare.test/'),
      );
    });
  });
}
