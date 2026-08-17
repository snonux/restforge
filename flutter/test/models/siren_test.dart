// Pure Dart unit tests: no WidgetTester, no device. Port of
// pebble/tools/test-siren.js — see AGENTS.md section 5 ("Test style").
//
// The rule these tests exist to hold is that an unknown class, rel, action
// name or field type is ordinary, not an error. A hypermedia client that
// throws on something it has not seen before has to be updated every time
// the server grows a feature, which is the failure the design is meant to
// avoid. So most of what follows is malformed or unfamiliar input that must
// come back as "not offered" (or a renderable [Failure]) rather than as an
// exception.
//
// Sample rels, classes, action names and property names below are
// deliberately made up (never a real API's vocabulary), the same way the
// fixture DOC in test-siren.js is.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/models/siren.dart';

final Map<String, dynamic> docJson = {
  'class': ['home'],
  'title': 'Example service',
  'properties': {'apiVersion': 1, 'version': 'v1.2.3'},
  'entities': [
    {
      'class': ['widget'],
      'properties': {'name': 'a'},
    },
    {
      'class': ['widget'],
      'href': '/widgets/b',
    },
  ],
  'links': [
    {
      'rel': ['self'],
      'href': '/',
    },
    {
      'rel': ['collection', 'widgets'],
      'href': '/widgets',
    },
  ],
  'actions': [
    {
      'name': 'restart',
      'method': 'POST',
      'href': '/restart',
      'title': 'Restart the service',
      'fields': [
        {'name': 'force', 'type': 'checkbox', 'title': 'Really?'},
      ],
    },
    {'name': 'peek', 'href': '/peek'},
  ],
};

void main() {
  final doc = Entity.fromJson(docJson);

  group('accessors', () {
    test('properties are returned', () {
      expect(doc.properties['version'], 'v1.2.3');
    });

    test('classes are returned', () {
      expect(doc.classes.first, 'home');
    });

    test('links are returned', () {
      expect(doc.links.length, 2);
    });

    test('actions are returned', () {
      expect(doc.actions.length, 2);
    });

    test('sub-entities are returned', () {
      expect(doc.entities.length, 2);
    });
  });

  group('lookup', () {
    test('a link is found by its first rel', () {
      expect(doc.follow('self'), '/');
    });

    test('a link is found by a later rel', () {
      // Siren's rel is a list; a link is found by any of its rels, not
      // just the first, because which one a server puts first is not
      // part of the contract.
      expect(doc.follow('widgets'), '/widgets');
    });

    test('an absent rel is null, not an error', () {
      expect(doc.follow('nothing-like-this'), isNull);
    });

    test('an action is found by name', () {
      expect(doc.actionByName('restart')?.href, '/restart');
    });

    test('an absent action is null, not an error', () {
      expect(doc.actionByName('never-offered'), isNull);
    });

    test('fields come back as a list', () {
      expect(doc.actionByName('restart')?.fields.length, 1);
    });

    test('an action without fields has none', () {
      expect(doc.actionByName('peek')?.fields.length, 0);
    });
  });

  group('method', () {
    test('a declared method is used', () {
      expect(doc.actionByName('restart')?.method, 'POST');
    });

    test('an undeclared method defaults to GET', () {
      // Siren's default. A server that means to change something says so.
      expect(doc.actionByName('peek')?.method, 'GET');
    });
  });

  group('references', () {
    test('an embedded entity is not a reference', () {
      expect(doc.entities[0].isReference, isFalse);
    });

    test('a bare href is a reference', () {
      expect(doc.entities[1].isReference, isTrue);
    });
  });

  group('label', () {
    test('a title is preferred', () {
      expect(doc.label, 'Example service');
    });

    test('a name is next', () {
      expect(Action.fromJson({'name': 'restart'}).label, 'restart');
    });

    test('an identifying property beats the class', () {
      // Found against a real API: entities sharing one class rendered as
      // identical rows, because the only thing distinguishing them was a
      // property. A list where every row reads the same is not a list.
      final entity = Entity.fromJson({
        'class': ['host', 'f'],
        'properties': {'address': '10.0.0.1', 'name': 'f0'},
      });
      expect(entity.label, 'f0');
    });

    test('a numeric identifier counts', () {
      final entity = Entity.fromJson({
        'class': ['ticket'],
        'properties': {'id': 7},
      });
      expect(entity.label, '7');
    });

    test('the identifying key is reported', () {
      final entity = Entity.fromJson({
        'properties': {'address': 'x', 'name': 'f0'},
      });
      expect(entity.identifier, 'name');
    });

    test('an entity with no identifier reports none', () {
      final entity = Entity.fromJson({
        'properties': {'address': 'x'},
      });
      expect(entity.identifier, isNull);
    });

    test('a title still wins over a property', () {
      final entity = Entity.fromJson({
        'title': 'Top shelf',
        'properties': {'name': 'top'},
      });
      expect(entity.label, 'Top shelf');
    });

    test('a class is next', () {
      final entity = Entity.fromJson({
        'class': ['widget', 'large'],
      });
      expect(entity.label, 'widget large');
    });

    test('a rel is last', () {
      expect(
        Link.fromJson({
          'rel': ['collection'],
        }).label,
        'collection',
      );
    });

    // Never invent a name: an invented name is a claim about what the
    // thing is, and this client has no basis for one.
    test('nothing recognisable yields nothing', () {
      expect(Entity.fromJson(<String, dynamic>{}).label, '');
    });

    test('a missing node yields nothing', () {
      expect(Entity.fromJson(null).label, '');
    });
  });

  group('version check', () {
    test('a supported version proceeds', () {
      final entity = Entity.fromJson({
        'properties': {'apiVersion': 1},
      });
      expect(entity.versionProblem, isNull);
    });

    test('an older version proceeds', () {
      final entity = Entity.fromJson({
        'properties': {'apiVersion': 0},
      });
      expect(entity.versionProblem, isNull);
    });

    test('a newer version stops', () {
      final entity = Entity.fromJson({
        'properties': {'apiVersion': 2},
      });
      expect(entity.versionProblem, contains('apiVersion 2'));
    });

    test('an absent version proceeds', () {
      // A server that declares nothing is making no claim this client can
      // act on, and refusing to proceed would break it against every
      // server that never had the field.
      final entity = Entity.fromJson({'properties': <String, dynamic>{}});
      expect(entity.versionProblem, isNull);
    });

    test('a non-numeric version proceeds', () {
      final entity = Entity.fromJson({
        'properties': {'apiVersion': 'one'},
      });
      expect(entity.versionProblem, isNull);
    });
  });

  // The important half: nothing below is well-formed, and none of it may
  // throw.
  group('malformed input', () {
    final junk = <dynamic>[
      null,
      <String, dynamic>{},
      <dynamic>[],
      'a string',
      42,
      {
        'links': 'not a list',
        'actions': null,
        'entities': 7,
        'properties': <dynamic>[],
      },
      {
        'links': [
          {'rel': 'self', 'href': '/'},
        ],
      },
      {
        'links': [
          {
            'rel': ['self'],
          },
        ],
      },
      {
        'actions': [<String, dynamic>{}],
      },
    ];

    test('malformed documents never throw', () {
      for (final input in junk) {
        expect(() {
          final entity = Entity.fromJson(input);
          entity.classes;
          entity.properties;
          entity.links;
          entity.actions;
          entity.entities;
          entity.follow('self');
          entity.actionByName('restart');
          entity.label;
          entity.versionProblem;
          entity.summary;
        }, returnsNormally);
      }
    });

    test('a string rel is still matched', () {
      // A lenient server may send a bare string where Siren specifies a
      // list.
      final entity = Entity.fromJson({
        'links': [
          {'rel': 'self', 'href': '/'},
        ],
      });
      expect(entity.follow('self'), '/');
    });

    test('a link without an href is not offered', () {
      // A link with no href cannot be followed, so it is not a link.
      final entity = Entity.fromJson({
        'links': [
          {
            'rel': ['self'],
          },
        ],
      });
      expect(entity.follow('self'), isNull);
    });
  });

  group('summary', () {
    test('the summary counts what is there', () {
      final text = doc.summary;
      expect(text, contains('2 link(s)'));
      expect(text, contains('2 action(s)'));
      expect(text, contains('2 entity(ies)'));
    });

    test('the summary names the class', () {
      // Counts, not names: the summary has to stay useful against a
      // server this client has never seen. The class is the one exception
      // — it is shown, not interpreted.
      expect(doc.summary, contains('class [home]'));
    });
  });

  // parseSirenDocument is new relative to siren.js: siren.js is only ever
  // handed an already-decoded object (http.js owns JSON.parse). Here the
  // JSON-text boundary lives in this model instead, so its "never throw"
  // guarantee is tested at that boundary too.
  group('parseSirenDocument', () {
    test('a well-formed document parses to Ok', () {
      final result = parseSirenDocument(jsonEncode(docJson));

      expect(result, isA<Ok<Entity>>());
      final entity = (result as Ok<Entity>).value;
      expect(entity.label, 'Example service');
      expect(entity.links.length, 2);
    });

    test('invalid JSON syntax is a renderable failure, not a throw', () {
      expect(
        () => parseSirenDocument('{not valid json'),
        returnsNormally,
      );

      final result = parseSirenDocument('{not valid json');
      expect(result, isA<Err<Entity>>());
      expect((result as Err<Entity>).failure.kind, FailureKind.parse);
    });

    test('a JSON value that is not an object is a renderable failure', () {
      for (final body in ['[1,2,3]', '"just a string"', '42', 'null']) {
        final result = parseSirenDocument(body);
        expect(result, isA<Err<Entity>>());
        expect((result as Err<Entity>).failure.kind, FailureKind.parse);
      }
    });

    test('garbage bytes never throw', () {
      expect(
        () => parseSirenDocument('not json at all, just noise {{{'),
        returnsNormally,
      );
    });
  });
}
