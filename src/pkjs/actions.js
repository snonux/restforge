/* Siren action policy: filling an action's fields, phrasing the confirmation
 * the user is asked for, invoking it, and the one retry the hypermedia
 * contract allows on a 409.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * This sits on top of nav.js rather than beside it: everything below asks
 * nav.js to render and send a frame, or to fetch and re-fetch a document, and
 * never touches the navigation stack directly.  What belongs here instead is
 * the policy nav.js has no business knowing -- which field a checkbox fills,
 * what makes a confirmation sentence, and the narrow, contract-permitted
 * exception where repeating a request is the right thing to do.
 *
 * The one rule worth restating because it is easy to get backwards: a 409 gets
 * the same treatment as a success everywhere below except the one case that
 * was just confirmed.  The contract says to re-fetch and re-render, never to
 * retry, because a 409 means the state acted on was stale and the remedy is to
 * look again -- retrying would repeat a request the server already judged
 * wrong.
 */

'use strict';

var http = require('./http');
var live = require('./live');
var nav = require('./nav');
var render = require('./render');
var siren = require('./siren');

/* HTTP methods that only read.  RFC 9110 calls these safe, and the division is
 * the whole basis for asking before acting: anything outside this set is a
 * request to change something, and on a device with four buttons that must not
 * happen on the same press that opens a property. */
var SAFE_METHODS = { GET: true, HEAD: true, OPTIONS: true, TRACE: true };

/* The action awaiting confirmation, or null.  Held here rather than on the
 * watch because it carries an href, and the watch never sees one.
 *
 * nav.js's idle-refresh timer also has to know whether this is set (see
 * hasPending and nav.setActionPendingCheck) -- a confirm overlay dismissed by
 * the watch itself, rather than answered, clears nav.js's overlay flag without
 * this being cleared, and a refresh landing on top of that would silently
 * invalidate a question the user has not answered yet. */
var pending = null;

/* A confirmation the user gave, kept just long enough to answer a server that
 * asks for it after the fact.
 *
 * The contract allows this narrow case: an action whose required checkbox the
 * user explicitly ticked can still come back 409, because the server judges
 * the request twice against budgets that can change in between.  Re-sending
 * the same confirmed field once is legitimate; synthesising a confirmation
 * nobody gave is not, which is why this records only what was actually
 * confirmed, and only briefly. */
var confirmedAt = null;

/* How long a confirmation stays good for a retry. */
var CONFIRMATION_TTL_MS = 60000;

/* hasPending is nav.js's window into this module's one piece of state it
 * needs to know about, wired in by session.js -- see the comment on
 * `pending` above and on nav.js's idleRefreshable. */
function hasPending() {
  return !!pending;
}

/* cancelPending abandons whatever question was on screen, without sending
 * anything.  Called by session.js when the screen an action was offered on is
 * being left, so the question does not outlive the document it was about. */
function cancelPending() {
  pending = null;
}

/* fieldValues fills an action's fields generically.  Nothing here knows what
 * any field means:
 *
 *  - a checkbox carries the user's confirmation, because a required checkbox
 *    *is* the confirmation this app already asked for;
 *  - anything else takes the default the server offered in "value".
 *
 * A required field with neither of those is refused rather than guessed at.
 * Inventing a value for a field a server marked required is how a client ends
 * up doing something nobody asked for. */
function fieldValues(action, confirmed, spoken) {
  var list = siren.fields(action);
  var values = {};
  var missing = null;
  for (var i = 0; i < list.length; i++) {
    var field = list[i];
    if (!field || typeof field.name !== 'string') {
      continue;
    }
    if (field.type === 'checkbox') {
      values[field.name] = confirmed ? 'true' : 'false';
    } else if (spoken && spoken.name === field.name) {
      values[field.name] = spoken.text;
    } else if (field.value !== undefined && field.value !== null) {
      values[field.name] = String(field.value);
    } else if (field.required && !missing) {
      /* Asked for by voice rather than refused -- but only the first one.
       * Filling several fields by dictation on a watch is worse than saying
       * plainly that this is not the device for it. */
      missing = field;
    } else if (field.required) {
      return { problem: 'needs values for several fields, which is more than ' +
                        'this app can ask for on a watch' };
    }
  }
  return { values: values, missing: missing };
}

/* fieldLabel is what to call a field on screen: the server's wording first,
 * then the name it addresses the field by. */
function fieldLabel(field) {
  return (typeof field.title === 'string' && field.title) ? field.title
                                                          : field.name;
}

/* confirmationText is what the user reads before pressing SELECT.  A required
 * checkbox's title is preferred over everything else, because that sentence is
 * the server explaining the consequence -- it is written for exactly this
 * moment and nothing this app could add would improve it. */
function confirmationText(action) {
  var list = siren.fields(action);
  for (var i = 0; i < list.length; i++) {
    if (list[i] && list[i].type === 'checkbox' && list[i].required &&
        typeof list[i].title === 'string' && list[i].title) {
      return list[i].title;
    }
  }
  return siren.label(action) + '?  ' + siren.method(action) + ' to this server.';
}

function hasRequiredCheckbox(action) {
  var list = siren.fields(action);
  for (var i = 0; i < list.length; i++) {
    if (list[i] && list[i].type === 'checkbox' && list[i].required) {
      return true;
    }
  }
  return false;
}

/* askAction puts the question on screen and remembers what it was about.  A
 * safe method is not a change and goes straight through -- Siren allows a GET
 * action, and stopping to confirm a read would be noise. */
function askAction(name) {
  var action = siren.action(nav.top().entity, name);
  if (!action) {
    /* The server withdrew it between rendering and pressing.  That is a real
     * answer -- it means this cannot be done now -- so re-render rather than
     * inventing a request. */
    console.log('action "' + name + '" is no longer offered');
    nav.sendCurrent('Not offered');
    return;
  }
  pending = { name: name, href: action.href, method: siren.method(action) };
  if (SAFE_METHODS[pending.method]) {
    invoke(true);
    return;
  }
  nav.send(nav.overlay(nav.documentFrame(), siren.label(action),
           confirmationText(action), nav.OVERLAY_CONFIRM));
}

/* invoke performs the pending action, then re-fetches.
 *
 * The re-fetch is unconditional and is the contract's own rule: a client must
 * not carry a document across an action.  Whatever the action changed, the
 * only trustworthy account of the result is the server's next answer, not the
 * one we were holding when we asked. */
function invoke(confirmed, spoken) {
  var here = nav.top();
  var action = here ? siren.action(here.entity, pending.name) : null;
  if (!action) {
    pending = null;
    nav.sendCurrent('Not offered');
    return;
  }

  var filled = fieldValues(action, confirmed, spoken);
  if (filled.problem) {
    invokeRefused(filled.problem);
    return;
  }
  if (filled.missing) {
    invokeAskForValue(filled.missing);
    return;
  }

  invokeSend(here, action, confirmed, filled.values);
}

/* invokeRefused reports a field problem fieldValues() would rather not guess
 * around -- see the comment there.  Split out of invoke() to keep that
 * function to the shape of its own decision tree. */
function invokeRefused(problem) {
  var refused = pending.name;
  pending = null;
  nav.send(nav.overlay(nav.documentFrame('Cannot send'), refused, problem));
}

/* invokeAskForValue asks out loud for a required field with no default and no
 * confirmation to stand in for it, rather than inventing one -- a value
 * nobody supplied for a field the server marked required is how a client
 * does something nobody asked for. */
function invokeAskForValue(missing) {
  pending.awaiting = missing.name;
  nav.send(nav.overlay(nav.documentFrame(), fieldLabel(missing),
           'Say the value for "' + missing.name + '".', nav.OVERLAY_TEXT));
}

/* invokeSend is the case where there is nothing left to ask: every field has
 * a value, so the request goes out. */
function invokeSend(here, action, confirmed, values) {
  var label = siren.label(action);
  console.log('invoking "' + pending.name + '" (' + pending.method + ')');
  nav.sendLoading(here.title || label);

  /* Remembered for the one retry the contract allows; see confirmedAt. */
  if (confirmed && hasRequiredCheckbox(action)) {
    confirmedAt = { name: pending.name, values: values, at: Date.now() };
  }

  var href = pending.href;
  var method = pending.method;
  var name = pending.name;
  pending = null;
  http.request(nav.backend(), href, method, values, function (error, result) {
    afterAction(error, result, label, name, href, method);
  });
}

/* retryable answers the narrow question the contract permits: did the user
 * confirm *this* action, just now?  Anything else is a no. */
function retryable(name) {
  return !!(confirmedAt && confirmedAt.name === name &&
            Date.now() - confirmedAt.at < CONFIRMATION_TTL_MS);
}

/* resultBanner reports what came back without interpreting it.  A "state"
 * property is used because it is the server's own word for what happened; it
 * is repeated, not read for meaning. */
function resultBanner(result) {
  var state = siren.properties(result.entity).state;
  if (typeof state === 'string' && state) {
    return state;
  }
  return result.status === 202 ? 'Accepted' : 'Done';
}

/* describeResult renders the response body generically, so a job id or an
 * error the server put in the response is not thrown away.  Following it
 * properly -- polling a job until it finishes -- is the next step. */
function describeResult(result) {
  return describeEntity(result.entity) || 'HTTP ' + result.status;
}

function describeEntity(entity) {
  var values = siren.properties(entity);
  var parts = [];
  for (var key in values) {
    if (Object.prototype.hasOwnProperty.call(values, key)) {
      parts.push(key + ': ' + render.text(values[key]));
    }
  }
  return parts.join('   ');
}

/* afterAction records the outcome and re-fetches the document underneath it.
 *
 * A 409 gets the same treatment as a success on purpose: the contract says to
 * re-fetch and re-render, never to retry, because a 409 means the state we
 * acted on was stale and the remedy is to look again.  Retrying would repeat
 * a request the server has already judged wrong. */
function afterAction(error, result, label, name, href, method) {
  if (error) {
    afterActionError(error, label, name, href, method);
    return;
  }
  afterActionSuccess(result, label);
}

/* afterActionError handles a failed request, including the one retry the
 * contract allows -- taken once and only for a confirmation the user actually
 * gave.  A server can judge the same request twice on budgets that changed in
 * between, and re-sending the confirmed field is the documented remedy. */
function afterActionError(error, label, name, href, method) {
  if (error.kind === http.CONFLICT && retryable(name)) {
    var values = confirmedAt.values;
    confirmedAt = null;
    console.log('conflict on a confirmed action: re-sending it once');
    http.request(nav.backend(), href, method, values, function (err, res) {
      afterAction(err, res, label, null, href, method);
    });
    return;
  }
  nav.setNotice({ banner: nav.bannerFor(error.kind), state: nav.stateFor(error.kind),
                  heading: label, body: error.message });
  console.log('action failed: ' + error.kind + ' ' + error.status + ' ' +
              error.message);
  /* An auth failure or an unreachable server tells us nothing new about the
   * document, and re-fetching would only fail the same way. */
  if (error.kind !== http.CONFLICT) {
    nav.sendCurrent();
    return;
  }
  nav.refetchCurrent();
}

/* afterActionSuccess handles a request the server accepted.  Not finished
 * when the request was: a response still running is handed to live.js to
 * keep watching, rather than shown as a result that has already stopped
 * being true. */
function afterActionSuccess(result, label) {
  confirmedAt = null;
  console.log('action ok: ' + result.status + ' ' +
              siren.summary(result.entity));

  if (live.start(nav.backend(), nav.top(), result, liveHandlers(label))) {
    nav.setNotice({ banner: resultBanner(result), heading: label,
                    body: describeResult(result) });
    nav.sendCurrent();
    return;
  }

  nav.setNotice({ banner: resultBanner(result), heading: label,
                  body: describeResult(result) });
  nav.refetchCurrent();
}

/* liveHandlers renders each stage of something still happening.  Progress is
 * reported in the banner with the server's own words for it -- the step, if it
 * gives one -- and nothing is interpreted along the way. */
function liveHandlers(label) {
  return {
    onProgress: function (entity) {
      var values = siren.properties(entity);
      var step = typeof values.step === 'string' && values.step
        ? values.step : String(values.state);
      nav.sendCurrent(step);
    },
    onDone: function (entity) {
      /* The job stopped; now the document it acted on is worth re-reading,
       * because that is where the effect shows. */
      nav.setNotice({ banner: String(siren.properties(entity).state || 'Done'),
                      heading: label, body: describeEntity(entity) });
      nav.refetchCurrent();
    },
    onGiveUp: function () {
      /* Not a failure of the job -- a failure to keep watching it.  Saying so
       * is the honest report; claiming it finished would not be. */
      nav.setNotice({ banner: 'Gave up', state: nav.STATE_UNREACHABLE, heading: label,
                      body: 'Stopped watching. The server may still be working on ' +
                            'this; refresh to look again.' });
      nav.refetchCurrent();
    }
  };
}

/* answer handles the reply to a confirmation, or to a request for a value. */
function answer(confirmed, text) {
  if (!pending) {
    /* The question was superseded before it was answered. */
    nav.sendCurrent();
    return;
  }
  if (!confirmed) {
    console.log('declined "' + pending.name + '"');
    pending = null;
    nav.sendCurrent();
    return;
  }
  if (pending.awaiting) {
    answerSpoken(text);
    return;
  }
  invoke(true);
}

/* answerSpoken handles the reply to the "say the value" prompt, split out of
 * answer() so that function stays a plain dispatch over the shape of the
 * reply. */
function answerSpoken(text) {
  if (typeof text !== 'string' || !text) {
    /* Confirmed but with nothing said: an empty value is not an answer. */
    var name = pending.awaiting;
    pending = null;
    nav.send(nav.overlay(nav.documentFrame('No value'), name,
             'Nothing was heard, so nothing was sent.'));
    return;
  }
  var spoken = { name: pending.awaiting, text: text };
  pending.awaiting = null;
  invoke(true, spoken);
}

module.exports = {
  hasPending: hasPending,
  cancelPending: cancelPending,
  askAction: askAction,
  answer: answer
};
