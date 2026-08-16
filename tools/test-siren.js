#!/usr/bin/env node
/* Checks siren.js.
 *
 * The rule these tests exist to hold is that an unknown class, rel, action
 * name or field type is ordinary, not an error. A hypermedia client that
 * throws on something it has not seen before has to be updated every time the
 * server grows a feature, which is the failure the design is meant to avoid.
 * So most of what follows is malformed or unfamiliar input that must come back
 * as "not offered" rather than as an exception.
 *
 * Run with: just test
 */

'use strict';

var path = require('path');
var siren = require(path.join(__dirname, '..', 'src', 'pkjs', 'siren'));

var failures = 0;

function assert(label, condition) {
  console.log((condition ? 'ok   ' : 'FAIL ') + label);
  if (!condition) { failures++; }
}

var DOC = {
  class: ['home'],
  title: 'Example service',
  properties: { apiVersion: 1, version: 'v1.2.3' },
  entities: [
    { class: ['widget'], properties: { name: 'a' } },
    { class: ['widget'], href: '/widgets/b' }
  ],
  links: [
    { rel: ['self'], href: '/' },
    { rel: ['collection', 'widgets'], href: '/widgets' }
  ],
  actions: [
    { name: 'restart', method: 'POST', href: '/restart',
      title: 'Restart the service',
      fields: [{ name: 'force', type: 'checkbox', title: 'Really?' }] },
    { name: 'peek', href: '/peek' }
  ]
};

function testAccessors() {
  assert('properties are returned', siren.properties(DOC).version === 'v1.2.3');
  assert('classes are returned', siren.classes(DOC)[0] === 'home');
  assert('links are returned', siren.links(DOC).length === 2);
  assert('actions are returned', siren.actions(DOC).length === 2);
  assert('sub-entities are returned', siren.subEntities(DOC).length === 2);
}

function testLookup() {
  assert('a link is found by its first rel',
         siren.follow(DOC, 'self') === '/');
  /* Siren's rel is a list; a link is found by any of its rels, not just the
   * first, because which one a server puts first is not part of the contract. */
  assert('a link is found by a later rel',
         siren.follow(DOC, 'widgets') === '/widgets');
  assert('an absent rel is null, not an error',
         siren.follow(DOC, 'nothing-like-this') === null);
  assert('an action is found by name',
         siren.action(DOC, 'restart').href === '/restart');
  assert('an absent action is null, not an error',
         siren.action(DOC, 'never-offered') === null);
  assert('fields come back as a list',
         siren.fields(siren.action(DOC, 'restart')).length === 1);
  assert('an action without fields has none',
         siren.fields(siren.action(DOC, 'peek')).length === 0);
}

function testMethod() {
  assert('a declared method is used',
         siren.method(siren.action(DOC, 'restart')) === 'POST');
  /* Siren's default. A server that means to change something says so. */
  assert('an undeclared method defaults to GET',
         siren.method(siren.action(DOC, 'peek')) === 'GET');
}

function testReferences() {
  var embedded = siren.subEntities(DOC)[0];
  var reference = siren.subEntities(DOC)[1];
  assert('an embedded entity is not a reference', !siren.isReference(embedded));
  assert('a bare href is a reference', siren.isReference(reference));
}

function testLabel() {
  assert('a title is preferred', siren.label(DOC) === 'Example service');
  assert('a name is next',
         siren.label({ name: 'restart' }) === 'restart');
  /* Found against a real API: four entities sharing one class rendered as four
   * identical rows, because the only thing distinguishing them was a property.
   * A list where every row reads the same is not a list. */
  assert('an identifying property beats the class',
         siren.label({ class: ['host', 'f'],
                       properties: { ip: '10.0.0.1', name: 'f0' } }) === 'f0');
  assert('a numeric identifier counts',
         siren.label({ class: ['job'], properties: { id: 7 } }) === '7');
  assert('the identifying key is reported',
         siren.identifier({ properties: { ip: 'x', name: 'f0' } }) === 'name');
  assert('an entity with no identifier reports none',
         siren.identifier({ properties: { ip: 'x' } }) === null);
  assert('a title still wins over a property',
         siren.label({ title: 'Top shelf', properties: { name: 'top' } }) ===
         'Top shelf');
  assert('a class is next',
         siren.label({ class: ['widget', 'large'] }) === 'widget large');
  assert('a rel is last',
         siren.label({ rel: ['collection'] }) === 'collection');
  /* Never invent a name: an invented name is a claim about what the thing is,
   * and this app has no basis for one. */
  assert('nothing recognisable yields nothing', siren.label({}) === '');
  assert('a missing node yields nothing', siren.label(null) === '');
}

function testVersionCheck() {
  assert('a supported version proceeds',
         siren.versionProblem({ properties: { apiVersion: 1 } }) === null);
  assert('an older version proceeds',
         siren.versionProblem({ properties: { apiVersion: 0 } }) === null);
  assert('a newer version stops',
         /apiVersion 2/.test(siren.versionProblem({ properties: { apiVersion: 2 } })));
  /* A server that declares nothing is making no claim we can act on, and
   * erroring would break this app against every server that never had the
   * field. */
  assert('an absent version proceeds',
         siren.versionProblem({ properties: {} }) === null);
  assert('a non-numeric version proceeds',
         siren.versionProblem({ properties: { apiVersion: 'one' } }) === null);
}

/* The important half: nothing below is well-formed, and none of it may throw. */
function testMalformed() {
  var junk = [
    null, undefined, {}, [], 'a string', 42,
    { links: 'not a list', actions: null, entities: 7, properties: [] },
    { links: [{ rel: 'self', href: '/' }] },
    { links: [{ rel: ['self'] }] },
    { actions: [{}] }
  ];
  var threw = null;
  for (var i = 0; i < junk.length; i++) {
    try {
      siren.properties(junk[i]);
      siren.classes(junk[i]);
      siren.links(junk[i]);
      siren.actions(junk[i]);
      siren.subEntities(junk[i]);
      siren.follow(junk[i], 'self');
      siren.action(junk[i], 'restart');
      siren.label(junk[i]);
      siren.versionProblem(junk[i]);
      siren.summary(junk[i]);
    } catch (error) {
      threw = 'input ' + i + ': ' + error;
      break;
    }
  }
  assert('malformed documents never throw' + (threw ? ' (' + threw + ')' : ''),
         threw === null);

  /* A lenient server may send a bare string where Siren specifies a list. */
  assert('a string rel is still matched',
         siren.follow({ links: [{ rel: 'self', href: '/' }] }, 'self') === '/');
  /* A link with no href cannot be followed, so it is not a link. */
  assert('a link without an href is not offered',
         siren.follow({ links: [{ rel: ['self'] }] }, 'self') === null);
}

function testSummary() {
  var text = siren.summary(DOC);
  assert('the summary counts what is there',
         /2 link\(s\)/.test(text) && /2 action\(s\)/.test(text) &&
         /2 entity\(ies\)/.test(text));
  /* Counts, not names: the summary has to stay useful against a server this
   * app has never seen. */
  assert('the summary names the class', /class \[home\]/.test(text));
}

testAccessors();
testLookup();
testMethod();
testReferences();
testLabel();
testVersionCheck();
testMalformed();
testSummary();

if (failures) {
  console.log(failures + ' failure(s)');
  process.exit(1);
}
console.log('siren: all checks passed');
