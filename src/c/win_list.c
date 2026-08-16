/* The list window.  See win_list.h for the design rationale. */

#include "win_list.h"
#include "comm.h"
#include "doc.h"
#include "layout.h"

/* One section per contiguous run of row kinds.  Five is the number of kinds
 * that exist, and JS emits rows already grouped, so this never overflows in
 * practice — but a stray interleaved kind folds into the previous section
 * rather than being dropped. */
#define MAX_SECTIONS 5

typedef struct {
  DocRowKind kind;
  int first;
  int count;
} Section;

static Window *s_window;
static MenuLayer *s_menu;
static Layer *s_banner;
static Section s_sections[MAX_SECTIONS];
static int s_section_count;

static const char *section_title(DocRowKind kind) {
  switch (kind) {
    case DocRowProperty: return "Properties";
    case DocRowEntity: return "Entities";
    case DocRowLink: return "Links";
    case DocRowAction: return "Actions";
    case DocRowBackend: return "Backends";
    case DocRowQuick: return "Quick";
    default: return "";
  }
}

static void compute_sections(void) {
  s_section_count = 0;
  int rows = doc_row_count();

  for (int i = 0; i < rows; i++) {
    DocRowKind kind = doc_row_kind(i);
    bool extends_last = s_section_count > 0 &&
                        s_sections[s_section_count - 1].kind == kind;
    if (extends_last || s_section_count == MAX_SECTIONS) {
      s_sections[s_section_count - 1].count++;
    } else {
      s_sections[s_section_count].kind = kind;
      s_sections[s_section_count].first = i;
      s_sections[s_section_count].count = 1;
      s_section_count++;
    }
  }
}

/* absolute_row maps a MenuIndex back onto doc.c's flat row list, which is the
 * only index JS understands. */
static int absolute_row(MenuIndex *index) {
  if (index->section >= (uint16_t)s_section_count) {
    return -1;
  }
  return s_sections[index->section].first + (int)index->row;
}

static uint16_t get_num_sections(MenuLayer *menu, void *context) {
  (void)menu;
  (void)context;
  return (uint16_t)(s_section_count > 0 ? s_section_count : 1);
}

static uint16_t get_num_rows(MenuLayer *menu, uint16_t section, void *context) {
  (void)menu;
  (void)context;
  if (section >= (uint16_t)s_section_count) {
    return 0;
  }
  return (uint16_t)s_sections[section].count;
}

static int16_t get_header_height(MenuLayer *menu, uint16_t section,
                                 void *context) {
  (void)menu;
  (void)context;
  /* A single-section list needs no header: the banner already names the
   * screen, and a header would cost a row of screen height for nothing. */
  if (s_section_count <= 1) {
    return 0;
  }
  return layout_header_height();
}

static void draw_header(GContext *ctx, const Layer *cell_layer,
                        uint16_t section, void *context) {
  (void)context;
  if (section >= (uint16_t)s_section_count) {
    return;
  }
  /* Headers are a label on a group, not reading matter, so they take the small
   * face — and the same round inset as an unfocused row, since a header sits
   * wherever the scroll puts it, including the narrow ends of the circle. */
  GRect bounds = layer_get_bounds(cell_layer);
  GRect box = grect_inset(bounds,
                          GEdgeInsets(0, layout_cell_inset_h(bounds, false)));
  graphics_context_set_text_color(ctx, GColorDarkGray);
  graphics_draw_text(ctx, section_title(s_sections[section].kind),
                     layout_font_sub(), box, GTextOverflowModeTrailingEllipsis,
                     PBL_IF_ROUND_ELSE(GTextAlignmentCenter, GTextAlignmentLeft),
                     NULL);
}

static int16_t get_cell_height(MenuLayer *menu, MenuIndex *index,
                               void *context) {
  (void)context;
  int row = absolute_row(index);
  if (row < 0) {
    return 0;
  }
  MenuIndex selected = menu_layer_get_selected_index(menu);
  bool focused = menu_index_compare(&selected, index) == 0;

  GRect bounds = layer_get_bounds(menu_layer_get_layer(menu));
  return layout_cell_height(doc_row_label(row), doc_row_sublabel(row),
                            layout_row_width(bounds), focused);
}

/* draw_row is where the readability requirement actually lands.  The focused
 * row is the reading surface: the 34px face, wrapped over up to four lines.
 * Every other row is an index entry at 22px over up to two — smaller than the
 * focused row, but showing far more of the label than one large truncated line
 * ever did.  See layout.h for why smaller can be the more legible choice. */
static void draw_row(GContext *ctx, const Layer *cell_layer, MenuIndex *index,
                     void *context) {
  (void)context;
  int row = absolute_row(index);
  if (row < 0) {
    return;
  }

  bool focused = menu_cell_layer_is_highlighted(cell_layer);
  GRect bounds = layer_get_bounds(cell_layer);
  /* The horizontal inset is not a constant on a round display: see
   * layout_cell_inset_h.  The cell's own bounds are full width wherever the
   * row sits, so the bezel, not the cell, is what would clip an unfocused row
   * near the top or bottom of the circle. */
  GRect box = grect_inset(bounds,
                          GEdgeInsets(4, layout_cell_inset_h(bounds, focused)));
  GFont font = layout_label_font(doc_row_label(row), box.size.w, focused);
  GTextAlignment align =
      PBL_IF_ROUND_ELSE(GTextAlignmentCenter, GTextAlignmentLeft);
  int max_lines = focused ? LAYOUT_MAX_FOCUSED_LINES : LAYOUT_MAX_INDEX_LINES;

  graphics_context_set_text_color(ctx, focused ? GColorWhite : GColorBlack);
  int16_t used_h = layout_text_height(doc_row_label(row), font, box.size.w,
                                      max_lines);
  GRect label_box = GRect(box.origin.x, box.origin.y, box.size.w, used_h);
  /* Ellipsised, not plain word-wrap: graphics_draw_text clips to the graphics
   * context, not to the box it was handed, so word-wrap happily draws a line
   * past the bottom of the cell and over the next row.  TrailingEllipsis wraps
   * within the box and stops at its edge, which is what the measured height
   * assumed. */
  graphics_draw_text(ctx, doc_row_label(row), font, label_box,
                     GTextOverflowModeTrailingEllipsis, align, NULL);

  const char *sub = doc_row_sublabel(row);
  int sub_lines = layout_sub_lines(
      layout_label_lines(doc_row_label(row), box.size.w, focused), focused);
  if (!sub || sub_lines == 0) {
    return;
  }
  /* The sublabel carries a property's value — the half the user came for — so
   * it gets its own line rather than being run together with the key.  Its box
   * is sized to exactly what layout_cell_height reserved, so a long value
   * ellipsises instead of wrapping past the bottom of the cell. */
  GFont sub_font = focused ? layout_font_row() : layout_font_sub();
  int16_t sub_h = layout_text_height(sub, sub_font, box.size.w, sub_lines);
  GRect sub_box =
      GRect(box.origin.x, box.origin.y + used_h, box.size.w, sub_h);
  graphics_context_set_text_color(ctx, focused ? GColorWhite : GColorDarkGray);
  graphics_draw_text(ctx, sub, sub_font, sub_box,
                     GTextOverflowModeTrailingEllipsis, align, NULL);
}

static void select_click(MenuLayer *menu, MenuIndex *index, void *context) {
  (void)context;
  (void)menu;
  int row = absolute_row(index);
  if (row >= 0) {
    comm_send_cmd(CommCmdActivateRow, row);
  }
}

/* The long-press menu.
 *
 * Long-press used to refresh outright.  It now opens a short list, because a
 * second verb was needed -- remembering the focused row as a shortcut -- and
 * spending a row of every document on it would cost more than it is worth.
 * What the list offers depends on where you are: a saved shortcut can be
 * forgotten, anything else can be remembered. */
static ActionMenu *s_action_menu;
static ActionMenuLevel *s_action_level;
static int s_long_pressed_row;

static void on_refresh(ActionMenu *menu, const ActionMenuItem *item,
                       void *context) {
  (void)menu;
  (void)item;
  (void)context;
  comm_send_cmd(CommCmdRefresh, 0);
}

static void on_save(ActionMenu *menu, const ActionMenuItem *item,
                    void *context) {
  (void)menu;
  (void)item;
  (void)context;
  comm_send_cmd(CommCmdSaveQuick, s_long_pressed_row);
}

static void on_remove(ActionMenu *menu, const ActionMenuItem *item,
                      void *context) {
  (void)menu;
  (void)item;
  (void)context;
  comm_send_cmd(CommCmdRemoveQuick, s_long_pressed_row);
}

static void action_menu_closed(ActionMenu *menu, const ActionMenuItem *item,
                               void *context) {
  (void)menu;
  (void)item;
  (void)context;
  action_menu_hierarchy_destroy(s_action_level, NULL, NULL);
  s_action_level = NULL;
  s_action_menu = NULL;
}

static void select_long_click(MenuLayer *menu, MenuIndex *index,
                              void *context) {
  (void)menu;
  (void)context;
  int row = absolute_row(index);
  if (row < 0) {
    return;
  }
  s_long_pressed_row = row;

  bool is_quick = doc_row_kind(row) == DocRowQuick;
  s_action_level = action_menu_level_create(2);
  if (is_quick) {
    action_menu_level_add_action(s_action_level, "Remove", on_remove, NULL);
  } else {
    action_menu_level_add_action(s_action_level, "Add to quick menu", on_save,
                                 NULL);
    action_menu_level_add_action(s_action_level, "Refresh", on_refresh, NULL);
  }

  ActionMenuConfig config = (ActionMenuConfig){
      .root_level = s_action_level,
      .colors = { .background = GColorDarkCandyAppleRed,
                  .foreground = GColorWhite },
      .align = ActionMenuAlignCenter,
      .will_close = action_menu_closed,
  };
  s_action_menu = action_menu_open(&config);
}

/* BACK pops the navigation stack, which lives in JS — the watch has one window
 * and no idea where it is, so popping the window instead would exit the app
 * from the middle of a document.
 *
 * At the root there is nothing left to pop, and BACK has to do what BACK does
 * everywhere else on the watch: leave.  Intercepting it there as well would
 * trap the user in the app with no way out. */
static void back_click(ClickRecognizerRef recognizer, void *context) {
  (void)recognizer;
  (void)context;
  if (doc_at_root()) {
    window_stack_pop_all(true);
    return;
  }
  comm_send_cmd(CommCmdBack, 0);
}

static ClickConfigProvider s_menu_click_config;

static void click_config(void *context) {
  if (s_menu_click_config) {
    s_menu_click_config(context);
  }
  window_single_click_subscribe(BUTTON_ID_BACK, back_click);
}

/* draw_banner fills the band with the state colour and writes the message over
 * it, falling back to the title when there is nothing wrong.  An error or an
 * "unreachable" always outranks the name of the screen it happened on.
 *
 * This is drawn rather than delegated to a TextLayer so the text can sit lower
 * than the band's top edge: on a round display the top of the circle is far
 * too narrow to hold a line of 26px type. */
static void draw_banner(Layer *layer, GContext *ctx) {
  GRect bounds = layer_get_bounds(layer);
  const char *message = doc_message();
  const char *text = (message && message[0]) ? message : doc_title();

  graphics_context_set_fill_color(ctx, layout_state_color(doc_state()));
  graphics_fill_rect(ctx, bounds, 0, GCornerNone);

  /* The insets are the chord calculation in layout_banner_height() made
   * concrete: text starts low enough, and is narrow enough, that its widest
   * line still lies inside the circle.  A wider box loses the first and last
   * character to the bezel — which is silent, since nothing is clipped at the
   * text-layout level, only at the frame buffer. */
  GRect box = grect_inset(
      bounds, GEdgeInsets(PBL_IF_ROUND_ELSE(26, 3),
                          PBL_IF_ROUND_ELSE(bounds.size.w / 5, 6), 0,
                          PBL_IF_ROUND_ELSE(bounds.size.w / 5, 6)));
  graphics_context_set_text_color(ctx, GColorWhite);
  graphics_draw_text(ctx, text ? text : "", layout_font_row(), box,
                     GTextOverflowModeTrailingEllipsis, GTextAlignmentCenter,
                     NULL);

  /* A mark for "this is still moving".  It is deliberately a second channel:
   * while something is running the banner also carries the server's own word
   * for the step, and that text is the real signal.  A dot alone would be a
   * claim nobody could read. */
  if (doc_live()) {
    graphics_context_set_fill_color(ctx, GColorWhite);
    graphics_fill_circle(ctx, GPoint(PBL_IF_ROUND_ELSE(bounds.size.w / 2,
                                                       bounds.size.w - 11),
                                     PBL_IF_ROUND_ELSE(11, 9)), 4);
  }
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect bounds = layer_get_bounds(root);
  window_set_background_color(window, GColorWhite);

  int16_t banner_h = layout_banner_height();
  s_banner = layer_create(GRect(0, 0, bounds.size.w, banner_h));
  layer_set_update_proc(s_banner, draw_banner);
  layer_add_child(root, s_banner);

  s_menu = menu_layer_create(
      GRect(0, banner_h, bounds.size.w, bounds.size.h - banner_h));
  menu_layer_set_callbacks(s_menu, NULL, (MenuLayerCallbacks){
                                             .get_num_sections = get_num_sections,
                                             .get_num_rows = get_num_rows,
                                             .get_header_height = get_header_height,
                                             .draw_header = draw_header,
                                             .get_cell_height = get_cell_height,
                                             .draw_row = draw_row,
                                             .select_click = select_click,
                                             .select_long_click = select_long_click,
                                         });
  menu_layer_set_normal_colors(s_menu, GColorWhite, GColorBlack);
  menu_layer_set_highlight_colors(s_menu, GColorDarkCandyAppleRed, GColorWhite);
  /* Centre-focused on both shapes, for two different reasons that happen to
   * want the same thing.  On a round display it keeps the selected row on the
   * widest part of the circle, the only place a 34px line has room.  On either
   * shape it is also the mode the SDK supports a *taller focused row* in: with
   * it off, the layout is computed from cell heights that go stale the moment
   * the selection moves, and a row measured as an index entry but drawn as the
   * reading surface spills over its neighbours. */
  menu_layer_set_center_focused(s_menu, true);
  layer_add_child(root, menu_layer_get_layer(s_menu));

  menu_layer_set_click_config_onto_window(s_menu, window);
  s_menu_click_config = window_get_click_config_provider(window);
  window_set_click_config_provider_with_context(window, click_config, s_menu);

  win_list_update();
}

static void window_unload(Window *window) {
  (void)window;
  menu_layer_destroy(s_menu);
  layer_destroy(s_banner);
  s_menu = NULL;
  s_banner = NULL;
}

void win_list_push(void) {
  s_window = window_create();
  window_set_window_handlers(s_window, (WindowHandlers){
                                           .load = window_load,
                                           .unload = window_unload,
                                       });
  window_stack_push(s_window, true);
}

void win_list_update(void) {
  if (!s_menu) {
    return;
  }
  compute_sections();
  layer_mark_dirty(s_banner);
  menu_layer_reload_data(s_menu);
}

void win_list_deinit(void) {
  window_destroy(s_window);
  s_window = NULL;
}
