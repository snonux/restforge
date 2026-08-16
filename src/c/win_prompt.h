/* The confirmation window.
 *
 * Every action that is not a safe HTTP method goes through here before
 * anything is sent.  This is not caution for its own sake: the watch has four
 * buttons and lives on a wrist, and a document whose rows are "a property", "a
 * link" and "power off three machines" cannot be navigated safely if the last
 * one happens on the same single press as the first two.  RFC 9110 already
 * divides methods into safe and unsafe; that division is the gate, and it
 * needs no knowledge of any particular server.
 *
 * What it shows is the server's own wording — an action's title, and the title
 * of the confirmation field if it has one, verbatim and scrollable, because
 * that sentence is where the server explains what the user is about to do and
 * this app has nothing to add to it.
 *
 * SELECT confirms, BACK declines.  Both are reported to JS: unlike the reading
 * window, dismissing this one is an answer, and JS is holding a pending action
 * until it hears which.
 */

#pragma once

#include <pebble.h>
#include <stdbool.h>

/* Pushes the window, or redraws it with the current frame if already up. */
void win_prompt_show(void);

/* Dismisses it without answering.  Used when a new frame supersedes the
 * question — JS has already moved on, so there is nothing to reply to. */
void win_prompt_hide(void);

bool win_prompt_visible(void);

void win_prompt_deinit(void);
