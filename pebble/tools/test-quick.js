#!/usr/bin/env node
/* Checks quick.js.
 *
 * A shortcut is a stored intention, and the ways it can go wrong are all
 * versions of pointing somewhere other than where the user meant: at a
 * reordered backend, at an action the server has since withdrawn, or at an
 * address that was never the server's to give. Most of what follows is that.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');

var storage = {};
global.localStorage = {
  getItem: function (k) {
    return Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : null;
  },
  setItem: function (k, v) { storage[k] = String(v); },
  removeItem: function (k) { delete storage[k]; }
};

var realLog = console.log;
console.log = function () {};
var settings = require(path.join(__dirname, '..', 'src', 'pkjs', 'settings'));
var quick = require(path.join(__dirname, '..', 'src', 'pkjs', 'quick'));

var failures = 0;

function assert(label, condition) {
  realLog((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

var A = 'https://a.example/api/';
var B = 'https://b.example/api/';

function setUp(backends) {
  storage = {};
  settings.save(backends || [
    { name: 'alpha', baseUrl: A, secret: 'k' },
    { name: 'beta', baseUrl: B, secret: 'k' }
  ]);
}

function action(label, base, holder, name) {
  return { label: label, backendName: 'x', baseUrl: base,
           kind: quick.KIND_ACTION, holder: holder, name: name };
}

function document_(label, base, href) {
  return { label: label, backendName: 'x', baseUrl: base,
           kind: quick.KIND_DOCUMENT, href: href };
}

function testAddAndList() {
  setUp();
  quick.add(action('Power off', A, '/api/', 'power-off'));
  quick.add(document_('Status', A, '/api/status'));
  assert('shortcuts are kept in order',
         quick.load().map(function (i) { return i.label; }).join(',') ===
         'Power off,Status');
  assert('a shortcut row is a quick row', quick.rows()[0].kind === 'q');
  assert('the row names the backend it belongs to',
         quick.rows()[0].sublabel === 'alpha');
}

/* Saving the same row twice is a natural thing to do. */
function testAddIsIdempotent() {
  setUp();
  quick.add(action('Power off', A, '/api/', 'power-off'));
  quick.add(action('Power off (renamed)', A, '/api/', 'power-off'));
  assert('saving the same target twice does not duplicate it',
         quick.count() === 1);
  assert('and the newer label wins',
         quick.load()[0].label === 'Power off (renamed)');
}

/* The failure this guards: reordering backends must not re-point a shortcut
 * at a different server. */
function testBackendResolvedByUrlNotPosition() {
  setUp();
  quick.add(action('Power off', B, '/api/', 'power-off'));
  assert('resolves to the backend it was saved from',
         quick.backendFor(quick.get(0)).name === 'beta');

  settings.save([{ name: 'beta', baseUrl: B, secret: 'k' },
                 { name: 'alpha', baseUrl: A, secret: 'k' }]);
  assert('still resolves after the list is reordered',
         quick.backendFor(quick.get(0)).baseUrl === B);

  settings.save([{ name: 'beta renamed', baseUrl: B, secret: 'k' }]);
  assert('still resolves after the backend is renamed',
         quick.backendFor(quick.get(0)).baseUrl === B);
}

/* Kept and marked, not silently dropped: something the user saved going
 * missing without explanation is worse than a row that says so. */
function testMissingBackendIsShown() {
  setUp();
  quick.add(action('Power off', A, '/api/', 'power-off'));
  settings.save([{ name: 'beta', baseUrl: B, secret: 'k' }]);
  assert('a shortcut to a deleted backend survives', quick.count() === 1);
  assert('it resolves to nothing', quick.backendFor(quick.get(0)) === null);
  assert('and the row says so', quick.rows()[0].sublabel === 'backend removed');
}

function testIncompleteIsRefused() {
  setUp();
  /* An action with nowhere to look itself up next time. */
  assert('an action without a holder is refused',
         quick.add({ label: 'x', baseUrl: A, kind: quick.KIND_ACTION,
                     name: 'power-off' }) === null);
  assert('an action without a name is refused',
         quick.add({ label: 'x', baseUrl: A, kind: quick.KIND_ACTION,
                     holder: '/api/' }) === null);
  assert('a document without an address is refused',
         quick.add(document_('x', A, '')) === null);
  assert('nothing was stored', quick.count() === 0);
}

function testRemove() {
  setUp();
  quick.add(action('one', A, '/api/', 'a'));
  quick.add(action('two', A, '/api/', 'b'));
  assert('remove takes the named one', quick.remove(0) === true);
  assert('the other survives', quick.load()[0].label === 'two');
  assert('removing past the end is refused', quick.remove(9) === false);
}

function testCap() {
  setUp();
  for (var i = 0; i < quick.MAX_QUICK + 5; i++) {
    quick.add(action('n' + i, A, '/api/', 'a' + i));
  }
  assert('the list is capped at ' + quick.MAX_QUICK,
         quick.count() === quick.MAX_QUICK);
}

function testCorruptStorage() {
  setUp();
  storage['restforge.quick'] = 'not json';
  assert('unparseable storage reads as empty', quick.load().length === 0);
  storage['restforge.quick'] = '{"a":1}';
  assert('a non-array reads as empty', quick.load().length === 0);
  storage['restforge.quick'] = '[null,3,{"label":"x"}]';
  assert('unusable members are dropped', quick.load().length === 0);
}

/* An action is stored by name, never by its own href: the server withdraws
 * actions as its state changes, and a remembered href would outlive that. */
function testActionStoresNameNotHref() {
  setUp();
  quick.add({ label: 'Power off', baseUrl: A, kind: quick.KIND_ACTION,
              holder: '/api/', name: 'power-off',
              href: '/api/power/all/off' });
  var stored = quick.get(0);
  assert('the action name is what is kept', stored.name === 'power-off');
  assert('the holder document is what is kept', stored.holder === '/api/');
}

testAddAndList();
testAddIsIdempotent();
testBackendResolvedByUrlNotPosition();
testMissingBackendIsShown();
testIncompleteIsRefused();
testRemove();
testCap();
testCorruptStorage();
testActionStoresNameNotHref();

if (failures) {
  realLog(failures + ' failure(s)');
  process.exit(1);
}
realLog('quick: all checks passed');
