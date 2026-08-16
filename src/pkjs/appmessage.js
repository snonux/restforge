/* AppMessage framing between PebbleKit JS and the watch.
 *
 * ES5 ONLY.  The emulator's PKJS runs a modern V8 and will happily accept
 * arrow functions, block-scoped declarations and template literals; it does no
 * transpilation, and the device contract is ES5.1.  So the emulator is a false
 * green for syntax, and anything newer than ES5 here ships broken.
 *
 * A frame is one screen: a title, a list of rows, a message, an overlay
 * (heading and body) and a few scalars.  It is serialised into a single string and chunked, because the
 * watch's inbox size is negotiated with the *phone app* at runtime -- 8200
 * bytes when it advertises the relevant protocol capability, 2026 otherwise --
 * and is not something that can be inferred from the watch model.  The watch
 * reports what it actually got in the INBOX key and we size chunks to fit.
 *
 * The scalars ride on the final chunk so the watch applies them atomically
 * with the text they describe. */

'use strict';

var keys = require('message_keys');

/* Separators, matching src/c/doc.c.  sanitise() below guarantees they cannot
 * occur inside a field, so no escaping is needed. */
var FIELD_SEP = '\x1f';
var ROW_SEP = '\n';
var SUB_SEP = '\t';

/* Bounds on what a screen may contain.  The watch caps rows too; exceeding
 * either is reported to the user as an extra "(N more)" row rather than
 * silently dropping the tail. */
var MAX_ROWS = 40;
var MAX_LABEL = 48;

/* Chunk sizing.  1000 is the same conservative ceiling the SDK's own
 * _pkjs_message_wrapper.js uses.  The 64-byte reserve covers the dictionary
 * header plus seven tuple headers. */
var MAX_CHUNK = 1000;
var MIN_CHUNK = 64;
var CHUNK_RESERVE = 64;
var DEFAULT_CHUNK = 512;

var MAX_ATTEMPTS = 3;
var RETRY_DELAY_MS = 500;

var inboxSize = 0;

/* noteInbox records the watch's negotiated inbox size, which it stamps on
 * every message it sends up. */
function noteInbox(payload) {
  if (payload && payload.INBOX) {
    var size = Number(payload.INBOX);
    if (size > 0 && size !== inboxSize) {
      inboxSize = size;
      console.log('watch inbox size is ' + inboxSize + ' bytes');
    }
  }
}

function chunkSize() {
  var usable = inboxSize > 0 ? inboxSize - CHUNK_RESERVE : DEFAULT_CHUNK;
  if (usable > MAX_CHUNK) {
    usable = MAX_CHUNK;
  }
  if (usable < MIN_CHUNK) {
    usable = MIN_CHUNK;
  }
  return usable;
}

/* sanitise removes every control character.  This is what makes the three
 * separators safe: a server is free to put a newline or a 0x1f inside a JSON
 * string, and one arriving in a title would otherwise split a row. */
function sanitise(value) {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value).replace(/[\x00-\x1f\x7f]/g, ' ').replace(/\s+/g, ' ').replace(/^ | $/g, '');
}

function truncate(text, max) {
  if (text.length <= max) {
    return text;
  }
  return text.slice(0, max - 1) + '…';
}

/* buildPayload serialises a frame into the single string the watch parses. */
function buildPayload(frame) {
  var rows = frame.rows || [];
  var labels = [];
  var kinds = '';
  var limit = rows.length > MAX_ROWS ? MAX_ROWS - 1 : rows.length;

  for (var i = 0; i < limit; i++) {
    var label = truncate(sanitise(rows[i].label), MAX_LABEL);
    var sub = sanitise(rows[i].sublabel);
    labels.push(sub ? label + SUB_SEP + truncate(sub, MAX_LABEL) : label);
    kinds += rows[i].kind || 'p';
  }
  if (rows.length > MAX_ROWS) {
    labels.push('(' + (rows.length - limit) + ' more)');
    kinds += 'p';
  }

  return [
    truncate(sanitise(frame.title), MAX_LABEL),
    labels.join(ROW_SEP),
    kinds,
    sanitise(frame.message),
    truncate(sanitise(frame.heading), MAX_LABEL),
    /* Deliberately not truncated: the overlay body exists precisely because
     * the text did not fit anywhere else, and the watch scrolls it. */
    sanitise(frame.prompt)
  ].join(FIELD_SEP);
}

function splitChunks(payload) {
  var size = chunkSize();
  var chunks = [];
  for (var at = 0; at < payload.length; at += size) {
    chunks.push(payload.slice(at, at + size));
  }
  /* An empty payload still needs one chunk, or the watch never completes the
   * frame and keeps showing the previous screen. */
  return chunks.length ? chunks : [''];
}

/* Only one frame may be in flight: two overlapping transmissions interleave
 * their chunks and the watch reassembles neither.  A frame offered while one
 * is sending replaces any other frame waiting behind it, because the newest
 * screen is the only one worth drawing -- showing a superseded one first would
 * just be a flicker of stale state. */
var sending = false;
var queued = null;

function finished() {
  sending = false;
  if (queued) {
    var next = queued;
    queued = null;
    send(next);
  }
}

/* send transmits a frame, one chunk in flight at a time.  A failed chunk is
 * retried in place; exhausting the attempts abandons the frame rather than
 * leaving the watch reassembling forever -- the watch discards a partial frame
 * as soon as the next one starts at index 0. */
function send(frame) {
  if (sending) {
    queued = frame;
    return;
  }
  sending = true;

  var chunks = splitChunks(buildPayload(frame));
  var index = 0;
  var attempts = 0;

  function step() {
    if (index >= chunks.length) {
      finished();
      return;
    }
    var message = {};
    message[keys.PAYLOAD] = chunks[index];
    message[keys.CHUNK_IDX] = index;
    message[keys.CHUNK_N] = chunks.length;

    if (index === chunks.length - 1) {
      message[keys.STATE] = frame.state || 0;
      message[keys.PROMPT_KIND] = frame.promptKind || 0;
      message[keys.LIVE] = frame.live ? 1 : 0;
      message[keys.ROOT] = frame.atRoot ? 1 : 0;
      message[keys.SEQ] = frame.seq || 0;
    }

    Pebble.sendAppMessage(message, function () {
      index++;
      attempts = 0;
      step();
    }, function (error) {
      attempts++;
      console.log('chunk ' + index + '/' + chunks.length + ' failed (attempt ' +
                  attempts + '): ' + JSON.stringify(error));
      if (attempts < MAX_ATTEMPTS) {
        setTimeout(step, RETRY_DELAY_MS);
      } else {
        console.log('abandoning frame after ' + attempts + ' failed attempts');
        finished();
      }
    });
  }

  step();
}

module.exports = {
  send: send,
  noteInbox: noteInbox,
  sanitise: sanitise,
  /* Exposed so tools/test-doc-roundtrip.js can feed the exact bytes this
   * module would send over the wire into the real doc.c parser, instead of
   * a second reimplementation of this serialisation drifting out of sync
   * with it. */
  buildPayload: buildPayload
};
