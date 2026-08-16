/* RESTForge companion — PebbleKit JS entry point.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * All networking lives on this side: the watch has no IP stack, and a Siren
 * document is a few kilobytes of JSON that has no business in 128 KB of watch
 * RAM.  This file is wiring only — it translates the four events PebbleKit JS
 * gives us into calls on session.js, and does nothing else.
 */

'use strict';

var configpage = require('./configpage');
var session = require('./session');
var settings = require('./settings');

/* Mirrors CommCmd in src/c/comm.h. */
var CMD_LIST_BACKENDS = 0;
var CMD_OPEN_BACKEND = 1;
var CMD_ACTIVATE_ROW = 2;
var CMD_BACK = 3;
var CMD_REFRESH = 4;
var CMD_ANSWER = 5;
var CMD_DISMISS = 6;

/* parseConfigResponse copes with both close paths.  The phone hands back the
 * fragment of pebblejs://close#… ; the emulator hands back the query string of
 * its own /close? request.  Whether that arrives already percent-decoded
 * depends on the path taken, so try it as-is and then decoded rather than
 * assuming. */
function parseConfigResponse(response) {
  if (!response) {
    return null;
  }
  try {
    return JSON.parse(response);
  } catch (error) {
    /* Not JSON yet — fall through to the encoded case. */
  }
  try {
    return JSON.parse(decodeURIComponent(response));
  } catch (error) {
    console.log('settings page returned something unparseable: ' + error);
    return null;
  }
}

function dispatch(cmd, index) {
  if (cmd === CMD_LIST_BACKENDS) {
    session.listBackends();
  } else if (cmd === CMD_OPEN_BACKEND) {
    session.openBackend(index);
  } else if (cmd === CMD_ACTIVATE_ROW) {
    /* One command for every row: at depth 0 the rows are backends and deeper
     * they are parts of a document, but the watch does not know the difference
     * and does not need to — session.js holds the targets. */
    session.activate(index);
  } else if (cmd === CMD_BACK) {
    session.back();
  } else if (cmd === CMD_REFRESH) {
    session.refresh();
  } else if (cmd === CMD_DISMISS) {
    session.dismissed();
  }
}

/* dispatchAnswer is separate because the answer arrives with its own key
 * rather than a row index: the watch is replying to a question, not pointing
 * at a row. */
function dispatchAnswer(payload) {
  session.answer(payload.ANSWER === 1, payload.ANSWER_TEXT);
}

Pebble.addEventListener('ready', function () {
  console.log('RESTForge companion ready, ' + settings.count() + ' backend(s)');
  session.listBackends();
});

Pebble.addEventListener('appmessage', function (event) {
  var payload = event.payload || {};
  session.noteInbox(payload);
  session.setSeq(payload.SEQ || 0);
  console.log('cmd ' + payload.CMD + ' idx ' + payload.IDX + ' seq ' +
              (payload.SEQ || 0));
  if (payload.CMD === CMD_ANSWER) {
    dispatchAnswer(payload);
  } else {
    dispatch(payload.CMD, payload.IDX || 0);
  }
});

Pebble.addEventListener('showConfiguration', function () {
  var uri = configpage.dataUri(settings.load(), settings.DEFAULT_AUTH_HEADER,
                               settings.MAX_BACKENDS);
  /* Logged as a length, never as content: the URI has the secrets in it.  The
   * length is the useful half anyway — a webview that silently refuses a large
   * data: URI is the failure this line exists to diagnose. */
  console.log('settings page: ' + uri.length + ' byte data: URI');
  Pebble.openURL(uri);
});

Pebble.addEventListener('webviewclosed', function (event) {
  var config = parseConfigResponse(event && event.response);
  if (!config) {
    /* Cancelled, or the webview was dismissed.  Leave storage alone — an empty
     * response must not be read as "the user deleted every backend". */
    console.log('settings page closed without saving');
    return;
  }
  settings.save(config);
  /* Back to the picker: the backend that was open may no longer exist, and its
   * secret or base URL may have changed underneath it. */
  session.listBackends('Saved');
});
