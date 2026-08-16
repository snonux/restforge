#!/usr/bin/env node
/* Checks url.js against the reference test vectors in RFC 3986 section 5.4.
 *
 * This module is a hand-written stand-in for a URL constructor that PebbleKit
 * JS does not have, so nothing else validates it -- and a resolution bug is
 * exactly the kind that looks like the server misbehaving: the request goes
 * out, it just goes somewhere else. The specification ships its own test
 * suite, so use it rather than inventing cases.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');
var url = require(path.join(__dirname, '..', 'src', 'pkjs', 'url'));

var BASE = 'http://a/b/c/d;p?q';

/* RFC 3986 section 5.4.1 -- normal examples. */
var NORMAL = [
  ['g:h', 'g:h'],
  ['g', 'http://a/b/c/g'],
  ['./g', 'http://a/b/c/g'],
  ['g/', 'http://a/b/c/g/'],
  ['/g', 'http://a/g'],
  ['//g', 'http://g'],
  ['?y', 'http://a/b/c/d;p?y'],
  ['g?y', 'http://a/b/c/g?y'],
  ['#s', 'http://a/b/c/d;p?q#s'],
  ['g#s', 'http://a/b/c/g#s'],
  ['g?y#s', 'http://a/b/c/g?y#s'],
  [';x', 'http://a/b/c/;x'],
  ['g;x', 'http://a/b/c/g;x'],
  ['g;x?y#s', 'http://a/b/c/g;x?y#s'],
  ['', 'http://a/b/c/d;p?q'],
  ['.', 'http://a/b/c/'],
  ['./', 'http://a/b/c/'],
  ['..', 'http://a/b/'],
  ['../', 'http://a/b/'],
  ['../g', 'http://a/b/g'],
  ['../..', 'http://a/'],
  ['../../', 'http://a/'],
  ['../../g', 'http://a/g']
];

/* RFC 3986 section 5.4.2 -- abnormal examples. These are the ones a naive
 * split/join implementation gets wrong. */
var ABNORMAL = [
  ['../../../g', 'http://a/g'],
  ['../../../../g', 'http://a/g'],
  ['/./g', 'http://a/g'],
  ['/../g', 'http://a/g'],
  ['g.', 'http://a/b/c/g.'],
  ['.g', 'http://a/b/c/.g'],
  ['g..', 'http://a/b/c/g..'],
  ['..g', 'http://a/b/c/..g'],
  ['./../g', 'http://a/b/g'],
  ['./g/.', 'http://a/b/c/g/'],
  ['g/./h', 'http://a/b/c/g/h'],
  ['g/../h', 'http://a/b/c/h'],
  ['g;x=1/./y', 'http://a/b/c/g;x=1/y'],
  ['g;x=1/../y', 'http://a/b/c/y'],
  /* Dot segments inside a query or fragment are data, not path syntax. */
  ['g?y/./x', 'http://a/b/c/g?y/./x'],
  ['g?y/../x', 'http://a/b/c/g?y/../x'],
  ['g#s/./x', 'http://a/b/c/g#s/./x'],
  ['g#s/../x', 'http://a/b/c/g#s/../x']
];

var failures = 0;

function assert(label, condition, detail) {
  console.log((condition ? 'ok   ' : 'FAIL ') + label + (condition ? '' : '  ' + detail));
  if (!condition) { failures++; }
}

function runVectors(name, vectors) {
  for (var i = 0; i < vectors.length; i++) {
    var href = vectors[i][0];
    var want = vectors[i][1];
    var got = url.resolve(href, BASE);
    assert(name + ': "' + href + '"', got === want, 'got ' + got + ', want ' + want);
  }
}

runVectors('normal', NORMAL);
runVectors('abnormal', ABNORMAL);

/* The shapes this app actually meets: a root-relative href from a server
 * behind a reverse proxy, resolved against a configured base. */
function testRealShapes() {
  var base = 'https://host.example.org/cgi-bin/app/';
  assert('root-relative href keeps the configured origin',
         url.resolve('/cgi-bin/app/status', base) ===
         'https://host.example.org/cgi-bin/app/status');
  assert('a relative href resolves under the base',
         url.resolve('status', base) ===
         'https://host.example.org/cgi-bin/app/status');
  /* The reason settings.js appends a trailing slash: without one the base's
   * last segment is discarded and every href lands one level too high. */
  assert('a base without a trailing slash loses its last segment',
         url.resolve('status', 'https://host.example.org/cgi-bin/app') ===
         'https://host.example.org/cgi-bin/status');
  assert('an absolute href overrides the base entirely',
         url.resolve('https://other.example.org/x', base) ===
         'https://other.example.org/x');
}

function testHelpers() {
  assert('origin strips the path',
         url.origin('https://host.example.org/a/b?c=d') === 'https://host.example.org');
  assert('path keeps the query',
         url.path('https://host.example.org/a/b?c=d') === '/a/b?c=d');
  assert('a non-absolute URL has no origin', url.origin('/a/b') === '');
}

function testFormEncoding() {
  assert('form encoding uses + for spaces',
         url.encodeForm({ note: 'a b' }) === 'note=a+b');
  assert('form encoding escapes separators',
         url.encodeForm({ 'a&b': 'c=d' }) === 'a%26b=c%3Dd');
  assert('booleans are stringified',
         url.encodeForm({ force: true }) === 'force=true');
  assert('an empty field set is an empty body', url.encodeForm({}) === '');
}

testRealShapes();
testHelpers();
testFormEncoding();

if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('url: all checks passed');
