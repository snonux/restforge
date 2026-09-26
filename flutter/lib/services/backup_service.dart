/// Exporting every setting to one file, and restoring from it.
///
/// Everything this app persists is two lists: the backends
/// ([SettingsService], with each backend's secret in secure storage) and the
/// saved shortcuts ([QuickService]). There is nothing else — no other
/// preference, no launcher shortcut, no home-screen widget, no database —
/// so a backup is exactly those two lists, secrets included, and a restore
/// puts exactly those two lists back. The point is a full restore after an
/// uninstall has wiped the app's data (moving from one build of the app to
/// another signed with a different key is the usual reason), and a backup
/// without the keys would not be one.
///
/// **The file holds every secret in plain text.** That is the trade the user
/// asked for, and the screen that writes it says so before it does. It is
/// the only place a secret leaves [SettingsService]'s custody, and it goes
/// only to a file the user chose, never to a log: [SettingsBackup.toString]
/// and [Backend.toString] both leave it out.
///
/// The file is versioned JSON:
///
/// ```json
/// {
///   "app": "org.buetow.restforge",
///   "format": "restforge-settings",
///   "formatVersion": 1,
///   "exportedAt": "2026-09-26T10:00:00.000Z",
///   "backends":  [{"name", "baseUrl", "authHeader", "secret", "startRel"}],
///   "shortcuts": [{"label", "backendName", "baseUrl", "kind", "holder", "name", "href"}]
/// }
/// ```
///
/// **Import replaces, it does not merge.** A backup is a snapshot, and the
/// question it answers is "put me back where I was". A merge would have to
/// decide what a backend with the same name but a different URL means, and
/// whichever answer it picked would sometimes be the wrong one, silently.
/// Replacing is predictable, and the screen asks first, naming what is about
/// to be overwritten.
///
/// **Import is all or nothing.** [parse] refuses the whole file — with a
/// reason a person can act on — when it is not ours, is from a newer format
/// than this build understands, or holds an entry the stores would silently
/// drop or confuse (a backend with no name, two backends with the same name
/// and base URL — they would share one secret — a shortcut with nowhere to
/// go, more than the stores hold). Restoring half a backup and calling it
/// done is exactly the kind of quiet partial answer the rest of this app
/// refuses to give. Keys it does not know are ignored, so a file from a
/// later version that only *added* fields still imports. A backend whose
/// secret is empty in the file (its key could not be read when the backup
/// was made) is imported without overwriting a key already stored for it.
library;

import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/result.dart';
import 'quick_service.dart';
import 'settings_service.dart';

/// The two lists a backup holds. A plain value, like [Backend] and
/// [QuickItem] — it stays next to its one service for the same reason they
/// do (see AGENTS.md section 4).
@immutable
class SettingsBackup {
  const SettingsBackup({required this.backends, required this.shortcuts});

  final List<Backend> backends;
  final List<QuickItem> shortcuts;

  // Counts only: [Backend.toString] already leaves secrets out, but a
  // backup's string form has no reason to list anything at all.
  @override
  String toString() =>
      'SettingsBackup(${backends.length} backend(s), ${shortcuts.length} shortcut(s))';
}

class BackupService {
  BackupService({
    required SettingsService settings,
    required QuickService quick,
  }) : _settings = settings,
       _quick = quick;

  final SettingsService _settings;
  final QuickService _quick;

  /// The Android application id — what tells this app's backup apart from
  /// any other JSON file, including another RESTForge client's.
  static const String appId = 'org.buetow.restforge';
  static const String format = 'restforge-settings';

  /// Bumped only when an older build could no longer read the file
  /// correctly. A field that is merely added does not need it: unknown keys
  /// are ignored on import.
  static const int formatVersion = 1;

  /// Anything bigger than this is not a settings file: two lists of at most
  /// a dozen entries each, every field capped, come to a few kilobytes.
  static const int maxFileBytes = 1024 * 1024;

  /// What is stored right now, secrets included.
  Future<SettingsBackup> current() async => SettingsBackup(
    backends: await _settings.loadBackends(),
    shortcuts: await _quick.load(),
  );

  /// Every setting, as the text of a backup file.
  Future<String> exportJson({DateTime? now}) async =>
      encode(await current(), now: now);

  /// [backup] as the text of a backup file. Pure; [exportJson] is this plus
  /// reading the stores.
  static String encode(SettingsBackup backup, {DateTime? now}) {
    final document = <String, dynamic>{
      'app': appId,
      'format': format,
      'formatVersion': formatVersion,
      'exportedAt': (now ?? DateTime.now()).toUtc().toIso8601String(),
      'backends': [
        for (final b in backup.backends)
          {
            'name': b.name,
            'baseUrl': b.baseUrl,
            'authHeader': b.authHeader,
            'secret': b.secret,
            'startRel': b.startRel,
          },
      ],
      'shortcuts': [for (final item in backup.shortcuts) item.toJson()],
    };
    return '${const JsonEncoder.withIndent('  ').convert(document)}\n';
  }

  /// A file name for a new backup, dated so that several can sit side by
  /// side in one folder.
  static String suggestedFileName(DateTime now) {
    String two(int n) => n.toString().padLeft(2, '0');
    return 'restforge-settings-${now.year}${two(now.month)}${two(now.day)}'
        '-${two(now.hour)}${two(now.minute)}.json';
  }

  /// Reads and checks a backup file without touching storage — see the
  /// module comment for what is refused and why. The [Err]'s message is
  /// meant to be shown to the user as is.
  static Result<SettingsBackup> parse(String text) {
    if (text.length > maxFileBytes) {
      return _refuse(
        'This file is too large to be a RESTForge settings backup.',
      );
    }
    final Object? decoded;
    try {
      decoded = jsonDecode(text);
    } on FormatException {
      return _refuse(
        'This is not a RESTForge settings backup: it is not JSON.',
      );
    }
    if (decoded is! Map) {
      return _refuse('This is not a RESTForge settings backup.');
    }

    final app = decoded['app'];
    if (app is String && app != appId) {
      return _refuse(
        'This backup belongs to another app ("${_clip(app)}"), not RESTForge.',
      );
    }
    if (app != appId || decoded['format'] != format) {
      return _refuse('This is not a RESTForge settings backup.');
    }
    final version = decoded['formatVersion'];
    if (version is! int || version < 1) {
      return _refuse('This backup has an unknown format version.');
    }
    if (version > formatVersion) {
      return _refuse(
        'This backup was made by a newer RESTForge (format $version; this '
        'version reads up to $formatVersion). Update the app to import it.',
      );
    }

    final rawBackends = decoded['backends'];
    final rawShortcuts = decoded['shortcuts'];
    if (rawBackends is! List || rawShortcuts is! List) {
      return _refuse(
        'This backup is incomplete: it has no backend or shortcut list.',
      );
    }
    if (rawBackends.length > SettingsService.maxBackends) {
      return _refuse(
        'This backup has ${rawBackends.length} backends; RESTForge holds at '
        'most ${SettingsService.maxBackends}.',
      );
    }
    if (rawShortcuts.length > QuickService.maxQuick) {
      return _refuse(
        'This backup has ${rawShortcuts.length} shortcuts; RESTForge holds at '
        'most ${QuickService.maxQuick}.',
      );
    }

    final backends = <Backend>[];
    for (var i = 0; i < rawBackends.length; i++) {
      final entry = rawBackends[i];
      if (entry is! Map) {
        return _refuse('Backend ${i + 1} in this backup is not a backend.');
      }
      final backend = SettingsService.normalise(
        Map<String, dynamic>.from(entry),
      );
      final problem = SettingsService.validateAddress(backend);
      if (problem != null) {
        return _refuse('Backend ${i + 1} in this backup: $problem.');
      }
      // A secret is filed under name and base URL together, so two backends
      // sharing both would share one key and one of them would silently get
      // the other's secret.
      final twin = backends.indexWhere(
        (b) => b.name == backend.name && b.baseUrl == backend.baseUrl,
      );
      if (twin != -1) {
        return _refuse(
          'Backends ${twin + 1} and ${i + 1} in this backup have the same '
          'name and base URL.',
        );
      }
      backends.add(backend);
    }

    final shortcuts = <QuickItem>[];
    for (var i = 0; i < rawShortcuts.length; i++) {
      final entry = rawShortcuts[i];
      if (entry is! Map ||
          (entry['kind'] != 'action' && entry['kind'] != 'document')) {
        return _refuse('Shortcut ${i + 1} in this backup is not a shortcut.');
      }
      final item = QuickService.normalise(Map<String, dynamic>.from(entry));
      if (!QuickService.isUsable(item)) {
        return _refuse('Shortcut ${i + 1} in this backup is incomplete.');
      }
      shortcuts.add(item);
    }

    return Ok(SettingsBackup(backends: backends, shortcuts: shortcuts));
  }

  /// Replaces every stored setting with [backup] — see the module comment on
  /// why replace, not merge. Backends first, then shortcuts; if either write
  /// fails, both are rolled back to what was there before, so a failed
  /// import leaves the app as it found it rather than half restored. If the
  /// rollback fails too, the message says so, because then the app is in
  /// neither state and the user needs to check it.
  ///
  /// Removing a backend here also removes its secret, exactly as deleting it
  /// in the editor does — but only once both writes have succeeded
  /// ([SettingsService.dropOrphanedSecrets]), so a rollback never has to
  /// restore a secret it may not have been able to read. A backend whose
  /// secret is empty — in the backup, or in the snapshot a rollback restores
  /// because it could not be read — keeps whatever secret is already stored
  /// for it: [SettingsService.saveBackends] never writes an empty one.
  Future<Result<SettingsBackup>> apply(SettingsBackup backup) async {
    final before = await current();

    // The replaced backends' secrets stay until both writes have worked, so
    // a rollback can still find them — including any it could not read.
    final savedBackends = await _settings.saveBackends(
      backup.backends,
      keepOrphanedSecrets: true,
    );
    if (savedBackends case Err(:final failure)) {
      return Err(await _rollBack(failure, before, shortcutsTouched: false));
    }
    final savedShortcuts = await _quick.save(backup.shortcuts);
    if (savedShortcuts case Err(:final failure)) {
      return Err(await _rollBack(failure, before, shortcutsTouched: true));
    }
    await _settings.dropOrphanedSecrets(before.backends);
    debugPrint('backup: restored $backup');
    return Ok(backup);
  }

  /// Puts [before] back after a failed import and returns the failure to
  /// report: [cause] as it was when the rollback worked, or [cause] plus
  /// what the rollback could not undo when it did not.
  Future<Failure> _rollBack(
    Failure cause,
    SettingsBackup before, {
    required bool shortcutsTouched,
  }) async {
    final problems = <String>[];
    if (await _settings.saveBackends(before.backends) case Err(
      :final failure,
    )) {
      problems.add(failure.message);
    }
    if (shortcutsTouched) {
      if (await _quick.save(before.shortcuts) case Err(:final failure)) {
        problems.add(failure.message);
      }
    }
    if (problems.isEmpty) {
      return Failure(
        kind: cause.kind,
        message: 'Import failed; your settings are unchanged. ${cause.message}',
      );
    }
    return Failure(
      kind: cause.kind,
      message:
          'Import failed (${cause.message}), and putting the previous '
          'settings back failed too (${problems.join('; ')}). Check your '
          'backends and shortcuts before relying on them.',
    );
  }

  static Result<SettingsBackup> _refuse(String message) =>
      Err(Failure(kind: FailureKind.parse, message: message));

  static String _clip(String text) =>
      text.length > 64 ? '${text.substring(0, 64)}…' : text;
}
