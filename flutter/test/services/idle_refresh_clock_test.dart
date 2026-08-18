// Pure Dart unit tests for the idle-refresh clock (task a21): no
// WidgetTester, no device, no socket. See AGENTS.md section 5 ("Test
// style").
//
// These cases moved out of nav_service_test.dart when the idle-refresh
// clock was extracted from NavService into its own collaborator
// (idle_refresh_clock.dart) — they pin the clock's behaviour, which used
// to live in NavService: it re-reads the document on top of the stack after
// the idle interval, a pending-action hook suppresses it (the unwired
// default does not), the foreground gate suppresses it and resumes on
// returning, an embedded document (no address) is never scheduled, and a
// failed idle refresh keeps the document on screen and reports the
// failure (the invariant NavService exists to protect, reached through the
// same refresh() every other refresh uses).
//
// The timer factory is faked (FakeTimers) so idleRefreshInterval is driven
// without an actual 60-second wait, exactly as nav_service_test.dart's
// FakeTimers (mirroring test-nav.js's fake setTimeout/tick) did before the
// extraction.

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/idle_refresh_clock.dart';
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

http.Response jsonResponse(Object body, {int status = 200}) => http.Response(
  jsonEncode(body),
  status,
  headers: {'content-type': 'application/json'},
);

Row rowNamed(RenderedDocument page, String label) => page.rows.firstWhere(
  (row) => row.label == label,
  orElse: () => throw StateError('no row "$label"'),
);

/// A fake, wall-clock-free timer, driven by [tick] — the Dart equivalent of
/// test-nav.js's fake `setTimeout`/`tick()`. Handed to [IdleRefreshClock] as
/// `createTimer` by [Env], so [idleRefreshInterval] is driven without an
/// actual 60-second wait.
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
  /// [NavService]'s `notifyListeners` -> [IdleRefreshClock]'s `reconsider`
  /// listener — so the pending list is re-scanned after each firing rather
  /// than snapshotted once, same as test-nav.js's `tick()`. The short real
  /// delay after each firing lets the fetch a fired timer starts (answered by
  /// the in-memory [MockClient] in [Env], not a real socket) actually resolve
  /// before the next timer is chosen.
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

  @override
  int get tick => _active ? 0 : 1;
}

/// A fake backend driven the same way nav_service_test.dart's `Env` is: a
/// route table keyed by the exact URL requested, a log of what was
/// requested, and a hand-toggled [unreachable] switch for the failure case
/// the failed-refresh test covers.
class Env {
  Env({FakeTimers? timers}) : timers = timers ?? FakeTimers() {
    final client = MockClient((request) async {
      requested.add('${request.method} ${request.url}');
      if (unreachable) {
        throw Exception('connection refused');
      }
      final body = routes[request.url.toString()];
      if (body == null) {
        return jsonResponse(
          {'properties': {'message': 'no such thing here'}},
          status: 404,
        );
      }
      return jsonResponse(body);
    });
    final http = HttpService(client: client, log: (_) {});
    nav = NavService(http: http);
    clock = IdleRefreshClock(nav: nav, createTimer: this.timers.createTimer);
  }

  final FakeTimers timers;
  final Map<String, Object> routes = {base: root};
  final List<String> requested = [];
  bool unreachable = false;

  late final NavService nav;
  late final IdleRefreshClock clock;

  void reset() => requested.clear();
}

void main() {
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

  test('a pending-action hook suppresses the refresh; the unwired default does '
      'not', () async {
    final timers = FakeTimers();
    final env = Env(timers: timers);
    await env.nav.openRoot(backend);
    env.reset();

    // With nothing wired, an action question is never pending as far as
    // the clock is concerned — mirrors the unwired default nav.js's
    // actionPending has before session.js runs setActionPendingCheck.
    await timers.tick(idleRefreshInterval);
    expect(
      env.requested,
      ['GET $base'],
      reason:
          'with no hook wired, idle refresh behaves as if nothing is pending',
    );

    env.reset();
    env.clock.setActionPendingCheck(() => true);
    await timers.tick(idleRefreshInterval);
    expect(
      env.requested,
      isEmpty,
      reason:
          'a pending-action hook that answers true suppresses the idle refresh',
    );
  });

  test('is suppressed while the app is not in the foreground, and resumes on '
      'returning', () async {
    final timers = FakeTimers();
    final env = Env(timers: timers);
    await env.nav.openRoot(backend);
    env.clock.setInForeground(false);
    env.reset();

    await timers.tick(idleRefreshInterval);
    expect(
      env.requested,
      isEmpty,
      reason:
          'polling somebody else\'s API from the background is not something the user asked for',
    );

    env.clock.setInForeground(true);
    await timers.tick(idleRefreshInterval);
    expect(env.requested, [
      'GET $base',
    ], reason: 'returning to the foreground resumes the clock');
  });

  test('an embedded document has no address, so nothing is scheduled', () async {
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
  });

  test('a failed idle refresh keeps the document on screen and reports the '
      'failure', () async {
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
  });
}
