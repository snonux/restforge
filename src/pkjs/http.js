/* HTTP for one configured backend.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * There is no fetch() and there are no promises here, so this is
 * XMLHttpRequest with a callback: done(error, result).  Exactly one of the two
 * is ever non-null, and done is called exactly once per request.
 *
 * Two things in here are not obvious and both were verified against the
 * emulator's XHR rather than assumed:
 *
 * 1. On a DNS failure, a TLS error or a refused connection, status is set to 0
 *    and only loadend and readystatechange fire -- **onerror never fires**.  A
 *    client that keys "unreachable" off onerror simply hangs.  So the check is
 *    readyState === 4 && status === 0, and onerror is a belt-and-braces path
 *    that may never run.
 *
 * 2. A POST must carry Content-Length, which means send('') and never send().
 *    A body-less send() produces a request some servers -- bozohttpd among
 *    them -- reject outright.
 *
 * The key goes in a header, never in the query string: request URIs are
 * written to the server's access log and, behind a reverse proxy, to the
 * proxy's log as well, so a key in a URL ends up on disk on several machines.
 * Nothing in this file logs a header value.
 */

'use strict';

var url = require('./url');

/* Timeouts.  A read is quick or it is broken; an action that changes physical
 * state can legitimately take a long time, so it gets its own budget rather
 * than making every request wait a minute before giving up. */
var GET_TIMEOUT_MS = 20000;
var ACTION_TIMEOUT_MS = 60000;

/* Error kinds.  These are the vocabulary session.js maps onto a DocState, and
 * the distinction that matters most is Unreachable against everything else: a
 * request that never arrived tells you nothing about the state of the thing
 * you asked about. */
var UNREACHABLE = 'unreachable';
var TIMEOUT = 'timeout';
var AUTH = 'auth';
var CONFLICT = 'conflict';
var SERVER = 'server';
var CLIENT = 'client';
var PARSE = 'parse';
var CONFIG = 'config';

function fail(kind, status, message) {
  return { kind: kind, status: status || 0, message: message };
}

/* parseBody returns the decoded document, or undefined when the body was not
 * JSON.  An error response is allowed to carry an explanation in the same
 * shape, so this runs regardless of status. */
function parseBody(text) {
  if (!text) {
    return undefined;
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    return undefined;
  }
}

/* serverMessage digs the server's own wording out of an error envelope.  Its
 * wording beats anything this app could invent, because it is the only party
 * that knows why it said no. */
function serverMessage(entity) {
  if (entity && entity.properties && typeof entity.properties.message === 'string') {
    return entity.properties.message;
  }
  return null;
}

/* classify maps an HTTP status onto an error kind, or returns null when the
 * response is a success.  202 is a success: it means the work was accepted and
 * is still running, which is a normal answer, not a failure. */
function classify(status) {
  if (status >= 200 && status < 300) {
    return null;
  }
  if (status === 401 || status === 403) {
    return AUTH;
  }
  if (status === 409) {
    return CONFLICT;
  }
  if (status >= 500) {
    return SERVER;
  }
  return CLIENT;
}

function describe(kind, status, entity) {
  var fromServer = serverMessage(entity);
  if (fromServer) {
    return fromServer;
  }
  if (kind === AUTH) {
    return 'auth rejected';
  }
  if (kind === CONFLICT) {
    return 'state changed';
  }
  return 'HTTP ' + status;
}

/* logResponse records what came back.  Every response header is logged rather
 * than a chosen few: which header identifies the answering node, or the cache,
 * or the proxy, is a property of the deployment, and naming one here would be
 * knowledge of a particular server. */
function logResponse(method, target, xhr, startedAt) {
  var elapsed = Date.now() - startedAt;
  console.log(method + ' ' + url.path(target) + ' -> ' + xhr.status +
              ' (' + elapsed + 'ms)');
  var headers = xhr.getAllResponseHeaders ? xhr.getAllResponseHeaders() : '';
  /* One console.log per header rather than one with embedded newlines: the log
   * viewer stamps a timestamp and a source location on each call, so a
   * multi-line message loses both on every line after the first. */
  var lines = typeof headers === 'string' ? headers.split(/\r?\n/) : [];
  for (var i = 0; i < lines.length; i++) {
    /* A header line always has a colon.  Anything else is the runtime telling
     * us there were no headers at all -- which is what an unreachable host
     * looks like from here. */
    if (lines[i].indexOf(':') > 0) {
      console.log('  ' + lines[i]);
    }
  }
}

/* setHeaders applies the auth header and the ones we always send.  A header
 * name the user typed can be rejected by the XHR implementation, so this
 * reports a config problem rather than letting the exception escape into an
 * event handler where nothing would catch it. */
function setHeaders(xhr, backend, hasBody) {
  try {
    xhr.setRequestHeader(backend.authHeader, backend.secret);
    xhr.setRequestHeader('Accept', 'application/vnd.siren+json, application/json');
    if (hasBody) {
      xhr.setRequestHeader('Content-Type', 'application/x-www-form-urlencoded');
    }
  } catch (error) {
    return fail(CONFIG, 0, 'bad auth header name "' + backend.authHeader + '"');
  }
  return null;
}

/* finish turns a completed exchange into either an error or a result. */
function finish(method, target, xhr, startedAt, done) {
  logResponse(method, target, xhr, startedAt);

  if (xhr.status === 0) {
    /* See the note at the top: this, not onerror, is how an unreachable host
     * actually presents itself.  The message names the host, because the one
     * thing worth knowing here is which host could not be reached -- the
     * banner already says that it could not. */
    done(fail(UNREACHABLE, 0, 'no answer from ' + (url.origin(target) || target)),
         null);
    return;
  }

  var entity = parseBody(xhr.responseText);
  var kind = classify(xhr.status);
  if (kind) {
    done(fail(kind, xhr.status, describe(kind, xhr.status, entity)), null);
    return;
  }
  if (entity === undefined) {
    done(fail(PARSE, xhr.status, 'response was not JSON'), null);
    return;
  }
  done(null, { status: xhr.status, entity: entity, url: target });
}

/* request performs one HTTP exchange against a configured backend.
 *
 * The href is whatever the server put in the document -- absolute, root-relative
 * or relative -- and is resolved against the backend's base URL.  Only the
 * scheme and authority come from us; the path always came from the server. */
function request(backend, href, method, fields, done) {
  var verb = (method || 'GET').toUpperCase();
  var target = url.resolve(href, backend.baseUrl);
  var body = fields ? url.encodeForm(fields) : null;
  var isRead = verb === 'GET' || verb === 'HEAD';
  var startedAt = Date.now();
  var settled = false;

  function settle(error, result) {
    if (settled) {
      return;
    }
    settled = true;
    done(error, result);
  }

  var xhr = new XMLHttpRequest();
  xhr.open(verb, target, true);
  xhr.timeout = isRead ? GET_TIMEOUT_MS : ACTION_TIMEOUT_MS;

  var problem = setHeaders(xhr, backend, !isRead);
  if (problem) {
    settle(problem, null);
    return;
  }

  xhr.onreadystatechange = function () {
    /* The settled check is not redundant with settle()'s own: a response that
     * arrives after a timeout would otherwise still be logged, which reads in
     * the log as a second, contradictory outcome for one request. */
    if (xhr.readyState === 4 && !settled) {
      finish(verb, target, xhr, startedAt, settle);
    }
  };
  xhr.ontimeout = function () {
    console.log(verb + ' ' + url.path(target) + ' -> timed out after ' +
                xhr.timeout + 'ms');
    settle(fail(TIMEOUT, 0, 'timed out'), null);
  };
  /* Documented for completeness; on this runtime it does not fire for the
   * connection failures you would expect it to. */
  xhr.onerror = function () {
    settle(fail(UNREACHABLE, 0,
                'no answer from ' + (url.origin(target) || target)), null);
  };

  console.log(verb + ' ' + url.origin(target) + url.path(target));
  /* send('') rather than send() on anything with a body, so Content-Length is
   * present even when the body is empty. */
  xhr.send(isRead ? null : (body === null ? '' : body));
}

/* get is the common case, spelled out so callers do not pass three nulls. */
function get(backend, href, done) {
  request(backend, href, 'GET', null, done);
}

module.exports = {
  UNREACHABLE: UNREACHABLE,
  TIMEOUT: TIMEOUT,
  AUTH: AUTH,
  CONFLICT: CONFLICT,
  SERVER: SERVER,
  CLIENT: CLIENT,
  PARSE: PARSE,
  CONFIG: CONFIG,
  GET_TIMEOUT_MS: GET_TIMEOUT_MS,
  ACTION_TIMEOUT_MS: ACTION_TIMEOUT_MS,
  request: request,
  get: get
};
