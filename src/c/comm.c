/* AppMessage transport.  See comm.h for the design rationale. */

#include "comm.h"
#include "doc.h"

static CommFrameHandler s_handler;

/* Reassembly state for the frame currently arriving. */
static char *s_accum;
static size_t s_accum_len;
static int s_expect_chunks;
static int s_next_chunk;

/* Scalars ride on the final chunk so they are applied atomically with the text
 * they describe — a STATE that arrived before its message would flash the
 * wrong colour. */
static DocMeta s_pending_meta;

/* Sequence number echoed by JS.  A reply carrying an older SEQ than the last
 * command we sent describes a screen the user has already left, so it is
 * dropped rather than rendered. */
static int32_t s_seq;

static void discard_frame(const char *why) {
  if (s_accum) {
    APP_LOG(APP_LOG_LEVEL_WARNING, "discarding partial frame: %s", why);
  }
  free(s_accum);
  s_accum = NULL;
  s_accum_len = 0;
  s_expect_chunks = 0;
  s_next_chunk = 0;
}

/* append_chunk grows the reassembly buffer by one chunk.  Returns false when
 * the allocation fails, which on a 128 KB platform means the frame was
 * unreasonably large and dropping it is the right answer. */
static bool append_chunk(const char *text, size_t len) {
  char *grown = realloc(s_accum, s_accum_len + len + 1);
  if (!grown) {
    discard_frame("out of memory");
    return false;
  }
  memcpy(grown + s_accum_len, text, len);
  s_accum = grown;
  s_accum_len += len;
  s_accum[s_accum_len] = '\0';
  return true;
}

static void read_scalars(DictionaryIterator *iter) {
  Tuple *t = dict_find(iter, MESSAGE_KEY_STATE);
  s_pending_meta.state = t ? (DocState)t->value->int32 : DocStateOk;

  t = dict_find(iter, MESSAGE_KEY_PROMPT_KIND);
  s_pending_meta.overlay = t ? (DocOverlay)t->value->int32 : DocOverlayNone;

  t = dict_find(iter, MESSAGE_KEY_LIVE);
  s_pending_meta.live = t && t->value->int32 != 0;

  t = dict_find(iter, MESSAGE_KEY_ROOT);
  s_pending_meta.at_root = t && t->value->int32 != 0;
}

/* True when the final chunk's SEQ predates the command we most recently sent
 * — session.js echoes back the SEQ of the command a reply answers (see
 * session.js setSeq/send), so an older value means the user has already
 * moved past the screen this reply describes.
 *
 * Two values are never stale.  A missing SEQ can only come from JS that
 * predates the field.  And SEQ 0 means the frame answers no command at all:
 * JS sends one unprompted when its own "ready" fires, before it has ever seen
 * a command to echo, while the watch has already sent its opening request and
 * moved s_seq to 1.  Discarding that one leaves the app on its blank starting
 * screen until the user presses something — which is exactly what it did until
 * an end-to-end run caught it. */
static bool reply_is_stale(DictionaryIterator *iter) {
  Tuple *t = dict_find(iter, MESSAGE_KEY_SEQ);
  if (!t || t->value->int32 == 0) {
    return false;
  }
  return t->value->int32 < s_seq;
}

/* complete_frame hands ownership of the reassembly buffer to doc.c. */
static void complete_frame(void) {
  char *payload = s_accum;
  s_accum = NULL;
  s_accum_len = 0;
  s_expect_chunks = 0;
  s_next_chunk = 0;

  doc_set_frame(payload, s_pending_meta);
  if (s_handler) {
    s_handler();
  }
}

static void inbox_received(DictionaryIterator *iter, void *context) {
  (void)context;

  Tuple *payload = dict_find(iter, MESSAGE_KEY_PAYLOAD);
  if (!payload) {
    return;
  }

  Tuple *idx_tuple = dict_find(iter, MESSAGE_KEY_CHUNK_IDX);
  Tuple *n_tuple = dict_find(iter, MESSAGE_KEY_CHUNK_N);
  int idx = idx_tuple ? (int)idx_tuple->value->int32 : 0;
  int n = n_tuple ? (int)n_tuple->value->int32 : 1;

  if (idx == 0) {
    discard_frame("superseded by a new frame");
    s_expect_chunks = n;
  } else if (idx != s_next_chunk || n != s_expect_chunks) {
    /* A gap means a chunk was dropped in transit.  There is no retransmit
     * protocol here on purpose: JS re-sends the whole frame on failure, so
     * dropping the partial one and waiting is both simpler and correct. */
    discard_frame("chunk out of order");
    return;
  }

  if (!append_chunk(payload->value->cstring, strlen(payload->value->cstring))) {
    return;
  }
  s_next_chunk = idx + 1;

  if (s_next_chunk >= s_expect_chunks) {
    if (reply_is_stale(iter)) {
      discard_frame("stale reply, seq predates last command sent");
      return;
    }
    read_scalars(iter);
    complete_frame();
  }
}

static void inbox_dropped(AppMessageResult reason, void *context) {
  (void)context;
  /* An oversized message reports APP_MSG_BUSY (64) here, not
   * APP_MSG_BUFFER_OVERFLOW — the SDK docs are stale on this point. */
  APP_LOG(APP_LOG_LEVEL_ERROR, "inbox dropped, reason 0x%x", (unsigned)reason);
  discard_frame("inbox drop");
}

/* No automatic retry here by design (see comm.h comm_send_cmd): re-sending
 * would need to remember the failed message, and for comm_send_answer that
 * means holding onto a dictation transcription pointer whose owner (the
 * DictationSession in win_prompt.c) may already be gone by the time this
 * fires. The user's next button press re-issues the command instead. */
static void outbox_failed(DictionaryIterator *iter, AppMessageResult reason,
                          void *context) {
  (void)iter;
  (void)context;
  APP_LOG(APP_LOG_LEVEL_ERROR, "outbox failed, reason 0x%x", (unsigned)reason);
}

static void outbox_sent(DictionaryIterator *iter, void *context) {
  (void)iter;
  (void)context;
}

void comm_init(CommFrameHandler handler) {
  s_handler = handler;

  app_message_register_inbox_received(inbox_received);
  app_message_register_inbox_dropped(inbox_dropped);
  app_message_register_outbox_failed(outbox_failed);
  app_message_register_outbox_sent(outbox_sent);

  /* Asking for the maximum is clamped rather than rejected, and the firmware
   * logs a note that the size is not portable — which is exactly why we send
   * the negotiated value up instead of letting JS guess it. */
  const uint32_t inbox = app_message_inbox_size_maximum();
  const uint32_t outbox = app_message_outbox_size_maximum();
  AppMessageResult result = app_message_open(inbox, outbox);
  APP_LOG(APP_LOG_LEVEL_INFO, "app_message_open(%u, %u) -> 0x%x",
          (unsigned)inbox, (unsigned)outbox, (unsigned)result);
}

void comm_deinit(void) {
  discard_frame("shutting down");
  s_handler = NULL;
}

/* begin_outbox opens the outbox and stamps every message with the negotiated
 * inbox size and the current sequence number, so JS never has to ask for
 * either. */
static DictionaryIterator *begin_outbox(CommCmd cmd) {
  DictionaryIterator *iter = NULL;
  AppMessageResult result = app_message_outbox_begin(&iter);
  if (result != APP_MSG_OK || !iter) {
    APP_LOG(APP_LOG_LEVEL_ERROR, "outbox_begin failed, 0x%x", (unsigned)result);
    return NULL;
  }
  dict_write_int32(iter, MESSAGE_KEY_CMD, (int32_t)cmd);
  dict_write_int32(iter, MESSAGE_KEY_SEQ, ++s_seq);
  dict_write_int32(iter, MESSAGE_KEY_INBOX,
                   (int32_t)app_message_inbox_size_maximum());
  return iter;
}

void comm_send_cmd(CommCmd cmd, int32_t index) {
  DictionaryIterator *iter = begin_outbox(cmd);
  if (!iter) {
    return;
  }
  dict_write_int32(iter, MESSAGE_KEY_IDX, index);
  app_message_outbox_send();
}

void comm_send_answer(bool confirmed, const char *text) {
  DictionaryIterator *iter = begin_outbox(CommCmdAnswer);
  if (!iter) {
    return;
  }
  dict_write_int32(iter, MESSAGE_KEY_ANSWER, confirmed ? 1 : 0);
  if (text) {
    dict_write_cstring(iter, MESSAGE_KEY_ANSWER_TEXT, text);
  }
  app_message_outbox_send();
}
