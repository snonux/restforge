/// Backend definitions and their secrets.
///
/// This is the Dart port of `pebble/src/pkjs/settings.js` — see that file's
/// header comment for the reasoning this one carries over unchanged. It is
/// still the only module that touches a secret: a secret goes into an
/// Authorization-style request header and into the settings screen, never
/// anywhere else. In particular it is never sent in a query string, because a
/// key in a URL is written to the server's request log and, behind a reverse
/// proxy, the proxy's log too.
///
/// The one deliberate difference from the Pebble version: `localStorage`
/// there is one flat JSON blob, readable by anything that can read the
/// phone's app-data XML file. Here a backend's secret is split off and kept
/// in [flutter_secure_storage] (Android Keystore-backed), while everything
/// else — name, base URL, auth header, start rel — stays in
/// [shared_preferences], an ordinary, unencrypted preference. Losing the
/// prefs file leaks which servers exist, never the keys to them.
///
/// Storage is one JSON array under a single shared_preferences key, exactly
/// like the Pebble version's single `localStorage` key. shared_preferences
/// already returns null for a key that was never set, so both ends are
/// guarded the same way settings.js guards `localStorage`: encode on the way
/// in, and treat anything that does not decode into an array of objects as
/// "no backends" rather than throwing during startup. A thrown exception here
/// would leave the app on a blank screen with no way to reach the settings
/// screen and fix it — see AGENTS.md section 5 ("Error model").
///
/// This storage pattern is copied deliberately by `quick_service.dart` (see
/// its module comment); do not extract a shared helper until a *third*
/// prefs-JSON-array store appears (Rule-of-Three).
///
/// A backend is:
///   name       display name, shown on the backend picker
///   baseUrl    the Siren API root, absolute, ending in '/'
///   authHeader header the secret is sent in, default X-API-Key
///   secret     the API key (secure storage only, never in shared_preferences)
///   startRel   optional link rel to follow immediately after the root
library;

import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/failure.dart';
import '../models/result.dart';

/// One configured backend. A plain value with no behaviour beyond reading
/// itself — it would live under `lib/models/` if anything besides
/// [SettingsService] needed it (see AGENTS.md section 4); today it doesn't,
/// so it stays next to its one caller rather than adding a file for its own
/// sake.
@immutable
class Backend {
  final String name;
  final String baseUrl;
  final String authHeader;
  final String secret;
  final String startRel;

  const Backend({
    required this.name,
    required this.baseUrl,
    this.authHeader = SettingsService.defaultAuthHeader,
    this.secret = '',
    this.startRel = '',
  });

  Backend copyWith({String? secret}) => Backend(
    name: name,
    baseUrl: baseUrl,
    authHeader: authHeader,
    secret: secret ?? this.secret,
    startRel: startRel,
  );

  /// The subset persisted to shared_preferences — deliberately excludes
  /// [secret]. See the module comment for why.
  Map<String, dynamic> _toMetadata() => {
    'name': name,
    'baseUrl': baseUrl,
    'authHeader': authHeader,
    'startRel': startRel,
  };

  @override
  bool operator ==(Object other) =>
      other is Backend &&
      other.name == name &&
      other.baseUrl == baseUrl &&
      other.authHeader == authHeader &&
      other.secret == secret &&
      other.startRel == startRel;

  @override
  int get hashCode => Object.hash(name, baseUrl, authHeader, secret, startRel);

  // Secret deliberately excluded: this is what a debug log or a test
  // failure message prints, and a secret must never land in a log.
  @override
  String toString() =>
      'Backend(name: $name, baseUrl: $baseUrl, authHeader: $authHeader, startRel: $startRel)';
}

/// The minimal secret-storage contract [SettingsService] needs.
///
/// [FlutterSecureStorage] satisfies this already, via [SecureSecretStore]
/// below. Tests supply a plain in-memory fake instead: the real plugin talks
/// to a platform channel that does not exist in a `flutter test` process, and
/// shared_preferences already ships its own test double
/// (`SharedPreferences.setMockInitialValues`) so only this side needs one.
abstract class SecretStore {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
}

/// Keystore-backed [SecretStore], via `EncryptedSharedPreferences` on
/// Android — see the module comment and flutter/README.md's "Configuring a
/// backend" section for why the secret alone gets this treatment.
class SecureSecretStore implements SecretStore {
  SecureSecretStore({FlutterSecureStorage? storage})
    : _storage =
          storage ??
          const FlutterSecureStorage(
            aOptions: AndroidOptions(encryptedSharedPreferences: true),
          );

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) => _storage.write(key: key, value: value);

  @override
  Future<void> delete(String key) => _storage.delete(key: key);
}

class SettingsService {
  static const String defaultAuthHeader = 'X-API-Key';

  /// Bounds, so a pathological config cannot make the settings screen or the
  /// backend picker unusable. These are generous: the limit that actually
  /// matters is screen space, exactly the reasoning in
  /// `pebble/src/pkjs/settings.js` — carried over unchanged even though
  /// there's no watch here, because the phone screen has the same practical
  /// ceiling.
  static const int maxBackends = 12;
  static const int maxFieldLength = 256;

  static const String _storageKey = 'restforge.backends';
  static const String _secretKeyPrefix = 'restforge.secret.';

  SettingsService({SecretStore? secretStore}) : _secrets = secretStore ?? SecureSecretStore();

  final SecretStore _secrets;

  /// Coerces one stored or submitted map into the shape above, filling in
  /// defaults. Does not reject anything — [validate] does that, and only for
  /// input a user just typed. Storage that has drifted (an older layout, a
  /// hand-edited value) degrades to something usable instead of taking the
  /// app down at startup.
  static Backend normalise(Map<String, dynamic>? raw) {
    final map = raw ?? const <String, dynamic>{};
    var baseUrl = _trim(map['baseUrl']);
    // Every href in a Siren document is resolved against this, and RFC 3986
    // resolution drops the last path segment of the base unless it ends in
    // '/'. Fixing it here rather than rejecting it saves the user from a
    // class of error whose only symptom is a 404 two screens later.
    if (baseUrl.isNotEmpty && !baseUrl.endsWith('/')) {
      baseUrl = '$baseUrl/';
    }
    final authHeader = _trim(map['authHeader']);
    return Backend(
      name: _trim(map['name']),
      baseUrl: baseUrl,
      authHeader: authHeader.isEmpty ? defaultAuthHeader : authHeader,
      secret: _trim(map['secret']),
      startRel: _trim(map['startRel']),
    );
  }

  /// Returns a human-readable problem, or null when the backend is usable.
  /// Meant to be called by the settings screen before it closes, so the
  /// wording is shown to the user.
  static String? validate(Backend backend) {
    if (backend.name.isEmpty) {
      return 'Name is required';
    }
    if (backend.baseUrl.isEmpty) {
      return 'Base URL is required';
    }
    if (!RegExp(r'^https?://[^\s/]+/').hasMatch(backend.baseUrl)) {
      return 'Base URL must be absolute, e.g. https://host/path/';
    }
    if (backend.secret.isEmpty) {
      return 'Secret is required';
    }
    return null;
  }

  /// Returns the stored backends, always as a list of normalised objects,
  /// each with its secret filled in from secure storage. Anything
  /// unparseable is reported and treated as empty: the app then shows "needs
  /// config", which is both true and recoverable, whereas throwing here
  /// would leave it on a blank screen with no way back. Never throws.
  Future<List<Backend>> loadBackends() async {
    final metadata = await _loadMetadata();
    if (metadata.isEmpty) {
      return metadata;
    }
    // A secure-storage read failure (a corrupted/locked Android Keystore, an
    // I/O error during an encrypted read — the kinds of platform failures
    // AGENTS.md section 8 lists) degrades to an empty secret, never a throw:
    // this method runs at startup, so a throw would leave the app with no way
    // to reach the settings screen and re-enter the key (see the module
    // comment's "Never throws" contract). The backend still loads, with an
    // empty secret the user can refill.
    final secrets = await Future.wait(metadata.map(_readSecret));
    return [
      for (var i = 0; i < metadata.length; i++)
        metadata[i].copyWith(secret: secrets[i] ?? ''),
    ];
  }

  /// Reads one backend's secret, treating a platform read failure as a
  /// missing one (null) — see [loadBackends]. A missing or unreadable secret
  /// both end up as the empty string in the returned [Backend].
  Future<String?> _readSecret(Backend backend) async {
    try {
      return await _secrets.read(_secretKey(backend));
    } catch (error) {
      debugPrint('settings: could not read the secret for "${backend.name}": $error');
      return null;
    }
  }

  /// Same as [loadBackends] but without the secret hydration — used both as
  /// the first half of [loadBackends] and, in [saveBackends], to find out
  /// which secrets a save is about to orphan without paying for a secure
  /// storage read per backend just to throw the value away.
  Future<List<Backend>> _loadMetadata() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_storageKey);
    if (raw == null || raw.isEmpty) {
      return const [];
    }

    Object? decoded;
    try {
      decoded = jsonDecode(raw);
    } catch (error) {
      debugPrint('settings: stored config is not JSON, ignoring: $error');
      return const [];
    }
    if (decoded is! List) {
      debugPrint('settings: stored config is not an array, ignoring');
      return const [];
    }

    return _normaliseStoredList(decoded);
  }

  /// Normalises a decoded JSON array into backends, dropping anything that
  /// cannot be a backend (a junk array member) or cannot be opened or
  /// labelled (no name, no base URL), and capping at [maxBackends].
  List<Backend> _normaliseStoredList(List<dynamic> parsed) {
    final backends = <Backend>[];
    for (final entry in parsed) {
      if (backends.length >= maxBackends) {
        break;
      }
      if (entry is! Map) {
        continue;
      }
      final backend = normalise(Map<String, dynamic>.from(entry));
      if (backend.name.isNotEmpty && backend.baseUrl.isNotEmpty) {
        backends.add(backend);
      }
    }
    return backends;
  }

  /// Persists [backends]: normalises and caps them exactly like [loadBackends]
  /// reads them, splits each one across shared_preferences (everything but
  /// the secret) and secure storage (the secret alone), and deletes the
  /// secure entry for any backend that is no longer present — covering both
  /// an explicit delete and a rename or base-URL edit, either of which
  /// changes the key a secret is filed under (see [_secretKey]). Returns the
  /// persisted, normalised list on success; a request that failed at the
  /// platform layer (a full disk, a locked Keystore) comes back as an [Err]
  /// rather than a partial, silently-inconsistent write.
  Future<Result<List<Backend>>> saveBackends(List<Backend> backends) async {
    final clean = _capAndFilter(backends);
    try {
      final previous = await _loadMetadata();
      await _persistMetadata(clean);
      await _persistSecrets(clean, previous);
    } catch (error) {
      return Err(Failure(kind: FailureKind.config, message: 'settings: could not save: $error'));
    }
    debugPrint('settings: saved ${clean.length} backend(s)');
    return Ok(clean);
  }

  List<Backend> _capAndFilter(List<Backend> backends) {
    final clean = <Backend>[];
    for (final backend in backends) {
      if (clean.length >= maxBackends) {
        break;
      }
      final normalised = normalise(backend._toMetadata()..['secret'] = backend.secret);
      if (normalised.name.isNotEmpty && normalised.baseUrl.isNotEmpty) {
        clean.add(normalised);
      }
    }
    return clean;
  }

  Future<void> _persistMetadata(List<Backend> clean) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_storageKey, jsonEncode(clean.map((b) => b._toMetadata()).toList()));
  }

  Future<void> _persistSecrets(List<Backend> clean, List<Backend> previous) async {
    for (final backend in clean) {
      await _secrets.write(_secretKey(backend), backend.secret);
    }
    final keepKeys = clean.map(_secretKey).toSet();
    for (final old in previous) {
      final oldKey = _secretKey(old);
      if (!keepKeys.contains(oldKey)) {
        await _secrets.delete(oldKey);
      }
    }
  }

  Future<int> count() async => (await loadBackends()).length;

  Future<Backend?> getBackend(int index) async {
    final backends = await loadBackends();
    if (index < 0 || index >= backends.length) {
      return null;
    }
    return backends[index];
  }

  /// The secure-storage key a backend's secret is filed under: derived from
  /// the backend's name and base URL, not from its position in the list. A
  /// list index is not stable across a delete (the entries after it shift
  /// down), so keying secrets by index would either strand a secret under
  /// the wrong backend or require re-keying every entry below the one
  /// removed on every save. Keying by content instead means a rename or a
  /// base-URL edit is, for secret-storage purposes, a new backend — its old
  /// secret is exactly what [_persistSecrets] cleans up as orphaned.
  static String _secretKey(Backend backend) =>
      '$_secretKeyPrefix${Uri.encodeComponent(backend.name)}|${Uri.encodeComponent(backend.baseUrl)}';

  static String _trim(dynamic value) {
    if (value == null) {
      return '';
    }
    final text = value.toString().trim();
    return text.length > maxFieldLength ? text.substring(0, maxFieldLength) : text;
  }
}
