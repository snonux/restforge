/* AppMessage transport between the watch and PebbleKit JS.
 *
 * Everything the watch can say is "the user did X to row N".  Everything JS
 * says back is one rendered frame.  Neither direction carries a URL or a
 * secret; see doc.h.
 *
 * Frames are chunked because the inbox size is negotiated at runtime and is
 * not a property of the watch model: app_message_inbox_size_maximum() returns
 * 8200 only when the connected phone app advertises the relevant protocol
 * capability, 2026 otherwise, and 126 when JS is not enabled at all.  The
 * watch therefore reports whatever it actually got (the INBOX key) and JS
 * sizes its chunks to fit.  Assuming 8200 works on a developer's phone and
 * fails on a user's. */

#pragma once

#include <pebble.h>
#include <stdbool.h>

/* Commands the watch sends up, in the CMD key. */
typedef enum {
  CommCmdListBackends = 0,
  CommCmdOpenBackend = 1,
  CommCmdActivateRow = 2,
  CommCmdBack = 3,
  CommCmdRefresh = 4,
  CommCmdAnswer = 5,
  CommCmdDismiss = 6,
} CommCmd;

/* Called once a complete frame has been reassembled into the doc module. */
typedef void (*CommFrameHandler)(void);

void comm_init(CommFrameHandler handler);
void comm_deinit(void);

/* Sends a command with a row or backend index.  Fire-and-forget: a send that
 * fails is only logged by the outbox-failed handler (see comm.c) — there is
 * no automatic retry.  The user pressing the button again is the retry.
 * Silently queueing is worse — it makes a stale press arrive minutes later
 * against a different document. */
void comm_send_cmd(CommCmd cmd, int32_t index);

/* Answers a pending prompt.  `text` may be NULL for a checkbox confirmation. */
void comm_send_answer(bool confirmed, const char *text);
