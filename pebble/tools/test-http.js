#!/usr/bin/env node
/* Checks http.js against a stub XMLHttpRequest.
 *
 * The cases worth writing down are the failure ones, because the emulator's
 * XHR behaves differently from a browser's in ways that are easy to get wrong
 * and hard to notice:
 *
 *  - a refused connection or a DNS failure sets status 0 and fires only
 *    readystatechange, never onerror, so a client waiting for onerror hangs;
 *  - a POST must carry Content-Length, so send('') and never send().
 *
 * The stub below reproduces both, and the log is captured so the test can
 * assert the thing that would be worst to get wrong: that the secret never
 * reaches it.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');

/* --- log capture -------------------------------------------------------- */

var logLines = [];
var realLog = console.log;
console.log = function () {
  logLines.push(Array.prototype.slice.call(arguments).join(' '));
};

/* --- the stub ----------------------------------------------------------- */

/* scenario is set by each test and drives the exchange when send() is called. */
var scenario = null;

function FakeXHR() {
  this.readyState = 0;
  this.status = 0;
  this.responseText = '';
  this.timeout = 0;
  this.headers = {};
  this.responseHeaders = '';
  this.sent = false;
  this.sentBody = undefined;
}

FakeXHR.prototype.open = function (method, url) {
  this.method = method;
  this.url = url;
  this.readyState = 1;
};

FakeXHR.prototype.setRequestHeader = function (name, value) {
  /* A real implementation rejects a name that is not a token, which is how a
   * typo in the configured auth header name presents itself. */
  if (!/^[A-Za-z0-9!#$%&'*+.^_`|~-]+$/.test(name)) {
    throw new Error('invalid header name');
  }
  this.headers[name] = value;
};

FakeXHR.prototype.getAllResponseHeaders = function () {
  return this.responseHeaders;
};

FakeXHR.prototype.send = function (body) {
  this.sent = true;
  this.sentBody = body;
  var xhr = this;
  if (scenario) {
    scenario(xhr);
  }
};

/* respond completes the exchange the way a real XHR does. */
function respond(xhr, status, text, headers) {
  xhr.status = status;
  xhr.responseText = text === undefined ? '' : text;
  xhr.responseHeaders = headers || 'content-type: application/json\r\n';
  xhr.readyState = 4;
  xhr.onreadystatechange();
}

/* unreachable is the emulator's actual behaviour on a connection failure:
 * status 0, readyState 4, and onerror is never called. */
function unreachable(xhr) {
  xhr.status = 0;
  xhr.responseText = '';
  xhr.readyState = 4;
  xhr.onreadystatechange();
}

global.XMLHttpRequest = FakeXHR;

var http = require(path.join(__dirname, '..', 'src', 'pkjs', 'http'));

/* --- harness ------------------------------------------------------------ */

var failures = 0;
var results = [];

function assert(label, condition) {
  results.push((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

var SECRET = 'this-is-the-secret-value';

var BACKEND = {
  name: 'test',
  baseUrl: 'https://host.example.org/cgi-bin/app/',
  authHeader: 'X-API-Key',
  secret: SECRET,
  startRel: ''
};

/* capture runs one request and hands the test what came back, plus the stub
 * that served it. */
function capture(href, method, fields, drive, check) {
  scenario = drive;
  var seen = { calls: 0 };
  var xhrSeen = null;
  var realOpen = FakeXHR.prototype.open;
  FakeXHR.prototype.open = function () {
    xhrSeen = this;
    return realOpen.apply(this, arguments);
  };
  http.request(BACKEND, href, method, fields, function (error, result) {
    seen.calls++;
    seen.error = error;
    seen.result = result;
  });
  FakeXHR.prototype.open = realOpen;
  check(seen, xhrSeen);
}

/* --- the request itself ------------------------------------------------- */

function testSuccessfulGet() {
  capture('/cgi-bin/app/status', 'GET', null, function (xhr) {
    respond(xhr, 200, '{"class":["status"],"properties":{"apiVersion":1}}');
  }, function (seen, xhr) {
    assert('a 200 yields a result', !seen.error && !!seen.result);
    assert('the callback runs exactly once', seen.calls === 1);
    assert('the document is parsed',
           seen.result.entity.properties.apiVersion === 1);
    assert('the href is resolved against the base',
           xhr.url === 'https://host.example.org/cgi-bin/app/status');
    assert('the secret goes in the configured header',
           xhr.headers['X-API-Key'] === SECRET);
    assert('the secret is never in the URL', xhr.url.indexOf(SECRET) < 0);
    assert('a GET sends no body', xhr.sentBody === null);
    assert('a GET gets the short timeout', xhr.timeout === http.GET_TIMEOUT_MS);
  });
}

function testActionPost() {
  capture('/cgi-bin/app/act', 'POST', { force: true }, function (xhr) {
    respond(xhr, 202, '{"properties":{"state":"running"}}');
  }, function (seen, xhr) {
    assert('a 202 is a success, not a failure', !seen.error && !!seen.result);
    assert('202 is reported as the status', seen.result.status === 202);
    assert('a POST declares its content type',
           xhr.headers['Content-Type'] === 'application/x-www-form-urlencoded');
    assert('the fields are form-encoded', xhr.sentBody === 'force=true');
    assert('an action gets the long timeout',
           xhr.timeout === http.ACTION_TIMEOUT_MS);
  });
}

function testEmptyPostStillHasABody() {
  capture('/cgi-bin/app/act', 'POST', null, function (xhr) {
    respond(xhr, 200, '{}');
  }, function (seen, xhr) {
    /* send('') and never send(): the empty string is what produces a
     * Content-Length header, which some servers require on any POST. */
    assert('a field-less POST sends an empty string, not nothing',
           xhr.sentBody === '');
  });
}

/* --- failures ----------------------------------------------------------- */

function testUnreachable() {
  capture('/cgi-bin/app/status', 'GET', null, unreachable, function (seen) {
    assert('status 0 at readyState 4 is unreachable',
           seen.error && seen.error.kind === http.UNREACHABLE);
    assert('unreachable reports no result', seen.result === null);
    assert('unreachable still calls back exactly once', seen.calls === 1);
  });
}

function testTimeout() {
  capture('/cgi-bin/app/status', 'GET', null, function (xhr) {
    xhr.ontimeout();
  }, function (seen) {
    assert('a timeout is its own kind',
           seen.error && seen.error.kind === http.TIMEOUT);
  });
}

function testTimeoutThenLateResponse() {
  capture('/cgi-bin/app/status', 'GET', null, function (xhr) {
    xhr.ontimeout();
    /* A late response after a timeout must not deliver a second callback --
     * the caller has already moved on and rendered the timeout. */
    respond(xhr, 200, '{}');
  }, function (seen) {
    assert('a response after a timeout is ignored', seen.calls === 1);
    assert('the timeout is what is reported',
           seen.error.kind === http.TIMEOUT);
  });
}

function testStatusMapping() {
  var cases = [
    [401, http.AUTH], [403, http.AUTH], [409, http.CONFLICT],
    [404, http.CLIENT], [400, http.CLIENT], [500, http.SERVER],
    [503, http.SERVER]
  ];
  for (var i = 0; i < cases.length; i++) {
    (function (status, kind) {
      capture('/x', 'GET', null, function (xhr) {
        respond(xhr, status, '{}');
      }, function (seen) {
        assert(status + ' maps to ' + kind,
               seen.error && seen.error.kind === kind);
        assert(status + ' reports its status', seen.error.status === status);
      });
    }(cases[i][0], cases[i][1]));
  }
}

function testServerWording() {
  capture('/x', 'GET', null, function (xhr) {
    respond(xhr, 409, '{"properties":{"message":"a job is already running"}}');
  }, function (seen) {
    /* The server is the only party that knows why it said no, so its wording
     * beats anything this app could invent. */
    assert('the server\'s own message is used',
           seen.error.message === 'a job is already running');
  });
}

function testUnparseableBody() {
  capture('/x', 'GET', null, function (xhr) {
    respond(xhr, 200, '<html>not siren at all</html>');
  }, function (seen) {
    assert('a 200 that is not JSON is a parse error',
           seen.error && seen.error.kind === http.PARSE);
  });
}

function testBadHeaderName() {
  var broken = { name: 'x', baseUrl: BACKEND.baseUrl, authHeader: 'X API Key',
                 secret: SECRET, startRel: '' };
  var calls = 0;
  var error = null;
  scenario = function (xhr) { respond(xhr, 200, '{}'); };
  http.request(broken, '/x', 'GET', null, function (err) {
    calls++;
    error = err;
  });
  assert('an unusable auth header name is a config problem',
         error && error.kind === http.CONFIG);
  assert('a config problem calls back once and sends nothing', calls === 1);
}

/* --- what reaches the log ----------------------------------------------- */

function testLogHygiene() {
  var joined = logLines.join('\n');
  assert('the secret never reaches the log', joined.indexOf(SECRET) < 0);
  assert('the response headers are logged',
         joined.indexOf('content-type: application/json') >= 0);
  assert('the status is logged', /-> 200 \(\d+ms\)/.test(joined));
}

testSuccessfulGet();
testActionPost();
testEmptyPostStillHasABody();
testUnreachable();
testTimeout();
testTimeoutThenLateResponse();
testStatusMapping();
testServerWording();
testUnparseableBody();
testBadHeaderName();
testLogHygiene();

console.log = realLog;
for (var i = 0; i < results.length; i++) {
  console.log(results[i]);
}
if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('http: all checks passed');
