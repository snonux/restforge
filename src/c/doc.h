/* The document currently on screen.
 *
 * This is everything the watch knows about the backend it is browsing, and it
 * is deliberately almost nothing: a title, a list of rows, and a state.  The
 * watch never learns a URL, an href, a method or a secret — PebbleKit JS keeps
 * all of those and the watch addresses a row only by its index.  That is the
 * hypermedia rule ("never build a URL") surviving the AppMessage boundary, and
 * it is also why a compromised watch app cannot leak an API key.
 *
 * Rows arrive as one newline-separated string, each row being
 * "label\tsublabel" with the tab and sublabel optional.  doc.c rewrites the
 * separators to NUL in place, so the row table is pointers into one buffer and
 * there is exactly one allocation per document. */

#pragma once

#include <pebble.h>
#include <stdbool.h>

/* Mirrors the STATE key.  The distinction between Error and Unreachable is the
 * single most important thing in this app: a failed request tells you that you
 * could not ask, not that the answer was "no".  Collapsing the two is how a
 * client ends up reporting a healthy homelab as powered down because the phone
 * lost signal. */
typedef enum {
  DocStateOk = 0,
  DocStateLoading = 1,
  DocStateError = 2,
  DocStateUnreachable = 3,
  DocStateNeedsConfig = 4,
} DocState;

/* Mirrors the KINDS key, one character per row.  Kind drives the accessory and
 * what activating the row means; it never changes how the text is rendered. */
typedef enum {
  DocRowProperty = 'p',
  DocRowEntity = 'e',
  DocRowLink = 'l',
  DocRowAction = 'a',
  DocRowBackend = 'b',
  /* A saved shortcut on the opening screen. */
  DocRowQuick = 'q',
} DocRowKind;

/* What, if anything, should be shown on top of the list.
 *
 * An overlay does not replace the document — the list underneath is still the
 * same document, and dismissing the overlay reveals it unchanged.  That is why
 * an overlay travels with the document it belongs to rather than being a
 * document of its own: the watch can dismiss it without asking JS anything,
 * and JS's navigation stack never learns that it happened. */
typedef enum {
  DocOverlayNone = 0,
  DocOverlayConfirm = 1,
  DocOverlayText = 2,
  DocOverlayDetail = 3,
} DocOverlay;

/* Bound on rows kept.  JS truncates to this too and appends an "(N more)" row
 * when it drops any, so overflow is visible rather than silent. */
#define DOC_MAX_ROWS 40

/* Everything about a frame that is not text.  Grouped rather than passed as
 * five parameters because they arrive together, on the final chunk, and are
 * applied together — a state that landed before the message it describes would
 * flash the wrong colour. */
typedef struct {
  DocState state;
  DocOverlay overlay;
  bool live;
  /* True when this screen is the root of the navigation — the backend picker.
   * BACK leaves the app from there rather than going deeper into nothing, and
   * only JS knows how deep the stack is, so it has to say. */
  bool at_root;
} DocMeta;

/* Replaces the current document from a completed frame.  `payload` is the
 * reassembled chunk buffer and is consumed destructively — doc.c takes
 * ownership and frees it on the next call or at deinit.  Passing NULL clears
 * the rows but keeps the state and message, which is what an error on top of
 * an already-loaded screen wants. */
void doc_set_frame(char *payload, DocMeta meta);

/* Clears everything.  Used when switching backends, where keeping the previous
 * screen's rows would be actively misleading. */
void doc_reset(void);

void doc_deinit(void);

const char *doc_title(void);
const char *doc_message(void);
/* The overlay's short heading — a property's key, or an action's name. */
const char *doc_heading(void);
/* The overlay's body — a property's full value, or a field's title verbatim.
 * This is the one string in a frame that is deliberately not truncated: it
 * exists precisely because it did not fit on a row. */
const char *doc_prompt(void);
DocState doc_state(void);
DocOverlay doc_overlay(void);
bool doc_live(void);
/* True on the backend picker, where BACK means "leave the app". */
bool doc_at_root(void);

int doc_row_count(void);
const char *doc_row_label(int index);
/* NULL when the row has no second line. */
const char *doc_row_sublabel(int index);
DocRowKind doc_row_kind(int index);

/* True when activating this row asks the backend to change something, so the
 * UI can mark it and the prompt flow knows to expect a reply. */
bool doc_row_is_action(int index);
