#!/usr/bin/env node
/* Checks actions.js in isolation: filling a Siren action's fields, phrasing
 * its confirmation, invoking it, and the one retry the hypermedia contract
 * allows on a 409 -- driven directly through actions.js's own API
 * (askAction/answer) against a document nav.js is left holding, rather than
 * through session.js's activate/back.
 *
 * These are the tests where a bug means an unwanted request reaching a real
 * server, so they check what was sent as well as what was shown -- and the
 * boundary between this module and nav.js: nav.js is required and used for
 * real here (openRoot, lastFrame), not stubbed, because the whole point of
 * the split is that actions.js has no navigation state of its own to fake.
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
var sentBodies = [];

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
function respondFromRoutes(body) {
  requested.push(this.method + ' ' + this.url);
  sentBodies.push(body);
  var route = routes[this.method + ' ' + this.url] || routes[this.url];
  this.status = route ? (route.status || 200) : 404;
  this.responseText = route
    ? JSON.stringify(route.body)
    : '{"properties":{"message":"no such thing here"}}';
  this.readyState = 4;
  this.onreadystatechange();
}
FakeXHR.prototype.send = respondFromRoutes;
global.XMLHttpRequest = FakeXHR;

var realLog = console.log;
console.log = function () {};

var settings = require(path.join(__dirname, '..', 'src', 'pkjs', 'settings'));
var nav = require(path.join(__dirname, '..', 'src', 'pkjs', 'nav'));
var actions = require(path.join(__dirname, '..', 'src', 'pkjs', 'actions'));

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
  sentBodies = [];
}

/* --- fixtures -------------------------------------------------------------- */

var ROOT = {
  class: ['pantry'], title: 'The pantry',
  properties: { apiVersion: 1, kettle: 'cold' },
  links: [{ rel: ['self'], href: '/' }],
  actions: [
    { name: 'brew', title: 'Brew a pot of tea', method: 'POST',
      href: '/brew', fields: [] },
    { name: 'cool', title: 'Let the kettle cool', method: 'POST', href: '/cool',
      fields: [{ name: 'confirm', type: 'checkbox', required: true,
                 title: 'The kettle is still hot. Cool it anyway?' }] },
    { name: 'peek', title: 'Look inside', href: '/peek' }
  ]
};

var BASE = 'http://pantry.example/';

function setUp() {
  routes = {};
  routes[BASE] = { body: ROOT };
  routes['POST ' + BASE + 'brew'] = { status: 200,
    body: { properties: { state: 'done', id: 7 } } };
  routes['POST ' + BASE + 'cool'] = { body: { properties: { state: 'done' } } };
  routes['GET ' + BASE + 'peek'] = { body: { properties: { seen: true } } };
  settings.save([{ name: 'pantry', baseUrl: BASE, secret: 'open-sesame' }]);
  nav.setActionPendingCheck(function () { return false; });
  reset();
}

var failures = 0;

function assert(label, condition) {
  realLog((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

/* openRoot leaves the root document on screen (via nav.js, for real) and the
 * recorders empty, returning the frame as it was before the reset. */
function openRoot() {
  setUp();
  nav.listBackends();
  nav.openBackend(0);
  var frame = lastFrame();
  reset();
  return frame;
}

/* --- the tests ------------------------------------------------------------ */

function testHasPendingTracksAskAction() {
  openRoot();
  assert('nothing pending before a question is asked', !actions.hasPending());

  actions.askAction('brew');
  assert('a question awaiting confirmation is pending', actions.hasPending());

  actions.cancelPending();
  assert('cancelPending clears it', !actions.hasPending());
  assert('nothing was sent by asking then cancelling', requested.length === 0);
}

function testUnsafeActionAsksFirst() {
  openRoot();
  actions.askAction('brew');

  var frame = lastFrame();
  assert('an unsafe action asks before acting', requested.length === 0);
  assert('the question is a confirmation, not a reading',
         frame.overlay === nav.OVERLAY_CONFIRM);
  assert('the question is headed by the server\'s wording',
         frame.heading === 'Brew a pot of tea');
}

function testSafeActionGoesStraightThrough() {
  openRoot();
  actions.askAction('peek');
  assert('a safe action is not gated', requested[0] === 'GET ' + BASE + 'peek');
  assert('nothing is left pending once it went straight through',
         !actions.hasPending());
}

function testDeclining() {
  openRoot();
  actions.askAction('brew');
  reset();
  actions.answer(false);

  assert('declining sends nothing', requested.length === 0);
  assert('declining clears the question', !actions.hasPending());
  assert('declining leaves the document in place',
         lastFrame().title === 'The pantry');
}

function testConfirmingInvokesAndRefetches() {
  openRoot();
  actions.askAction('brew');
  reset();
  actions.answer(true);

  assert('confirming posts to the href the server gave',
         requested[0] === 'POST ' + BASE + 'brew');
  /* The contract's rule: never carry a document across an action. */
  assert('the document is re-fetched afterwards', requested[1] === 'GET ' + BASE);
  var frame = lastFrame();
  assert('the outcome survives the re-fetch', frame.message === 'done');
  assert('the response body is readable', /id: 7/.test(frame.prompt));
}

function testRequiredCheckboxFillsFromTheConfirmation() {
  openRoot();
  actions.askAction('cool');
  assert('the checkbox title is the question asked',
         lastFrame().prompt === 'The kettle is still hot. Cool it anyway?');

  reset();
  actions.answer(true);
  assert('confirming fills the checkbox', sentBodies[0] === 'confirm=true');
}

function testConflictRefetchesAndDoesNotRetryWithoutConfirmation() {
  openRoot();
  routes['POST ' + BASE + 'brew'] = { status: 409,
    body: { properties: { message: 'a brew is already running' } } };
  actions.askAction('brew');
  reset();
  actions.answer(true);

  var posts = 0;
  for (var i = 0; i < requested.length; i++) {
    if (requested[i].indexOf('POST') === 0) { posts++; }
  }
  /* "brew" has no required checkbox, so nothing was confirmed in the sense
   * the contract's retry exception covers -- inventing a confirmation nobody
   * gave is exactly what this must not do. */
  assert('an action with no required checkbox is never retried', posts === 1);
  assert('a 409 re-fetches instead', requested[1] === 'GET ' + BASE);
  assert('the conflict is reported', lastFrame().message === 'Conflict');
}

function testAuthFailureDoesNotRefetch() {
  openRoot();
  routes['POST ' + BASE + 'brew'] = { status: 401,
    body: { properties: { message: 'API key rejected' } } };
  actions.askAction('brew');
  reset();
  actions.answer(true);

  assert('an auth failure does not re-fetch', requested.length === 1);
  assert('the auth failure is reported', lastFrame().message === 'Auth');
}

function testWithdrawnAction() {
  openRoot();
  actions.askAction('brew');
  /* The confirmation is on screen; now the document it was about is gone --
   * the way nav.back() popping the only document on the stack leaves nav.js
   * (top() becomes null, same as if the stack had never been pushed to). */
  nav.listBackends();
  reset();
  actions.answer(true);
  assert('answering a question whose document is gone sends nothing',
         requested.length === 0);
}

/* labelAction opens a document offering a single unsafe "label" action with
 * the given fields, then walks it through the confirm step exactly as
 * activate()+answer() would: askAction() only ever shows the confirmation for
 * an unsafe method, so the field-filling behaviour under test only runs once
 * that confirmation is answered. */
function labelAction(fields) {
  setUp();
  routes[BASE] = { body: {
    class: ['pantry'], title: 'The pantry',
    links: [{ rel: ['self'], href: '/' }],
    actions: [{ name: 'label', title: 'Label a jar', method: 'POST',
                href: '/label', fields: fields }]
  } };
  routes['POST ' + BASE + 'label'] = { body: { properties: { state: 'done' } } };
  nav.listBackends();
  nav.openBackend(0);
  reset();
  actions.askAction('label');
  reset();
  actions.answer(true);
}

function testRequiredFieldIsAskedFor() {
  labelAction([{ name: 'text', type: 'text', required: true,
                 title: 'What should the jar say?' }]);
  assert('nothing is sent while a value is missing', requested.length === 0);
  assert('the value is asked for out loud', lastFrame().overlay === nav.OVERLAY_TEXT);
  assert('the question uses the server\'s wording',
         lastFrame().heading === 'What should the jar say?');

  reset();
  actions.answer(true, 'plum jam');
  assert('what was said is what is sent', sentBodies[0] === 'text=plum+jam');
}

function testNothingHeardSendsNothing() {
  labelAction([{ name: 'text', type: 'text', required: true }]);
  reset();
  actions.answer(true, '');
  assert('an empty transcription sends nothing', requested.length === 0);
  assert('and says so', /Nothing was heard/.test(lastFrame().prompt));
}

function testDefaultsAreUsedWithoutAsking() {
  labelAction([{ name: 'text', type: 'text', required: true, value: 'jam' }]);
  assert('a server-supplied default is used as-is', sentBodies[0] === 'text=jam');
}

function testSeveralMissingFieldsAreRefused() {
  labelAction([{ name: 'text', type: 'text', required: true },
               { name: 'colour', type: 'text', required: true }]);
  assert('more than one missing value is refused, not dictated',
         requested.length === 0);
  assert('and the refusal explains itself', /more than/.test(lastFrame().prompt));
}

/* --- the bounded retry ---------------------------------------------------
 *
 * The one case where repeating a request is right: the user ticked a
 * required checkbox, and the server judged the same request twice on
 * budgets that changed in between.
 */

function testConfirmedActionRetriesOnce() {
  openRoot();
  var attempts = 0;
  routes['POST ' + BASE + 'cool'] = { status: 409,
    body: { properties: { message: 'needs confirmation' } } };
  var saved = FakeXHR.prototype.send;
  FakeXHR.prototype.send = function (body) {
    if (this.method === 'POST') {
      attempts++;
      if (attempts === 2) {
        routes['POST ' + BASE + 'cool'] = { body: { properties: { state: 'done' } } };
      }
    }
    saved.call(this, body);
  };

  actions.askAction('cool');
  reset();
  actions.answer(true);
  FakeXHR.prototype.send = saved;

  assert('a confirmed action is retried exactly once', attempts === 2);
  assert('both attempts carried the confirmation',
         sentBodies[0] === 'confirm=true' && sentBodies[1] === 'confirm=true');
  assert('the retry succeeding is what is reported', lastFrame().message === 'done');
}

function testRetryIsNotRepeated() {
  openRoot();
  routes['POST ' + BASE + 'cool'] = { status: 409,
    body: { properties: { message: 'still no' } } };
  actions.askAction('cool');
  reset();
  actions.answer(true);

  var posts = 0;
  for (var i = 0; i < requested.length; i++) {
    if (requested[i].indexOf('POST') === 0) { posts++; }
  }
  assert('a retry that also conflicts is not retried again', posts === 2);
  assert('and the conflict is reported', lastFrame().message === 'Conflict');
}

function testRetryExpiresAfterTheTTL() {
  openRoot();
  routes['POST ' + BASE + 'cool'] = { status: 200,
    body: { properties: { state: 'done' } } };
  actions.askAction('cool');
  reset();
  actions.answer(true);

  /* The confirmation succeeded outright, so nothing is retried, but the
   * remembered confirmation is now stale by definition: this checks that a
   * *second*, independent 409 on the same action outside the TTL window is
   * not mistaken for the confirmation that has already been spent. */
  routes['POST ' + BASE + 'cool'] = { status: 409,
    body: { properties: { message: 'no' } } };
  actions.askAction('cool');
  tick(70000);
  reset();
  actions.answer(false);
  assert('a stale confirmation is not carried into an unrelated question',
         requested.length === 0);
}

testHasPendingTracksAskAction();
testUnsafeActionAsksFirst();
testSafeActionGoesStraightThrough();
testDeclining();
testConfirmingInvokesAndRefetches();
testRequiredCheckboxFillsFromTheConfirmation();
testConflictRefetchesAndDoesNotRetryWithoutConfirmation();
testAuthFailureDoesNotRefetch();
testWithdrawnAction();
testRequiredFieldIsAskedFor();
testNothingHeardSendsNothing();
testDefaultsAreUsedWithoutAsking();
testSeveralMissingFieldsAreRefused();
testConfirmedActionRetriesOnce();
testRetryIsNotRepeated();
testRetryExpiresAfterTheTTL();

if (failures) {
  realLog(failures + ' failure(s)');
  process.exit(1);
}
realLog('actions: all checks passed');
