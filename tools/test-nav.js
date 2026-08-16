#!/usr/bin/env node
/* Checks nav.js in isolation: the navigation stack, frame assembly, fetching
 * and the idle-refresh clock, driven directly rather than through session.js.
 *
 * This is the module the idle-refresh bug (see nav.js's comment on
 * idleRefreshable) lives in, so the idle-refresh cases here are the point of
 * the file: nav.js has no actions.js of its own to ask whether a question is
 * pending, only the hook session.js wires in production
 * (setActionPendingCheck) -- so this checks both that the hook works, and
 * that nav.js behaves sanely with nothing wired to it at all, which is what a
 * unit test of nav.js alone actually exercises.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');
var Module = require('module');

/* --- the runtime PebbleKit JS provides and node does not ----------------- */

var MESSAGE_KEYS = {
  CMD: 1, IDX: 2, ANSWER: 3, ANSWER_TEXT: 4, INBOX: 5, SEQ: 6,
  CHUNK_IDX: 7, CHUNK_N: 8, PAYLOAD: 9, STATE: 10, PROMPT_KIND: 11, LIVE: 12,
  ROOT: 13
};

var realResolve = Module._resolveFilename;
Module._resolveFilename = function (request) {
  if (request === 'message_keys') {
    return 'message_keys';
  }
  return realResolve.apply(this, arguments);
};
require.cache['message_keys'] = {
  id: 'message_keys', filename: 'message_keys', loaded: true,
  exports: MESSAGE_KEYS
};

/* A clock the tests drive by hand, exactly as test-session.js does -- the
 * idle timer is a state machine over time and the only deterministic way to
 * test it is to own the clock. */
var now = 1000000;
var timers = [];
var nextTimerId = 1;

global.setTimeout = function (fn, ms) {
  var id = nextTimerId++;
  timers.push({ id: id, at: now + (ms || 0), fn: fn });
  return id;
};
global.clearTimeout = function (id) {
  for (var i = 0; i < timers.length; i++) {
    if (timers[i].id === id) {
      timers.splice(i, 1);
      return;
    }
  }
};
Date.now = function () { return now; };

function tick(ms) {
  var until = now + ms;
  for (;;) {
    var next = null;
    for (var i = 0; i < timers.length; i++) {
      if (timers[i].at <= until && (!next || timers[i].at < next.at)) {
        next = timers[i];
      }
    }
    if (!next) {
      break;
    }
    now = next.at;
    global.clearTimeout(next.id);
    next.fn();
  }
  now = until;
}

var storage = {};
global.localStorage = {
  getItem: function (key) {
    return Object.prototype.hasOwnProperty.call(storage, key) ? storage[key] : null;
  },
  setItem: function (key, value) { storage[key] = String(value); },
  removeItem: function (key) { delete storage[key]; }
};

var sent = [];
global.Pebble = {
  sendAppMessage: function (message, onSuccess) {
    sent.push(message);
    onSuccess();
  }
};

var routes = {};
var requested = [];

function FakeXHR() {
  this.readyState = 0;
  this.status = 0;
  this.responseText = '';
  this.timeout = 0;
}
FakeXHR.prototype.open = function (method, url) {
  this.method = method;
  this.url = url;
};
FakeXHR.prototype.setRequestHeader = function () {};
FakeXHR.prototype.getAllResponseHeaders = function () { return ''; };
FakeXHR.prototype.send = function () {
  requested.push(this.method + ' ' + this.url);
  var route = routes[this.method + ' ' + this.url] || routes[this.url];
  this.status = route ? (route.status || 200) : 404;
  this.responseText = route
    ? JSON.stringify(route.body)
    : '{"properties":{"message":"no such thing here"}}';
  this.readyState = 4;
  this.onreadystatechange();
};
global.XMLHttpRequest = FakeXHR;

var realLog = console.log;
console.log = function () {};

var settings = require(path.join(__dirname, '..', 'src', 'pkjs', 'settings'));
var nav = require(path.join(__dirname, '..', 'src', 'pkjs', 'nav'));

/* --- decoding what the watch would have received ------------------------- */

var FIELD_SEP = '\x1f';

function frames() {
  var out = [];
  var buffer = '';
  for (var i = 0; i < sent.length; i++) {
    var message = sent[i];
    if (message[MESSAGE_KEYS.CHUNK_IDX] === 0) {
      buffer = '';
    }
    buffer += message[MESSAGE_KEYS.PAYLOAD];
    if (message[MESSAGE_KEYS.CHUNK_IDX] ===
        message[MESSAGE_KEYS.CHUNK_N] - 1) {
      var parts = buffer.split(FIELD_SEP);
      out.push({
        title: parts[0],
        rows: parts[1] ? parts[1].split('\n') : [],
        kinds: parts[2] || '',
        message: parts[3] || '',
        heading: parts[4] || '',
        prompt: parts[5] || '',
        state: message[MESSAGE_KEYS.STATE],
        overlay: message[MESSAGE_KEYS.PROMPT_KIND],
        atRoot: message[MESSAGE_KEYS.ROOT],
        seq: message[MESSAGE_KEYS.SEQ]
      });
    }
  }
  return out;
}

function lastFrame() {
  var all = frames();
  return all[all.length - 1];
}

function reset() {
  sent = [];
  requested = [];
}

function labels(frame) {
  var out = [];
  for (var i = 0; i < frame.rows.length; i++) {
    out.push(frame.rows[i].split('\t')[0]);
  }
  return out;
}

function indexOfRow(frame, label) {
  return labels(frame).indexOf(label);
}

/* --- fixtures -------------------------------------------------------------- */

var ROOT = {
  class: ['pantry'], title: 'The pantry',
  properties: { apiVersion: 1, kettle: 'cold' },
  entities: [{ class: ['shelf'], title: 'Top shelf',
               properties: { name: 'top', jars: 4 } }],
  links: [{ rel: ['self'], href: '/' }, { rel: ['shelves'], href: '/shelves' }],
  actions: []
};

var SHELVES = {
  class: ['shelf-list'], title: 'Shelves',
  properties: { count: 1 },
  links: [{ rel: ['self'], href: '/shelves' }]
};

var BASE = 'http://pantry.example/';

function setUp() {
  routes = {};
  routes[BASE] = { body: ROOT };
  routes[BASE + 'shelves'] = { body: SHELVES };
  settings.save([
    { name: 'pantry', baseUrl: BASE, secret: 'open-sesame' },
    { name: 'other', baseUrl: 'http://other.example/', secret: 'x' }
  ]);
  /* Reset nav.js's idle-refresh hook to its unwired default before every
   * test, so one test's wiring cannot leak into the next. */
  nav.setActionPendingCheck(function () { return false; });
  reset();
}

var failures = 0;

function assert(label, condition) {
  realLog((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

/* --- the tests ----------------------------------------------------------- */

function testPickerFrame() {
  setUp();
  nav.listBackends();
  var frame = lastFrame();
  assert('the picker lists every backend', frame.rows.length === 2);
  assert('the picker rows are backend rows', frame.kinds === 'bb');

  storage = {};
  reset();
  nav.listBackends();
  frame = lastFrame();
  assert('no backends is needs-config', frame.state === nav.STATE_NEEDS_CONFIG);
}

function testOpenBackendAndFetch() {
  setUp();
  nav.listBackends();
  reset();
  nav.openBackend(0);

  assert('opening a backend fetches its base URL',
         requested[0] === 'GET ' + BASE);
  var frame = lastFrame();
  assert('the document title comes from the server', frame.title === 'The pantry');

  var row = nav.rowAt(indexOfRow(frame, 'shelves'));
  assert('rowAt returns the target a link row was rendered with',
         row.target.type === 'fetch' && row.target.href === '/shelves');

  reset();
  nav.fetch(row.target.href, 'shelves', false);
  assert('fetch follows the href it was given',
         requested[0] === 'GET ' + BASE + 'shelves');
  assert('the fetched document is shown', lastFrame().title === 'Shelves');
}

function testOpenEmbeddedAndBack() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  var root = lastFrame();
  reset();

  var row = nav.rowAt(indexOfRow(root, 'Top shelf'));
  nav.openEmbedded(row.target.index);
  assert('an embedded entity is opened without a request', requested.length === 0);
  assert('the embedded entity is shown', lastFrame().title === 'Top shelf');

  reset();
  nav.back();
  assert('back returns to the previous document',
         lastFrame().title === 'The pantry');

  reset();
  nav.back();
  assert('back at the root returns to the picker', lastFrame().kinds === 'bb');
}

function testRefreshAndEmbeddedHasNoAddress() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  reset();

  nav.refresh();
  assert('refresh re-fetches the current document', requested[0] === 'GET ' + BASE);

  var row = nav.rowAt(indexOfRow(lastFrame(), 'Top shelf'));
  nav.openEmbedded(row.target.index);
  reset();
  nav.refresh();
  assert('an embedded document cannot be re-fetched', requested.length === 0);
  assert('it is re-rendered instead', lastFrame().title === 'Top shelf');
}

function testFailureKeepsTheDocument() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  var root = lastFrame();

  routes = {};
  reset();
  nav.refresh();

  var frame = lastFrame();
  assert('a failed refresh keeps the document on screen',
         labels(frame).join(',') === labels(root).join(','));
  assert('the banner names the error kind', frame.message === 'Error');
  assert('the reason is readable', frame.overlay === nav.OVERLAY_DETAIL &&
         frame.prompt === 'no such thing here');
}

function testUnreachableMapping() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);

  FakeXHR.prototype.send = function () {
    requested.push(this.method + ' ' + this.url);
    this.status = 0;
    this.responseText = '';
    this.readyState = 4;
    this.onreadystatechange();
  };
  reset();
  nav.refresh();
  assert('status 0 maps to unreachable, not error',
         lastFrame().state === nav.STATE_UNREACHABLE);
  FakeXHR.prototype.send = function () {
    requested.push(this.method + ' ' + this.url);
    var route = routes[this.method + ' ' + this.url] || routes[this.url];
    this.status = route ? (route.status || 200) : 404;
    this.responseText = route ? JSON.stringify(route.body)
      : '{"properties":{"message":"no such thing here"}}';
    this.readyState = 4;
    this.onreadystatechange();
  };
}

/* --- idle refresh -----------------------------------------------------
 *
 * The bug this module's own comment documents: idleRefreshable() has to
 * weigh three independent reasons to hold off (live mode, an open overlay,
 * a pending action question) and get all three right, because getting any
 * one wrong either spams a background refresh over something the user is
 * reading, or silently stops refreshing at all.
 */

function testIdleRefreshFires() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  reset();
  tick(60000);
  assert('the visible document is re-read when idle',
         requested[requested.length - 1] === 'GET ' + BASE);
}

function testIdleRefreshSuppressedByOverlay() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  nav.send(nav.overlay(nav.documentFrame(), 'heading', 'body'));
  reset();
  tick(60000);
  assert('an open overlay suppresses the idle refresh', requested.length === 0);

  /* Found in live use: the watch dismisses a reading overlay by itself, and
   * without a word from it the suppression latched on and the background
   * refresh never resumed -- this is the fix's own regression test. */
  nav.setSeq(99);
  nav.dismissed();
  reset();
  tick(60000);
  assert('dismissing the overlay lets the idle refresh resume',
         requested[requested.length - 1] === 'GET ' + BASE);
}

function testIdleRefreshSuppressedByActionPendingHook() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  reset();

  /* With nothing wired, an action question is never pending as far as nav.js
   * is concerned -- this is the unwired default a fresh require() of nav.js
   * has, before session.js runs its setActionPendingCheck wiring. */
  tick(60000);
  assert('with no hook wired, idle refresh behaves as if nothing is pending',
         requested.length === 1);

  reset();
  nav.setActionPendingCheck(function () { return true; });
  tick(60000);
  assert('a pending-action hook that answers true suppresses idle refresh',
         requested.length === 0);

  nav.setActionPendingCheck(function () { return false; });
}

function testIdleRefreshFailureDoesNotOpenOverlay() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  routes = {};
  reset();
  tick(60000);
  assert('a failed idle refresh does not open an overlay',
         lastFrame().overlay === nav.OVERLAY_NONE);
  assert('but it does say so', lastFrame().message === 'Error');
}

function testSetSeqClearsOverlay() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  nav.send(nav.overlay(nav.documentFrame(), 'h', 'b'));

  /* Any press means the user is still here and has moved past whatever was
   * on screen, overlay included -- setSeq is what clears the flag, and the
   * next frame sent is what re-schedules the idle timer against the cleared
   * flag (setSeq alone schedules nothing; it only runs ahead of whatever
   * command the watch actually sent). */
  nav.setSeq(7);
  reset();
  nav.refresh();
  reset();
  tick(60000);
  assert('once the overlay flag is cleared, idle refresh runs again',
         requested[requested.length - 1] === 'GET ' + BASE);
}

function testSwitchingBackendsResetsTheStack() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  var row = nav.rowAt(indexOfRow(lastFrame(), 'shelves'));
  nav.fetch(row.target.href, 'shelves', false);
  reset();

  nav.listBackends();
  nav.openBackend(1);
  reset();
  nav.back();
  assert('switching backends discards the old stack', lastFrame().kinds === 'bb');
}

testPickerFrame();
testOpenBackendAndFetch();
testOpenEmbeddedAndBack();
testRefreshAndEmbeddedHasNoAddress();
testFailureKeepsTheDocument();
testUnreachableMapping();
testIdleRefreshFires();
testIdleRefreshSuppressedByOverlay();
testIdleRefreshSuppressedByActionPendingHook();
testIdleRefreshFailureDoesNotOpenOverlay();
testSetSeqClearsOverlay();
testSwitchingBackendsResetsTheStack();

if (failures) {
  realLog(failures + ' failure(s)');
  process.exit(1);
}
realLog('nav: all checks passed');
