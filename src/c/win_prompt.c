/* The confirmation window.  See win_prompt.h for the rationale.
 *
 * The scrollable pane (heading + body, measurement, layout, click-config
 * chaining) is shared with win_detail.c; see win_scroll_text.h for the
 * split.  This file supplies what is specific to confirming: the footer
 * band, dictation, and SELECT/BACK both answering before they dismiss. */

#include "win_prompt.h"
#include "comm.h"
#include "doc.h"
#include "layout.h"
#include "win_scroll_text.h"

static Window *s_window;
static WinScrollText s_scroll_text;
static Layer *s_footer;

/* answered guards against reporting twice.  BACK both answers and pops, and
 * the pop can arrive after JS has already replied to the answer. */
static bool s_answered;

#ifdef PBL_MICROPHONE
/* Created on demand, because a dictation session holds resources and most
 * confirmations never need one. */
static DictationSession *s_dictation;
/* Longer than any field a watch should be filling in by voice, and short
 * enough that the buffer is not worth worrying about. */
#define TRANSCRIPT_MAX 256
#endif

/* True when the question needs a spoken value rather than a yes or no. */
static bool wants_text(void) {
  return doc_overlay() == DocOverlayText;
}

/* draw_footer spells out what the two buttons do.  A confirmation that does
 * not say which press means yes is not a confirmation — and unlike a phone,
 * there is nothing on screen to tap, so the mapping has to be written down. */
static void draw_footer(Layer *layer, GContext *ctx) {
  GRect bounds = layer_get_bounds(layer);
  graphics_context_set_fill_color(ctx, GColorDarkCandyAppleRed);
  graphics_fill_rect(ctx, bounds, 0, GCornerNone);

  /* Anchored to the top of the band: on a round display the band's lower edge
   * is the bottom of the circle, where there is no room for text. */
  GRect box = grect_inset(
      bounds, GEdgeInsets(PBL_IF_ROUND_ELSE(4, 5),
                          PBL_IF_ROUND_ELSE(bounds.size.w / 5, 6), 0,
                          PBL_IF_ROUND_ELSE(bounds.size.w / 5, 6)));
  graphics_context_set_text_color(ctx, GColorWhite);
  graphics_draw_text(ctx, wants_text() ? "SELECT to speak\nBACK cancels"
                                       : "SELECT confirms\nBACK cancels",
                     layout_font_sub(), box, GTextOverflowModeTrailingEllipsis,
                     GTextAlignmentCenter, NULL);
}

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

static void answer_text(bool confirmed, const char *text) {
  if (s_answered) {
    return;
  }
  s_answered = true;
  comm_send_answer(confirmed, text);
}

static void answer(bool confirmed) {
  answer_text(confirmed, NULL);
}

#ifdef PBL_MICROPHONE
/* on_dictation reports whatever the user said, verbatim.  A failed or
 * cancelled dictation is a decline, not an empty string: sending "" would fill
 * a field the server asked for with a value nobody spoke. */
static void on_dictation(DictationSession *session, DictationSessionStatus status,
                         char *transcription, void *context) {
  (void)session;
  (void)context;
  if (status == DictationSessionStatusSuccess && transcription) {
    answer_text(true, transcription);
  } else {
    APP_LOG(APP_LOG_LEVEL_INFO, "dictation ended without text, status %d",
            (int)status);
    answer(false);
    /* win_prompt_hide(), not window_stack_remove() directly -- see the
     * comment on win_prompt_hide for why. */
    win_prompt_hide();
  }
}
#endif

/* start_dictation asks for the field's value by voice.
 *
 * The session can be NULL at runtime even on a watch with a microphone -- it
 * needs a phone that supports voice -- so this reports the decline rather than
 * pretending the question was never asked. */
static void start_dictation(void) {
#ifdef PBL_MICROPHONE
  if (!s_dictation) {
    s_dictation = dictation_session_create(TRANSCRIPT_MAX, on_dictation, NULL);
  }
  if (s_dictation) {
    dictation_session_start(s_dictation);
    return;
  }
  APP_LOG(APP_LOG_LEVEL_WARNING, "no dictation session available");
#endif
  answer(false);
  /* win_prompt_hide(), not window_stack_remove() directly -- see the comment
   * on win_prompt_hide for why. */
  win_prompt_hide();
}

static void select_click(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  if (wants_text()) {
    start_dictation();
    return;
  }
  answer(true);
  /* The window stays up until JS replies with a frame that supersedes it, so
   * the user sees that the press registered rather than a screen that looks
   * unchanged while a request is in flight. */
}

static void back_click(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  /* Declining is an answer, not a dismissal: JS is holding a pending action
   * and would otherwise keep holding it. */
  answer(false);
  /* win_prompt_hide(), not window_stack_remove() directly -- see the comment
   * on win_prompt_hide for why. */
  win_prompt_hide();
}

/* UP and DOWN keep scrolling (wired by win_scroll_text_create) — the
 * sentence being confirmed is often longer than the screen, and it is
 * exactly the text that must be read. */
static void click_config(void *context) {
  win_scroll_text_chain_click_config(s_scroll_text.prior_click_config,
                                     context);
  window_single_click_subscribe(BUTTON_ID_SELECT, select_click);
  window_single_click_subscribe(BUTTON_ID_BACK, back_click);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect bounds = layer_get_bounds(root);
  ContentRect content = layout_content_rect(bounds);
  int16_t footer_h = layout_footer_height();
  window_set_background_color(window, GColorWhite);

  /* Bottom margin is the footer band, not a symmetric inset -- unlike
   * win_detail.c, which has no footer. */
  GRect frame = GRect(content.ox, content.oy, content.cw,
                      (int16_t)(bounds.size.h - content.oy - footer_h));
  s_scroll_text = win_scroll_text_create(
      window, frame, GColorDarkCandyAppleRed, GColorBlack, click_config);

  s_footer = layer_create(GRect(0, (int16_t)(bounds.size.h - footer_h),
                                bounds.size.w, footer_h));
  layer_set_update_proc(s_footer, draw_footer);
  layer_add_child(root, s_footer);

  update_content();
}

static void window_unload(Window *window) {
  (void)window;
  win_scroll_text_destroy(&s_scroll_text);
  layer_destroy(s_footer);
  s_footer = NULL;
}

void win_prompt_show(void) {
  if (!s_window) {
    s_window = window_create();
    window_set_window_handlers(s_window, (WindowHandlers){
                                             .load = window_load,
                                             .unload = window_unload,
                                         });
  }
  if (win_prompt_visible()) {
    update_content();
    return;
  }
  s_answered = false;
  window_stack_push(s_window, true);
}

/* win_prompt_hide is the only path that may remove s_window: every button and
 * dictation handler in this file answers first (if it hasn't already) and
 * then calls here rather than window_stack_remove() directly, so the pane's
 * text is always cleared before the window can be redrawn on its way off the
 * stack -- see win_scroll_text.h for why that matters.
 *
 * restforge.c also calls this directly, without an answer, when JS sends a
 * frame that supersedes an unanswered prompt: setting s_answered here is what
 * stops a stale answer from going out once the question is moot.  When a
 * caller in this file has already answered, the assignment is simply a
 * no-op -- answer()/answer_text() already set it. */
void win_prompt_hide(void) {
  if (!win_prompt_visible()) {
    return;
  }
  s_answered = true;
  win_scroll_text_clear(&s_scroll_text);
  window_stack_remove(s_window, true);
}

bool win_prompt_visible(void) {
  return s_window && window_stack_contains_window(s_window);
}

void win_prompt_deinit(void) {
#ifdef PBL_MICROPHONE
  if (s_dictation) {
    dictation_session_destroy(s_dictation);
    s_dictation = NULL;
  }
#endif
  if (s_window) {
    window_destroy(s_window);
    s_window = NULL;
  }
}
