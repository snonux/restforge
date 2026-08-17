#!/usr/bin/env node
/* Checks settings.js against a localStorage shim.
 *
 * The cases that matter are the degenerate ones: storage that was written by
 * an older layout, hand-edited, or truncated.  settings.js runs inside the
 * 'ready' handler, so anything it throws leaves the watch on a blank screen
 * with no way to reach the settings page and fix it.  Every such case must
 * degrade to "no backends", which is recoverable.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');

global.localStorage = (function () {
  var store = {};
  return {
    getItem: function (key) {
      /* Real localStorage returns null, not undefined, for a missing key. */
      return Object.prototype.hasOwnProperty.call(store, key) ? store[key] : null;
    },
    setItem: function (key, value) { store[key] = String(value); },
    removeItem: function (key) { delete store[key]; }
  };
}());

var settings = require(path.join(__dirname, '..', 'src', 'pkjs', 'settings'));

var STORAGE_KEY = 'restforge.backends';
var failures = 0;

function assert(label, condition) {
  console.log((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

function testEmpty() {
  localStorage.removeItem(STORAGE_KEY);
  assert('no storage means no backends',
         settings.load().length === 0 && settings.count() === 0);
  assert('no backends means no picker rows', settings.rows().length === 0);
}

function testNormalisation() {
  settings.save([{ name: 'homelab', baseUrl: 'https://host.example.org/cgi-bin/app',
                   secret: 'k' }]);
  var stored = settings.get(0);
  /* Without the trailing slash, RFC 3986 resolution drops the last segment and
   * every href in the document resolves one level too high. */
  assert('a trailing slash is added',
         stored.baseUrl === 'https://host.example.org/cgi-bin/app/');
  assert('the auth header defaults',
         stored.authHeader === settings.DEFAULT_AUTH_HEADER);
  assert('an out-of-range index is null',
         settings.get(5) === null && settings.get(-1) === null);
}

function testPickerRows() {
  settings.save([{ name: 'homelab', baseUrl: 'https://host.example.org/cgi-bin/app/',
                   secret: 'SECRET-MUST-NOT-APPEAR' }]);
  var row = settings.rows()[0];
  /* The host, not the whole URL: at 26px a full URL ellipsises into something
   * that cannot be told apart from any other backend's. */
  assert('the picker row shows the host', row.sublabel === 'host.example.org');
  assert('the picker row is a backend row', row.kind === 'b');
  /* rows() feeds straight into an AppMessage.  Anything in it reaches the
   * watch, so the secret must not be anywhere in the structure. */
  assert('nothing bound for the watch carries the secret',
         JSON.stringify(settings.rows()).indexOf('SECRET-MUST-NOT-APPEAR') < 0);
}

function testIncompleteEntriesDropped() {
  settings.save([{ name: '', baseUrl: 'https://a/' },
                 { name: 'good', baseUrl: 'https://b/', secret: 'x' }]);
  assert('an unnamed backend is dropped',
         settings.load().length === 1 && settings.load()[0].name === 'good');
}

function testCorruptStorage() {
  localStorage.setItem(STORAGE_KEY, 'not json at all');
  assert('unparseable storage reads as empty', settings.load().length === 0);

  localStorage.setItem(STORAGE_KEY, '{"backends":1}');
  assert('a non-array reads as empty', settings.load().length === 0);

  localStorage.setItem(STORAGE_KEY, '[null,3,"x"]');
  assert('junk array members are dropped', settings.load().length === 0);
}

function testValidation() {
  assert('a missing secret is reported',
         settings.validate(settings.normalise({ name: 'a', baseUrl: 'https://h/' })) ===
         'Secret is required');
  assert('a relative base URL is reported',
         /absolute/.test(settings.validate(
             settings.normalise({ name: 'a', baseUrl: '/x/', secret: 's' }))));
  assert('a complete backend validates',
         settings.validate(settings.normalise(
             { name: 'a', baseUrl: 'https://h/x', secret: 's' })) === null);
}

function testCap() {
  var many = [];
  for (var i = 0; i < settings.MAX_BACKENDS + 8; i++) {
    many.push({ name: 'b' + i, baseUrl: 'https://h/' + i + '/', secret: 's' });
  }
  settings.save(many);
  assert('the backend count is capped at ' + settings.MAX_BACKENDS,
         settings.load().length === settings.MAX_BACKENDS);
}

testEmpty();
testNormalisation();
testPickerRows();
testIncompleteEntriesDropped();
testCorruptStorage();
testValidation();
testCap();

if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('settings: all checks passed');
