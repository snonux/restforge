/* Stand-in for the Pebble SDK's <pebble.h>, used only to host-build doc.c for
 * tools/test-doc-roundtrip.js.
 *
 * doc.c/doc.h touch nothing SDK-specific -- no Window, no Layer, no graphics
 * call -- so the real header is only pulled in for the handful of libc
 * declarations (malloc/free, strchr/strlen, size_t, bool) it happens to
 * re-export. Providing exactly those here lets doc.c compile as ordinary
 * host C, which is what makes it possible to run the real parser (not a
 * reimplementation of it) against a JS-built payload in CI. If doc.c ever
 * starts using an actual SDK symbol, this shim will fail to compile and that
 * is the signal to either extend it or reconsider the round-trip test. */

#pragma once

#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
