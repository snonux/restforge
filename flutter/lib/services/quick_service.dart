/// Saved shortcuts to somewhere you go often.
///
/// This is the Dart port of `pebble/src/pkjs/quick.js` — see that file's
/// header for the full reasoning, which carries over unchanged. Browsing to
/// a deep action costs a lot of taps; a shortcut remembers a destination so
/// it can be reached from the opening screen instead. What it remembers is
/// deliberately not the whole answer:
///
///  - **An action is stored by name, with the address of the document that
///    offered it** — never the action's own href. Actions come and go as
///    server state changes, and their hrefs are the server's business.
///    Running one means re-fetching the holder and looking the name up
///    again, exactly as if the user had walked there by hand; if it is no
///    longer offered, that is a real answer, reported rather than hidden.
///  - **A backend is stored by base URL, not by position** — see
///    [backendFor]. Reordering the backend list (`settings_service.dart`)
///    must not silently re-point a saved shortcut at a different server.
///
/// **A shortcut is a shortcut through the *navigation*, not through the
/// *deciding*.** This file only stores and resolves — it never fetches a
/// document or invokes an action itself, exactly as `quick.js` has no
/// `runQuick` of its own: that composition lives one level up, in
/// `session.js` there and, on this port, in `session.dart` once a future
/// task (`x11`, per that file's module comment) wires it in. Running an
/// action shortcut, when that wiring exists, means re-fetching [QuickItem.holder]
/// and asking [QuickItem.name] of it through the ordinary
/// `SessionService.activate`/`ActionService.ask` confirmation flow — never a
/// shortcut that skips the confirmation a hand-reached action would get.
///
/// **The storage pattern is `h11`'s** (`settings_service.dart`): a single
/// JSON array under one `shared_preferences` key, decode-on-the-way-in with
/// anything that fails to parse or fails to be usable degrading to "no
/// shortcuts" rather than throwing at startup — the opening screen must
/// still come up even if this preference is corrupt. Nothing stored here is
/// a secret, so unlike a [Backend]'s API key, everything lives in
/// `shared_preferences`; there is no secure-storage half to this module.
library;

import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/failure.dart';
import '../models/result.dart';
import 'settings_service.dart';

/// What a shortcut points at. Mirrors `KIND_ACTION`/`KIND_DOCUMENT` in
/// `quick.js`, as a closed enum rather than a pair of string constants —
/// there is nothing dynamic about the set on this side of the port, so the
/// analyzer can hold callers to it exhaustively.
enum QuickKind { action, document }

/// One saved shortcut. A plain value with no behaviour beyond reading and
/// persisting itself — it would live under `lib/models/` if anything besides
/// [QuickService] needed it (see AGENTS.md section 4); today only this file
/// does, so it stays next to its one caller, exactly as [Backend] does in
/// `settings_service.dart`.
///
/// [holder] and [name] are what an action shortcut is looked up by;
/// [href] is what a document shortcut is fetched from — see the module
/// comment for why an action never stores an href of its own. [backendName]
/// is the backend's display name at the moment the shortcut was saved: kept
/// for schema parity with `quick.js` (which stores it the same way) even
/// though nothing on this port reads it back yet — [backendFor] resolves the
/// *current* name by [baseUrl] instead, precisely so a rename is reflected
/// rather than frozen at save time.
@immutable
class QuickItem {
  final String label;
  final String backendName;
  final String baseUrl;
  final QuickKind kind;
  final String holder;
  final String name;
  final String href;

  const QuickItem({
    required this.label,
    this.backendName = '',
    required this.baseUrl,
    required this.kind,
    this.holder = '',
    this.name = '',
    this.href = '',
  });

  /// The subset persisted to shared_preferences — everything, since none of
  /// it is a secret (see the module comment).
  Map<String, dynamic> _toJson() => {
    'label': label,
    'backendName': backendName,
    'baseUrl': baseUrl,
    'kind': kind == QuickKind.action ? 'action' : 'document',
    'holder': holder,
    'name': name,
    'href': href,
  };

  @override
  bool operator ==(Object other) =>
      other is QuickItem &&
      other.label == label &&
      other.backendName == backendName &&
      other.baseUrl == baseUrl &&
      other.kind == kind &&
      other.holder == holder &&
      other.name == name &&
      other.href == href;

  @override
  int get hashCode =>
      Object.hash(label, backendName, baseUrl, kind, holder, name, href);

  @override
  String toString() =>
      'QuickItem(label: $label, baseUrl: $baseUrl, kind: $kind, holder: $holder, name: $name, href: $href)';
}

class QuickService {
  QuickService({SettingsService? settings}) : _settings = settings ?? SettingsService();

  final SettingsService _settings;

  /// More than this and the opening screen stops being quicker than
  /// browsing. Mirrors `MAX_QUICK` in `quick.js`, bound-checked the same way
  /// [SettingsService.maxBackends] is.
  static const int maxQuick = 12;
  static const int maxFieldLength = 256;

  static const String _storageKey = 'restforge.quick';

  /// Coerces one stored or submitted map into the shape above. Does not
  /// reject anything — [_usable] does that. Storage that has drifted (an
  /// older layout, a hand-edited value) degrades to something usable instead
  /// of taking the app down at startup — same reasoning as
  /// [SettingsService.normalise].
  static QuickItem normalise(Map<String, dynamic>? raw) {
    final map = raw ?? const <String, dynamic>{};
    return QuickItem(
      label: _trim(map['label']),
      backendName: _trim(map['backendName']),
      baseUrl: _trim(map['baseUrl']),
      kind: map['kind'] == 'action' ? QuickKind.action : QuickKind.document,
      holder: _trim(map['holder']),
      name: _trim(map['name']),
      href: _trim(map['href']),
    );
  }

  /// Rejects a shortcut that could not be acted on. An action needs
  /// somewhere to look itself up next time; a document needs an address.
  /// Mirrors `usable()` in `quick.js`.
  static bool _usable(QuickItem item) {
    if (item.label.isEmpty || item.baseUrl.isEmpty) {
      return false;
    }
    return item.kind == QuickKind.action
        ? item.holder.isNotEmpty && item.name.isNotEmpty
        : item.href.isNotEmpty;
  }

  /// Whether [a] and [b] point at the same target — used by [add] to decide
  /// whether saving is an update rather than a new entry. Mirrors `same()`
  /// in `quick.js`.
  static bool _same(QuickItem a, QuickItem b) =>
      a.baseUrl == b.baseUrl &&
      a.kind == b.kind &&
      a.holder == b.holder &&
      a.name == b.name &&
      a.href == b.href;

  /// Returns the stored shortcuts, always normalised, usable and capped at
  /// [maxQuick]. Anything unparseable is reported and treated as empty — the
  /// opening screen simply shows no shortcuts, which is both true and
  /// recoverable. Never throws. Mirrors `load()` in `quick.js`.
  Future<List<QuickItem>> load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_storageKey);
    if (raw == null || raw.isEmpty) {
      return const [];
    }

    Object? decoded;
    try {
      decoded = jsonDecode(raw);
    } catch (error) {
      debugPrint('quick: stored shortcuts are not JSON, ignoring: $error');
      return const [];
    }
    if (decoded is! List) {
      debugPrint('quick: stored shortcuts are not an array, ignoring');
      return const [];
    }
    return _normaliseStoredList(decoded);
  }

  /// Normalises a decoded JSON array into shortcuts, dropping anything that
  /// cannot be a shortcut (a junk array member) or is not [_usable], and
  /// capping at [maxQuick]. Mirrors the loop in `load()`/`save()` in
  /// `quick.js`.
  List<QuickItem> _normaliseStoredList(List<dynamic> parsed) {
    final out = <QuickItem>[];
    for (final entry in parsed) {
      if (out.length >= maxQuick) {
        break;
      }
      if (entry is! Map) {
        continue;
      }
      final item = normalise(Map<String, dynamic>.from(entry));
      if (_usable(item)) {
        out.add(item);
      }
    }
    return out;
  }

  /// Persists [list]: normalises, drops anything unusable and caps at
  /// [maxQuick] exactly like [load] reads it back. Returns the persisted
  /// list on success; a platform-layer failure (a full disk) comes back as
  /// an [Err] rather than a partial, silently-inconsistent write — same
  /// contract as [SettingsService.saveBackends]. Mirrors `save()` in
  /// `quick.js`.
  Future<Result<List<QuickItem>>> save(List<QuickItem> list) async {
    final clean = _capAndFilter(list);
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_storageKey, jsonEncode(clean.map((i) => i._toJson()).toList()));
    } catch (error) {
      debugPrint('quick: could not save: $error');
      return Err(Failure(kind: FailureKind.config, message: 'quick: could not save: $error'));
    }
    return Ok(clean);
  }

  List<QuickItem> _capAndFilter(List<QuickItem> list) {
    final clean = <QuickItem>[];
    for (final item in list) {
      if (clean.length >= maxQuick) {
        break;
      }
      final normalised = normalise(item._toJson());
      if (_usable(normalised)) {
        clean.add(normalised);
      }
    }
    return clean;
  }

  Future<int> count() async => (await load()).length;

  Future<QuickItem?> get(int index) async {
    final list = await load();
    if (index < 0 || index >= list.length) {
      return null;
    }
    return list[index];
  }

  /// Appends a shortcut, replacing any identical one rather than
  /// accumulating duplicates — saving the same row twice is a natural thing
  /// to do and should be idempotent. Returns null, refusing to save, when
  /// [item] is incomplete ([_usable]) or the list is already at [maxQuick].
  /// Mirrors `add()` in `quick.js`.
  Future<QuickItem?> add(QuickItem item) async {
    final wanted = normalise(item._toJson());
    if (!_usable(wanted)) {
      debugPrint('quick: refusing to save an incomplete shortcut');
      return null;
    }

    final list = await load();
    final existing = list.indexWhere((stored) => _same(stored, wanted));
    if (existing != -1) {
      final updated = List<QuickItem>.of(list)..[existing] = wanted;
      await save(updated);
      return wanted;
    }

    if (list.length >= maxQuick) {
      debugPrint('quick: already holding $maxQuick shortcuts');
      return null;
    }
    await save([...list, wanted]);
    return wanted;
  }

  /// Drops the shortcut at [index]. Mirrors `remove()` in `quick.js`.
  Future<bool> remove(int index) async {
    final list = await load();
    if (index < 0 || index >= list.length) {
      return false;
    }
    final updated = List<QuickItem>.of(list)..removeAt(index);
    await save(updated);
    return true;
  }

  /// Resolves [item] against the configured backends, by base URL — see the
  /// module comment — so that renaming or reordering them does not re-point
  /// it somewhere else. Returns null when the backend it referred to is
  /// gone, which a caller reports rather than hides (see `quick.js`'s
  /// `rows()`, not ported here — row-shaping for a screen is that screen's
  /// job, same as `settings_service.dart` leaving its own `rows()` behind).
  /// Mirrors `backendFor()` in `quick.js`.
  Future<Backend?> backendFor(QuickItem item) async {
    final backends = await _settings.loadBackends();
    for (final backend in backends) {
      if (backend.baseUrl == item.baseUrl) {
        return backend;
      }
    }
    return null;
  }

  static String _trim(dynamic value) {
    if (value == null) {
      return '';
    }
    final text = value.toString().trim();
    return text.length > maxFieldLength ? text.substring(0, maxFieldLength) : text;
  }
}
