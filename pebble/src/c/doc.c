/* The document currently on screen.  See doc.h for the design rationale. */

#include "doc.h"

/* Frame layout, matching src/pkjs/appmessage.js:
 *   title \x1f rows \x1f kinds \x1f message \x1f heading \x1f prompt
 * with rows being "label\tsublabel" separated by newlines.  A frame that ends
 * early simply leaves the remaining fields empty, which is what makes adding a
 * field to the end of this list a compatible change.  JS strips every
 * control character from the text it puts in here, so these three separators
 * cannot occur inside a field and no escaping is needed. */
#define FIELD_SEP '\x1f'
#define ROW_SEP '\n'
#define SUB_SEP '\t'

typedef struct {
  const char *label;
  const char *sublabel;
  DocRowKind kind;
} DocRow;

/* One allocation holds the whole frame; everything below points into it. */
static char *s_payload;
static const char *s_title = "";
static const char *s_message = "";
static const char *s_heading = "";
static const char *s_prompt = "";
static DocRow s_rows[DOC_MAX_ROWS];
static int s_row_count;
static DocMeta s_meta = { DocStateLoading, DocOverlayNone, false, true };

/* next_token cuts *cursor at the first `sep`, returns the token, and advances
 * the cursor past it.  On the last token the cursor becomes NULL, which is how
 * callers tell "no separator was present" from "an empty field followed". */
static char *next_token(char **cursor, char sep) {
  char *start = *cursor;
  if (!start) {
    return NULL;
  }
  char *hit = strchr(start, sep);
  if (hit) {
    *hit = '\0';
    *cursor = hit + 1;
  } else {
    *cursor = NULL;
  }
  return start;
}

/* or_empty keeps every accessor non-NULL so callers never have to guard. */
static const char *or_empty(const char *s) { return s ? s : ""; }

static void parse_rows(char *rows, const char *kinds) {
  size_t kind_count = kinds ? strlen(kinds) : 0;
  char *cursor = rows;

  while (cursor && *cursor && s_row_count < DOC_MAX_ROWS) {
    char *row = next_token(&cursor, ROW_SEP);
    char *sub_cursor = row;
    char *label = next_token(&sub_cursor, SUB_SEP);

    /* A trailing newline yields one empty row; drop it rather than render a
     * blank selectable line. */
    if ((!label || !label[0]) && (!sub_cursor || !sub_cursor[0])) {
      continue;
    }

    s_rows[s_row_count].label = or_empty(label);
    s_rows[s_row_count].sublabel =
        (sub_cursor && sub_cursor[0]) ? sub_cursor : NULL;
    /* A kinds string shorter than the row list is not an error — an older or
     * newer JS bundle may send fewer kinds, and an unknown kind renders as a
     * plain property rather than failing. */
    s_rows[s_row_count].kind = (size_t)s_row_count < kind_count
                                   ? (DocRowKind)kinds[s_row_count]
                                   : DocRowProperty;
    s_row_count++;
  }
}

void doc_set_frame(char *payload, DocMeta meta) {
  free(s_payload);
  s_payload = payload;
  s_row_count = 0;
  s_title = "";
  s_message = "";
  s_heading = "";
  s_prompt = "";
  s_meta = meta;

  if (!payload) {
    return;
  }

  char *cursor = payload;
  s_title = or_empty(next_token(&cursor, FIELD_SEP));
  char *rows = next_token(&cursor, FIELD_SEP);
  const char *kinds = or_empty(next_token(&cursor, FIELD_SEP));
  s_message = or_empty(next_token(&cursor, FIELD_SEP));
  s_heading = or_empty(next_token(&cursor, FIELD_SEP));
  s_prompt = or_empty(next_token(&cursor, FIELD_SEP));

  if (rows) {
    parse_rows(rows, kinds);
  }
}

void doc_reset(void) {
  DocMeta meta = { DocStateLoading, DocOverlayNone, false, true };
  doc_set_frame(NULL, meta);
}

void doc_deinit(void) {
  free(s_payload);
  s_payload = NULL;
  s_row_count = 0;
}

const char *doc_title(void) { return s_title; }
const char *doc_message(void) { return s_message; }
const char *doc_heading(void) { return s_heading; }
const char *doc_prompt(void) { return s_prompt; }
DocState doc_state(void) { return s_meta.state; }
DocOverlay doc_overlay(void) { return s_meta.overlay; }
bool doc_live(void) { return s_meta.live; }
bool doc_at_root(void) { return s_meta.at_root; }
int doc_row_count(void) { return s_row_count; }

static bool valid(int index) { return index >= 0 && index < s_row_count; }

const char *doc_row_label(int index) {
  return valid(index) ? s_rows[index].label : "";
}

const char *doc_row_sublabel(int index) {
  return valid(index) ? s_rows[index].sublabel : NULL;
}

DocRowKind doc_row_kind(int index) {
  return valid(index) ? s_rows[index].kind : DocRowProperty;
}

bool doc_row_is_action(int index) {
  return doc_row_kind(index) == DocRowAction;
}
