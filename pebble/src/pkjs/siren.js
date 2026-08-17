/* Siren helpers.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * Everything here is generic by construction.  There is no rel, no class, no
 * action name and no property name written down in this file, and there must
 * never be: a client that knows a server's vocabulary has to be updated when
 * the server changes, which is the failure hypermedia exists to avoid.
 *
 * The contract these helpers exist to keep is that a client locates things by
 * rel and by name, uses the href and method it was given, and treats an
 * unknown class, rel, name or field type as ordinary -- not as an error.  So
 * every lookup below returns null for "not offered" rather than throwing, and
 * every accessor tolerates a missing or wrongly-typed member.  "Not offered"
 * is a normal answer: it usually means the server has decided this cannot be
 * done right now, and the reason is visible elsewhere in the document.
 */

'use strict';

/* The highest apiVersion this client was written against.  A server carrying a
 * higher one may have changed something we would silently misread, so we stop
 * and say so instead of guessing.  This is a plain property check -- nothing
 * about it is specific to any API. */
var SUPPORTED_API_VERSION = 1;

function isObject(value) {
  return value !== null && typeof value === 'object' &&
         Object.prototype.toString.call(value) !== '[object Array]';
}

function isArray(value) {
  return Object.prototype.toString.call(value) === '[object Array]';
}

/* array returns a member that should be a list, as a list.  A server is free
 * to omit it; a broken one might send something else entirely. */
function array(entity, member) {
  return entity && isArray(entity[member]) ? entity[member] : [];
}

function properties(entity) {
  return entity && isObject(entity.properties) ? entity.properties : {};
}

function classes(entity) {
  return array(entity, 'class');
}

function links(entity) {
  return array(entity, 'links');
}

function actions(entity) {
  return array(entity, 'actions');
}

function subEntities(entity) {
  return array(entity, 'entities');
}

function fields(action) {
  return array(action, 'fields');
}

/* has tests membership in a rel or class list.  Both are arrays of strings in
 * Siren, but a lenient server may send a bare string. */
function has(list, value) {
  if (typeof list === 'string') {
    return list === value;
  }
  if (!isArray(list)) {
    return false;
  }
  for (var i = 0; i < list.length; i++) {
    if (list[i] === value) {
      return true;
    }
  }
  return false;
}

/* link finds a link by rel.  Returns null when there is none -- never throws,
 * because a rel being absent is the server declining to offer something, not a
 * malformed document. */
function link(entity, rel) {
  var list = links(entity);
  for (var i = 0; i < list.length; i++) {
    if (has(list[i].rel, rel) && typeof list[i].href === 'string') {
      return list[i];
    }
  }
  return null;
}

/* follow returns the href of the link with this rel, or null. */
function follow(entity, rel) {
  var found = link(entity, rel);
  return found ? found.href : null;
}

/* action finds an offered action by name.  Absent means "not available right
 * now", which is a legitimate answer and not something to route around. */
function action(entity, name) {
  var list = actions(entity);
  for (var i = 0; i < list.length; i++) {
    if (list[i].name === name) {
      return list[i];
    }
  }
  return null;
}

/* method returns the HTTP method an action asks for.  Siren's default is GET;
 * servers that mean to change something say so explicitly. */
function method(action) {
  return action && typeof action.method === 'string'
    ? action.method.toUpperCase() : 'GET';
}

/* An entity that is only a link -- Siren allows a sub-entity to be either an
 * embedded representation or a reference to one. */
function isReference(entity) {
  return !!(entity && typeof entity.href === 'string' &&
            !isObject(entity.properties) && !isArray(entity.entities));
}

/* Properties that identify *which* thing this is, tried in order when the
 * document carries no wording of its own.
 *
 * This is a convention of the format rather than knowledge of any server:
 * Siren itself uses "name" to identify actions and fields, and "name"/"id" are
 * what hypermedia APIs conventionally call the identifier of an entity.
 *
 * It earns its place empirically.  A collection whose members share one class
 * -- four hosts all classed ["host"], say -- renders as four identical rows
 * without it, and a list where every row reads the same is not a list.  What
 * the app still does not do is interpret the value: it is displayed exactly as
 * sent, and no meaning is attached to it beyond "this is what it is called". */
var IDENTIFYING_PROPERTIES = ['name', 'title', 'id'];

/* identifier returns the property key that names this entity, or null.
 * Exposed so a caller can avoid printing the same value twice -- once as the
 * label and again in the summary underneath it. */
function identifier(node) {
  var values = properties(node);
  for (var i = 0; i < IDENTIFYING_PROPERTIES.length; i++) {
    var key = IDENTIFYING_PROPERTIES[i];
    var value = values[key];
    if ((typeof value === 'string' && value) || typeof value === 'number') {
      return key;
    }
  }
  return null;
}

/* label is what to put on screen for something.  Preference order is the
 * server's own wording first (title), then the name Siren gives an action or
 * field, then the entity's own identifying property, then its vocabulary
 * (class or rel), then nothing -- the app never invents a name for a thing it
 * does not understand, because an invented name is a claim about what it is. */
function label(node) {
  if (!node) {
    return '';
  }
  if (typeof node.title === 'string' && node.title) {
    return node.title;
  }
  if (typeof node.name === 'string' && node.name) {
    return node.name;
  }
  var key = identifier(node);
  if (key) {
    return String(properties(node)[key]);
  }
  var vocabulary = classes(node);
  if (vocabulary.length) {
    return vocabulary.join(' ');
  }
  if (isArray(node.rel) && node.rel.length) {
    return node.rel.join(' ');
  }
  return '';
}

/* versionProblem returns a message when the server speaks a version this
 * client was not written against, or null to proceed.  A missing or
 * non-numeric apiVersion proceeds: a server that does not declare one is not
 * making a claim we can act on, and erroring would break clients against
 * servers that never had the field. */
function versionProblem(entity) {
  var version = properties(entity).apiVersion;
  if (typeof version !== 'number') {
    return null;
  }
  if (version > SUPPORTED_API_VERSION) {
    return 'server speaks apiVersion ' + version + ', this app understands ' +
           SUPPORTED_API_VERSION;
  }
  return null;
}

/* summary is a one-line description of a document, for the log.  It counts
 * what is there rather than naming it, so it stays useful against a server
 * this app has never seen. */
function summary(entity) {
  return 'class [' + classes(entity).join(' ') + '] ' +
         links(entity).length + ' link(s), ' +
         actions(entity).length + ' action(s), ' +
         subEntities(entity).length + ' entity(ies), ' +
         Object.keys(properties(entity)).length + ' propertie(s)';
}

module.exports = {
  SUPPORTED_API_VERSION: SUPPORTED_API_VERSION,
  properties: properties,
  classes: classes,
  links: links,
  actions: actions,
  subEntities: subEntities,
  fields: fields,
  has: has,
  link: link,
  follow: follow,
  action: action,
  method: method,
  isReference: isReference,
  identifier: identifier,
  label: label,
  versionProblem: versionProblem,
  summary: summary
};
