/* Navigation: where we are, what is on screen, and how it got there.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * The navigation stack lives here, on the phone, and not on the watch.  The
 * watch has one list window and no idea how deep it is; it reports "row N was
 * pressed" and "back was pressed", and this module decides what that means.
 * That is what keeps every href and every secret off the watch, and it is also
 * why BACK has to be intercepted there rather than popping a window.
 *
 * One rule from the hypermedia contract shapes almost everything below: a
 * request that failed is not an answer.  A failure never replaces the document
 * on screen with an empty one -- it leaves the last good document exactly
 * where it was and puts the reason on top of it, because "I could not ask" and
 * "the answer was no" are different things and only one of them is about the
 * server.
 *
 * What this module does *not* own: the policy around a Siren action -- filling
 * its fields, phrasing its confirmation, retrying it once on a 409 -- lives in
 * actions.js, on top of this one.  This module knows nothing about that
 * policy; it only renders frames, fetches documents and runs the idle-refresh
 * clock.  session.js wires the two together and is the only module that
 * requires both.
 */

'use strict';

var appmessage = require('./appmessage');
var http = require('./http');
var live = require('./live');
var render = require('./render');
var quick = require('./quick');
var settings = require('./settings');
var siren = require('./siren');

/* Mirrors DocState in src/c/doc.h. */
var STATE_OK = 0;
var STATE_LOADING = 1;
var STATE_ERROR = 2;
var STATE_UNREACHABLE = 3;
var STATE_NEEDS_CONFIG = 4;

/* Mirrors DocOverlay in src/c/doc.h. */
var OVERLAY_NONE = 0;
var OVERLAY_CONFIRM = 1;
var OVERLAY_TEXT = 2;
var OVERLAY_DETAIL = 3;

/* Matches DOC_MAX_ROWS in src/c/doc.h.  Capping here rather than letting
 * appmessage.js do it keeps the row indices the watch sends back aligned with
 * the targets below -- a truncation the state machine did not know about would
 * silently shift every index past it. */
var MAX_ROWS = 40;

/* The backend being browsed, and the documents we walked through to get here.
 * Each stack frame is { entity, href, title }; href may be null for a
 * sub-entity that was embedded rather than linked, which only means it cannot
 * be re-fetched on its own. */
var backendIndex = -1;
var backend = null;
var stack = [];

/* Targets for the rows currently on the watch, indexed exactly as the watch
 * indexes them. */
var rows = [];

/* A banner and optional overlay to apply to the next document frame.  An
 * action's outcome has to survive the re-fetch that the contract requires
 * after it -- otherwise the re-fetched document would arrive clean and the
 * user would never learn what the action did.  Set from outside this module
 * (actions.js, via setNotice) but only ever read and cleared here, so the
 * shape of a notice is not this module's business -- only that it exists. */
var notice = null;

/* True while something is on screen to be read.  Its single purpose is to stop
 * a background refresh yanking the screen out from under someone mid-sentence.
 *
 * It is exact rather than a guess: it is set from the frame that was sent, and
 * cleared by anything the watch says -- including the message the watch sends
 * when the reader dismisses the overlay themselves.  Without that message this
 * would latch on forever and the background refresh would never resume, which
 * is precisely what it did until a live test caught it. */
var overlayShowing = false;

/* How often to re-read the visible document when nothing else is going on.
 * A watchface people glance at should not be showing minute-old state. */
var IDLE_MS = 60000;
var idleTimer = null;

/* Echoed back so the watch can drop a reply to a command it has moved past. */
var seq = 0;

/* actionPending is asked by idleRefreshable(), below.  A pending action
 * question has to hold off the idle refresh exactly as an open overlay does --
 * including in the gap where the watch has dismissed the overlay without the
 * question being answered, which overlayShowing alone does not cover (see the
 * comment on idleRefreshable).  "pending" is actions.js's state, though, and
 * this module must not require actions.js to read it: actions.js already
 * requires nav.js for frame assembly and sending, and the other direction
 * would make the two require() each other.  So this is asked through a hook
 * instead of a require -- wired once, by session.js, the module whose job is
 * to wire the two together. */
var actionPending = function () { return false; };

function setActionPendingCheck(fn) {
  actionPending = fn;
}

function setSeq(value) {
  seq = value || 0;
  /* Any press means the user is still here and has moved past whatever was on
   * screen, overlay included. */
  overlayShowing = false;
}

function send(frame) {
  frame.seq = seq;
  /* Derived from the frame rather than set where overlays are built: a frame
   * without one supersedes whatever was on screen, so this stays true without
   * anybody having to remember to clear it. */
  overlayShowing = !!frame.promptKind;
  appmessage.send(frame);
  scheduleIdle();
}

function top() {
  return stack.length ? stack[stack.length - 1] : null;
}

/* currentBackend is a live accessor rather than a value handed out once:
 * openBackend and listBackends reassign backend as the user moves between
 * servers, and actions.js needs whichever one is current at the moment it
 * sends a request, not whichever one was current when it first asked. */
function currentBackend() {
  return backend;
}

/* stateFor maps an http.js error kind onto a DocState. */
function stateFor(kind) {
  if (kind === http.UNREACHABLE || kind === http.TIMEOUT) {
    return STATE_UNREACHABLE;
  }
  if (kind === http.CONFIG) {
    return STATE_NEEDS_CONFIG;
  }
  return STATE_ERROR;
}

/* bannerFor is a word, not a sentence: the banner is a band across the top of
 * a round display.  The server's own wording goes in the overlay underneath
 * it, where there is a whole screen to read it on. */
function bannerFor(kind) {
  if (kind === http.UNREACHABLE) {
    return 'Unreachable';
  }
  if (kind === http.TIMEOUT) {
    return 'Timed out';
  }
  if (kind === http.AUTH) {
    return 'Auth';
  }
  if (kind === http.CONFLICT) {
    return 'Conflict';
  }
  if (kind === http.CONFIG) {
    return 'Config';
  }
  return 'Error';
}

/* --- frames -------------------------------------------------------------- */

/* The opening screen: saved shortcuts first, then the backends they came
 * from.  Shortcuts lead the list because reaching one in a single press is
 * the whole point of having saved it; with none saved the screen is exactly
 * what it was before. */
function pickerFrame(message) {
  var shortcuts = quick.rows();
  var picker = settings.rows();

  rows = [];
  for (var q = 0; q < shortcuts.length; q++) {
    rows.push({ target: { type: 'quick', index: q } });
  }
  for (var i = 0; i < picker.length; i++) {
    rows.push({ target: { type: 'backend', index: i } });
  }

  if (!picker.length && !shortcuts.length) {
    /* Not an empty list: an empty menu is what a backend with nothing in it
     * would look like, and this is not that. */
    return { title: 'RESTForge', state: STATE_NEEDS_CONFIG, atRoot: true,
             message: message || 'No backends',
             rows: [{ label: 'Open the Pebble app',
                      sublabel: 'Settings on RESTForge', kind: 'p' }] };
  }
  return { title: 'RESTForge', state: STATE_OK, atRoot: true,
           message: message, rows: shortcuts.concat(picker) };
}

/* capped trims the row list to what the watch can hold, and says so with a row
 * rather than dropping the tail silently. */
function capped(list) {
  if (list.length <= MAX_ROWS) {
    return list;
  }
  var kept = list.slice(0, MAX_ROWS - 1);
  kept.push({ label: '(' + (list.length - kept.length) + ' more)',
              kind: render.KIND_PROPERTY, target: { type: 'none' } });
  return kept;
}

/* documentFrame renders the document on top of the stack and remembers its row
 * targets.  Every frame that shows a document goes through here, so the targets
 * and what the watch is looking at cannot drift apart. */
function documentFrame(message) {
  var here = top();
  var page = render.document(here.entity, here.title);
  rows = capped(page.rows);
  /* Not the root: BACK here pops the navigation stack rather than leaving the
   * app, and only this side knows how deep the stack is. */
  return { title: page.title, state: STATE_OK, atRoot: false, message: message,
           rows: rows, live: live.isLive() };
}

/* rowAt is how a row activated on the watch is turned back into the target it
 * was rendered with.  Kept private to this module everywhere except here:
 * activate() in session.js has to read a target to decide whether a press
 * means "go somewhere" or "ask about doing something", but nothing outside
 * this module should be able to see the row list itself. */
function rowAt(index) {
  return rows[index];
}

/* applyNotice folds an action's outcome onto the frame that follows it, and
 * consumes it -- a notice describes one moment and must not reappear on the
 * next refresh. */
function applyNotice(frame) {
  if (!notice) {
    return frame;
  }
  frame.message = notice.banner;
  if (notice.state !== undefined) {
    frame.state = notice.state;
  }
  if (notice.body) {
    overlay(frame, notice.heading, notice.body);
  }
  notice = null;
  return frame;
}

/* setNotice records a banner and optional overlay for the next frame this
 * module sends.  The only caller is actions.js, once an action's outcome (or
 * a live job's progress) is known -- this module never decides what a notice
 * says, only when it stops applying. */
function setNotice(value) {
  notice = value;
}

/* overlay puts a heading and a body on top of a frame without disturbing it.
 * The kind decides which window the watch opens: something to read, or a
 * question it must answer. */
function overlay(frame, heading, body, kind) {
  frame.heading = heading;
  frame.prompt = body;
  frame.promptKind = kind === undefined ? OVERLAY_DETAIL : kind;
  return frame;
}

/* --- idle refresh --------------------------------------------------------
 *
 * Quiet on purpose: no loading frame, and a failure sets the banner rather
 * than throwing an overlay over whatever the user is looking at.  A refresh
 * nobody asked for should never take the screen away from them.
 */

/* idleRefreshable answers whether a background refresh is safe right now.
 * actionPending() is the subtle one: a required checkbox's confirm overlay
 * dismissed by the watch itself (not answered) clears overlayShowing without
 * clearing the question actions.js is still waiting on, and a refresh landing
 * on top of that would silently invalidate an answer the user has not given
 * yet. */
function idleRefreshable() {
  return stack.length > 0 && !live.isLive() && !overlayShowing &&
         !actionPending() && top() && top().href;
}

/* dismissed is the watch reporting that the reader closed an overlay.  setSeq
 * has already cleared the flag by the time this runs; what is left is to start
 * the idle clock again, which only send() otherwise does. */
function dismissed() {
  scheduleIdle();
}

function scheduleIdle() {
  if (idleTimer) {
    clearTimeout(idleTimer);
    idleTimer = null;
  }
  if (idleRefreshable()) {
    idleTimer = setTimeout(idleRefresh, IDLE_MS);
  } else {
    /* Worth a line: a background refresh that quietly stops happening looks
     * like nothing at all, and is how this went unnoticed until a live test. */
    console.log('idle refresh not scheduled (live=' + live.isLive() +
                ' overlay=' + overlayShowing + ' depth=' + stack.length + ')');
  }
}

function idleRefresh() {
  idleTimer = null;
  if (!idleRefreshable()) {
    return;
  }
  var here = top();
  http.get(backend, here.href, function (error, result) {
    /* Whatever it was refreshing may be long gone by now. */
    if (top() !== here) {
      return;
    }
    if (error) {
      var frame = documentFrame(bannerFor(error.kind));
      frame.state = stateFor(error.kind);
      send(frame);
      return;
    }
    stack[stack.length - 1] = { entity: result.entity, href: here.href,
                                title: here.title };
    sendCurrent();
  });
}

function sendCurrent(message) {
  send(applyNotice(stack.length ? documentFrame(message) : pickerFrame(message)));
}

/* sendFailure is the rule at the top of this file, made concrete: the frame
 * underneath is whatever was already there, and the reason goes on top of it
 * where it can be read in full and dismissed. */
function sendFailure(error) {
  console.log('request failed: ' + error.kind + ' ' + error.status + ' ' +
              error.message);
  var frame = stack.length ? documentFrame(bannerFor(error.kind))
                           : pickerFrame(bannerFor(error.kind));
  frame.state = stateFor(error.kind);
  send(overlay(frame, bannerFor(error.kind), error.message));
}

function sendLoading(title) {
  /* The targets go with the rows.  Leaving the previous document's targets in
   * place would mean a press during the load acting on a row that is no longer
   * on screen. */
  rows = [];
  /* A load always happens inside a backend, so BACK still means "go back". */
  send({ title: title, state: STATE_LOADING, atRoot: false, message: 'Loading',
         rows: [], promptKind: OVERLAY_NONE });
}

/* --- fetching ------------------------------------------------------------ */

/* selfHref is how a document knows its own address, so it can be re-fetched.
 * "self" is a registered link relation, not one server's vocabulary. */
function selfHref(entity) {
  return siren.follow(entity, 'self');
}

function push(entity, href, title) {
  stack.push({ entity: entity, href: href || selfHref(entity), title: title || '' });
  sendCurrent();
}

/* fetch retrieves a document and pushes or replaces it.  Replacing is what a
 * refresh does: the document is the same one, just newer. */
function fetch(href, title, replace) {
  if (!replace) {
    /* Navigating somewhere new: whatever was being watched belonged to the
     * screen being left. */
    live.stop();
  }
  sendLoading(title || (top() ? top().title : backend.name));
  http.get(backend, href, function (error, result) {
    if (error) {
      sendFailure(error);
      return;
    }
    console.log(title + ': ' + siren.summary(result.entity));
    if (replace && stack.length) {
      stack[stack.length - 1] = { entity: result.entity, href: href,
                                  title: title || '' };
      sendCurrent();
    } else {
      push(result.entity, href, title);
    }
  });
}

/* refetchCurrent replaces the document on top of the stack with a fresh copy.
 * An embedded document has no address of its own, so re-rendering is the
 * honest option there; the notice still explains what happened.  Exposed for
 * actions.js: an action's re-fetch afterwards is the contract's own rule (see
 * afterAction in actions.js), not a choice this module makes. */
function refetchCurrent() {
  var here = top();
  if (!here || !here.href) {
    sendCurrent();
    return;
  }
  fetch(here.href, here.title, true);
}

/* fetchRoot opens a backend at its base URL, then follows the configured
 * startRel if it has one.  The rel is the only server-specific string anywhere
 * in this app, and the user typed it into the settings page themselves. */
function fetchRoot() {
  sendLoading(backend.name);
  http.get(backend, backend.baseUrl, function (error, result) {
    if (error) {
      sendFailure(error);
      return;
    }
    /* Checked before anything is rendered: a server speaking a version this
     * app was not written against may have changed the meaning of something it
     * would otherwise display confidently and wrongly. */
    var problem = siren.versionProblem(result.entity);
    if (problem) {
      sendFailure({ kind: http.CLIENT, status: 0, message: problem });
      return;
    }
    push(result.entity, backend.baseUrl, backend.name);
    if (backend.startRel) {
      followStart(result.entity);
    }
  });
}

function followStart(root) {
  var href = siren.follow(root, backend.startRel);
  if (!href) {
    console.log('no link with rel "' + backend.startRel + '" on the root');
    return;
  }
  fetch(href, backend.startRel, false);
}

/* --- commands ------------------------------------------------------------ */

function listBackends(message) {
  live.stop();
  backendIndex = -1;
  backend = null;
  stack = [];
  send(pickerFrame(message));
}

function openBackend(index) {
  var chosen = settings.get(index);
  if (!chosen) {
    listBackends();
    return;
  }
  live.stop();
  backendIndex = index;
  backend = chosen;
  /* A new backend starts with nothing carried over: keeping the previous
   * backend's document on screen while loading another one would be showing
   * one server's state under another server's name. */
  stack = [];
  console.log('open "' + backend.name + '" at ' + backend.baseUrl +
              ' (auth header ' + backend.authHeader + ')');
  fetchRoot();
}

/* openEmbedded pushes a sub-entity that arrived inside the document we are
 * already looking at.  Nothing is fetched: it is already in hand, and asking
 * the server for it again could legitimately return something different. */
function openEmbedded(childIndex) {
  var children = siren.subEntities(top().entity);
  var child = children[childIndex];
  if (!child) {
    return;
  }
  push(child, selfHref(child), siren.label(child));
}

/* back pops one document.  At the bottom of a backend's stack it returns to
 * the picker, which is the only place the watch can go that is not a
 * document.  An overlay is dismissed by the watch itself and never reaches
 * here.  Clearing whatever action question the screen being left was showing
 * is session.js's job (it owns the wiring to actions.js); by the time this
 * runs there is nothing here that needs to know one was ever pending. */
function back() {
  live.stop();
  if (!stack.length) {
    listBackends();
    return;
  }
  stack.pop();
  if (!stack.length) {
    listBackends();
    return;
  }
  sendCurrent();
}

/* refresh re-fetches what is on screen.  A document that arrived embedded in
 * another one has no address of its own, so there is nothing to re-fetch --
 * re-rendering what we have is the honest answer, rather than silently
 * fetching its parent and hoping the same child is still there. */
function refresh() {
  var here = top();
  if (!here) {
    listBackends();
    return;
  }
  if (!here.href) {
    console.log('nothing to refresh: this document was embedded, not linked');
    sendCurrent();
    return;
  }
  fetch(here.href, here.title, true);
}

/* adopt switches to a backend without fetching its root.  A shortcut jumps
 * straight to somewhere inside a backend, so the usual "open the root first"
 * of openBackend would be a wasted request and a screen nobody asked for. */
function adopt(chosen) {
  live.stop();
  backend = chosen;
  backendIndex = -1;
  /* Nothing carried over, for the same reason openBackend clears it. */
  stack = [];
}

/* openQuickDocument follows a saved document shortcut: adopt its backend,
 * then fetch the address it saved.  The stack starts empty, so BACK from it
 * returns to the opening screen rather than into a history nobody walked. */
function openQuickDocument(item, chosen) {
  adopt(chosen);
  fetch(item.href, item.label, false);
}

/* openQuickHolder fetches the document that offers a saved action, and hands
 * it to the callback once it is on the stack.  The action is looked up by name in
 * whatever comes back, never at a remembered href: the server withdraws
 * actions as its state changes, and "not offered right now" is a real answer
 * that has to be able to surface. */
function openQuickHolder(item, chosen, then) {
  adopt(chosen);
  sendLoading(item.label);
  http.get(chosen, item.holder, function (error, result) {
    if (error) {
      sendFailure(error);
      return;
    }
    stack = [{ entity: result.entity, href: item.holder, title: item.label }];
    then();
  });
}

module.exports = {
  pickerFrame: pickerFrame,
  openQuickDocument: openQuickDocument,
  openQuickHolder: openQuickHolder,
  STATE_OK: STATE_OK,
  STATE_LOADING: STATE_LOADING,
  STATE_ERROR: STATE_ERROR,
  STATE_UNREACHABLE: STATE_UNREACHABLE,
  STATE_NEEDS_CONFIG: STATE_NEEDS_CONFIG,
  OVERLAY_NONE: OVERLAY_NONE,
  OVERLAY_CONFIRM: OVERLAY_CONFIRM,
  OVERLAY_TEXT: OVERLAY_TEXT,
  OVERLAY_DETAIL: OVERLAY_DETAIL,
  setActionPendingCheck: setActionPendingCheck,
  setSeq: setSeq,
  send: send,
  top: top,
  backend: currentBackend,
  bannerFor: bannerFor,
  stateFor: stateFor,
  documentFrame: documentFrame,
  rowAt: rowAt,
  setNotice: setNotice,
  overlay: overlay,
  dismissed: dismissed,
  sendCurrent: sendCurrent,
  sendLoading: sendLoading,
  fetch: fetch,
  refetchCurrent: refetchCurrent,
  listBackends: listBackends,
  openBackend: openBackend,
  openEmbedded: openEmbedded,
  back: back,
  refresh: refresh
};
