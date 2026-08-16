#!/usr/bin/env node
/* Writes the settings page to a file for `pebble emu-app-config --file`.
 *
 * The emulator cannot follow Pebble.openURL to a data: URI — desktop browsers
 * refuse top-level navigation to one — so pebble-tool offers --file instead.
 * This generator exists so that file is produced from src/pkjs/configpage.js
 * rather than hand-maintained: the emulator and the phone must be looking at
 * the same page, or testing the settings flow proves nothing.
 *
 * Usage: node tools/gen-config-page.js [out.html] [seed.json]
 *
 * seed.json, if given, is an array of backends to prefill — handy for testing
 * the edit and reorder paths without retyping. Do not commit one: it would
 * contain a secret.
 */

'use strict';

var fs = require('fs');
var path = require('path');

var configpage = require(path.join(__dirname, '..', 'src', 'pkjs', 'configpage'));

var out = process.argv[2] || path.join(__dirname, '..', 'build', 'config.html');
var seedPath = process.argv[3];

var backends = [];
if (seedPath && fs.existsSync(seedPath)) {
  backends = JSON.parse(fs.readFileSync(seedPath, 'utf8'));
}

/* Mirrors the defaults settings.js exports.  Duplicated rather than required
 * because settings.js reaches for localStorage, which node does not have. */
var DEFAULT_AUTH_HEADER = 'X-API-Key';
var MAX_BACKENDS = 12;

fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, configpage.html(backends, DEFAULT_AUTH_HEADER, MAX_BACKENDS));
console.log('wrote ' + out + ' (' + backends.length + ' backend(s) prefilled)');
