// Pure Dart unit tests: no WidgetTester, no device. Port of
// pebble/tools/test-render.js — see AGENTS.md section 5 ("Test style").
//
// The property worth testing here is negative: rendering must not
// interpret. A client that turns "ping: false" into "off", or hides a link
// it does not recognise, is making claims the server did not make — and the
// rendered screen is the whole output a person sees. So most of these
// assertions are that values survive untouched and that nothing is dropped.
//
// Vocabulary in the fixture below is deliberately made up (never a real
// API's), the same way DOC is in test-render.js.

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/siren.dart';
import 'package:restforge/services/render_service.dart';

final Entity doc = Entity.fromJson({
  'class': ['pantry'],
  'title': 'The pantry',
  'properties': {'kettle': 'cold', 'brews': 0, 'tidy': false, 'missing': null},
  'entities': [
    {
      'class': ['shelf'],
      'title': 'Top shelf',
      'properties': {'name': 'top', 'jars': 4, 'reachable': true},
    },
    {
      'class': ['shelf'],
      'title': 'Bottom shelf',
      'href': '/shelves/bottom',
    },
  ],
  'links': [
    {
      'rel': ['self'],
      'href': '/',
    },
    {
      'rel': ['shelves'],
      'href': '/shelves',
    },
  ],
  'actions': [
    {
      'name': 'brew',
      'title': 'Brew a pot of tea',
      'method': 'POST',
      'href': '/brew',
      'fields': [
        {'name': 'strength', 'type': 'range'},
      ],
    },
    {'name': 'peek', 'href': '/peek'},
  ],
});

List<RowKind> kindsOf(RenderedDocument page) => [for (final row in page.rows) row.kind];

Row rowNamed(RenderedDocument page, String label) =>
    page.rows.firstWhere((row) => row.label == label, orElse: () => throw StateError('no row "$label"'));

void main() {
  group('order and completeness', () {
    final page = document(doc);

    test('the title comes from the document', () {
      expect(page.title, 'The pantry');
    });

    test('rows are in document order', () {
      // Siren's own order: properties, entities, links, actions. Choosing a
      // different one would mean deciding which part of somebody else's
      // document matters most.
      expect(kindsOf(page), [
        RowKind.property,
        RowKind.property,
        RowKind.property,
        RowKind.property,
        RowKind.entity,
        RowKind.entity,
        RowKind.link,
        RowKind.link,
        RowKind.action,
        RowKind.action,
      ]);
    });

    test('nothing is dropped', () {
      expect(page.rows.length, 4 + doc.entities.length + doc.links.length + doc.actions.length);
    });

    test('the self link is offered like any other', () {
      // Including the ones a client might be tempted to hide.
      expect(() => rowNamed(page, 'self'), returnsNormally);
    });
  });

  group('values are not interpreted', () {
    final page = document(doc);

    test('a false boolean stays false', () {
      expect(rowNamed(page, 'tidy').sublabel, 'false');
    });

    test('a zero stays zero', () {
      expect(rowNamed(page, 'brews').sublabel, '0');
    });

    test('a null stays null', () {
      // null is a value the server chose to send, and it is not the same as
      // a property being absent.
      expect(rowNamed(page, 'missing').sublabel, 'null');
    });

    test('a string is passed through', () {
      expect(rowNamed(page, 'kettle').sublabel, 'cold');
    });
  });

  group('targets', () {
    final page = document(doc);

    test('a property opens the reading window', () {
      final property = rowNamed(page, 'kettle').target;
      expect(property, isA<DetailTarget>());
      expect((property as DetailTarget).body, 'cold');
    });

    test('an embedded entity is opened locally', () {
      // Already in hand: opening it asks the server nothing, and asking
      // again could legitimately return something different.
      final embedded = rowNamed(page, 'Top shelf').target;
      expect(embedded, isA<EmbeddedTarget>());
      expect((embedded as EmbeddedTarget).index, 0);
    });

    test('an embedded entity is summarised', () {
      expect(rowNamed(page, 'Top shelf').sublabel, 'name top  jars 4');
    });

    test('a referenced entity is fetched', () {
      final reference = rowNamed(page, 'Bottom shelf').target;
      expect(reference, isA<FetchTarget>());
      expect((reference as FetchTarget).href, '/shelves/bottom');
    });

    test('a link is fetched by its href', () {
      final link = rowNamed(page, 'shelves').target;
      expect(link, isA<FetchTarget>());
      expect((link as FetchTarget).href, '/shelves');
    });
  });

  group('actions', () {
    final page = document(doc);

    test('an action shows its title and is addressed by name', () {
      // The title is the server's sentence for a person; the name is an
      // identifier. The sentence wins.
      final titled = rowNamed(page, 'Brew a pot of tea');
      expect(titled.target, isA<ActionTarget>());
      expect((titled.target as ActionTarget).name, 'brew');
    });

    test('an action shows its method and field count', () {
      expect(rowNamed(page, 'Brew a pot of tea').sublabel, 'POST, 1 field(s)');
    });

    test('an action without a title falls back to its name', () {
      expect(() => rowNamed(page, 'peek'), returnsNormally);
    });

    test('an action without a method defaults to GET', () {
      // Siren's default method, for an action that does not say.
      expect(rowNamed(page, 'peek').sublabel, 'GET');
    });
  });

  group('entities sharing a class', () {
    // Regression: entities distinguished only by a property.
    final hosts = Entity.fromJson({
      'class': ['status'],
      'entities': [
        {
          'class': ['host', 'f'],
          'properties': {'ip': '10.0.0.1', 'ms': 15, 'name': 'f0'},
        },
        {
          'class': ['host', 'f'],
          'properties': {'ip': '10.0.0.2', 'ms': 24, 'name': 'f1'},
        },
      ],
    });
    final page = document(hosts);

    test('entities sharing a class are told apart', () {
      expect(page.rows[0].label, 'f0');
      expect(page.rows[1].label, 'f1');
    });

    test('the summary does not repeat the label', () {
      // The label is not repeated below itself: on this screen the summary
      // has room for two facts and one of them must not be the heading
      // again.
      expect(page.rows[0].sublabel, 'ip 10.0.0.1  ms 15');
    });
  });

  group('unfamiliar and malformed documents', () {
    test('an unfamiliar document still renders completely', () {
      // A document made entirely of things this app has never seen. It must
      // render completely, because "I do not recognise this" is not a
      // reason to withhold it from the person using it.
      final alien = Entity.fromJson({
        'class': ['quux'],
        'properties': {
          'zork': {
            'nested': [1, 2],
          },
        },
        'links': [
          {
            'rel': ['frobnicate'],
            'href': '/f',
          },
        ],
        'actions': [
          {'name': 'gorp', 'method': 'DELETE', 'href': '/g'},
        ],
      });
      final page = document(alien, 'fallback');
      expect(page.rows.length, 3);
      expect(rowNamed(page, 'zork').sublabel, '{"nested":[1,2]}');
      expect(page.title, 'quux');
    });

    test('an empty document renders no rows and uses the fallback title', () {
      final empty = document(Entity(), 'fallback');
      expect(empty.rows, isEmpty);
      expect(empty.title, 'fallback');
    });

    test('a missing document does not throw', () {
      final missing = document(null, 'fallback');
      expect(missing.rows, isEmpty);
      expect(missing.title, 'fallback');
    });
  });
}
