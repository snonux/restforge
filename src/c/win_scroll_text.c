/* Shared scrollable heading+body text pane.  See win_scroll_text.h for the
 * rationale and for what deliberately stays out of this file. */

#include "win_scroll_text.h"

#include "doc.h"

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

WinScrollText win_scroll_text_create(Window *window, GRect frame,
                                      GColor heading_colour,
                                      GColor body_colour,
                                      ClickConfigProvider click_config) {
  WinScrollText scroll_text = {0};
  scroll_text.scroll = scroll_layer_create(frame);
  scroll_layer_set_shadow_hidden(scroll_text.scroll, true);
  /* Paging on round: a partially visible line at the edge of a circle is
   * unreadable, so move a screenful at a time rather than a few pixels. */
  scroll_layer_set_paging(scroll_text.scroll, PBL_IF_ROUND_ELSE(true, false));

  scroll_text.heading = make_text_layer(layout_font_row(), heading_colour);
  scroll_text.body = make_text_layer(layout_font_body(), body_colour);
  scroll_layer_add_child(scroll_text.scroll,
                         text_layer_get_layer(scroll_text.heading));
  scroll_layer_add_child(scroll_text.scroll,
                         text_layer_get_layer(scroll_text.body));
  layer_add_child(window_get_root_layer(window),
                  scroll_layer_get_layer(scroll_text.scroll));

  /* Capture whatever click config the window had -- normally none -- before
   * overwriting it, so the caller's own provider can chain to it. */
  scroll_layer_set_click_config_onto_window(scroll_text.scroll, window);
  scroll_text.prior_click_config = window_get_click_config_provider(window);
  window_set_click_config_provider_with_context(window, click_config,
                                                scroll_text.scroll);
  return scroll_text;
}

void win_scroll_text_destroy(WinScrollText *scroll_text) {
  text_layer_destroy(scroll_text->heading);
  text_layer_destroy(scroll_text->body);
  scroll_layer_destroy(scroll_text->scroll);
  scroll_text->heading = NULL;
  scroll_text->body = NULL;
  scroll_text->scroll = NULL;
  scroll_text->prior_click_config = NULL;
}

/* update_layout is the frame math shared by every caller: heading above
 * body, both measured from their own font, pad/slack applied consistently.
 * Split out of win_scroll_text_update so that function stays a short
 * "fetch the text, then lay it out" shell rather than growing past the
 * point a single function is easy to read. */
static void update_layout(WinScrollText *scroll_text, ContentRect content,
                          const char *heading, const char *body) {
  /* text_width is the usable width inside the scroll view.  On a round
   * display content.cw is layout_content_rect's inscribed rectangle: unlike
   * a MenuLayer, a ScrollLayer does not know about the shape of the screen
   * and will happily draw text into the corners, where there is no screen. */
  int16_t width = (int16_t)(content.cw - 2 * WIN_SCROLL_TEXT_PAD);

  /* The measured height is what the glyphs occupy; a TextLayer clips to its
   * frame exactly, so a descender on the last line lands on the boundary
   * and loses a row or two of pixels.  A couple of pixels of slack costs
   * nothing in a scrolling view and is visible if it is missing. */
  int16_t heading_h = (int16_t)(layout_text_height(heading, layout_font_row(),
                                                    width,
                                                    LAYOUT_UNBOUNDED_LINES) +
                                WIN_SCROLL_TEXT_SLACK);
  int16_t body_h = (int16_t)(layout_text_height(body, layout_font_body(),
                                                width, LAYOUT_UNBOUNDED_LINES) +
                             WIN_SCROLL_TEXT_SLACK);

  layer_set_frame(text_layer_get_layer(scroll_text->heading),
                  GRect(WIN_SCROLL_TEXT_PAD, 0, width, heading_h));
  layer_set_frame(
      text_layer_get_layer(scroll_text->body),
      GRect(WIN_SCROLL_TEXT_PAD, (int16_t)(heading_h + WIN_SCROLL_TEXT_PAD),
           width, body_h));

  /* The trailing pad keeps the last line clear of the bottom of the screen
   * -- on a round display the bottom of the screen is a curve. */
  int16_t total = (int16_t)(heading_h + body_h + 2 * WIN_SCROLL_TEXT_PAD);
  scroll_layer_set_content_size(scroll_text->scroll, GSize(content.cw, total));
  scroll_layer_set_content_offset(scroll_text->scroll, GPoint(0, 0), false);
}

/* win_scroll_text_update re-lays the pane around whatever the current frame
 * carries.  Both heights are measured from the fonts rather than assumed,
 * because the whole reason a caller needs this pane at all is that the text
 * did not fit somewhere it was assumed to. */
void win_scroll_text_update(WinScrollText *scroll_text, ContentRect content) {
  if (!scroll_text->scroll) {
    return;
  }
  const char *heading = doc_heading();
  const char *body = doc_prompt();
  text_layer_set_text(scroll_text->heading, heading);
  text_layer_set_text(scroll_text->body, body);
  update_layout(scroll_text, content, heading, body);
}

void win_scroll_text_clear(WinScrollText *scroll_text) {
  /* See win_scroll_text.h's file header: these hold raw pointers into
   * doc.c's payload buffer, and must be cleared before the window that owns
   * them can be redrawn on its way off the stack. */
  text_layer_set_text(scroll_text->heading, "");
  text_layer_set_text(scroll_text->body, "");
}

void win_scroll_text_chain_click_config(ClickConfigProvider previous,
                                         void *context) {
  if (previous) {
    previous(context);
  }
}
