/* Saved shortcuts to somewhere you go often.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * Browsing to a deep action costs a lot of button presses -- on the homelab's
 * root, the last action is fifteen rows down. A quick item remembers a
 * destination so it can be reached from the opening screen instead.
 *
 * What it remembers is deliberately not the whole answer:
 *
 *  - **An action is stored by name, with the address of the document that
 *    offered it** -- never the action's own href. Actions come and go as
 *    server state changes, and their hrefs are the server's business. Running
 *    one means re-fetching the holder and looking the name up again, exactly
 *    as if you had walked there; if it is no longer offered, that is a real
 *    answer and it is reported.
 *  - **A backend is stored by base URL, not by position.** Reordering the
 *    backend list must not silently re-point a saved shortcut at a different
 *    server.
 *
 * A shortcut is a shortcut through the *navigation*, not through the
 * deciding: an action reached this way still goes through the same
 * confirmation as one reached by hand.
 */

'use strict';

var settings = require('./settings');

var STORAGE_KEY = 'restforge.quick';

/* More than this and the opening screen stops being quicker than browsing. */
var MAX_QUICK = 12;

var KIND_ACTION = 'action';
var KIND_DOCUMENT = 'document';

function trim(value) {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value).replace(/^\s+|\s+$/g, '').slice(0, 256);
}

/* normalise coerces one stored item into the shape above.  Anything that has
 * drifted degrades to something usable rather than throwing during startup --
 * the same reasoning as settings.js. */
function normalise(raw) {
  var item = raw && typeof raw === 'object' ? raw : {};
  return {
    label: trim(item.label),
    backendName: trim(item.backendName),
    baseUrl: trim(item.baseUrl),
    kind: item.kind === KIND_ACTION ? KIND_ACTION : KIND_DOCUMENT,
    holder: trim(item.holder),
    name: trim(item.name),
    href: trim(item.href)
  };
}

/* usable rejects items that could not be acted on.  An action needs somewhere
 * to look itself up; a document needs an address. */
function usable(item) {
  if (!item.label || !item.baseUrl) {
    return false;
  }
  return item.kind === KIND_ACTION ? !!(item.holder && item.name)
                                   : !!item.href;
}

function load() {
  var raw;
  try {
    raw = localStorage.getItem(STORAGE_KEY);
  } catch (error) {
    console.log('quick: localStorage unavailable: ' + error);
    return [];
  }
  if (!raw) {
    return [];
  }
  var parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    console.log('quick: stored shortcuts are not JSON, ignoring: ' + error);
    return [];
  }
  if (Object.prototype.toString.call(parsed) !== '[object Array]') {
    return [];
  }
  var out = [];
  for (var i = 0; i < parsed.length && out.length < MAX_QUICK; i++) {
    var item = normalise(parsed[i]);
    if (usable(item)) {
      out.push(item);
    }
  }
  return out;
}

function save(list) {
  var clean = [];
  for (var i = 0; i < list.length && clean.length < MAX_QUICK; i++) {
    var item = normalise(list[i]);
    if (usable(item)) {
      clean.push(item);
    }
  }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(clean));
  } catch (error) {
    console.log('quick: could not save: ' + error);
    return load();
  }
  return clean;
}

function count() {
  return load().length;
}

function get(index) {
  var list = load();
  return (index < 0 || index >= list.length) ? null : list[index];
}

/* add appends a shortcut, replacing any identical one rather than
 * accumulating duplicates -- saving the same row twice is a natural thing to
 * do and should be idempotent. */
function add(item) {
  var wanted = normalise(item);
  if (!usable(wanted)) {
    console.log('quick: refusing to save an incomplete shortcut');
    return null;
  }
  var list = load();
  for (var i = 0; i < list.length; i++) {
    if (same(list[i], wanted)) {
      list[i] = wanted;
      save(list);
      return wanted;
    }
  }
  if (list.length >= MAX_QUICK) {
    console.log('quick: already holding ' + MAX_QUICK + ' shortcuts');
    return null;
  }
  list.push(wanted);
  save(list);
  return wanted;
}

function same(a, b) {
  return a.baseUrl === b.baseUrl && a.kind === b.kind &&
         a.holder === b.holder && a.name === b.name && a.href === b.href;
}

function remove(index) {
  var list = load();
  if (index < 0 || index >= list.length) {
    return false;
  }
  list.splice(index, 1);
  save(list);
  return true;
}

/* backendFor resolves a shortcut against the configured backends, by base URL
 * so that renaming or reordering them does not re-point it somewhere else.
 * Returns null when the backend it referred to is gone. */
function backendFor(item) {
  var backends = settings.load();
  for (var i = 0; i < backends.length; i++) {
    if (backends[i].baseUrl === item.baseUrl) {
      return backends[i];
    }
  }
  return null;
}

/* rows renders the Quick section for the opening screen.  A shortcut whose
 * backend has been deleted is still shown, marked -- silently dropping
 * something the user saved would leave them wondering where it went. */
function rows() {
  var list = load();
  var out = [];
  for (var i = 0; i < list.length; i++) {
    var backend = backendFor(list[i]);
    out.push({
      label: list[i].label,
      sublabel: backend ? backend.name : 'backend removed',
      kind: 'q'
    });
  }
  return out;
}

module.exports = {
  KIND_ACTION: KIND_ACTION,
  KIND_DOCUMENT: KIND_DOCUMENT,
  MAX_QUICK: MAX_QUICK,
  load: load,
  save: save,
  count: count,
  get: get,
  add: add,
  remove: remove,
  backendFor: backendFor,
  rows: rows
};
