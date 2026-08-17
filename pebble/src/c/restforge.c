/* RESTForge — a generic Siren hypermedia browser for the wrist.
 *
 * The watch renders documents; PebbleKit JS on the phone does every HTTP
 * request, parses the Siren JSON, and keeps the navigation stack, the hrefs
 * and the API secrets.  Nothing server-specific lives on either side: the app
 * knows Siren, and a backend works because it speaks Siren, not because the
 * app knows anything about it.
 *
 * This file is only the wiring — startup order, and handing a completed frame
 * from comm.c to the list window. */

#include <pebble.h>

#include "comm.h"
#include "doc.h"
#include "layout.h"
#include "win_detail.h"
#include "win_list.h"
#include "win_prompt.h"

/* Called by comm.c once a chunked frame has been fully reassembled into
 * doc.c.  Redrawing on frame completion rather than per chunk is what keeps a
 * half-arrived document from ever being shown.
 *
 * The list is always updated, overlay or not: an overlay sits on top of the
 * document it came with, so the document underneath has to be current when the
 * user dismisses it. */
static void on_frame(void) {
  win_list_update();

  DocOverlay overlay = doc_overlay();
  /* Hide first, then show: the two overlays are mutually exclusive, and a
   * confirmation replaced by an error must not leave the question on screen
   * underneath the answer. */
  if (overlay != DocOverlayDetail) {
    win_detail_hide();
  }
  if (overlay != DocOverlayConfirm && overlay != DocOverlayText) {
    win_prompt_hide();
  }
  if (overlay == DocOverlayDetail) {
    win_detail_show();
  } else if (overlay == DocOverlayConfirm || overlay == DocOverlayText) {
    /* Both are questions; the difference is only how the answer is given. */
    win_prompt_show();
  }
}

static void init(void) {
  layout_init();
  doc_reset();
  comm_init(on_frame);
  win_list_push();

  /* Ask for the backend list straight away.  If JS is not ready yet the send
   * fails and is logged; the user pressing SELECT or BACK retries, and JS also
   * pushes an unsolicited frame once its own "ready" fires. */
  comm_send_cmd(CommCmdListBackends, 0);
}

static void deinit(void) {
  win_prompt_deinit();
  win_detail_deinit();
  win_list_deinit();
  comm_deinit();
  doc_deinit();
  layout_deinit();
}

int main(void) {
  init();
  app_event_loop();
  deinit();
}
