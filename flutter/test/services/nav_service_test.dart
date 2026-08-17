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
// testSwitchingBackendsResetsTheStack, testIdleRefreshFires,
// testIdleRefreshSuppressedByActionPendingHook, and (translated —
// see below) testIdleRefreshFailureDoesNotOpenOverlay.
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
// comment on why. The "idle refresh" group adds two cases with no
// pebble/tools/test-nav.js counterpart at all: the app leaving and
// returning to the foreground (this port's own, Pebble-less, second
// suppression condition), and an embedded document (no address to refresh)
// holding the clock off, which test-nav.js's fixtures never happen to
// exercise for idle refresh specifically.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart' show AppLifecycleState;
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

/// A fake, wall-clock-free timer, driven by [tick] — the Dart equivalent of
/// test-nav.js's fake `setTimeout`/`tick()` (see the comment at the top of
/// that file). Handed to [NavService] as `createTimer` by [Env] when a test
/// passes one in, so [idleRefreshInterval] is driven without an actual
/// 60-second wait.
class FakeTimers {
  Duration _elapsed = Duration.zero;
  final List<_FakeTimer> _pending = [];

  Timer createTimer(Duration duration, void Function() callback) {
    final timer = _FakeTimer(this, _elapsed + duration, callback);
    _pending.add(timer);
    return timer;
  }

  void _remove(_FakeTimer timer) => _pending.remove(timer);

  /// Advances the clock by [duration], firing every timer due at or before
  /// the new time, earliest first. A fired callback may reschedule itself —
  /// the idle refresh rearming its own timer once it settles, via
  /// [NavService]'s own `_notify` — so the pending list is re-scanned after
  /// each firing rather than snapshotted once, same as test-nav.js's
  /// `tick()`. The short real delay after each firing lets the fetch a fired
  /// timer starts (answered by the in-memory [MockClient] in [Env], not a
  /// real socket) actually resolve before the next timer is chosen.
  Future<void> tick(Duration duration) async {
    final until = _elapsed + duration;
    for (;;) {
      _FakeTimer? next;
      for (final timer in _pending) {
        if (timer.fireAt <= until &&
            (next == null || timer.fireAt < next.fireAt)) {
          next = timer;
        }
      }
      if (next == null) break;
      _elapsed = next.fireAt;
      _pending.remove(next);
      next.callback();
      for (var i = 0; i < 5; i++) {
        await Future<void>.delayed(Duration.zero);
      }
    }
    _elapsed = until;
  }
}

class _FakeTimer implements Timer {
  _FakeTimer(this._clock, this.fireAt, this.callback);

  final FakeTimers _clock;
  final Duration fireAt;
  final void Function() callback;
  bool _active = true;

  @override
  void cancel() {
    if (_active) {
      _active = false;
      _clock._remove(this);
    }
  }

  @override
  bool get isActive => _active;

  // A one-shot timer never repeats, so it fires at most once — mirrors
  // dart:async's own one-shot Timer, whose `tick` is 0 before firing and 1
  // after. Nothing in NavService reads this; it exists only to satisfy the
  // Timer interface.
  @override
  int get tick => _active ? 0 : 1;
}

/// A fake backend, driven the same way test-nav.js's FakeXHR is: a route
/// table keyed by the exact URL requested, a log of what was requested, and
/// a couple of hand-toggled switches ([unreachable], [gate]) for the
/// failure and mid-flight cases those tests exist to cover.
class Env {
  Env({FakeTimers? timers}) {
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
      createTimer: timers?.createTimer,
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
    test('switches backend without fetching, discarding the old stack', () async {
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
    });

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

  group('idle refresh', () {
    // The idle clock re-reads the document on top of the stack every
    // idleRefreshInterval when nothing else is going on — see
    // nav_service.dart's module comment for the two things that hold it off
    // and why a background refresh's failure is already covered by the same
    // invariant every other fetch is.
    test(
      're-reads the document on top of the stack after the idle interval',
      () async {
        final timers = FakeTimers();
        final env = Env(timers: timers);
        await env.nav.openRoot(backend);
        env.reset();

        await timers.tick(idleRefreshInterval);

        expect(env.requested, [
          'GET $base',
        ], reason: 'the visible document is re-read when idle');
      },
    );

    test(
      'a pending-action hook suppresses the refresh; the unwired default does not',
      () async {
        final timers = FakeTimers();
        final env = Env(timers: timers);
        await env.nav.openRoot(backend);
        env.reset();

        // With nothing wired, an action question is never pending as far as
        // NavService is concerned — mirrors the unwired default nav.js's
        // actionPending has before session.js runs setActionPendingCheck.
        await timers.tick(idleRefreshInterval);
        expect(
          env.requested,
          ['GET $base'],
          reason:
              'with no hook wired, idle refresh behaves as if nothing is pending',
        );

        env.reset();
        env.nav.setActionPendingCheck(() => true);
        await timers.tick(idleRefreshInterval);
        expect(
          env.requested,
          isEmpty,
          reason:
              'a pending-action hook that answers true suppresses the idle refresh',
        );
      },
    );

    test(
      'is suppressed while the app is not in the foreground, and resumes on returning',
      () async {
        final timers = FakeTimers();
        final env = Env(timers: timers);
        await env.nav.openRoot(backend);
        env.nav.didChangeAppLifecycleState(AppLifecycleState.paused);
        env.reset();

        await timers.tick(idleRefreshInterval);
        expect(
          env.requested,
          isEmpty,
          reason:
              'polling somebody else\'s API from the background is not something the user asked for',
        );

        env.nav.didChangeAppLifecycleState(AppLifecycleState.resumed);
        await timers.tick(idleRefreshInterval);
        expect(env.requested, [
          'GET $base',
        ], reason: 'returning to the foreground resumes the clock');
      },
    );

    test(
      'an embedded document has no address, so nothing is scheduled',
      () async {
        final timers = FakeTimers();
        final env = Env(timers: timers);
        await env.nav.openRoot(backend);
        final target =
            rowNamed(env.nav.document!, 'Top shelf').target as EmbeddedTarget;
        env.nav.openEmbedded(target.index);
        env.reset();

        await timers.tick(idleRefreshInterval);

        expect(
          env.requested,
          isEmpty,
          reason:
              'an embedded document cannot be re-fetched at all, idle or otherwise',
        );
      },
    );

    test(
      'a failed idle refresh keeps the document on screen and reports the failure',
      () async {
        final timers = FakeTimers();
        final env = Env(timers: timers);
        await env.nav.openRoot(backend);
        final beforeTitle = env.nav.document!.title;
        env.routes.clear();
        env.reset();

        await timers.tick(idleRefreshInterval);

        expect(
          env.nav.document?.title,
          beforeTitle,
          reason:
              'a refresh nobody asked for must never blank the screen, same as any other failure',
        );
        expect(env.nav.state, DocumentState.error);
        expect(env.requested, ['GET $base']);
      },
    );
  });
}
