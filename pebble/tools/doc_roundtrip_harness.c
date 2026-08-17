/* Host-only CLI driving the real doc.c parser for tools/test-doc-roundtrip.js.
 *
 * Reads one wire-frame payload from stdin (exactly what appmessage.js's
 * buildPayload() produces -- no chunking, since doc_set_frame() only ever
 * sees a payload after comm.c has reassembled the chunks), feeds it to
 * doc_set_frame(), and prints the parsed fields to stdout in a format the
 * Node test can parse back apart. sanitise() in appmessage.js strips every
 * control character before a field reaches the wire, so none of these
 * fields can contain '\n' or '\t' -- that is what makes a plain
 * line/tab-delimited dump safe here without inventing a second escaping
 * scheme on top of the one doc.c already speaks. */

#include <stdio.h>
#include "doc.h"

static char kind_char(DocRowKind kind) { return (char)kind; }

int main(void) {
  /* Slurp all of stdin into one NUL-terminated buffer; doc_set_frame() takes
   * ownership and frees it, matching how comm.c hands it a reassembled
   * chunk buffer. */
  size_t cap = 4096;
  size_t len = 0;
  char *buf = malloc(cap);
  if (!buf) {
    return 1;
  }

  int c;
  while ((c = getchar()) != EOF) {
    if (len + 1 >= cap) {
      cap *= 2;
      char *grown = realloc(buf, cap);
      if (!grown) {
        free(buf);
        return 1;
      }
      buf = grown;
    }
    buf[len++] = (char)c;
  }
  buf[len] = '\0';

  DocMeta meta = { DocStateOk, DocOverlayNone, false, false };
  doc_set_frame(buf, meta);

  printf("TITLE\t%s\n", doc_title());
  printf("MESSAGE\t%s\n", doc_message());
  printf("HEADING\t%s\n", doc_heading());
  printf("PROMPT\t%s\n", doc_prompt());
  printf("ROWS\t%d\n", doc_row_count());
  for (int i = 0; i < doc_row_count(); i++) {
    const char *sub = doc_row_sublabel(i);
    /* '\x01' is not producible by sanitise() (it strips 0x00-0x1f), so it is
     * a safe stand-in for "doc_row_sublabel() returned NULL" that the Node
     * side can tell apart from an actual (never-empty) sublabel string. */
    printf("ROW\t%d\t%c\t%s\t%s\n", i, kind_char(doc_row_kind(i)),
           doc_row_label(i), sub ? sub : "\x01");
  }

  doc_deinit();
  return 0;
}
