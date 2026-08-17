// Pure Dart unit tests: no WidgetTester, no device. See AGENTS.md section 5
// ("Test style"). Ported from pebble/tools/test-url.js, which checks
// pebble/src/pkjs/url.js against the RFC 3986 section 5.4 reference test
// vectors -- the specification ships its own test suite, so this file uses
// it rather than inventing cases. Every vector is kept even though Dart's
// Uri.resolve gets each one right on the first try: they are the
// specification, not scaffolding that became unnecessary once Uri existed.

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/services/url_resolver.dart' as url;

const base = 'http://a/b/c/d;p?q';

void main() {
  group('resolve: RFC 3986 5.4.1 normal examples', () {
    const vectors = {
      'g:h': 'g:h',
      'g': 'http://a/b/c/g',
      './g': 'http://a/b/c/g',
      'g/': 'http://a/b/c/g/',
      '/g': 'http://a/g',
      '//g': 'http://g',
      '?y': 'http://a/b/c/d;p?y',
      'g?y': 'http://a/b/c/g?y',
      '#s': 'http://a/b/c/d;p?q#s',
      'g#s': 'http://a/b/c/g#s',
      'g?y#s': 'http://a/b/c/g?y#s',
      ';x': 'http://a/b/c/;x',
      'g;x': 'http://a/b/c/g;x',
      'g;x?y#s': 'http://a/b/c/g;x?y#s',
      '': 'http://a/b/c/d;p?q',
      '.': 'http://a/b/c/',
      './': 'http://a/b/c/',
      '..': 'http://a/b/',
      '../': 'http://a/b/',
      '../g': 'http://a/b/g',
      '../..': 'http://a/',
      '../../': 'http://a/',
      '../../g': 'http://a/g',
    };

    vectors.forEach((href, want) {
      test('"$href"', () {
        expect(url.resolve(href, base), want);
      });
    });
  });

  group('resolve: RFC 3986 5.4.2 abnormal examples', () {
    // These are the ones a naive split/join implementation gets wrong.
    const vectors = {
      '../../../g': 'http://a/g',
      '../../../../g': 'http://a/g',
      '/./g': 'http://a/g',
      '/../g': 'http://a/g',
      'g.': 'http://a/b/c/g.',
      '.g': 'http://a/b/c/.g',
      'g..': 'http://a/b/c/g..',
      '..g': 'http://a/b/c/..g',
      './../g': 'http://a/b/g',
      './g/.': 'http://a/b/c/g/',
      'g/./h': 'http://a/b/c/g/h',
      'g/../h': 'http://a/b/c/h',
      'g;x=1/./y': 'http://a/b/c/g;x=1/y',
      'g;x=1/../y': 'http://a/b/c/y',
      // Dot segments inside a query or fragment are data, not path syntax.
      'g?y/./x': 'http://a/b/c/g?y/./x',
      'g?y/../x': 'http://a/b/c/g?y/../x',
      'g#s/./x': 'http://a/b/c/g#s/./x',
      'g#s/../x': 'http://a/b/c/g#s/../x',
    };

    vectors.forEach((href, want) {
      test('"$href"', () {
        expect(url.resolve(href, base), want);
      });
    });
  });

  group('resolve: the shapes this app actually meets', () {
    const realBase = 'https://host.example.org/cgi-bin/app/';

    test('a root-relative href keeps the configured origin', () {
      expect(url.resolve('/cgi-bin/app/status', realBase),
          'https://host.example.org/cgi-bin/app/status');
    });

    test('a relative href resolves under the base', () {
      expect(url.resolve('status', realBase),
          'https://host.example.org/cgi-bin/app/status');
    });

    test('a base without a trailing slash loses its last segment', () {
      // The reason settings.js appends a trailing slash: without one the
      // base's last segment is discarded and every href lands one level
      // too high.
      expect(url.resolve('status', 'https://host.example.org/cgi-bin/app'),
          'https://host.example.org/cgi-bin/status');
    });

    test('an absolute href overrides the base entirely', () {
      expect(url.resolve('https://other.example.org/x', realBase),
          'https://other.example.org/x');
    });
  });

  group('origin and path helpers', () {
    test('origin strips the path', () {
      expect(url.origin('https://host.example.org/a/b?c=d'),
          'https://host.example.org');
    });

    test('path keeps the query', () {
      expect(url.path('https://host.example.org/a/b?c=d'), '/a/b?c=d');
    });

    test('a non-absolute URL has no origin', () {
      expect(url.origin('/a/b'), '');
    });
  });

  group('encodeForm', () {
    test('uses + for spaces', () {
      expect(url.encodeForm({'note': 'a b'}), 'note=a+b');
    });

    test('escapes separators', () {
      expect(url.encodeForm({'a&b': 'c=d'}), 'a%26b=c%3Dd');
    });

    test('stringifies booleans', () {
      expect(url.encodeForm({'force': true}), 'force=true');
    });

    test('an empty field set is an empty body', () {
      expect(url.encodeForm({}), '');
    });
  });

  group('inputs Uri.parse rejects (beyond the ported suite)', () {
    // url.js's regex-based parser accepts anything, so it never throws.
    // Uri.parse is stricter about what an RFC 3986 URI reference looks
    // like and does throw FormatException on some inputs -- see the
    // doc comment on url_resolver.dart. These cases are not in
    // pebble/tools/test-url.js because that gap does not exist in
    // JavaScript; they exist here to pin the fallback behaviour that
    // closes it, so a malformed base (a typo'd port in a hand-typed
    // settings field) or a malformed href (a hostile or buggy server)
    // degrades instead of crashing the caller.
    test('a base with an unparsable port falls back to the href', () {
      expect(url.resolve('status', 'http://host:abc/path'), 'status');
    });

    test('an unparsable url has no origin', () {
      expect(url.origin('://bad'), '');
    });

    test('an unparsable url falls back to itself for path', () {
      expect(url.path('://bad'), '://bad');
    });
  });
}
