/* RESTForge — main watchapp entry point.
 *
 * Skeleton app adapted from FastForge. Renders a single window with a title
 * line and a live wall-clock that updates every minute via TickTimerService.
 * SELECT reloads the window as a placeholder for future menu navigation. */

#include <pebble.h>

static Window *s_main_window;
static TextLayer *s_title_layer;
static TextLayer *s_clock_layer;
static char s_clock_text[16];

static void update_clock(struct tm *tick_time) {
  if (!tick_time) {
    time_t now = time(NULL);
    tick_time = localtime(&now);
    if (!tick_time) {
      return;
    }
  }

  if (clock_is_24h_style()) {
    strftime(s_clock_text, sizeof(s_clock_text), "%H:%M", tick_time);
  } else {
    strftime(s_clock_text, sizeof(s_clock_text), "%I:%M", tick_time);
  }
  text_layer_set_text(s_clock_layer, s_clock_text);
}

static void tick_handler(struct tm *tick_time, TimeUnits units_changed) {
  (void)units_changed;
  update_clock(tick_time);
}

static void select_click_handler(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  /* Placeholder: future menu / action goes here. */
}

static void click_config_provider(void *context) {
  (void)context;
  window_single_click_subscribe(BUTTON_ID_SELECT, select_click_handler);
}

/* Layout helper: content origin and width for the current display shape.
 * On round displays (chalk, gabbro) the screen is circular, so we inset
 * 1/6 of the display width on every side to keep all text within the
 * visible circle.  On rectangular displays the inset is zero. */
typedef struct { int16_t ox; int16_t oy; int16_t cw; } ContentRect;
static ContentRect content_rect(GRect bounds) {
#ifdef PBL_ROUND
  int16_t inset = bounds.size.w / 6;
  return (ContentRect){ inset, inset, bounds.size.w - 2 * inset };
#else
  return (ContentRect){ 0, 0, bounds.size.w };
#endif
}

static void main_window_load(Window *window) {
  Layer *window_layer = window_get_root_layer(window);
  GRect bounds = layer_get_bounds(window_layer);
  ContentRect cr = content_rect(bounds);

  window_set_background_color(window, GColorBlack);

  s_title_layer = text_layer_create(GRect(cr.ox, cr.oy, cr.cw, 30));
  text_layer_set_background_color(s_title_layer, GColorClear);
  text_layer_set_text_color(s_title_layer, GColorWhite);
  text_layer_set_text_alignment(s_title_layer, GTextAlignmentCenter);
  text_layer_set_font(s_title_layer, fonts_get_system_font(FONT_KEY_GOTHIC_24_BOLD));
  text_layer_set_text(s_title_layer, "RESTForge");
  layer_add_child(window_layer, text_layer_get_layer(s_title_layer));

  s_clock_layer = text_layer_create(GRect(cr.ox, cr.oy + 40, cr.cw, 40));
  text_layer_set_background_color(s_clock_layer, GColorClear);
  text_layer_set_text_color(s_clock_layer, GColorWhite);
  text_layer_set_text_alignment(s_clock_layer, GTextAlignmentCenter);
  text_layer_set_font(s_clock_layer, fonts_get_system_font(FONT_KEY_BITHAM_42_BOLD));
  layer_add_child(window_layer, text_layer_get_layer(s_clock_layer));

  update_clock(NULL);
}

static void main_window_unload(Window *window) {
  text_layer_destroy(s_title_layer);
  text_layer_destroy(s_clock_layer);
  s_title_layer = NULL;
  s_clock_layer = NULL;
}

static void init(void) {
  s_main_window = window_create();
  window_set_click_config_provider(s_main_window, click_config_provider);
  window_set_window_handlers(s_main_window, (WindowHandlers){
    .load = main_window_load,
    .unload = main_window_unload,
  });
  window_stack_push(s_main_window, true);

  tick_timer_service_subscribe(MINUTE_UNIT, tick_handler);
}

static void deinit(void) {
  tick_timer_service_unsubscribe();
  window_destroy(s_main_window);
  s_main_window = NULL;
}

int main(void) {
  init();
  app_event_loop();
  deinit();
}