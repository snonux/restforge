/* Backend definitions and their secrets.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * This is the only module that touches a secret, and the secret never leaves
 * the phone: it goes into an Authorization-style request header in http.js and
 * into the settings page, and nowhere else.  In particular it is never sent to
 * the watch and never appears in a query string -- a key in a URL is written to
 * the server's request log and, behind a reverse proxy, the proxy's log too.
 *
 * Storage is one JSON array under a single localStorage key.  localStorage
 * coerces everything it stores to a string and returns null (not undefined) for
 * a key that was never set, so both ends are guarded: stringify on the way in,
 * and treat anything that does not parse into an array as "no backends" rather
 * than throwing during the ready handler.
 *
 * A backend is:
 *   name       display name, shown on the watch's picker
 *   baseUrl    the Siren API root, absolute, ending in '/'
 *   authHeader header the secret is sent in, default X-API-Key
 *   secret     the API key
 *   startRel   optional link rel to follow immediately after the root
 */

'use strict';

var STORAGE_KEY = 'restforge.backends';

var DEFAULT_AUTH_HEADER = 'X-API-Key';

/* Bounds, so a pathological config cannot make the page or the watch unusable.
 * These are generous: the limit that actually matters is screen space. */
var MAX_BACKENDS = 12;
var MAX_FIELD = 256;

function trim(value) {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value).replace(/^\s+|\s+$/g, '').slice(0, MAX_FIELD);
}

/* normalise coerces one stored or submitted object into the shape above,
 * filling in the defaults.  It does not reject anything -- validate() does
 * that, and only for input the user just typed.  Storage that has drifted (an
 * older layout, a hand-edited value) degrades to something usable instead of
 * taking the companion down at startup. */
function normalise(raw) {
  var backend = raw && typeof raw === 'object' ? raw : {};
  var baseUrl = trim(backend.baseUrl);
  /* Every href in a Siren document is resolved against this, and RFC 3986
   * resolution drops the last path segment of the base unless it ends in '/'.
   * Fixing it here rather than rejecting it saves the user from a class of
   * error whose only symptom is a 404 two screens later. */
  if (baseUrl && baseUrl.charAt(baseUrl.length - 1) !== '/') {
    baseUrl += '/';
  }
  return {
    name: trim(backend.name),
    baseUrl: baseUrl,
    authHeader: trim(backend.authHeader) || DEFAULT_AUTH_HEADER,
    secret: trim(backend.secret),
    startRel: trim(backend.startRel)
  };
}

/* validate returns a human-readable problem, or null when the backend is
 * usable.  Called by the settings page before it closes, so the wording is
 * shown to the user. */
function validate(backend) {
  if (!backend.name) {
    return 'Name is required';
  }
  if (!backend.baseUrl) {
    return 'Base URL is required';
  }
  if (!/^https?:\/\/[^\s/]+\//.test(backend.baseUrl)) {
    return 'Base URL must be absolute, e.g. https://host/path/';
  }
  if (!backend.secret) {
    return 'Secret is required';
  }
  return null;
}

/* load returns the stored backends, always as an array of normalised objects.
 * Anything unparseable is reported and treated as empty: the app then shows
 * "needs config", which is both true and recoverable, whereas throwing here
 * would leave the watch on a blank screen with no way back. */
function load() {
  var raw;
  try {
    raw = localStorage.getItem(STORAGE_KEY);
  } catch (error) {
    console.log('settings: localStorage unavailable: ' + error);
    return [];
  }
  if (!raw) {
    return [];
  }

  var parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    console.log('settings: stored config is not JSON, ignoring: ' + error);
    return [];
  }
  if (Object.prototype.toString.call(parsed) !== '[object Array]') {
    console.log('settings: stored config is not an array, ignoring');
    return [];
  }

  var backends = [];
  for (var i = 0; i < parsed.length && backends.length < MAX_BACKENDS; i++) {
    var backend = normalise(parsed[i]);
    /* A nameless or URL-less entry cannot be opened and cannot be labelled on
     * the picker, so it is dropped rather than shown as a dead row. */
    if (backend.name && backend.baseUrl) {
      backends.push(backend);
    }
  }
  return backends;
}

function save(backends) {
  var clean = [];
  var input = backends || [];
  for (var i = 0; i < input.length && clean.length < MAX_BACKENDS; i++) {
    var backend = normalise(input[i]);
    if (backend.name && backend.baseUrl) {
      clean.push(backend);
    }
  }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(clean));
  } catch (error) {
    console.log('settings: could not save: ' + error);
    return load();
  }
  console.log('settings: saved ' + clean.length + ' backend(s)');
  return clean;
}

function count() {
  return load().length;
}

function get(index) {
  var backends = load();
  if (index < 0 || index >= backends.length) {
    return null;
  }
  return backends[index];
}

/* rows renders the picker the watch shows at depth 0.  The sublabel is the
 * host, not the whole URL: at 26px a full URL is ellipsised into uselessness,
 * and the host is the part that distinguishes two backends.  The secret is of
 * course not included -- see the module comment. */
function rows() {
  var backends = load();
  var out = [];
  for (var i = 0; i < backends.length; i++) {
    var match = /^https?:\/\/([^/]+)/.exec(backends[i].baseUrl);
    out.push({
      label: backends[i].name,
      sublabel: match ? match[1] : backends[i].baseUrl,
      kind: 'b'
    });
  }
  return out;
}

module.exports = {
  DEFAULT_AUTH_HEADER: DEFAULT_AUTH_HEADER,
  MAX_BACKENDS: MAX_BACKENDS,
  normalise: normalise,
  validate: validate,
  load: load,
  save: save,
  count: count,
  get: get,
  rows: rows
};
