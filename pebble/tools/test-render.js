#!/usr/bin/env node
/* Checks render.js.
 *
 * The property worth testing here is negative: rendering must not interpret.
 * A client that turns "ping: false" into "off", or hides a link it does not
 * recognise, is making claims the server did not make -- and on a watch, where
 * the screen is the only thing the wearer sees, those claims are the whole
 * output. So most of these assertions are that values survive untouched and
 * that nothing is dropped.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');
var render = require(path.join(__dirname, '..', 'src', 'pkjs', 'render'));

var failures = 0;

function assert(label, condition) {
  console.log((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

var DOC = {
  class: ['pantry'],
  title: 'The pantry',
  properties: { kettle: 'cold', brews: 0, tidy: false, missing: null },
  entities: [
    { class: ['shelf'], title: 'Top shelf',
      properties: { name: 'top', jars: 4, reachable: true } },
    { class: ['shelf'], title: 'Bottom shelf', href: '/shelves/bottom' }
  ],
  links: [
    { rel: ['self'], href: '/' },
    { rel: ['shelves'], href: '/shelves' }
  ],
  actions: [
    { name: 'brew', title: 'Brew a pot of tea', method: 'POST', href: '/brew',
      fields: [{ name: 'strength', type: 'range' }] },
    { name: 'peek', href: '/peek' }
  ]
};

function kindsOf(page) {
  var out = '';
  for (var i = 0; i < page.rows.length; i++) {
    out += page.rows[i].kind;
  }
  return out;
}

function rowNamed(page, label) {
  for (var i = 0; i < page.rows.length; i++) {
    if (page.rows[i].label === label) {
      return page.rows[i];
    }
  }
  return null;
}

function testOrderAndCompleteness() {
  var page = render.document(DOC);
  assert('the title comes from the document', page.title === 'The pantry');
  /* Siren's own order: properties, entities, links, actions. Choosing a
   * different one would mean deciding which part of somebody else's document
   * matters most. */
  assert('rows are in document order', kindsOf(page) === 'ppppeellaa');
  assert('nothing is dropped', page.rows.length ===
         4 + DOC.entities.length + DOC.links.length + DOC.actions.length);
  /* Including the ones a client might be tempted to hide. */
  assert('the self link is offered like any other', !!rowNamed(page, 'self'));
}

function testValuesAreNotInterpreted() {
  var page = render.document(DOC);
  assert('a false boolean stays false', rowNamed(page, 'tidy').sublabel === 'false');
  assert('a zero stays zero', rowNamed(page, 'brews').sublabel === '0');
  /* null is a value the server chose to send, and it is not the same as a
   * property being absent. */
  assert('a null stays null', rowNamed(page, 'missing').sublabel === 'null');
  assert('a string is passed through',
         rowNamed(page, 'kettle').sublabel === 'cold');
}

function testTargets() {
  var page = render.document(DOC);
  var property = rowNamed(page, 'kettle');
  assert('a property opens the reading window',
         property.target.type === 'detail' && property.target.body === 'cold');

  var embedded = rowNamed(page, 'Top shelf');
  /* Already in hand: opening it asks the server nothing, and asking again
   * could legitimately return something different. */
  assert('an embedded entity is opened locally',
         embedded.target.type === 'embedded' && embedded.target.index === 0);
  assert('an embedded entity is summarised',
         embedded.sublabel === 'name top  jars 4');

  var reference = rowNamed(page, 'Bottom shelf');
  assert('a referenced entity is fetched',
         reference.target.type === 'fetch' &&
         reference.target.href === '/shelves/bottom');

  var link = rowNamed(page, 'shelves');
  assert('a link is fetched by its href',
         link.target.type === 'fetch' && link.target.href === '/shelves');
}

function testActions() {
  var page = render.document(DOC);
  /* The title is the server's sentence for a person; the name is an
   * identifier. The sentence wins. */
  var titled = rowNamed(page, 'Brew a pot of tea');
  assert('an action shows its title', !!titled);
  assert('an action is addressed by name',
         titled.target.type === 'action' && titled.target.name === 'brew');
  assert('an action shows its method and field count',
         titled.sublabel === 'POST, 1 field(s)');
  /* Siren's default method, for an action that does not say. */
  var untitled = rowNamed(page, 'peek');
  assert('an action without a title falls back to its name', !!untitled);
  assert('an action without a method defaults to GET',
         untitled.sublabel === 'GET');
}

/* Regression: entities distinguished only by a property. */
function testEntitiesSharingAClass() {
  var hosts = {
    class: ['status'],
    entities: [
      { class: ['host', 'f'], properties: { ip: '10.0.0.1', ms: 15, name: 'f0' } },
      { class: ['host', 'f'], properties: { ip: '10.0.0.2', ms: 24, name: 'f1' } }
    ]
  };
  var page = render.document(hosts);
  assert('entities sharing a class are told apart',
         page.rows[0].label === 'f0' && page.rows[1].label === 'f1');
  /* The label is not repeated below itself: on this screen the summary has
   * room for two facts and one of them must not be the heading again. */
  assert('the summary does not repeat the label',
         page.rows[0].sublabel === 'ip 10.0.0.1  ms 15');
}

function testUnfamiliarAndMalformed() {
  /* A document made entirely of things this app has never seen. It must render
   * completely, because "I do not recognise this" is not a reason to withhold
   * it from the person wearing the watch. */
  var alien = {
    class: ['quux'],
    properties: { zork: { nested: [1, 2] } },
    links: [{ rel: ['frobnicate'], href: '/f' }],
    actions: [{ name: 'gorp', method: 'DELETE', href: '/g' }]
  };
  var page = render.document(alien, 'fallback');
  assert('an unfamiliar document still renders', page.rows.length === 3);
  assert('a nested value is shown as its JSON',
         rowNamed(page, 'zork').sublabel === '{"nested":[1,2]}');
  assert('an untitled document falls back to its class', page.title === 'quux');

  var empty = render.document({}, 'fallback');
  assert('an empty document renders no rows', empty.rows.length === 0);
  assert('a nameless document uses the fallback title',
         empty.title === 'fallback');
  var junk = render.document(null, 'fallback');
  assert('a missing document does not throw', junk.rows.length === 0);
}

testOrderAndCompleteness();
testValuesAreNotInterpreted();
testTargets();
testActions();
testEntitiesSharingAClass();
testUnfamiliarAndMalformed();

if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('render: all checks passed');
