/// Turning a Siren entity into rows for the UI.
///
/// This is the Dart port of `pebble/src/pkjs/render.js` — see that file's
/// header and `pebble/docs/DESIGN.md` ("Rendering does not interpret") for
/// the reasoning this module exists to keep. A Siren entity has four
/// renderable parts, and [document] renders them in the order the
/// specification lists them — properties, sub-entities, links, actions.
/// Choosing a different order would mean deciding which part of somebody
/// else's document matters most, which is exactly the knowledge this app is
/// built not to have.
///
/// Two things this module deliberately does not do:
///
///  - It does not interpret a property. A value is shown as the server sent
///    it, so three states never collapse into two — see [text]. A client
///    that renders `ping: false` as "off" is asserting something the server
///    did not say.
///  - It does not hide anything. Every link, action and property is
///    offered, including the ones this app has no idea about, because "I do
///    not recognise this" is not a reason to withhold it from the person
///    using it.
///
/// Each [Row] carries a [RowTarget], which is what a caller does when the
/// row is activated. The UI layer reads a row's label, sublabel and kind,
/// and hands the target back to whichever service knows what it means — it
/// never inspects the target's internals itself.
///
/// Rendering a well-formed [Entity] cannot fail, so [document] returns a
/// [RenderedDocument] directly rather than a `Result` — `Result` is
/// reserved for operations with an actual failure mode (see AGENTS.md
/// section 5, "Error model"), and there is not one here.
library;

import 'dart:convert';

import '../models/siren.dart';

/// How many of an embedded entity's properties to summarise on its row.
/// Carried over from render.js's SUMMARY_PROPERTIES: two fit on the watch's
/// row at a glance, and the value is kept here too so an embedded entity
/// renders identically on both ports rather than the number quietly
/// drifting apart. The whole entity is one row-press away regardless.
const int _summaryProperties = 2;

/// What kind of Siren part a [Row] renders. Mirrors the KIND_* constants in
/// render.js (which matched `DocRowKind` in the watch's `src/c/doc.h`); the
/// watch/phone row-index protocol they served does not carry over here (see
/// flutter/AGENTS.md section 4, "what does not carry over"), so this is a
/// plain enum rather than a wire-format string.
enum RowKind { property, entity, link, action }

/// What activating a [Row] does. A sealed hierarchy rather than a
/// loosely-typed bag of fields, so a caller `switch`es exhaustively over the
/// possibilities the analyzer knows about instead of checking a `type`
/// string that could silently fail to match at runtime. The UI layer never
/// reads inside one of these — it hands the whole [RowTarget] to the service
/// that knows what to do with it.
sealed class RowTarget {
  const RowTarget();
}

/// Open the full value in a reading view. Carries the full value, not the
/// (possibly truncated) [Row.sublabel]: this target exists for values too
/// long to read on the row itself.
class DetailTarget extends RowTarget {
  final String heading;
  final String body;

  const DetailTarget({required this.heading, required this.body});
}

/// Follow an href — a link, or a sub-entity that is only a reference.
class FetchTarget extends RowTarget {
  final String href;

  const FetchTarget(this.href);
}

/// Open a sub-entity already embedded in the document in hand. Opening it
/// costs nothing and asks the server nothing, unlike [FetchTarget] — Siren
/// allows a sub-entity to be either, and the difference is invisible past
/// this point.
class EmbeddedTarget extends RowTarget {
  final int index;

  const EmbeddedTarget(this.index);
}

/// Perform a named action.
class ActionTarget extends RowTarget {
  final String name;

  const ActionTarget(this.name);
}

/// One row of a [RenderedDocument]. A plain value with no behaviour beyond
/// reading itself — it would live under `lib/models/` if anything besides
/// this file's [document] produced it (see flutter/AGENTS.md section 4);
/// today only rendering does, so it stays here rather than adding a file
/// for its own sake.
class Row {
  final String label;
  final String sublabel;
  final RowKind kind;
  final RowTarget target;

  const Row({
    required this.label,
    required this.sublabel,
    required this.kind,
    required this.target,
  });
}

/// An [Entity] turned into something a screen can list: a title and its
/// rows, in Siren's own order.
class RenderedDocument {
  final String title;
  final List<Row> rows;

  const RenderedDocument({required this.title, this.rows = const []});
}

/// Renders one JSON value for display. Objects and arrays are stringified
/// rather than summarised: a nested structure is rare in a property, and
/// showing its JSON is at least true, where a bare [Object.toString] is not.
///
/// Exposed (not just used internally) because `pebble/src/pkjs/actions.js`
/// reuses `render.text` the same way when it echoes a filled-in field value
/// back for confirmation — the Dart port of that pipeline will want the same
/// function rather than a second copy of it.
String text(dynamic value) {
  if (value == null) {
    return 'null';
  }
  if (value is Map || value is List) {
    try {
      return jsonEncode(value);
    } catch (_) {
      return '(unreadable)';
    }
  }
  return value.toString();
}

/// A one-line, generic summary of every property on [entity] — `key: value`
/// pairs joined with wide spaces, each value through [text] so a number, a
/// list or a nested object renders the same way it does in the document
/// itself. This is the body of an action-outcome notice (the message is the
/// server's `state`; this is the rest of what it sent), kept here rather than
/// in `session.dart` because rendering is this module's job, not a
/// coordinator's (`pebble/docs/DESIGN.md`, "Rendering does not interpret") —
/// and because `session.dart` used to have a second, bespoke property-dump
/// format for this, which is the kind of duplication that drifts. Mirrors
/// `describeEntity()` in `actions.js`.
String describe(Entity entity) {
  final parts = <String>[];
  entity.properties.forEach((key, value) {
    parts.add('$key: ${text(value)}');
  });
  return parts.join('   ');
}

List<Row> _propertyRows(Entity entity) {
  final rows = <Row>[];
  entity.properties.forEach((key, value) {
    final rendered = text(value);
    rows.add(Row(
      label: key,
      sublabel: rendered,
      kind: RowKind.property,
      // The full value, not the truncated sublabel: activating the row
      // opens it in the reading view, which exists for values this long.
      target: DetailTarget(heading: key, body: rendered),
    ));
  });
  return rows;
}

/// Builds the second line of a sub-entity's row from its first few
/// properties, in document order. Which properties matter is the server's
/// business; taking the first two is a presentation decision, not a
/// semantic one, and the whole entity is one row-press away.
///
/// The property already used as the row's label ([skipKey]) is skipped: on
/// a screen this size, spending one of two summary slots repeating the
/// heading is a waste of the only two facts the row can carry.
String _summarise(Entity entity, String? skipKey) {
  final parts = <String>[];
  for (final entry in entity.properties.entries) {
    if (entry.key == skipKey) {
      continue;
    }
    if (parts.length >= _summaryProperties) {
      break;
    }
    parts.add('${entry.key} ${text(entry.value)}');
  }
  return parts.join('  ');
}

List<Row> _entityRows(Entity entity) {
  final rows = <Row>[];
  for (var i = 0; i < entity.entities.length; i++) {
    final child = entity.entities[i];
    // When the label came from a property, that property is redundant in
    // the summary below it. When the label came from the title instead,
    // nothing needs to be skipped.
    final skipKey =
        (child.title != null && child.title!.isNotEmpty) ? null : child.identifier;
    final label = child.label.isNotEmpty ? child.label : 'entity ${i + 1}';
    rows.add(Row(
      label: label,
      sublabel: child.isReference ? '' : _summarise(child, skipKey),
      kind: RowKind.entity,
      // An embedded entity is already in hand, so opening it costs nothing
      // and asks the server nothing. A reference is only an href, so it has
      // to be fetched — Siren allows either, and the difference is
      // invisible here.
      target: child.isReference ? FetchTarget(child.href ?? '') : EmbeddedTarget(i),
    ));
  }
  return rows;
}

List<Row> _linkRows(Entity entity) {
  final rows = <Row>[];
  for (final link in entity.links) {
    final rels = link.rel.join(' ');
    final title = link.title ?? '';
    rows.add(Row(
      label: title.isNotEmpty ? title : (rels.isNotEmpty ? rels : 'link'),
      sublabel: title.isNotEmpty ? rels : '',
      kind: RowKind.link,
      target: FetchTarget(link.href),
    ));
  }
  return rows;
}

List<Row> _actionRows(Entity entity) {
  final rows = <Row>[];
  for (final action in entity.actions) {
    final fieldCount = action.fields.length;
    // The title is the server's own wording for what this does, and it is
    // always preferred: the name is an identifier, the title is a sentence
    // written for a person. [Action.label] already applies that order.
    rows.add(Row(
      label: action.label,
      sublabel: fieldCount > 0 ? '${action.method}, $fieldCount field(s)' : action.method,
      kind: RowKind.action,
      target: ActionTarget(action.name),
    ));
  }
  return rows;
}

/// Turns [entity] into the rows a screen lists, plus the row targets a
/// caller needs to act on a press. [fallbackTitle] is shown when the
/// document has no wording of its own — see [Entity.label].
///
/// [entity] is nullable so a caller holding "nothing fetched yet" can render
/// an empty screen without a special case of its own, mirroring
/// `render.document(null, fallbackTitle)` in render.js, which the same way
/// never throws on a missing document.
RenderedDocument document(Entity? entity, [String fallbackTitle = '']) {
  if (entity == null) {
    return RenderedDocument(title: fallbackTitle, rows: const []);
  }
  final rows = [
    ..._propertyRows(entity),
    ..._entityRows(entity),
    ..._linkRows(entity),
    ..._actionRows(entity),
  ];
  final title = entity.label.isNotEmpty ? entity.label : fallbackTitle;
  return RenderedDocument(title: title, rows: rows);
}
