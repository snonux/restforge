// Pure Dart unit tests: no WidgetTester, no device, no socket. See
// AGENTS.md section 5 ("Test style"). Ported from pebble/tools/test-live.js
// -- see live_service.dart's module comment and that file's header for the
// three details these tests exist to pin: where to poll, which answer is
// ours, and how long to wait.
//
// Matched from test-live.js: testWhatStartsAWatch, testPollTarget,
// testNothingToFollowMeansNoWatch, testSilenceIsNotCompletion,
// testProgressThenDone, testAnswerAboutAnotherJob, testNoJobHere,
// testFailedPollIsNotNews, testDeadlineFromTheServer, testFallbackDeadline,
// testBudgetIsReDerived, testStopIsFinal.
//
// test-live.js drives its fake setTimeout/Date.now from a single
// module-level clock and a hand-rolled tick() that fires every due timer in
// order, re-checking after each one in case its callback scheduled another
// that also falls inside the same advance. FakeClock below is the same
// idea, offered to LiveService as the injected now/createTimer rather than
// monkey-patched globals -- LiveService is a plain instance, so unlike
// test-live.js there is no module-level `watching`/`timer` to reset by hand
// between tests; each test gets its own Env and its own LiveService.

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:restforge/models/siren.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/live_service.dart';
import 'package:restforge/services/settings_service.dart';

const String base = 'http://host.example/';

final Backend backend = Backend(name: 'x', baseUrl: base, secret: 's');

/// The origin document links to a job by a rel that matches the job's
/// class -- mirrors test-live.js's `ORIGIN`.
final Origin origin = Origin(
  entity: Entity.fromJson({
    'class': ['pantry'],
    'links': [
      {
        'rel': ['self'],
        'href': '/',
      },
      {
        'rel': ['kettle-job'],
        'href': '/job',
      },
    ],
  }),
  href: base,
);

/// An action's response, as far as [LiveService.start] is concerned --
/// mirrors test-live.js's `job()`.
ActionOutcome job(Map<String, dynamic> props, {int status = 200}) => ActionOutcome(
  status: status,
  entity: Entity.fromJson({
    'class': ['kettle-job'],
    'properties': props,
  }),
);

/// A poll reply's JSON body -- mirrors test-live.js's `jobBody()`.
Map<String, dynamic> jobBody(Map<String, dynamic> props) => {
  'class': ['kettle-job'],
  'properties': props,
};

/// A fake, wall-clock-free timer/clock pair, driven by [tick] -- the Dart
/// equivalent of test-live.js's fake `setTimeout`/`Date.now`/`tick()`.
/// Handed to [LiveService] as `now`/`createTimer`, so a 24-second job (see
/// `pebble/tools/fake-siren-server.py`) or a 25-minute fallback deadline is
/// driven without an actual wait.
class FakeClock {
  FakeClock(this._now);

  DateTime _now;
  final List<_FakeTimer> _pending = [];

  DateTime now() => _now;

  Timer createTimer(Duration duration, void Function() callback) {
    final timer = _FakeTimer(this, _now.add(duration), callback);
    _pending.add(timer);
    return timer;
  }

  void _remove(_FakeTimer timer) => _pending.remove(timer);

  /// Advances the clock by [duration], firing every timer due at or before
  /// the new time, earliest first. A fired callback (always one of
  /// [LiveService]'s own -- reschedule itself, or give up) may schedule a
  /// further timer that also falls inside this same advance, so the pending
  /// list is re-scanned after each firing rather than snapshotted once --
  /// mirrors test-live.js's `tick()`. The short real delay after each
  /// firing lets the async HTTP round trip a poll starts (answered by the
  /// in-memory [MockClient] in [Env], not a real socket) actually resolve
  /// before the next timer is chosen; it costs milliseconds, never the
  /// deadline being tested.
  Future<void> tick(Duration duration) async {
    final until = _now.add(duration);
    for (;;) {
      _FakeTimer? next;
      for (final timer in _pending) {
        if (!timer.fireAt.isAfter(until) && (next == null || timer.fireAt.isBefore(next.fireAt))) {
          next = timer;
        }
      }
      if (next == null) break;
      _now = next.fireAt;
      _pending.remove(next);
      next.callback();
      for (var i = 0; i < 5; i++) {
        await Future<void>.delayed(Duration.zero);
      }
    }
    _now = until;
  }
}

class _FakeTimer implements Timer {
  _FakeTimer(this._clock, this.fireAt, this.callback);

  final FakeClock _clock;
  final DateTime fireAt;
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

  // A one-shot timer never repeats, so it fires at most once -- mirrors
  // dart:async's own one-shot Timer, whose `tick` is 0 before firing and 1
  // after. Nothing in LiveService reads this; it exists only to satisfy the
  // Timer interface.
  @override
  int get tick => _active ? 0 : 1;
}

/// Records what a watch reported through its [LiveHandlers] -- mirrors
/// test-live.js's `record()`.
class Seen {
  final List<String> progress = [];
  String? done;
  bool gaveUp = false;
}

LiveHandlers handlersFor(Seen seen) => LiveHandlers(
  onProgress: (entity) => seen.progress.add(entity.properties['step'] as String),
  onDone: (entity) => seen.done = entity.properties['state'] as String?,
  onGiveUp: () => seen.gaveUp = true,
);

/// One test's fixture: a fake backend serving [replyBody]/[replyStatus] (or
/// throwing, when [fail] is set) to every poll, a log of the URLs polled,
/// and the [FakeClock] the [LiveService] under test is wired to -- mirrors
/// the `FakeXHR`/`reply`/`polls` globals in test-live.js, minus the shared
/// mutable state: each test builds its own [Env] and its own [LiveService].
class Env {
  Env() {
    final client = MockClient((request) async {
      polls.add(request.url.toString());
      if (fail) {
        throw Exception('connection refused');
      }
      return http.Response(
        jsonEncode(replyBody ?? {}),
        replyStatus,
        headers: {'content-type': 'application/json'},
      );
    });
    live = LiveService(
      http: HttpService(client: client, log: (_) {}),
      now: clock.now,
      createTimer: clock.createTimer,
      log: (_) {},
    );
  }

  final FakeClock clock = FakeClock(DateTime.fromMillisecondsSinceEpoch(500000));
  final List<String> polls = [];
  Map<String, dynamic>? replyBody;
  int replyStatus = 200;

  /// Set true to make every poll behave like a dropped connection instead
  /// of consulting [replyBody] -- mirrors test-live.js swapping
  /// `FakeXHR.prototype.send`.
  bool fail = false;

  late final LiveService live;

  /// Starts a watch on a job entity with [props] -- mirrors test-live.js's
  /// `begin()`.
  ({bool started, Seen seen}) begin(Map<String, dynamic> props, {int status = 202}) {
    polls.clear();
    final seen = Seen();
    final started = live.start(backend, origin, job(props, status: status), handlersFor(seen));
    return (started: started, seen: seen);
  }
}

void main() {
  group('what counts as something to watch', () {
    test('a 202 starts a watch', () {
      expect(LiveService.shouldWatch(job({'id': 1}, status: 202)), isTrue);
    });

    test('an entity that says it is running starts a watch', () {
      expect(LiveService.shouldWatch(job({'state': 'running'})), isTrue);
    });

    // An ordinary answer is an answer. Watching it would be polling forever.
    test('a plain 200 does not', () {
      expect(LiveService.shouldWatch(job({'state': 'done'})), isFalse);
    });

    test('an entity with no state does not', () {
      expect(LiveService.shouldWatch(job({})), isFalse);
    });
  });

  // The poll target is found by matching the result's class against the
  // origin's link rels -- an idiom, not a path this app knows.
  group('the poll target', () {
    test('a matching rel is the thing to poll', () {
      expect(
        LiveService.pollTarget(
          origin.entity,
          Entity.fromJson({
            'class': ['kettle-job'],
          }),
        ),
        '/job',
      );
    });

    test('an unmatched class polls nothing in particular', () {
      expect(
        LiveService.pollTarget(
          origin.entity,
          Entity.fromJson({
            'class': ['unrelated'],
          }),
        ),
        isNull,
      );
    });
  });

  // Found by looking at a real API: falling back to polling the origin
  // document looks helpful and is a lie. The origin does not report the
  // job's state, so the first poll would read its silence as completion and
  // announce that the work finished seconds after it started.
  test('with nothing the server offers to track it, no watch starts', () async {
    final env = Env();
    final seen = Seen();

    final started = env.live.start(
      backend,
      origin,
      ActionOutcome(
        status: 202,
        entity: Entity.fromJson({
          'class': ['unrelated'],
        }),
      ),
      handlersFor(seen),
    );

    expect(started, isFalse);
    await env.clock.tick(pollInterval * 3);
    expect(env.polls, isEmpty, reason: 'nothing is polled');
    expect(seen.done, isNull, reason: 'nothing is claimed to have finished');
  });

  // The same failure one step later: the thing being polled turns out not
  // to report progress at all.
  test('an entity that reports no state does not end the watch as done', () async {
    final env = Env();
    final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 120});
    env.replyBody = jobBody({'note': 'no state here'});

    await env.clock.tick(pollInterval);

    expect(run.seen.done, isNull);
    expect(run.seen.gaveUp, isTrue, reason: 'it stops, and says it stopped');
    expect(env.live.isLive, isFalse);
  });

  test('progress then done', () async {
    final env = Env();
    final run = env.begin({'state': 'running', 'id': 4, 'step': 'one', 'staleAfterSeconds': 120});
    expect(run.started, isTrue);
    expect(env.live.isLive, isTrue);

    env.replyBody = jobBody({'state': 'running', 'id': 4, 'step': 'two', 'staleAfterSeconds': 120});
    await env.clock.tick(pollInterval);
    expect(env.polls, ['${base}job'], reason: 'the job link is what gets polled');
    expect(run.seen.progress, ['two'], reason: 'progress is reported');

    env.replyBody = jobBody({'state': 'done', 'id': 4, 'staleAfterSeconds': 120});
    await env.clock.tick(pollInterval);
    expect(run.seen.done, 'done', reason: 'completion is reported once');
    expect(env.live.isLive, isFalse, reason: 'and the watch has ended');
  });

  // The one that matters: a poll answered by a machine that never saw this
  // job.
  test('a poll answered by a machine that never saw this job', () async {
    final env = Env();
    final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 120});

    env.replyBody = jobBody({'state': 'done', 'id': 99, 'staleAfterSeconds': 120});
    await env.clock.tick(pollInterval);
    expect(run.seen.done, isNull, reason: "another job's completion is not ours");
    expect(env.live.isLive, isTrue, reason: 'and the watch continues');

    env.replyBody = jobBody({'state': 'done', 'id': 4, 'staleAfterSeconds': 120});
    await env.clock.tick(pollInterval);
    expect(run.seen.done, 'done', reason: 'our own completion still ends it');
  });

  test('a state of none is not completion', () async {
    final env = Env();
    final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 120});

    // "I have no job" is not "the job finished".
    env.replyBody = jobBody({'state': 'none', 'id': 0});
    await env.clock.tick(pollInterval);

    expect(run.seen.done, isNull);
    expect(env.live.isLive, isTrue, reason: 'and the watch continues');
  });

  test('a failed poll says nothing about the job', () async {
    final env = Env();
    final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 120});
    env.fail = true;

    await env.clock.tick(pollInterval);

    expect(run.seen.done, isNull);
    expect(env.live.isLive, isTrue, reason: 'and does not end the watch');
  });

  group('the deadline', () {
    test('inside the budget it keeps going, past it it gives up', () async {
      final env = Env();
      final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 1});
      env.replyBody = jobBody({'state': 'running', 'id': 4, 'step': 'x', 'staleAfterSeconds': 1});

      await env.clock.tick(pollInterval);
      expect(env.live.isLive, isTrue, reason: 'inside the budget it keeps going');

      await env.clock.tick(budgetBuffer + pollInterval);
      expect(run.seen.gaveUp, isTrue, reason: 'past the budget it gives up');
      // Giving up is about this client, not about the job.
      expect(run.seen.done, isNull, reason: 'giving up is not completion');
    });

    // A server that never sends a budget gets the generous fallback,
    // because abandoning a job that is still running is the worse mistake.
    test('no advertised budget means the generous fallback', () async {
      final env = Env();
      final run = env.begin({'state': 'running', 'id': 4});
      env.replyBody = jobBody({'state': 'running', 'id': 4, 'step': 'x'});

      await env.clock.tick(pollInterval * 10);
      expect(env.live.isLive, isTrue);
      expect(run.seen.gaveUp, isFalse, reason: 'no advertised budget means the generous default');

      await env.clock.tick(fallbackBudget);
      expect(run.seen.gaveUp, isTrue, reason: 'which does eventually run out');
    });

    // The budget is re-derived every poll, so an early answer that carried
    // none does not fix a short deadline for the whole run.
    test('a later, larger budget replaces the first one', () async {
      final env = Env();
      final run = env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 1});
      env.replyBody = jobBody({
        'state': 'running',
        'id': 4,
        'step': 'x',
        'staleAfterSeconds': 3600,
      });

      await env.clock.tick(pollInterval);
      await env.clock.tick(budgetBuffer + pollInterval);

      expect(run.seen.gaveUp, isFalse);
      expect(env.live.isLive, isTrue);
    });
  });

  test('a stopped watch never polls again', () async {
    final env = Env();
    env.begin({'state': 'running', 'id': 4, 'staleAfterSeconds': 120});
    env.live.stop();
    env.polls.clear();

    await env.clock.tick(pollInterval * 3);

    expect(env.polls, isEmpty, reason: 'a stopped watch never polls again');
    expect(env.live.isLive, isFalse, reason: 'and reports itself stopped');
  });
}
