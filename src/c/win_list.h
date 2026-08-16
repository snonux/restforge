/* The list window — the only navigational screen in the app.
 *
 * It renders whatever doc.c currently holds, which is either the backend
 * picker or one Siren document.  Both are the same thing to the watch: rows
 * with a kind.  There is no separate "backend screen" and no separate
 * "status screen", because the server decides what a document contains and
 * the watch is not entitled to an opinion about it.
 *
 * Sections come from runs of the same row kind, which is why JS emits rows
 * grouped: properties, then sub-entities, then links, then actions. */

#pragma once

#include <pebble.h>

void win_list_push(void);
void win_list_deinit(void);

/* Re-reads doc.c and redraws.  Called when a frame completes. */
void win_list_update(void);
