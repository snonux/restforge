/// The Siren hypermedia document model, and the generic lookups a screen
/// uses to read one.
///
/// This is the Dart port of `pebble/src/pkjs/siren.js`; see that file's
/// header and `pebble/docs/DESIGN.md` ("The rule everything else follows
/// from") for the reasoning. Everything here is generic by construction:
/// there is no rel, no class, no action name and no property name written
/// down in this file, and there must never be one — a client that knows a
/// server's vocabulary has to be updated when the server changes, which is
/// the failure hypermedia exists to avoid. `just check`'s genericity grep
/// enforces this on every commit.
///
/// The contract these types exist to keep is that a caller locates things
/// by rel and by name, uses the href and method the server offered, and
/// treats an unknown class, rel, name or field type as ordinary — not as an
/// error. So every lookup below answers "not offered" (null, or an empty
/// list) rather than throwing, and every parser tolerates a missing or
/// wrongly-typed member: a document missing an optional part is not
/// malformed, and a malformed one is never allowed to crash the caller. See
/// [parseSirenDocument] for the one place actual JSON text is decoded — the
/// only case where "not usable" is reported as a [Failure] rather than as
/// an empty field.
library;

import 'dart:convert';

import 'failure.dart';
import 'result.dart';

/// The highest apiVersion this client was written against. A server
/// carrying a higher one may have changed something this client would
/// silently misread, so [Entity.versionProblem] stops and says so instead
/// of guessing. This is a plain property check — nothing about it is
/// specific to any API.
const int supportedApiVersion = 1;

/// Properties that identify *which* thing an entity is, tried in order when
/// the document carries no wording of its own (no title).
///
/// This is a convention of the Siren format rather than knowledge of any
/// particular server: Siren itself uses "name" to identify actions and
/// fields, and "name"/"id" are what hypermedia APIs conventionally call the
/// identifier of an entity.
///
/// It earns its place empirically. A collection whose members share one
/// class renders as a list of identical rows without it, and a list where
/// every row reads the same is not a list. What a caller still must not do
/// is interpret the value: it is displayed exactly as sent, and no meaning
/// is attached to it beyond "this is what it is called".
const List<String> identifyingProperties = ['name', 'title', 'id'];

// ---------------------------------------------------------------------------
// Tolerant JSON coercion. Every one of these accepts whatever a lenient or
// broken server sent and returns the "nothing offered" shape instead of
// throwing — mirrors isObject/isArray/array() in siren.js.
// ---------------------------------------------------------------------------

Map<String, dynamic> _asMap(dynamic value) =>
    value is Map<String, dynamic> ? value : const {};

List<dynamic> _asList(dynamic value) => value is List ? value : const [];

/// Coerces a Siren `rel`/`class` member to a list of strings. Siren defines
/// both as arrays of strings, but a lenient server may send a bare string;
/// a non-string entry inside a list simply never matches a lookup, exactly
/// as it would not in `has()` in siren.js.
List<String> _asStringList(dynamic value) {
  if (value is String) return [value];
  if (value is List) return value.whereType<String>().toList(growable: false);
  return const [];
}

String? _asNonEmptyString(dynamic value) =>
    value is String && value.isNotEmpty ? value : null;

String? _identifierKey(Map<String, dynamic> properties) {
  for (final key in identifyingProperties) {
    final value = properties[key];
    if ((value is String && value.isNotEmpty) || value is num) {
      return key;
    }
  }
  return null;
}

/// The label logic shared by [Entity.label], [Action.label], [Link.label]
/// and [Field.label] — mirrors label() in siren.js. Preference order is the
/// server's own wording first (title), then the name Siren gives an action
/// or field, then the node's own identifying property, then its vocabulary
/// (class, then rel), then nothing. A caller never invents a name for a
/// thing it does not understand, because an invented name is a claim about
/// what the thing is.
String _label({
  String? title,
  String? name,
  Map<String, dynamic>? properties,
  List<String> classes = const [],
  List<String> rel = const [],
}) {
  if (title != null && title.isNotEmpty) return title;
  if (name != null && name.isNotEmpty) return name;
  if (properties != null) {
    final key = _identifierKey(properties);
    if (key != null) return properties[key].toString();
  }
  if (classes.isNotEmpty) return classes.join(' ');
  if (rel.isNotEmpty) return rel.join(' ');
  return '';
}

/// A field an action's form offers to fill in.
///
/// Kept deliberately thin: siren.js never inspects a field's `type` or
/// `value` either, beyond counting fields — how a field is rendered and
/// validated is a concern of the render/action pipeline, not of this
/// document model.
class Field {
  final String name;
  final String? type;
  final dynamic value;
  final String? title;

  const Field({required this.name, this.type, this.value, this.title});

  factory Field.fromJson(dynamic json) {
    final map = _asMap(json);
    return Field(
      name: _asNonEmptyString(map['name']) ?? '',
      type: _asNonEmptyString(map['type']),
      value: map['value'],
      title: _asNonEmptyString(map['title']),
    );
  }

  /// What to show for this field on screen.
  String get label => _label(title: title, name: name);
}

/// A hypermedia link: a rel, an href to follow it to, and nothing this
/// client interprets beyond that.
class Link {
  final List<String> rel;

  /// '' when the server sent none. Such a link cannot be followed, so
  /// [Entity.linkByRel] and [Entity.follow] never match it — mirrors
  /// siren.js's `link()`, which only matches list entries with a string
  /// href.
  final String href;
  final List<String> classes;
  final String? title;
  final String? type;

  const Link({
    required this.rel,
    required this.href,
    this.classes = const [],
    this.title,
    this.type,
  });

  factory Link.fromJson(dynamic json) {
    final map = _asMap(json);
    return Link(
      rel: _asStringList(map['rel']),
      href: _asNonEmptyString(map['href']) ?? '',
      classes: _asStringList(map['class']),
      title: _asNonEmptyString(map['title']),
      type: _asNonEmptyString(map['type']),
    );
  }

  String get label => _label(title: title, classes: classes, rel: rel);
}

/// An action a server is currently offering: a method and href to send a
/// request to, and the fields its form asks for.
class Action {
  final String name;

  /// '' when the server sent none. An action with no href cannot be acted
  /// on; a caller checking for one before offering the action stays as
  /// generic as everything else here.
  final String href;

  /// Always uppercase; defaults to Siren's own default, `GET`, when the
  /// server did not say — mirrors method() in siren.js. A server that means
  /// to change something says so explicitly.
  final String method;
  final List<String> classes;
  final String? title;
  final String? type;
  final List<Field> fields;

  const Action({
    required this.name,
    required this.href,
    required this.method,
    this.classes = const [],
    this.title,
    this.type,
    this.fields = const [],
  });

  factory Action.fromJson(dynamic json) {
    final map = _asMap(json);
    final rawMethod = map['method'];
    return Action(
      name: _asNonEmptyString(map['name']) ?? '',
      href: _asNonEmptyString(map['href']) ?? '',
      method: rawMethod is String && rawMethod.isNotEmpty
          ? rawMethod.toUpperCase()
          : 'GET',
      classes: _asStringList(map['class']),
      title: _asNonEmptyString(map['title']),
      type: _asNonEmptyString(map['type']),
      fields: _asList(map['fields']).map(Field.fromJson).toList(growable: false),
    );
  }

  String get label => _label(title: title, name: name, classes: classes);
}

/// A Siren entity: a document in its own right when it is the root, or a
/// sub-entity when it is nested inside another entity's `entities`.
///
/// Siren allows a sub-entity to be either an embedded representation (its
/// own properties, sub-entities, links and actions) or a bare reference to
/// one (just a class, a rel to its parent, and an href) — see
/// [isReference]. Every list and map here defaults to empty rather than
/// null so a caller never has to null-check before reading one: an absent
/// member and an empty one mean the same thing everywhere in this model.
class Entity {
  final List<String> classes;
  final Map<String, dynamic> properties;

  /// Sub-entities, each of which may itself be [isReference].
  final List<Entity> entities;
  final List<Link> links;
  final List<Action> actions;
  final String? title;

  /// This entity's relation to its parent. Only meaningful when this
  /// [Entity] is a sub-entity; empty for a root document.
  final List<String> rel;

  /// Present when this entity carries an href — always true for a
  /// reference sub-entity, optionally present on an embedded one too.
  final String? href;

  /// True when this sub-entity is only a reference — a bare href — rather
  /// than an embedded representation. Mirrors isReference() in siren.js: an
  /// entity counts as embedded, not a reference, the moment it carries a
  /// `properties` object or an `entities` array of its own, even an empty
  /// one — the server chose to say "nothing here" rather than "look
  /// elsewhere", and that distinction is not this client's to erase.
  final bool isReference;

  const Entity({
    this.classes = const [],
    this.properties = const {},
    this.entities = const [],
    this.links = const [],
    this.actions = const [],
    this.title,
    this.rel = const [],
    this.href,
    this.isReference = false,
  });

  factory Entity.fromJson(dynamic json) {
    final map = _asMap(json);
    final rawProperties = map['properties'];
    final rawEntities = map['entities'];
    final href = _asNonEmptyString(map['href']);
    return Entity(
      classes: _asStringList(map['class']),
      properties: _asMap(rawProperties),
      entities:
          _asList(rawEntities).map(Entity.fromJson).toList(growable: false),
      links: _asList(map['links']).map(Link.fromJson).toList(growable: false),
      actions:
          _asList(map['actions']).map(Action.fromJson).toList(growable: false),
      title: _asNonEmptyString(map['title']),
      rel: _asStringList(map['rel']),
      href: href,
      isReference:
          href != null && rawProperties is! Map && rawEntities is! List,
    );
  }

  /// Finds a link by rel. Null when there is none — never a thrown error,
  /// because a rel being absent is the server declining to offer
  /// something, not a malformed document. A link is matched by any of its
  /// rels, not just the first, because which one a server puts first is
  /// not part of the contract.
  Link? linkByRel(String rel) {
    for (final candidate in links) {
      if (candidate.rel.contains(rel) && candidate.href.isNotEmpty) {
        return candidate;
      }
    }
    return null;
  }

  /// The href of the link with this rel, or null.
  String? follow(String rel) => linkByRel(rel)?.href;

  /// Finds an offered action by name. Null means "not available right
  /// now", which is a legitimate answer and not something to route around.
  Action? actionByName(String name) {
    for (final candidate in actions) {
      if (candidate.name == name) return candidate;
    }
    return null;
  }

  /// The property key that names this entity, or null. Exposed so a
  /// caller can avoid printing the same value twice — once as the label
  /// and again in the summary underneath it.
  String? get identifier => _identifierKey(properties);

  /// What to put on screen for this entity. See [_label] for the
  /// preference order.
  String get label =>
      _label(title: title, properties: properties, classes: classes, rel: rel);

  /// A message when the server speaks a version this client was not
  /// written against, or null to proceed. A missing or non-numeric
  /// `apiVersion` proceeds: a server that does not declare one is not
  /// making a claim this client can act on, and refusing to proceed would
  /// break it against every server that never had the field.
  String? get versionProblem {
    final version = properties['apiVersion'];
    if (version is! num) return null;
    if (version > supportedApiVersion) {
      return 'server speaks apiVersion $version, this app understands '
          '$supportedApiVersion';
    }
    return null;
  }

  /// A one-line description of this document, for logging. It counts what
  /// is there rather than naming it, so it stays useful against a server
  /// this client has never seen.
  String get summary =>
      'class [${classes.join(' ')}] ${links.length} link(s), '
      '${actions.length} action(s), ${entities.length} entity(ies), '
      '${properties.length} propertie(s)';
}

/// Parses a Siren document from an HTTP response body.
///
/// Never throws. Invalid JSON syntax and a body whose top level is not a
/// JSON object both come back as [Err] with [FailureKind.parse] — a
/// document the caller can render a reason for, not an exception unwinding
/// past the last good document still on screen (see AGENTS.md section 5,
/// "Error model"). A well-formed object missing links, actions,
/// sub-entities or properties is not a failure; [Entity.fromJson] treats
/// those exactly like every other accessor above — as simply absent.
Result<Entity> parseSirenDocument(String body) {
  final dynamic decoded;
  try {
    decoded = jsonDecode(body);
  } on FormatException catch (error) {
    return Err(Failure(
      kind: FailureKind.parse,
      message: 'invalid JSON: ${error.message}',
    ));
  }
  if (decoded is! Map) {
    return Err(Failure(
      kind: FailureKind.parse,
      message: 'expected a JSON object, got ${decoded.runtimeType}',
    ));
  }
  return Ok(Entity.fromJson(decoded));
}
