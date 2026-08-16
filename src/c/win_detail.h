/* The full-screen reading window.
 *
 * A menu row is one line of a size chosen so it can be read at arm's length,
 * which means a lot of values do not fit on one.  Rather than shrink the type
 * until everything fits — which would defeat the point of the type choice —
 * anything too long gets its own screen, at full size, in a ScrollLayer.
 *
 * It shows whatever the current frame's overlay carries: a property's whole
 * value, an error message too long for the banner, or a job's current step.
 * The window does not know which of those it is, and does not need to.
 *
 * It is pushed and popped by restforge.c in response to the frame's overlay
 * field, never by JS directly, and BACK dismisses it locally without telling
 * JS — the document underneath never changed, so there is nothing to tell. */

#pragma once

#include <pebble.h>
#include <stdbool.h>

/* Pushes the window, or redraws it with the current frame if already up. */
void win_detail_show(void);

/* Dismisses it if it is showing; harmless otherwise. */
void win_detail_hide(void);

bool win_detail_visible(void);

void win_detail_deinit(void);
