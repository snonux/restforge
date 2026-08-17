/* The full-screen reading window.  See win_detail.h for the rationale.
 *
 * The scrollable pane (heading + body, measurement, layout, click-config
 * chaining) is shared with win_prompt.c; see win_scroll_text.h for the
 * split.  This file supplies only what is specific to reading: no footer,
 * and BACK dismisses locally instead of answering. */

#include "win_detail.h"
#include "comm.h"
#include "layout.h"
#include "win_scroll_text.h"

static Window *s_window;
static WinScrollText s_scroll_text;

/* update_content re-lays the pane around whatever the current frame carries.
 * A no-op before the window has loaded once, since s_scroll_text.scroll is
 * only set from window_load onward. */
static void update_content(void) {
  if (!s_scroll_text.scroll) {
    return;
  }
  GRect bounds = layer_get_bounds(window_get_root_layer(s_window));
  win_scroll_text_update(&s_scroll_text, layout_content_rect(bounds));
}

/* BACK dismisses this window without asking anyone -- the document underneath
 * never changed, so there is nothing to fetch and nothing to wait for.  It
 * does, however, get *reported*: the phone suppresses its background refresh
 * while it believes something is being read, and with no word from here it
 * would believe that forever.  The message is one-way; the window is already
 * gone by the time it lands.
 *
 * Dismissal goes through win_detail_hide() rather than window_stack_remove()
 * directly, so the pane's text gets cleared first -- see win_scroll_text.h
 * for why that matters. */
static void dismiss_click(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  comm_send_cmd(CommCmdDismiss, 0);
  win_detail_hide();
}

/* UP and DOWN scroll (wired by win_scroll_text_create); BACK dismisses, via
 * dismiss_click above. */
static void click_config(void *context) {
  win_scroll_text_chain_click_config(s_scroll_text.prior_click_config,
                                     context);
  window_single_click_subscribe(BUTTON_ID_BACK, dismiss_click);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect bounds = layer_get_bounds(root);
  ContentRect content = layout_content_rect(bounds);
  window_set_background_color(window, GColorWhite);

  /* Symmetric top/bottom margin -- unlike win_prompt.c there is no footer
   * band eating into the bottom of the screen. */
  GRect frame = GRect(content.ox, content.oy, content.cw,
                      (int16_t)(bounds.size.h - 2 * content.oy));
  s_scroll_text = win_scroll_text_create(window, frame, GColorDarkGray,
                                         GColorBlack, click_config);

  update_content();
}

/* The layers are owned by the push, the window by the module.  Destroying the
 * window from inside its own unload handler would be freeing the object the
 * caller is still walking, so the window outlives every push and is destroyed
 * once, at shutdown. */
static void window_unload(Window *window) {
  (void)window;
  win_scroll_text_destroy(&s_scroll_text);
}

void win_detail_show(void) {
  if (!s_window) {
    s_window = window_create();
    window_set_window_handlers(s_window, (WindowHandlers){
                                             .load = window_load,
                                             .unload = window_unload,
                                         });
  }
  if (win_detail_visible()) {
    /* Already up: a new frame for the same overlay just re-lays the text. */
    update_content();
    return;
  }
  window_stack_push(s_window, true);
}

void win_detail_hide(void) {
  if (!win_detail_visible()) {
    return;
  }
  /* Clear the pane's text before the window can be drawn again on its way
   * out -- see win_scroll_text.h for why this must happen here. */
  win_scroll_text_clear(&s_scroll_text);
  window_stack_remove(s_window, true);
}

/* The window stack is the authority, not a flag of our own: other code (e.g.
 * win_list.c's window_stack_pop_all on exit) can clear this window off the
 * stack without going through win_detail_hide, so a flag of our own would
 * drift out of sync with it. */
bool win_detail_visible(void) {
  return s_window && window_stack_contains_window(s_window);
}

void win_detail_deinit(void) {
  if (s_window) {
    window_destroy(s_window);
    s_window = NULL;
  }
}
