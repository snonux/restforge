#!/usr/bin/env node
/* Exercises the settings page's own script outside a browser.
 *
 * The page is the one part of RESTForge that runs in neither the watch nor
 * PebbleKit JS, so nothing else covers it: a mistake here shows up as a
 * settings screen that will not close, which is indistinguishable from the
 * emulator's config plumbing being broken.  The DOM shim below is the minimum
 * the page actually touches.
 *
 * Run with: just test
 */

'use strict';

var vm = require('vm');
var path = require('path');

var configpage = require(path.join(__dirname, '..', 'src', 'pkjs', 'configpage'));

var DEFAULT_HEADER = 'X-API-Key';
var MAX_BACKENDS = 12;

/* --- the shim ---------------------------------------------------------- */

function makeNode(tag) {
  return {
    tagName: tag, className: '', id: '', type: '', value: '', placeholder: '',
    disabled: false, children: [], attrs: {}, onclick: null,
    get firstChild() { return this.children[0] || null; },
    appendChild: function (node) { this.children.push(node); return node; },
    setAttribute: function (key, value) { this.attrs[key] = value; },
    /* The page only ever assigns '' to clear a container. */
    set innerHTML(value) { if (value === '') { this.children = []; } },
    get innerHTML() { return ''; }
  };
}

function textNode(text) {
  var node = makeNode('#text');
  node.nodeValue = text;
  return node;
}

function findById(node, id) {
  if (node.id === id) {
    return node;
  }
  for (var i = 0; i < node.children.length; i++) {
    var hit = findById(node.children[i], id);
    if (hit) { return hit; }
  }
  return null;
}

function textOf(node) {
  if (node.nodeValue !== undefined) {
    return node.nodeValue;
  }
  var out = '';
  for (var i = 0; i < node.children.length; i++) {
    out += textOf(node.children[i]);
  }
  return out;
}

/* run loads the real page at the given address and returns handles to it.
 * `href` matters: it is what the page reads to decide which close mechanism
 * the host supports. */
function run(href, backends) {
  var body = makeNode('body');
  body.scrollHeight = 100;
  var fixed = {};
  ['list', 'error', 'add', 'save', 'cancel'].forEach(function (id) {
    var node = makeNode('div');
    node.id = id;
    body.appendChild(node);
    fixed[id] = node;
  });

  var navigated = [];
  var sandbox = {
    console: { log: function () {} },
    document: {
      body: body,
      createElement: makeNode,
      createTextNode: textNode,
      getElementById: function (id) { return findById(body, id); }
    },
    location: {
      get href() { return href; },
      set href(value) { navigated.push(value); }
    },
    window: { scrollTo: function () {} },
    encodeURIComponent: encodeURIComponent,
    decodeURIComponent: decodeURIComponent,
    JSON: JSON
  };

  var html = configpage.html(backends, DEFAULT_HEADER, MAX_BACKENDS);
  var script = /<script>([\s\S]*)<\/script>/.exec(html)[1];
  vm.createContext(sandbox);
  vm.runInContext(script, sandbox);
  return { doc: sandbox.document, fixed: fixed, navigated: navigated };
}

var failures = 0;

function assert(label, condition) {
  console.log((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

function backend(name, url) {
  return { name: name, baseUrl: url, authHeader: DEFAULT_HEADER,
           secret: 'secret-' + name, startRel: '' };
}

/* tools returns a card's button row, which the page appends last. */
function tools(card) {
  return card.children[card.children.length - 1];
}

var FILE_HREF = 'file:///tmp/config.html';

/* --- the page renders and refuses invalid input ------------------------ */

function testAddAndValidate() {
  var t = run(FILE_HREF, []);
  assert('empty state is stated, not blank',
         textOf(t.fixed.list).indexOf('No backends yet') >= 0);

  t.fixed.add.onclick();
  assert('add creates a card', t.fixed.list.children.length === 1);
  assert('auth header defaults to ' + DEFAULT_HEADER,
         t.doc.getElementById('authHeader-0').value === DEFAULT_HEADER);

  t.fixed.save.onclick();
  assert('save is refused with an empty name',
         textOf(t.fixed.error).indexOf('Name is required') >= 0);
  assert('a refused save navigates nowhere', t.navigated.length === 0);
}

/* --- the two close paths ----------------------------------------------- */

function testPhoneClose() {
  var t = run(FILE_HREF, []);
  t.fixed.add.onclick();
  t.doc.getElementById('name-0').value = 'homelab';
  t.doc.getElementById('baseUrl-0').value = 'https://example.org/api/';
  t.doc.getElementById('secret-0').value = 'sekrit';
  t.fixed.save.onclick();

  assert('phone close uses the pebblejs scheme',
         t.navigated.length === 1 &&
         t.navigated[0].indexOf('pebblejs://close#') === 0);
  var payload = JSON.parse(decodeURIComponent(t.navigated[0].split('#')[1]));
  assert('phone payload round-trips the typed values',
         payload.length === 1 && payload[0].name === 'homelab' &&
         payload[0].secret === 'sekrit');
}

function testEmulatorClose() {
  var href = FILE_HREF + '?return_to=' +
             encodeURIComponent('http://localhost:1234/close?');
  var t = run(href, [backend('a', 'https://host/x/')]);
  t.fixed.save.onclick();

  assert('emulator close honours return_to',
         t.navigated.length === 1 &&
         t.navigated[0].indexOf('http://localhost:1234/close?') === 0);
  var query = t.navigated[0].split('close?')[1];
  assert('emulator payload parses once decoded',
         JSON.parse(decodeURIComponent(query))[0].name === 'a');
}

function testCancel() {
  var t = run(FILE_HREF, [backend('a', 'https://host/x/')]);
  t.fixed.cancel.onclick();
  /* An empty response, which index.js must read as "leave storage alone" and
   * never as "the user deleted everything". */
  assert('cancel closes with an empty payload',
         t.navigated[0] === 'pebblejs://close#');
}

/* --- structural edits keep unsaved input ------------------------------- */

function testReorderKeepsEdits() {
  var t = run(FILE_HREF, [backend('one', 'https://host/1/'),
                          backend('two', 'https://host/2/')]);
  t.doc.getElementById('name-1').value = 'two edited';
  tools(t.fixed.list.children[1]).children[0].onclick();  /* Up */

  assert('reorder moves the card',
         t.doc.getElementById('name-0').value === 'two edited');
  assert('reorder keeps the untouched card',
         t.doc.getElementById('name-1').value === 'one');
}

function testDelete() {
  var t = run(FILE_HREF, [backend('one', 'https://host/1/'),
                          backend('two', 'https://host/2/')]);
  tools(t.fixed.list.children[0]).children[2].onclick();  /* Delete */

  assert('delete removes one card', t.fixed.list.children.length === 1);
  assert('delete removes the right card',
         t.doc.getElementById('name-0').value === 'two');
}

/* --- URL rules, which must match settings.js --------------------------- */

function testUrlRules() {
  var relative = run(FILE_HREF, [backend('x', '/cgi-bin/app/')]);
  relative.fixed.save.onclick();
  assert('a relative base URL is refused',
         textOf(relative.fixed.error).indexOf('must be absolute') >= 0);

  /* Accepted here because settings.js appends the slash; rejecting it would
   * make the user fix something the code fixes anyway. */
  var noSlash = run(FILE_HREF, [backend('x', 'https://host/cgi-bin/app')]);
  noSlash.fixed.save.onclick();
  assert('a missing trailing slash is accepted',
         noSlash.navigated.length === 1);
}

/* --- the page must be inert as a data: URI ----------------------------- */

function testDataUri() {
  var uri = configpage.dataUri([backend('x', 'https://host/x/')],
                               DEFAULT_HEADER, MAX_BACKENDS);
  var body = uri.slice('data:text/html,'.length);
  /* pebble-tool splices its return_to parameter in after the first '?', so a
   * raw '?' or '#' anywhere in the document would truncate the page. */
  assert('the data: URI contains no raw ? or #',
         body.indexOf('?') < 0 && body.indexOf('#') < 0);
}

/* --- a stored value cannot escape the inline script -------------------- */

function testScriptInjection() {
  var hostile = backend('</script><img src=x>', 'https://host/x/');
  var html = configpage.html([hostile], DEFAULT_HEADER, MAX_BACKENDS);
  assert('an embedded </script> is escaped',
         html.indexOf('</script><img') < 0);
  var t = run(FILE_HREF, [hostile]);
  assert('the hostile value survives as plain text',
         t.doc.getElementById('name-0').value === '</script><img src=x>');
}

/* --- a stored value cannot be reinterpreted as a replace() pattern ----- */

function testDollarPatternInjection() {
  /* '$&', '$$', '$`' and "$'" are special to String.prototype.replace()'s
   * replacement-string argument, regardless of whether the search pattern is
   * a string or a regex. If html() ever substitutes embed(backends) via a
   * plain string replacement again, a name containing '$&' would corrupt
   * itself with the matched placeholder text, and one containing '$`' would
   * splice raw page-script source into the JSON, breaking the script's
   * syntax outright. Covering both proves the replacer-function fix in
   * html() actually closes the class, not just today's known trigger. */
  var dollarAmp = backend('weird $& name', 'https://host/x/');
  var ampHtml = configpage.html([dollarAmp], DEFAULT_HEADER, MAX_BACKENDS);
  assert('a $& field is embedded verbatim, not replace()-interpolated',
         ampHtml.indexOf('weird $& name') >= 0);
  var ampRun = run(FILE_HREF, [dollarAmp]);
  assert('a $& field round-trips through the DOM',
         ampRun.doc.getElementById('name-0').value === 'weird $& name');

  var dollarBacktick = backend('weird $` name', 'https://host/x/');
  /* run() itself parses and executes the generated script via vm; if the
   * script's syntax were broken by a bad substitution, this would throw. */
  var backtickRun = run(FILE_HREF, [dollarBacktick]);
  assert('a $` field round-trips without breaking the script',
         backtickRun.doc.getElementById('name-0').value === 'weird $` name');
}

testAddAndValidate();
testPhoneClose();
testEmulatorClose();
testCancel();
testReorderKeepsEdits();
testDelete();
testUrlRules();
testDataUri();
testScriptInjection();
testDollarPatternInjection();

if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('configpage: all checks passed');
