/* Display geometry, fonts and colours.
 *
 * RESTForge targets exactly two watches — emery (Pebble Time 2, 200x228,
 * rectangular) and gabbro (Pebble Round 2, 260x260, round) — and both of them
 * are colour, have 128 KB of app RAM and allow 512-byte font glyphs.  That is
 * what makes the deliberately large type in this app affordable: the smaller
 * platforms cap glyphs at 256 bytes and could not render a 34px face.
 *
 * The fonts are subsetted in package.json, and two things about that are worth
 * knowing because neither fails loudly.  The subset has to cover more than
 * ASCII: this app renders whatever a server sends, and a character outside the
 * subset draws as a hollow box rather than an error.  And the SDK caps a font
 * at 256 glyphs and simply stops walking the typeface's character map when it
 * gets there — so a subset larger than that silently loses everything with a
 * higher codepoint, which is why the font resources set "extended": true.
 *
 * Nothing here is a compile-time constant beyond the shape switch.  Screen
 * dimensions come from layer_get_bounds() at runtime, and row heights are
 * measured from the actual font rather than guessed, because the whole point
 * of the type choice is that a label is readable at arm's length — which only
 * holds if the box it is drawn into was sized around it. */

#pragma once

#include <pebble.h>

/* Content origin and width for the current display shape.  A round display
 * clips the corners of any rectangular text box, so text windows inset on
 * every side; a rectangular display does not.  MenuLayer handles round
 * geometry itself and does not use this. */
typedef struct {
  int16_t ox;
  int16_t oy;
  int16_t cw;
} ContentRect;

ContentRect layout_content_rect(GRect bounds);

/* Loads the custom fonts.  Call once at startup, before any window loads.
 * Falls back to the largest system faces if a resource fails to load, so a
 * broken build degrades to something readable rather than to nothing. */
void layout_init(void);
void layout_deinit(void);

/* RF_BOLD_34 — the focused menu row and any detail headline.  This is the
 * "reading surface": the row the user has selected is the one that gets the
 * full size and wraps over several lines. */
GFont layout_font_focused(void);

/* RF_BOLD_26 — the banner, and the second line of a focused row. */
GFont layout_font_row(void);

/* RF_BOLD_22 — unfocused menu rows.
 *
 * Smaller than the banner on purpose, and the reasoning is worth writing down
 * because it looks like a retreat from the "type as large as possible" brief
 * and is the opposite.  An unfocused row is an index entry: its job is to let
 * you decide whether to go there.  At 26px a 200px-wide screen holds about
 * thirteen characters, so "Power off every f-host (f0-f3)" arrived as "Power
 * off ev…" — larger type that says less.  At 22px over two lines the whole
 * label fits, and a label you can read is more legible than a bigger one you
 * cannot. */
GFont layout_font_index(void);

/* RF_28 — detail body and prompt text, always inside a ScrollLayer. */
GFont layout_font_body(void);

/* A system face for the second line of an *unfocused* row.  A property's value
 * matters, but on a row the user has not selected it is context, not reading
 * matter — at 26px two such lines cost more screen height than the row is
 * worth.  The focused row still renders its value at full size. */
GFont layout_font_sub(void);

/* Height of the title/message banner, which is taller on a round display
 * because the top of the circle is too narrow to hold text at y=0. */
int16_t layout_banner_height(void);

/* Height of the confirmation window's footer band.  Taller than the banner
 * because it carries two lines rather than one, and taller again on a round
 * display for the same chord reason. */
int16_t layout_footer_height(void);

/* Width available to a menu row's text, insets already subtracted. */
int16_t layout_row_width(GRect bounds);

/* Horizontal inset for a cell's text box.
 *
 * On a round display this is much larger for an unfocused row, and the reason
 * is geometry rather than taste: a centre-focused MenuLayer draws the focused
 * cell across the middle of the circle, where the full width is available, but
 * unfocused cells sit near the top and bottom where the chord is far shorter.
 * A box sized for the middle is silently clipped by the bezel up there —
 * silently because clipping happens in the frame buffer, long after the text
 * layout decided the line fitted.  Insetting instead makes the text ellipsise,
 * which is visible and honest. */
int16_t layout_cell_inset_h(GRect bounds, bool focused);

/* Height a row needs for its text, measured with the font it will be drawn
 * in.  A focused row wraps up to LAYOUT_MAX_FOCUSED_LINES; an unfocused row is
 * always a single line.  Both are clamped so one pathological label cannot
 * push every other row off the screen. */
#define LAYOUT_MAX_FOCUSED_LINES 4
/* An unfocused label gets two lines, which is what makes most of them
 * readable without focusing them. */
#define LAYOUT_MAX_INDEX_LINES 2
int16_t layout_cell_height(const char *label, const char *sublabel,
                           int16_t width, bool focused);

/* Lines the second line of a row may occupy, given how many the label needed.
 *
 * On an unfocused row whose label already wrapped, the answer is none: the
 * label is what the row is for, and spending the remaining height repeating a
 * method name or an ellipsised value buys nothing. */
int layout_sub_lines(int label_lines, bool focused);

/* Lines `label` needs in the face its row will use. */
int layout_label_lines(const char *label, int16_t width, bool focused);

/* The face a row's label should be drawn in.
 *
 * For the focused row this is a fitting decision, not a fixed size: the 34px
 * face if the label fits the lines available, and the 26px one if it does not.
 * "As large as possible" and "readable" are the same requirement, and when
 * they pull apart it is because the type has grown large enough to stop the
 * words fitting — at which point the larger face is the less legible one.
 * Callers must use this for measuring and for drawing, or the two disagree. */
GFont layout_label_font(const char *label, int16_t width, bool focused);

/* Height `text` needs in `font` when wrapped into `width`, capped at
 * `max_lines`.  Exposed so the drawing code can size its text box to exactly
 * what layout_cell_height reserved — if the two disagree, text either spills
 * past the cell or is clipped mid-line. */
int16_t layout_text_height(const char *text, GFont font, int16_t width,
                           int max_lines);

/* Effectively no cap, for text that lives in a ScrollLayer and is meant to run
 * as long as it runs.  A real ceiling still exists so a pathological string
 * cannot ask for a layer taller than the graphics system will allocate. */
#define LAYOUT_UNBOUNDED_LINES 200

/* Height of a section header cell, or 0 when a section has no header. */
int16_t layout_header_height(void);

/* Colour for a DocState, used to tint the status bar.  Colour is always a
 * second channel here and never the only one — the state is also spelled out
 * in the message line, because a glance at a tinted bar is not a reading. */
GColor layout_state_color(int state);
