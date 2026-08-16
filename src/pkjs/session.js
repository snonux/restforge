/* The coordinator: wires the phone-side state machine together and is the
 * only module PebbleKit JS event handlers (index.js) talk to.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * This file used to hold the whole state machine -- navigation, frame
 * assembly, idle refresh, Siren action policy and the 409 retry, in one
 * 800-line module sharing ten globals between five different concerns.  It
 * is now split along those concerns: nav.js owns the navigation stack, frame
 * assembly, fetching and the idle-refresh clock; actions.js owns filling and
 * confirming a Siren action and its bounded retry.  Neither requires the
 * other's caller -- actions.js requires nav.js to render and send what it
 * decides, but nav.js requires nothing about actions.js.
 *
 * What is left here is what genuinely needs both: `activate`, because a row's
 * target can mean either "go somewhere" (nav.js) or "ask about doing
 * something" (actions.js), and the one piece of cross-module wiring neither
 * side can do to itself -- see the nav.setActionPendingCheck call below.
 */

'use strict';

var actions = require('./actions');
var appmessage = require('./appmessage');
var nav = require('./nav');

/* The idle-refresh timer has to hold off while an action question is
 * outstanding, even in the gap where the watch dismissed the confirm overlay
 * without answering it -- see the comments on nav.js's idleRefreshable and on
 * actions.js's `pending`.  nav.js cannot require actions.js to ask it
 * directly: actions.js already requires nav.js, and the reverse would make
 * the two modules require each other.  So the check is wired in here, once,
 * by the module whose job is to wire the pieces together. */
nav.setActionPendingCheck(actions.hasPending);

/* Passed through rather than exposing appmessage.js to index.js: the watch
 * stamps its negotiated inbox size on every message it sends, and the only
 * place that matters is the chunk sizing inside appmessage.js. */
function noteInbox(payload) {
  appmessage.noteInbox(payload);
}

/* activate turns a pressed row back into what it means.  One command covers
 * every row -- at depth 0 the rows are backends and deeper they are parts of
 * a document, but the watch does not know the difference and does not need
 * to; nav.js holds the targets and this function is the only place that
 * decides which module a target belongs to. */
function activate(index) {
  var row = nav.rowAt(index);
  if (!row || !row.target) {
    return;
  }
  var target = row.target;

  if (target.type === 'backend') {
    nav.openBackend(target.index);
  } else if (target.type === 'detail') {
    nav.send(nav.overlay(nav.documentFrame(), target.heading, target.body));
  } else if (target.type === 'embedded') {
    nav.openEmbedded(target.index);
  } else if (target.type === 'fetch') {
    nav.fetch(target.href, row.label, false);
  } else if (target.type === 'action') {
    actions.askAction(target.name);
  }
}

/* back pops one document.  Leaving the screen an action was offered on
 * abandons the question with it: actions.js clears what it was waiting for,
 * nav.js clears the screen it was waiting on -- neither module can do the
 * other's half of that on its own. */
function back() {
  actions.cancelPending();
  nav.back();
}

module.exports = {
  noteInbox: noteInbox,
  setSeq: nav.setSeq,
  answer: actions.answer,
  dismissed: nav.dismissed,
  listBackends: nav.listBackends,
  openBackend: nav.openBackend,
  activate: activate,
  back: back,
  refresh: nav.refresh
};
