#!/usr/bin/env node
/* Checks live.js in isolation.
 *
 * Everything here is about not lying while something is still happening. The
 * expensive mistake is treating an answer that is not about our job as if it
 * were -- a poll routed to a machine that never saw it -- because that reports
 * a job as finished seconds after it started, which on a homelab means telling
 * someone their machines are up when they are still booting.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');

/* --- a clock the test drives ------------------------------------------- */

var now = 500000;
var timers = [];
var nextId = 1;

global.setTimeout = function (fn, ms) {
  var id = nextId++;
  timers.push({ id: id, at: now + (ms || 0), fn: fn });
  return id;
};
global.clearTimeout = function (id) {
  for (var i = 0; i < timers.length; i++) {
    if (timers[i].id === id) { timers.splice(i, 1); return; }
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
    if (!next) { break; }
    now = next.at;
    global.clearTimeout(next.id);
    next.fn();
  }
  now = until;
}

/* --- a stub server ------------------------------------------------------ */

var reply = null;
var polls = [];

function FakeXHR() { this.readyState = 0; this.status = 0; this.responseText = ''; }
FakeXHR.prototype.open = function (method, url) { this.method = method; this.url = url; };
FakeXHR.prototype.setRequestHeader = function () {};
FakeXHR.prototype.getAllResponseHeaders = function () { return ''; };
FakeXHR.prototype.send = function () {
  polls.push(this.url);
  this.status = reply.status === undefined ? 200 : reply.status;
  this.responseText = JSON.stringify(reply.body || {});
  this.readyState = 4;
  this.onreadystatechange();
};
global.XMLHttpRequest = FakeXHR;

var realLog = console.log;
console.log = function () {};
var live = require(path.join(__dirname, '..', 'src', 'pkjs', 'live'));

var failures = 0;

function assert(label, condition) {
  realLog((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

var BACKEND = { name: 'x', baseUrl: 'http://host/', authHeader: 'K', secret: 's' };

/* The origin document links to a job by a rel that matches the job's class. */
var ORIGIN = {
  entity: { class: ['pantry'],
            links: [{ rel: ['self'], href: '/' },
                    { rel: ['kettle-job'], href: '/job' }] },
  href: 'http://host/'
};

function job(props, status) {
  return { status: status === undefined ? 200 : status,
           entity: { class: ['kettle-job'], properties: props } };
}

function jobBody(props) {
  return { body: { class: ['kettle-job'], properties: props } };
}

function record() {
  var seen = { progress: [], done: null, gaveUp: false };
  return {
    seen: seen,
    handlers: {
      onProgress: function (e) { seen.progress.push(e.properties.step); },
      onDone: function (e) { seen.done = e.properties.state; },
      onGiveUp: function () { seen.gaveUp = true; }
    }
  };
}

function begin(props, status) {
  polls = [];
  live.stop();
  var r = record();
  var started = live.start(BACKEND, ORIGIN, job(props, status), r.handlers);
  return { started: started, seen: r.seen };
}

/* --- what counts as something to watch ---------------------------------- */

function testWhatStartsAWatch() {
  assert('a 202 starts a watch',
         live.shouldWatch(job({ id: 1 }, 202)) === true);
  assert('an entity that says it is running starts a watch',
         live.shouldWatch(job({ state: 'running' }, 200)) === true);
  /* An ordinary answer is an answer. Watching it would be polling forever. */
  assert('a plain 200 does not',
         live.shouldWatch(job({ state: 'done' }, 200)) === false);
  assert('an entity with no state does not',
         live.shouldWatch(job({}, 200)) === false);
}

/* The poll target is found by matching the result's class against the origin's
 * link rels -- an idiom, not a path this app knows. */
function testPollTarget() {
  assert('a matching rel is the thing to poll',
         live.pollTarget(ORIGIN.entity,
                         { class: ['kettle-job'] }) === '/job');
  assert('an unmatched class polls nothing in particular',
         live.pollTarget(ORIGIN.entity, { class: ['unrelated'] }) === null);
}

/* Found by looking at a real API: falling back to polling the origin document
 * looks helpful and is a lie. The origin does not report the job's state, so
 * the first poll reads its silence as completion and announces that the work
 * finished seconds after it started. */
function testNothingToFollowMeansNoWatch() {
  polls = [];
  live.stop();
  var r = record();
  var started = live.start(
    BACKEND, ORIGIN,
    { status: 202, entity: { class: ['unrelated'], properties: {} } },
    r.handlers);
  assert('with nothing the server offers to track it, no watch starts',
         started === false);
  tick(live.POLL_MS * 3);
  assert('and nothing is polled', polls.length === 0);
  assert('and nothing is claimed to have finished', r.seen.done === null);
}

/* The same failure one step later: the thing being polled turns out not to
 * report progress at all. */
function testSilenceIsNotCompletion() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 120 }, 202);
  reply = { body: { class: ['kettle-job'], properties: { note: 'no state here' } } };
  tick(live.POLL_MS);
  assert('an entity that reports no state does not end the watch as done',
         run.seen.done === null);
  assert('it stops, and says it stopped', run.seen.gaveUp === true);
  assert('and it really has stopped', live.isLive() === false);
}

/* --- the actual watching ------------------------------------------------ */

function testProgressThenDone() {
  var run = begin({ state: 'running', id: 4, step: 'one',
                    staleAfterSeconds: 120 }, 202);
  assert('watching started', run.started === true);
  assert('and reports itself as live', live.isLive() === true);

  reply = jobBody({ state: 'running', id: 4, step: 'two',
                    staleAfterSeconds: 120 });
  tick(live.POLL_MS);
  assert('the job link is what gets polled', polls[0] === 'http://host/job');
  assert('progress is reported', run.seen.progress.join(',') === 'two');

  reply = jobBody({ state: 'done', id: 4, staleAfterSeconds: 120 });
  tick(live.POLL_MS);
  assert('completion is reported once', run.seen.done === 'done');
  assert('and the watch has ended', live.isLive() === false);
}

/* The one that matters: a poll answered by a machine that never saw this job. */
function testAnswerAboutAnotherJob() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 120 }, 202);
  reply = jobBody({ state: 'done', id: 99, staleAfterSeconds: 120 });
  tick(live.POLL_MS);
  assert('another job\'s completion is not ours', run.seen.done === null);
  assert('and the watch continues', live.isLive() === true);

  reply = jobBody({ state: 'done', id: 4, staleAfterSeconds: 120 });
  tick(live.POLL_MS);
  assert('our own completion still ends it', run.seen.done === 'done');
}

function testNoJobHere() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 120 }, 202);
  /* "I have no job" is not "the job finished". */
  reply = jobBody({ state: 'none', id: 0 });
  tick(live.POLL_MS);
  assert('a state of none is not completion', run.seen.done === null);
  assert('and the watch continues', live.isLive() === true);
  live.stop();
}

function testFailedPollIsNotNews() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 120 }, 202);
  var saved = FakeXHR.prototype.send;
  FakeXHR.prototype.send = function () {
    polls.push(this.url);
    this.status = 0;
    this.responseText = '';
    this.readyState = 4;
    this.onreadystatechange();
  };
  tick(live.POLL_MS);
  assert('a failed poll says nothing about the job', run.seen.done === null);
  assert('and does not end the watch', live.isLive() === true);
  FakeXHR.prototype.send = saved;
  live.stop();
}

/* --- the deadline ------------------------------------------------------- */

function testDeadlineFromTheServer() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 1 }, 202);
  reply = jobBody({ state: 'running', id: 4, step: 'x', staleAfterSeconds: 1 });
  tick(live.POLL_MS);
  assert('inside the budget it keeps going', live.isLive() === true);
  tick(live.BUFFER_MS + live.POLL_MS);
  assert('past the budget it gives up', run.seen.gaveUp === true);
  /* Giving up is about this client, not about the job. */
  assert('giving up is not completion', run.seen.done === null);
}

/* A server that never sends a budget gets the generous fallback, because
 * abandoning a job that is still running is the worse mistake. */
function testFallbackDeadline() {
  var run = begin({ state: 'running', id: 4 }, 202);
  reply = jobBody({ state: 'running', id: 4, step: 'x' });
  tick(live.POLL_MS * 10);
  assert('no advertised budget means the generous default',
         live.isLive() === true && run.seen.gaveUp === false);
  tick(live.FALLBACK_MS);
  assert('which does eventually run out', run.seen.gaveUp === true);
}

/* The budget is re-derived every poll, so an early answer that carried none
 * does not fix a short deadline for the whole run. */
function testBudgetIsReDerived() {
  var run = begin({ state: 'running', id: 4, staleAfterSeconds: 1 }, 202);
  reply = jobBody({ state: 'running', id: 4, step: 'x',
                    staleAfterSeconds: 3600 });
  tick(live.POLL_MS);
  tick(live.BUFFER_MS + live.POLL_MS);
  assert('a later, larger budget replaces the first one',
         run.seen.gaveUp === false && live.isLive() === true);
  live.stop();
}

function testStopIsFinal() {
  begin({ state: 'running', id: 4, staleAfterSeconds: 120 }, 202);
  live.stop();
  polls = [];
  tick(live.POLL_MS * 3);
  assert('a stopped watch never polls again', polls.length === 0);
  assert('and reports itself stopped', live.isLive() === false);
}

testWhatStartsAWatch();
testPollTarget();
testNothingToFollowMeansNoWatch();
testSilenceIsNotCompletion();
testProgressThenDone();
testAnswerAboutAnotherJob();
testNoJobHere();
testFailedPollIsNotNews();
testDeadlineFromTheServer();
testFallbackDeadline();
testBudgetIsReDerived();
testStopIsFinal();

if (failures) {
  realLog(failures + ' failure(s)');
  process.exit(1);
}
realLog('live: all checks passed');
