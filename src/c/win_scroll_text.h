/* Shared scrollable heading+body text pane used by win_detail.c and
 * win_prompt.c.
 *
 * Both windows are "some text, possibly longer than the screen, in a
 * ScrollLayer with a heading line above it": the reading window shows a
 * property's full value or an action's current step, the confirmation
 * window shows a field's title verbatim, and neither needs to know the
 * other exists.  What they do share -- byte for byte, until this file
 * existed -- is how that pane gets created, measured and laid out: the same
 * padding/slack tuning, the same descender-clipping workaround, and the
 * same dance to add a window's own SELECT/BACK handling on top of a
 * ScrollLayer's UP/DOWN paging without disabling it.
 *
 * What is NOT here: a footer, dismissal semantics, or what SELECT/BACK
 * actually do.  Those differ per window and stay in win_detail.c/
 * win_prompt.c, which each supply only their own click handlers and footer
 * (if any) around this pane.
 *
 * win_scroll_text_clear() exists because of a real bug, not for symmetry:
 * s_heading/s_body-equivalent TextLayers hold the raw pointers they were
 * given rather than copies, and doc.c's payload buffer they point into is
 * freed unconditionally on the next completed AppMessage frame -- well
 * before a window's unload handler runs, since that only fires once Pebble
 * finishes animating the window off the stack.  win_detail.c and
 * win_prompt.c both shipped with this dangling-pointer bug and were fixed
 * separately (commits 1cd78a5 and 839e5d1); every caller here must clear
 * before removing a window, and centralising the clear is what keeps a
 * third caller from reintroducing the bug a third time. */

#pragma once

#include <pebble.h>

#include "layout.h"

/* Padding around the text, and slack added below each measured block.  One
 * pair of constants instead of two near-identical ones per window, so a
 * layout fix (e.g. to the descender-clipping workaround) is made once. */
#define WIN_SCROLL_TEXT_PAD 4
#define WIN_SCROLL_TEXT_SLACK 3

/* The three layers that make up the pane, plus enough of the window's prior
 * click config to let a caller chain onto it.  Callers keep one of these as
 * a plain static, the same way they kept s_scroll/s_heading/s_body before
 * this was extracted. */
typedef struct {
  ScrollLayer *scroll;
  TextLayer *heading;
  TextLayer *body;
  /* The window's click config provider from before win_scroll_text_create()
   * installed its own -- normally NULL, forwarded so a caller's own
   * provider can chain to it via win_scroll_text_chain_click_config(). */
  ClickConfigProvider prior_click_config;
} WinScrollText;

/* Creates the scroll layer (sized to `frame`) and its heading/body text
 * layers, adds them as its children, adds the scroll layer to `window`'s
 * root layer, wires UP/DOWN paging onto `window`'s click config, then
 * installs `click_config` as the window's new provider with the scroll
 * layer itself as its context -- exactly what both callers did by hand
 * before this existed.  `frame` is the caller's to compute, since that is
 * where the two windows differ (a footer band eats into one, not the
 * other). */
WinScrollText win_scroll_text_create(Window *window, GRect frame,
                                      GColor heading_colour,
                                      GColor body_colour,
                                      ClickConfigProvider click_config);

/* Destroys the three layers created above and zeroes the struct. */
void win_scroll_text_destroy(WinScrollText *scroll_text);

/* Re-lays the heading and body around whatever doc_heading()/doc_prompt()
 * currently return, and resizes the scroll content to match, resetting the
 * scroll position to the top.  `content` is layout_content_rect(bounds) --
 * callers already compute that for their own layout (e.g. a footer band),
 * so this takes it rather than recomputing it from bounds itself.  A no-op
 * before the pane has been created (mirrors the `if (!s_scroll) return;`
 * guard both windows used to open update_content() with). */
void win_scroll_text_update(WinScrollText *scroll_text, ContentRect content);

/* Clears both text layers.  Callers must call this before removing a window
 * that owns a WinScrollText -- see the file header above for why. */
void win_scroll_text_clear(WinScrollText *scroll_text);

/* Calls `previous` if it is non-NULL.  The one-line pattern every window's
 * own click_config wrapper needs, to add SELECT/BACK handling on top of
 * whatever provider the window had before win_scroll_text_create() replaced
 * it -- named here so the intent (chaining, not replacing) reads the same
 * way at both call sites. */
void win_scroll_text_chain_click_config(ClickConfigProvider previous,
                                         void *context);
