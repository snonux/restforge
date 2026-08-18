// Pure Dart unit tests: no WidgetTester, no device, no socket. See
// AGENTS.md section 5 ("Test style"). Ported from the navigation-stack,
// fetch and four-state cases in pebble/tools/test-nav.js — the cases that
// belong to nav_service.dart's scope, see that file's module comment for
// what does not carry over.
//
// Matched from test-nav.js: testOpenBackendAndFetch (openRoot fetches the
// base URL; a link row can be followed), testOpenEmbeddedAndBack (an
// embedded entity opens without a request; back returns to the previous
// document), testRefreshAndEmbeddedHasNoAddress (refresh re-fetches; an
// embedded document is re-rendered instead), testFailureKeepsTheDocument
// (the invariant this module exists to protect), testUnreachableMapping
// (status 0 / a connection failure is unreachable, not error),
// testSwitchingBackendsResetsTheStack. The idle-refresh cases
// (testIdleRefreshFires, testIdleRefreshSuppressedByActionPendingHook,
// testIdleRefreshFailureDoesNotOpenOverlay) moved to
// idle_refresh_clock_test.dart with task a21, when the clock was extracted
// from NavService into its own collaborator (idle_refresh_clock.dart).
//
// Deliberately NOT ported: testPickerFrame (the backend picker is
// home_screen.dart's job here, not nav_service.dart's — see the module
// comment), testIdleRefreshSuppressedByOverlay and testSetSeqClearsOverlay
// (the watch's overlay/seq protocol has no equivalent on this port — see
// nav_service.dart's module comment on why an outstanding action question
// is a single hook here rather than nav.js's two flags; the gap those two
// tests exist to pin, a dismissed-but-unanswered confirmation still
// counting as pending, is action_service.dart's `hasPending` behaviour, and
// is covered by that file's own tests instead).
//
// Added beyond test-nav.js, because this module's own comment calls them
// out as things it owns: following startRel after the root (untested in
// test-nav.js itself — see nav.js's followStart), the root's apiVersion
// check, and the state sequence a fetch goes through (loading, then a
// terminal state) with the document left untouched throughout — this is
// where this port deliberately does more than nav.js can, see the module
// comment on why. The idle-refresh cases with no pebble/tools/test-nav.js
// counterpart at all (the app leaving and returning to the foreground —
// this port's own, Pebble-less, second suppression condition — and an
// embedded document with no address holding the clock off) live in
// idle_refresh_clock_test.dart now, alongside the rest of the idle clock
// tests, since task a21 extracted the clock out of this module.

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/nav_service.dart';
import 'package:restforge/services/render_service.dart';
import 'package:restforge/services/settings_service.dart';

const String base = 'https://pantry.example/';

final Backend backend = Backend(
  name: 'pantry',
  baseUrl: base,
  secret: 'open-sesame',
);

final Map<String, dynamic> root = {
  'class': ['pantry'],
  'title': 'The pantry',
  'properties': {'apiVersion': 1, 'kettle': 'cold'},
  'entities': [
    {
      'class': ['shelf'],
      'title': 'Top shelf',
      'properties': {'name': 'top', 'jars': 4},
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
  'actions': [],
};

final Map<String, dynamic> shelves = {
  'class': ['shelf-list'],
  'title': 'Shelves',
  'properties': {'count': 1},
  'links': [
    {
      'rel': ['self'],
      'href': '/shelves',
    },
  ],
};

http.Response jsonResponse(Object body, {int status = 200}) => http.Response(
  jsonEncode(body),
  status,
  headers: {'content-type': 'application/json'},
);

/// A fake backend, driven the same way test-nav.js's FakeXHR is: a route
/// table keyed by the exact URL requested, a log of what was requested, and
/// a couple of hand-toggled switches ([unreachable], [gate]) for the
/// failure and mid-flight cases those tests exist to cover.
class Env {
  Env() {
    final client = MockClient((request) async {
      requested.add('${request.method} ${request.url}');
      if (unreachable) {
        throw Exception('connection refused');
      }
      if (gate != null) {
        await gate!.future;
      }
      final body = routes[request.url.toString()];
      if (body == null) {
        return jsonResponse({
          'properties': {'message': 'no such thing here'},
        }, status: 404);
      }
      return jsonResponse(body);
    });
    nav = NavService(
      http: HttpService(client: client, log: (_) {}),
    );
  }

  final Map<String, Object> routes = {base: root, '${base}shelves': shelves};
  final List<String> requested = [];

  /// Set true to make every request behave like a dropped connection
  /// instead of consulting [routes] — mirrors test-nav.js swapping
  /// `FakeXHR.prototype.send`.
  bool unreachable = false;

  /// When set, every request waits on this before answering — lets a test
  /// inspect [NavService] state while a fetch is still in flight.
  Completer<void>? gate;

  late final NavService nav;

  void reset() => requested.clear();
}

Row rowNamed(RenderedDocument page, String label) => page.rows.firstWhere(
  (row) => row.label == label,
  orElse: () => throw StateError('no row "$label"'),
);

void main() {
  group('opening a backend', () {
    test('fetches the base URL and shows the document', () async {
      final env = Env();

      await env.nav.openRoot(backend);

      expect(env.requested, ['GET $base']);
      expect(env.nav.document?.title, 'The pantry');
      expect(env.nav.state, DocumentState.ok);
      expect(env.nav.backend, backend);
    });

    test('a link row can be followed', () async {
      final env = Env();
      await env.nav.openRoot(backend);
      final target =
          rowNamed(env.nav.document!, 'shelves').target as FetchTarget;
      env.reset();

      await env.nav.fetch(target.href, title: 'shelves');

      expect(env.requested, ['GET ${base}shelves']);
      expect(env.nav.document?.title, 'Shelves');
    });

    test('a root speaking an unsupported apiVersion is not pushed', () async {
      final env = Env();
      env.routes[base] = {
        'class': ['pantry'],
        'properties': {'apiVersion': 99},
        'links': [
          {
            'rel': ['self'],
            'href': '/',
          },
        ],
      };

      await env.nav.openRoot(backend);

      expect(env.nav.state, DocumentState.error);
      expect(env.nav.failure?.kind, FailureKind.client);
      expect(env.nav.document, isNull, reason: 'nothing was pushed');
    });
  });

  group('startRel', () {
    test(
      'is followed after the root, pushing a second document on top of it',
      () async {
        final env = Env();
        final startBackend = Backend(
          name: 'pantry',
          baseUrl: base,
          secret: 'open-sesame',
          startRel: 'shelves',
        );

        await env.nav.openRoot(startBackend);

        expect(env.requested, ['GET $base', 'GET ${base}shelves']);
        expect(env.nav.document?.title, 'Shelves');
        expect(
          env.nav.canGoBack,
          isTrue,
          reason: 'the root is still underneath it on the stack',
        );
      },
    );

    test('a rel the root does not offer just leaves the root open', () async {
      final env = Env();
      final startBackend = Backend(
        name: 'pantry',
        baseUrl: base,
        secret: 'open-sesame',
        startRel: 'no-such-rel',
      );

      await env.nav.openRoot(startBackend);

      expect(env.requested, ['GET $base']);
      expect(env.nav.document?.title, 'The pantry');
      expect(env.nav.state, DocumentState.ok);
    });
  });

  group('an embedded entity', () {
    test(
      'opens without a request, and back returns to the previous document',
      () async {
        final env = Env();
        await env.nav.openRoot(backend);
        final target =
            rowNamed(env.nav.document!, 'Top shelf').target as EmbeddedTarget;
        env.reset();

        env.nav.openEmbedded(target.index);
        expect(env.requested, isEmpty);
        expect(env.nav.document?.title, 'Top shelf');
        expect(env.nav.canGoBack, isTrue);

        env.nav.back();
        expect(env.nav.document?.title, 'The pantry');
        expect(env.nav.canGoBack, isFalse);
      },
    );

    test('cannot be re-fetched; refresh re-renders it instead', () async {
      final env = Env();
      await env.nav.openRoot(backend);
      final target =
          rowNamed(env.nav.document!, 'Top shelf').target as EmbeddedTarget;
      env.nav.openEmbedded(target.index);
      env.reset();

      await env.nav.refresh();

      expect(
        env.requested,
        isEmpty,
        reason: 'an embedded document has no address to refresh from',
      );
      expect(env.nav.document?.title, 'Top shelf');
    });
  });

  test('back at the root is a no-op', () async {
    final env = Env();
    await env.nav.openRoot(backend);

    env.nav.back();

    expect(env.nav.document?.title, 'The pantry');
  });

  test('refresh re-fetches the current document', () async {
    final env = Env();
    await env.nav.openRoot(backend);
    env.reset();

    await env.nav.refresh();

    expect(env.requested, ['GET $base']);
    expect(env.nav.state, DocumentState.ok);
  });

  group('opening a different backend', () {
    test('discards the old stack', () async {
      final env = Env();
      await env.nav.openRoot(backend);
      final target =
          rowNamed(env.nav.document!, 'shelves').target as FetchTarget;
      await env.nav.fetch(target.href, title: 'shelves');
      expect(env.nav.canGoBack, isTrue);

      const otherBase = 'https://other.example/';
      env.routes[otherBase] = {
        'title': 'Other root',
        'links': [
          {
            'rel': ['self'],
            'href': '/',
          },
        ],
      };
      final other = Backend(name: 'other', baseUrl: otherBase, secret: 'x');
      env.reset();

      await env.nav.openRoot(other);

      expect(env.nav.canGoBack, isFalse);
      expect(env.nav.document?.title, 'Other root');
      expect(env.nav.backend, other);
    });
  });

  group('adopt', () {
    // adopt() exists for exactly one caller, session.dart's runQuick (task
    // x11) -- see this method's own doc comment for why a saved shortcut
    // must not pay for the root fetch openRoot() always makes.
    test(
      'switches backend without fetching, discarding the old stack',
      () async {
        final env = Env();
        await env.nav.openRoot(backend);
        final target =
            rowNamed(env.nav.document!, 'shelves').target as FetchTarget;
        await env.nav.fetch(target.href, title: 'shelves');
        expect(env.nav.canGoBack, isTrue);
        env.reset();

        const otherBase = 'https://other.example/';
        final other = Backend(name: 'other', baseUrl: otherBase, secret: 'x');

        env.nav.adopt(other);

        expect(env.requested, isEmpty, reason: 'adopt fetches nothing');
        expect(env.nav.backend, other);
        expect(env.nav.document, isNull, reason: 'the old stack is gone');
        expect(env.nav.canGoBack, isFalse);
        expect(env.nav.state, DocumentState.ok);
      },
    );

    test('a fetch after adopt pushes onto a fresh, one-frame stack', () async {
      final env = Env();
      final other = Backend(
        name: 'other',
        baseUrl: 'https://other.example/',
        secret: 'x',
      );
      env.routes['https://other.example/somewhere'] = {
        'title': 'Elsewhere',
        'links': [
          {
            'rel': ['self'],
            'href': '/somewhere',
          },
        ],
      };

      env.nav.adopt(other);
      await env.nav.fetch('https://other.example/somewhere', title: 'x');

      expect(env.nav.document?.title, 'Elsewhere');
      expect(
        env.nav.canGoBack,
        isFalse,
        reason:
            'straight to the destination: nothing was walked to get here, '
            'so there is no history to pop back through',
      );
    });
  });

  group('a failed request', () {
    // This is the invariant pebble/docs/DESIGN.md calls "A failed request is
    // not an answer": the document already on screen must survive exactly
    // as it was, with the reason layered on top of it, never replaced by an
    // empty one.
    test('never replaces the document on screen with an empty one', () async {
      final env = Env();
      await env.nav.openRoot(backend);
      final before = env.nav.document!;
      final beforeLabels = [for (final row in before.rows) row.label];
      env.routes.clear();
      env.reset();

      await env.nav.refresh();

      final after = env.nav.document!;
      expect(
        [for (final row in after.rows) row.label],
        beforeLabels,
        reason: 'the last good document is untouched by a failure',
      );
      expect(after.title, before.title);
      expect(env.nav.state, DocumentState.error);
      expect(
        env.nav.failure?.message,
        'no such thing here',
        reason: "the server's own wording is readable",
      );
    });

    test(
      'a connection failure maps to unreachable, not error, and still keeps the document',
      () async {
        final env = Env();
        await env.nav.openRoot(backend);
        final beforeTitle = env.nav.document!.title;
        env.unreachable = true;
        env.reset();

        await env.nav.refresh();

        expect(env.nav.state, DocumentState.unreachable);
        expect(env.nav.document?.title, beforeTitle);
      },
    );
  });

  group('state transitions', () {
    test('a fetch goes through loading on its way to ok', () async {
      final env = Env();
      final states = <DocumentState>[];
      env.nav.addListener(() => states.add(env.nav.state));

      await env.nav.openRoot(backend);

      expect(states.first, DocumentState.loading);
      expect(states.last, DocumentState.ok);
    });

    test(
      'the document is untouched while a fetch is still in flight',
      () async {
        final env = Env();
        await env.nav.openRoot(backend);
        final beforeTitle = env.nav.document!.title;
        env.gate = Completer<void>();

        final inFlight = env.nav.refresh();
        expect(env.nav.state, DocumentState.loading);
        expect(
          env.nav.document?.title,
          beforeTitle,
          reason: 'a loading fetch must not blank the screen while it runs',
        );

        env.gate!.complete();
        await inFlight;
        expect(env.nav.state, DocumentState.ok);
      },
    );
  });

  group('dispose during an in-flight fetch (821)', () {
    // A fetch still in flight when the user backs away from the document (so
    // the NavService is disposed) must not notifyListeners on a disposed
    // ChangeNotifier — that trips its "used after dispose" assert in debug
    // and mutates a discarded service in release. The post-await guard in
    // openRoot/_fetch bails the in-flight op cleanly, and the notifyListeners
    // override is the backstop. Without either, completing the fetch here
    // would throw and fail the `await`.

    test(
      'an in-flight root fetch landing after dispose does not throw',
      () async {
        final env = Env();
        env.gate = Completer<void>();
        final open = env.nav.openRoot(backend);
        // The root fetch is held at the gate; dispose while it is in flight
        // (the user backed away before it landed).
        env.nav.dispose();
        env.gate!.complete();
        // Completing the fetch continues openRoot on a disposed NavService;
        // the guard returns before it can push/notify. No throw.
        await open;
      },
    );

    test('an in-flight refresh landing after dispose does not throw', () async {
      final env = Env();
      await env.nav.openRoot(backend);
      env.gate = Completer<void>();
      final refresh = env.nav.refresh();
      env.nav.dispose();
      env.gate!.complete();
      await refresh;
    });
  });
}
