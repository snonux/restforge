/* Watching something that is still happening.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * Some actions do not finish when the request does.  A server that answers 202,
 * or answers with an entity that says it is still running, is telling the
 * client to come back and look -- and until it stops saying that, the screen
 * is showing something that is no longer true.
 *
 * Everything here is generic, and three details are worth stating because each
 * one is a way a naive poller gets it wrong:
 *
 *  - **Where to poll.**  If the document the action came from has a link whose
 *    rel matches one of the returned entity's classes, that link is the thing
 *    to watch.  That is rel/class matching, an ordinary hypermedia idiom, and
 *    it needs no knowledge of what the resource is called.  Failing that, the
 *    origin document itself is re-fetched.
 *
 *  - **Which answer is ours.**  A load-balanced deployment can route a poll to
 *    a machine that never saw the job.  Such a reply is "no news", not "no
 *    job": a response whose id differs from the one we started, or whose state
 *    is the server's word for nothing-here, is skipped rather than treated as
 *    completion.  Reading it as completion is how a client reports a job as
 *    finished seconds after starting it.
 *
 *  - **How long to wait.**  The deadline comes from the server's own
 *    staleAfterSeconds and is re-derived on every poll, not fixed when polling
 *    began -- an early poll that landed on a machine with no job carries no
 *    budget to derive one from, and a later one will.  The fallback is
 *    deliberately generous, because giving up early on a job that is still
 *    running is worse than waiting.
 */

'use strict';

var http = require('./http');
var siren = require('./siren');

/* How often to ask.  Ten seconds is frequent enough that a step change is
 * visible while watching the watch, and rare enough that a long job is not a
 * hundred requests. */
var POLL_MS = 10000;

/* Slack added to the server's own staleness budget, covering the poll interval
 * and the round trip. */
var BUFFER_MS = 60000;

/* Used only when no response has ever carried a budget to derive one from --
 * an older server, or a run of polls that all landed somewhere without the
 * job.  Generous on purpose: this can only make the client wait longer than
 * necessary, never give up on a job that is still going. */
var FALLBACK_MS = 25 * 60 * 1000;

/* The state a server reports for "there is no such job here".  This is the one
 * string in this module that names a value rather than a structure, and it is
 * a guess that costs nothing if wrong: an unrecognised state that is not
 * "running" simply ends the watch, which is the same thing that happens today
 * for any other terminal state. */
var STATE_NONE = 'none';
var STATE_RUNNING = 'running';

var timer = null;
var watching = null;

function isLive() {
  return watching !== null;
}

/* pollTarget picks the resource to watch: a link on the origin document whose
 * rel matches a class of the thing the action returned.  Both arguments are
 * entities. */
function pollTarget(originEntity, resultEntity) {
  var classes = siren.classes(resultEntity);
  for (var i = 0; i < classes.length; i++) {
    var href = siren.follow(originEntity, classes[i]);
    if (href) {
      return href;
    }
  }
  /* No matching link.  There is then nothing to follow, and saying so is the
   * only honest answer -- see the note on start(). */
  return null;
}

/* running reports whether an entity says it is still in progress.  An entity
 * with no state at all is not making a claim, so it does not start a watch. */
function running(entity) {
  return siren.properties(entity).state === STATE_RUNNING;
}

/* judgeable reports whether an entity says anything at all about progress.
 * One that does not is not a job resource, and treating its silence as
 * completion is exactly the lie this module exists to prevent. */
function judgeable(entity) {
  return siren.properties(entity).state !== undefined;
}

/* shouldWatch decides whether an action's response is something to follow.
 * Either signal is enough: the status code, or the entity's own account. */
function shouldWatch(result) {
  return result.status === 202 || running(result.entity);
}

function budgetMs(entity) {
  var stale = siren.properties(entity).staleAfterSeconds;
  if (typeof stale === 'number' && stale > 0) {
    return stale * 1000 + BUFFER_MS;
  }
  return null;
}

/* relevant filters out answers that are not about our job.  Both cases mean
 * "ask again", not "it finished". */
function relevant(entity) {
  var values = siren.properties(entity);
  if (values.state === STATE_NONE) {
    return false;
  }
  if (watching.id !== undefined && values.id !== undefined &&
      values.id !== watching.id) {
    return false;
  }
  return true;
}

function stop() {
  if (timer) {
    clearTimeout(timer);
    timer = null;
  }
  watching = null;
}

function schedule() {
  timer = setTimeout(poll, POLL_MS);
}

function finish(entity, why) {
  var handlers = watching.handlers;
  console.log('live: finished (' + why + ')');
  stop();
  handlers.onDone(entity);
}

function poll() {
  timer = null;
  if (!watching) {
    return;
  }
  var current = watching;
  http.get(current.backend, current.href, function (error, result) {
    /* A watch that was stopped while the request was in flight -- the user
     * navigated away -- must not resurrect itself. */
    if (watching !== current) {
      return;
    }
    if (error) {
      /* A failed poll is not news about the job, only about the network.  Keep
       * asking until the deadline; that is the whole reason there is one. */
      console.log('live: poll failed (' + error.kind + '), still watching');
      checkDeadline(null);
      return;
    }
    handle(result.entity);
  });
}

/* checkDeadline gives up when the server's own budget has run out.  Returns
 * true when the watch has ended. */
function checkDeadline(entity) {
  if (entity) {
    var fresh = budgetMs(entity);
    if (fresh !== null) {
      watching.budget = fresh;
    }
  }
  var budget = watching.budget === null ? FALLBACK_MS : watching.budget;
  if (Date.now() - watching.startedAt > budget) {
    var handlers = watching.handlers;
    console.log('live: gave up after ' + budget + 'ms');
    stop();
    handlers.onGiveUp();
    return true;
  }
  schedule();
  return false;
}

function handle(entity) {
  if (!relevant(entity)) {
    console.log('live: answer is about a different job, or none — asking again');
    checkDeadline(entity);
    return;
  }
  /* Whatever we are polling does not report progress, so it cannot tell us the
   * job ended.  Stop watching and say we stopped -- the alternative is reading
   * silence as completion, which is how a client reports machines as up while
   * they are still booting. */
  if (!judgeable(entity)) {
    var handlers = watching.handlers;
    console.log('live: nothing here reports progress, stopping');
    stop();
    handlers.onGiveUp();
    return;
  }
  if (!running(entity)) {
    finish(entity, siren.properties(entity).state);
    return;
  }
  watching.handlers.onProgress(entity);
  checkDeadline(entity);
}

/* start begins watching, if there is anything to watch.  Returns true when a
 * watch was started, so the caller can tell "in progress" from "finished".
 *
 * The origin is the document the action was invoked from, as { entity, href }.
 * handlers: onProgress(entity), onDone(entity), onGiveUp()
 */
function start(backend, origin, result, handlers) {
  stop();
  if (!shouldWatch(result)) {
    return false;
  }

  /* Only a document the server pointed us at will do.  Falling back to the
   * origin document looks helpful and is not: the origin does not report the
   * job's state, so the first poll would read its silence as completion and
   * announce that the work had finished seconds after it started.  Better to
   * not claim to be watching -- the caller then simply re-reads the document
   * and reports what the action returned. */
  var href = pollTarget(origin.entity, result.entity);
  if (!href) {
    console.log('live: nothing the server offers tracks this, not watching');
    return false;
  }

  var values = siren.properties(result.entity);
  watching = {
    backend: backend,
    href: href,
    id: values.id,
    budget: budgetMs(result.entity),
    startedAt: Date.now(),
    handlers: handlers
  };
  console.log('live: watching ' + href + ' id ' +
              (values.id === undefined ? '(none)' : values.id) +
              ', budget ' + (watching.budget === null ? 'default' :
                             watching.budget + 'ms'));
  schedule();
  return true;
}

module.exports = {
  POLL_MS: POLL_MS,
  BUFFER_MS: BUFFER_MS,
  FALLBACK_MS: FALLBACK_MS,
  start: start,
  stop: stop,
  isLive: isLive,
  shouldWatch: shouldWatch,
  pollTarget: pollTarget
};
