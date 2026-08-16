/* The full-screen reading window.  See win_detail.h for the rationale. */

#include "win_detail.h"
#include "comm.h"
#include "doc.h"
#include "layout.h"

/* Breathing room around the text.  Small, because the type is large and the
 * screen is not: padding here is bought directly out of characters per line. */
#define DETAIL_PAD 4

/* Slack added below each measured block; see update_content. */
#define DETAIL_SLACK 3

static Window *s_window;
static ScrollLayer *s_scroll;
static TextLayer *s_heading;
static TextLayer *s_body;

/* text_width is the usable width inside the scroll view.  On a round display
 * this is layout_content_rect's inscribed rectangle: unlike a MenuLayer, a
 * ScrollLayer does not know about the shape of the screen and will happily
 * draw text into the corners, where there is no screen. */
static int16_t text_width(GRect bounds) {
  return (int16_t)(layout_content_rect(bounds).cw - 2 * DETAIL_PAD);
}

/* update_content re-lays the two text layers around whatever the current frame
 * carries, and sizes the scrollable area to match.  Both heights are measured
 * from the fonts rather than assumed, because the whole reason this window
 * exists is that the text did not fit somewhere it was assumed to. */
static void update_content(void) {
  if (!s_scroll) {
    return;
  }
  GRect bounds = layer_get_bounds(window_get_root_layer(s_window));
  ContentRect content = layout_content_rect(bounds);
  int16_t width = text_width(bounds);

  const char *heading = doc_heading();
  const char *body = doc_prompt();
  text_layer_set_text(s_heading, heading);
  text_layer_set_text(s_body, body);

  /* The measured height is what the glyphs occupy; a TextLayer clips to its
   * frame exactly, so a descender on the last line lands on the boundary and
   * loses a row or two of pixels.  A couple of pixels of slack costs nothing
   * in a scrolling view and is visible if it is missing. */
  int16_t heading_h = (int16_t)(layout_text_height(heading, layout_font_row(),
                                                   width, LAYOUT_UNBOUNDED_LINES) +
                                DETAIL_SLACK);
  int16_t body_h = (int16_t)(layout_text_height(body, layout_font_body(), width,
                                                LAYOUT_UNBOUNDED_LINES) +
                             DETAIL_SLACK);

  layer_set_frame(text_layer_get_layer(s_heading),
                  GRect(DETAIL_PAD, 0, width, heading_h));
  layer_set_frame(text_layer_get_layer(s_body),
                  GRect(DETAIL_PAD, (int16_t)(heading_h + DETAIL_PAD), width, body_h));

  /* The trailing pad keeps the last line clear of the bottom of the screen —
   * on a round display the bottom of the screen is a curve. */
  int16_t total = (int16_t)(heading_h + body_h + 2 * DETAIL_PAD);
  scroll_layer_set_content_size(s_scroll, GSize(content.cw, total));
  scroll_layer_set_content_offset(s_scroll, GPoint(0, 0), false);
}

static TextLayer *make_text_layer(GFont font, GColor colour) {
  TextLayer *layer = text_layer_create(GRect(0, 0, 1, 1));
  text_layer_set_font(layer, font);
  text_layer_set_text_color(layer, colour);
  text_layer_set_background_color(layer, GColorClear);
  text_layer_set_overflow_mode(layer, GTextOverflowModeWordWrap);
  text_layer_set_text_alignment(
      layer, PBL_IF_ROUND_ELSE(GTextAlignmentCenter, GTextAlignmentLeft));
  return layer;
}

/* BACK dismisses this window without asking anyone -- the document underneath
 * never changed, so there is nothing to fetch and nothing to wait for.  It
 * does, however, get *reported*: the phone suppresses its background refresh
 * while it believes something is being read, and with no word from here it
 * would believe that forever.  The message is one-way; the window is already
 * gone by the time it lands.
 *
 * Dismissal goes through win_detail_hide() rather than window_stack_remove()
 * directly, so s_heading/s_body get their text cleared first.  Those layers
 * hold raw pointers into doc.c's payload buffer, and window_unload() (which
 * would otherwise be the one to let go of them) does not run until Pebble
 * finishes animating the window off the stack -- a window that can outlive
 * the frame whose text it is still pointing at. */
static void dismiss_click(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  comm_send_cmd(CommCmdDismiss, 0);
  win_detail_hide();
}

static ClickConfigProvider s_scroll_click_config;

static void click_config(void *context) {
  if (s_scroll_click_config) {
    s_scroll_click_config(context);
  }
  window_single_click_subscribe(BUTTON_ID_BACK, dismiss_click);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect bounds = layer_get_bounds(root);
  ContentRect content = layout_content_rect(bounds);
  window_set_background_color(window, GColorWhite);

  s_scroll = scroll_layer_create(
      GRect(content.ox, content.oy, content.cw,
            (int16_t)(bounds.size.h - 2 * content.oy)));
  /* UP and DOWN scroll; BACK dismisses, via dismiss_click above. */
  scroll_layer_set_click_config_onto_window(s_scroll, window);
  s_scroll_click_config = window_get_click_config_provider(window);
  window_set_click_config_provider_with_context(window, click_config, s_scroll);
  scroll_layer_set_shadow_hidden(s_scroll, true);
  /* Paging on round: a partially visible line at the edge of a circle is
   * unreadable, so move a screenful at a time rather than a few pixels. */
  scroll_layer_set_paging(s_scroll, PBL_IF_ROUND_ELSE(true, false));

  s_heading = make_text_layer(layout_font_row(), GColorDarkGray);
  s_body = make_text_layer(layout_font_body(), GColorBlack);
  scroll_layer_add_child(s_scroll, text_layer_get_layer(s_heading));
  scroll_layer_add_child(s_scroll, text_layer_get_layer(s_body));
  layer_add_child(root, scroll_layer_get_layer(s_scroll));

  update_content();
}

/* The layers are owned by the push, the window by the module.  Destroying the
 * window from inside its own unload handler would be freeing the object the
 * caller is still walking, so the window outlives every push and is destroyed
 * once, at shutdown. */
static void window_unload(Window *window) {
  (void)window;
  text_layer_destroy(s_heading);
  text_layer_destroy(s_body);
  scroll_layer_destroy(s_scroll);
  s_heading = NULL;
  s_body = NULL;
  s_scroll = NULL;
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
  /* A TextLayer holds the pointer it was given rather than a copy, and the
   * strings above point into the frame buffer doc.c is about to free.  Clear
   * them before the window can be drawn again on its way out. */
  text_layer_set_text(s_heading, "");
  text_layer_set_text(s_body, "");
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
