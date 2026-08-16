/* Display geometry, fonts and colours.  See layout.h for the rationale. */

#include "layout.h"
#include "doc.h"

static GFont s_font_focused;
static GFont s_font_row;
static GFont s_font_index;
static GFont s_font_body;

/* Vertical padding inside a menu cell, above and below the text. */
#define CELL_PAD_V 6
/* Horizontal padding inside a menu cell, left and right. */
#define CELL_PAD_H 6

/* Floors, not targets.  The SDK's MENU_CELL_ROUND_* constants are sized for
 * chalk's 180x180; on gabbro's 260x260 with a 34px face the measured heights
 * come out well above them, and these only matter for a one-character row. */
#define MIN_FOCUSED_HEIGHT 44
#define MIN_ROW_HEIGHT 30

ContentRect layout_content_rect(GRect bounds) {
#ifdef PBL_ROUND
  /* A rectangle inscribed far enough inside the circle that its corners stay
   * visible.  w/6 leaves 174px of usable width on gabbro, which at 28px body
   * type is about 12 characters — tight, but this is only used for the
   * scrolling text windows, where wrapping is expected. */
  int16_t inset = bounds.size.w / 6;
  return (ContentRect){ inset, inset, bounds.size.w - 2 * inset };
#else
  return (ContentRect){ 0, 0, bounds.size.w };
#endif
}

/* load_font returns the custom font, or the largest comparable system face if
 * the resource is missing.  A missing resource is a build error, but failing
 * soft here keeps the app readable while one is being fixed.
 *
 * The check is on resource_size(), not on fonts_load_custom_font()'s return
 * value: the installed SDK's own header documents that function as never
 * returning NULL — on a missing/corrupt resource it silently substitutes
 * some SDK-internal default font and still returns a valid pointer.  A
 * `!font` check here would therefore never fire, so the APP_LOG line would
 * never diagnose the very problem it exists to catch, and the app would
 * silently draw in an arbitrary SDK default instead of the comparable system
 * face `fallback_key` deliberately names.  A zero-byte resource handle is the
 * signal fonts_load_custom_font() itself won't give us. */
static GFont load_font(uint32_t resource_id, const char *fallback_key) {
  ResHandle handle = resource_get_handle(resource_id);
  if (resource_size(handle) == 0) {
    APP_LOG(APP_LOG_LEVEL_ERROR, "font resource %u missing, using %s",
            (unsigned)resource_id, fallback_key);
    return fonts_get_system_font(fallback_key);
  }
  return fonts_load_custom_font(handle);
}

void layout_init(void) {
  s_font_focused = load_font(RESOURCE_ID_RF_BOLD_34, FONT_KEY_GOTHIC_28_BOLD);
  s_font_row = load_font(RESOURCE_ID_RF_BOLD_26, FONT_KEY_GOTHIC_24_BOLD);
  s_font_index = load_font(RESOURCE_ID_RF_BOLD_22, FONT_KEY_GOTHIC_18_BOLD);
  s_font_body = load_font(RESOURCE_ID_RF_28, FONT_KEY_GOTHIC_24);
}

void layout_deinit(void) {
  /* fonts_unload_custom_font tolerates a system font handle, but only custom
   * handles need it; unloading unconditionally is safe and keeps this short. */
  fonts_unload_custom_font(s_font_focused);
  fonts_unload_custom_font(s_font_row);
  fonts_unload_custom_font(s_font_index);
  fonts_unload_custom_font(s_font_body);
  s_font_focused = NULL;
  s_font_row = NULL;
  s_font_index = NULL;
  s_font_body = NULL;
}

GFont layout_font_focused(void) { return s_font_focused; }
GFont layout_font_row(void) { return s_font_row; }
GFont layout_font_index(void) { return s_font_index; }
GFont layout_font_body(void) { return s_font_body; }

GFont layout_font_sub(void) {
  return fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD);
}

int16_t layout_banner_height(void) {
  /* On a round display the top of the circle is only ~100px wide, so a banner
   * anchored at y=0 clips its own text.  Making the band deep enough to push
   * the text down to where the circle is wide is cheaper than trying to fit
   * the text into the cap.
   *
   * The number comes from the chord: on gabbro's 260px circle the usable width
   * at height y is 2*sqrt(130^2 - (130-y)^2), so the tops of the glyphs must
   * start around y=26 (156px) for LAYOUT_BANNER_INSET_H to fit inside the
   * circle, and the band has to be deep enough to hold a 26px line below that. */
  return PBL_IF_ROUND_ELSE(56, 34);
}

int16_t layout_footer_height(void) {
  return PBL_IF_ROUND_ELSE(58, 46);
}

int16_t layout_row_width(GRect bounds) {
#ifdef PBL_ROUND
  /* Centre-focused round menus draw the focused cell across the widest part of
   * the circle, so the usable width is larger than layout_content_rect's, but
   * still short of the full diameter. */
  return bounds.size.w - (bounds.size.w / 8) * 2 - 2 * CELL_PAD_H;
#else
  return bounds.size.w - 2 * CELL_PAD_H;
#endif
}

/* one_line_height measures a single line in `font`, which is how the line
 * count of a measured block is recovered. */
static int16_t one_line_height(GFont font) {
  GSize one = graphics_text_layout_get_content_size(
      "Ag", font, GRect(0, 0, 200, 400), GTextOverflowModeWordWrap,
      GTextAlignmentCenter);
  return one.h > 0 ? one.h : 1;
}

int16_t layout_cell_inset_h(GRect bounds, bool focused) {
#ifdef PBL_ROUND
  /* w/4 keeps an unfocused row inside the circle for every row position the
   * menu can put it in: at 65px of inset on gabbro the box is 130px wide, and
   * the chord is at least that from y=18 to y=242 of 260. */
  return focused ? CELL_PAD_H : (int16_t)(bounds.size.w / 4);
#else
  (void)bounds;
  (void)focused;
  return CELL_PAD_H;
#endif
}

/* layout_text_height returns the height text needs when wrapped into `width`,
 * capped at `max_lines` worth of that font's line height. */
int16_t layout_text_height(const char *text, GFont font, int16_t width,
                           int max_lines) {
  if (!text || !text[0]) {
    return 0;
  }
  /* A generous box: graphics_text_layout_get_content_size returns the height
   * actually used, so the ceiling only has to be large enough not to clip. */
  GRect box = GRect(0, 0, width, 400);
  GSize used = graphics_text_layout_get_content_size(
      text, font, box, GTextOverflowModeWordWrap, GTextAlignmentCenter);

  int16_t line_height = used.h;
  if (line_height <= 0) {
    return 0;
  }
  /* Cap at max_lines by measuring a single line and multiplying: one very long
   * label must not be allowed to fill the screen on its own. */
  GSize one = graphics_text_layout_get_content_size(
      "Ag", font, box, GTextOverflowModeWordWrap, GTextAlignmentCenter);
  int16_t cap = (int16_t)(one.h * max_lines);
  return used.h > cap ? cap : used.h;
}

GFont layout_label_font(const char *label, int16_t width, bool focused) {
  if (!focused) {
    return s_font_index;
  }
  if (!label || !label[0]) {
    return s_font_focused;
  }
  GSize used = graphics_text_layout_get_content_size(
      label, s_font_focused, GRect(0, 0, width, 800), GTextOverflowModeWordWrap,
      GTextAlignmentCenter);
  int16_t line = one_line_height(s_font_focused);
  return used.h <= line * LAYOUT_MAX_FOCUSED_LINES ? s_font_focused
                                                   : s_font_row;
}

int layout_label_lines(const char *label, int16_t width, bool focused) {
  GFont font = layout_label_font(label, width, focused);
  int cap = focused ? LAYOUT_MAX_FOCUSED_LINES : LAYOUT_MAX_INDEX_LINES;
  int16_t used = layout_text_height(label, font, width, cap);
  int16_t line = one_line_height(font);
  int lines = (used + line - 1) / line;
  return lines < 1 ? 1 : lines;
}

int layout_sub_lines(int label_lines, bool focused) {
  if (focused) {
    return 2;
  }
  /* The label already took the row.  See layout.h. */
  return label_lines > 1 ? 0 : 1;
}

int16_t layout_cell_height(const char *label, const char *sublabel,
                           int16_t width, bool focused) {
  GFont font = layout_label_font(label, width, focused);
  int lines = focused ? LAYOUT_MAX_FOCUSED_LINES : LAYOUT_MAX_INDEX_LINES;

  int16_t h = layout_text_height(label, font, width, lines);
  int sub_lines = layout_sub_lines(layout_label_lines(label, width, focused),
                                   focused);
  /* The sublabel carries a property's value, and is the half the user usually
   * wants, so it gets its own line rather than being appended to the key. */
  if (sublabel && sublabel[0] && sub_lines > 0) {
    GFont sub_font = focused ? s_font_row : layout_font_sub();
    h = (int16_t)(h + layout_text_height(sublabel, sub_font, width, sub_lines));
  }
  h = (int16_t)(h + 2 * CELL_PAD_V);

  int16_t floor_h = focused ? MIN_FOCUSED_HEIGHT : MIN_ROW_HEIGHT;
  return h < floor_h ? floor_h : h;
}

/* Measured from the face headers are actually drawn in, not from the SDK's
 * MENU_CELL_BASIC_HEADER_HEIGHT: that constant is sized for the system's small
 * header font, and a taller face drawn into it is clipped in half. */
int16_t layout_header_height(void) {
  int16_t text_h = layout_text_height("Ag", layout_font_sub(), 100, 1);
  return (int16_t)(text_h + CELL_PAD_V);
}

GColor layout_state_color(int state) {
  switch (state) {
    case DocStateLoading:
      return GColorPictonBlue;
    case DocStateError:
      return GColorFolly;
    /* Unreachable is amber, not red, and deliberately distinct: it means we
     * could not ask, which is not the same as a bad answer. */
    case DocStateUnreachable:
      return GColorChromeYellow;
    case DocStateNeedsConfig:
      return GColorVividViolet;
    case DocStateOk:
    default:
      return GColorBlack;
  }
}
