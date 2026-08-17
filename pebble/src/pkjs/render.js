/* Turning a Siren document into rows for the watch.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * A Siren entity has four renderable parts, and they are rendered in the order
 * the specification lists them -- properties, sub-entities, links, actions.
 * Choosing a different order would mean deciding which parts of somebody
 * else's document matter most, which is exactly the kind of knowledge this app
 * is built not to have.
 *
 * Two things this module deliberately does not do:
 *
 *  - It does not interpret a property.  A value is shown as the server sent
 *    it, so three states never collapse into two.  A client that renders
 *    "ping: false" as "off" is asserting something the server did not say.
 *  - It does not hide anything.  Every link, action and property is offered,
 *    including the ones this app has no idea about, because "I do not
 *    recognise this" is not a reason to withhold it from the person wearing
 *    the watch.
 *
 * Each row carries a target, which is what session.js does when the row is
 * activated.  The target never leaves the phone: the watch receives the label
 * and the kind, and sends back an index.
 */

'use strict';

var siren = require('./siren');

/* How many of an embedded entity's properties to summarise on its row.  Two
 * fit on a line at the size these are drawn; more would be ellipsised away. */
var SUMMARY_PROPERTIES = 2;

/* Row kinds, matching DocRowKind in src/c/doc.h. */
var KIND_PROPERTY = 'p';
var KIND_ENTITY = 'e';
var KIND_LINK = 'l';
var KIND_ACTION = 'a';

/* text renders one JSON value for display.  Objects and arrays are stringified
 * rather than summarised: a nested structure is rare in a property, and
 * showing its JSON is at least true, where "[object Object]" is not. */
function text(value) {
  if (value === null) {
    return 'null';
  }
  if (value === undefined) {
    return '';
  }
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value);
    } catch (error) {
      return '(unreadable)';
    }
  }
  return String(value);
}

function propertyRows(entity) {
  var properties = siren.properties(entity);
  var rows = [];
  for (var key in properties) {
    if (Object.prototype.hasOwnProperty.call(properties, key)) {
      rows.push({
        label: key,
        sublabel: text(properties[key]),
        kind: KIND_PROPERTY,
        /* The full value, not the truncated sublabel: activating the row opens
         * it in the reading window, which exists for values this long. */
        target: { type: 'detail', heading: key, body: text(properties[key]) }
      });
    }
  }
  return rows;
}

/* summarise builds the second line of a sub-entity's row from its first few
 * properties, in document order.  Which properties matter is the server's
 * business; taking the first two is a presentation decision, not a semantic
 * one, and the whole entity is one press away.
 *
 * The property already used as the row's label is skipped: on a screen this
 * size, spending one of two summary slots repeating the heading is a waste of
 * the only two facts the row can carry. */
function summarise(entity, skipKey) {
  var properties = siren.properties(entity);
  var parts = [];
  for (var key in properties) {
    if (Object.prototype.hasOwnProperty.call(properties, key) && key !== skipKey) {
      if (parts.length >= SUMMARY_PROPERTIES) {
        break;
      }
      parts.push(key + ' ' + text(properties[key]));
    }
  }
  return parts.join('  ');
}

function entityRows(entity) {
  var children = siren.subEntities(entity);
  var rows = [];
  for (var i = 0; i < children.length; i++) {
    var child = children[i];
    var reference = siren.isReference(child);
    /* When the label came from a property, that property is redundant below. */
    var named = child && typeof child.title === 'string' && child.title
      ? null : siren.identifier(child);
    rows.push({
      label: siren.label(child) || 'entity ' + (i + 1),
      sublabel: reference ? '' : summarise(child, named),
      kind: KIND_ENTITY,
      /* An embedded entity is already in hand, so opening it costs nothing and
       * asks the server nothing.  A reference is only an href, so it has to be
       * fetched -- Siren allows either, and the difference is invisible here. */
      target: reference
        ? { type: 'fetch', href: child.href }
        : { type: 'embedded', index: i }
    });
  }
  return rows;
}

function linkRows(entity) {
  var links = siren.links(entity);
  var rows = [];
  for (var i = 0; i < links.length; i++) {
    var rels = [].concat(links[i].rel || []).join(' ');
    var title = typeof links[i].title === 'string' ? links[i].title : '';
    rows.push({
      label: title || rels || 'link',
      sublabel: title ? rels : '',
      kind: KIND_LINK,
      target: { type: 'fetch', href: links[i].href }
    });
  }
  return rows;
}

function actionRows(entity) {
  var actions = siren.actions(entity);
  var rows = [];
  for (var i = 0; i < actions.length; i++) {
    var action = actions[i];
    var fields = siren.fields(action).length;
    /* The title is the server's own wording for what this does, and it is
     * always preferred: the name is an identifier, the title is a sentence
     * written for a person. */
    rows.push({
      label: siren.label(action),
      sublabel: siren.method(action) + (fields ? ', ' + fields + ' field(s)' : ''),
      kind: KIND_ACTION,
      target: { type: 'action', name: action.name }
    });
  }
  return rows;
}

/* document turns an entity into the frame the watch draws, plus the row
 * targets session.js needs to act on a press. */
function document(entity, fallbackTitle) {
  var rows = propertyRows(entity)
    .concat(entityRows(entity))
    .concat(linkRows(entity))
    .concat(actionRows(entity));

  return {
    title: siren.label(entity) || fallbackTitle || '',
    rows: rows
  };
}

module.exports = {
  KIND_PROPERTY: KIND_PROPERTY,
  KIND_ENTITY: KIND_ENTITY,
  KIND_LINK: KIND_LINK,
  KIND_ACTION: KIND_ACTION,
  text: text,
  document: document
};
