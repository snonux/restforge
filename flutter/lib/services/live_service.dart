/// Watching something that is still happening.
///
/// This is the Dart port of `pebble/src/pkjs/live.js` — see that file's
/// header and `pebble/docs/DESIGN.md` ("Do not claim a job finished") for
/// the full reasoning. Some actions do not finish when the request does. A
/// server that answers 202, or answers with an entity that says it is still
/// running, is telling the client to come back and look — and until it
/// stops saying that, the screen is showing something that is no longer
/// true.
///
/// Everything here is generic, and three details are worth restating
/// because each one is a way a naive poller gets it wrong:
///
///  - **Where to poll.** If the document the action came from has a link
///    whose rel matches one of the returned entity's classes, that link is
///    the thing to watch — [pollTarget] does ordinary rel/class matching,
///    an ordinary hypermedia idiom that needs no knowledge of what the
///    resource is called. Failing that, [start] refuses to watch at all
///    rather than falling back to the origin document: the origin does not
///    report the job's state, so the first poll would read its silence as
///    completion and announce the work had finished seconds after it
///    started.
///
///  - **Which answer is ours.** A load-balanced deployment can route a poll
///    to a machine that never saw the job. Such a reply is "no news", not
///    "no job": [relevant] skips a response whose `id` differs from the one
///    the watch started with, or whose `state` is the server's word for
///    nothing-here, rather than treating it as completion — see
///    [handle]. Reading it as completion is how a client reports a job as
///    finished seconds after starting it.
///
///  - **How long to wait.** The deadline comes from the server's own
///    `staleAfterSeconds` and is re-derived on every poll by
///    [checkDeadline], never fixed when polling began — an early poll that
///    landed on a machine with no job carries no budget to derive one from,
///    and a later one will. [fallbackBudget] is deliberately generous,
///    because giving up early on a job that is still running is worse than
///    waiting. A failed poll is news about the network, not the job, and is
///    never folded into a deadline check that also decides completion — see
///    [poll]. Giving up is reported through [LiveHandlers.onGiveUp],
///    distinctly from both success ([LiveHandlers.onDone]) and a network
///    failure, which is not reported to the handlers at all: it is only
///    ever "still watching, ask again".
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../models/result.dart';
import '../models/siren.dart';
import 'http_service.dart';
import 'settings_service.dart';

/// How often to ask. Ten seconds is frequent enough that a step change is
/// visible while watching, and rare enough that a long job is not a hundred
/// requests. Mirrors `POLL_MS` in live.js.
const Duration pollInterval = Duration(seconds: 10);

/// Slack added to the server's own staleness budget, covering the poll
/// interval and the round trip. Mirrors `BUFFER_MS`.
const Duration budgetBuffer = Duration(seconds: 60);

/// Used only when no response has ever carried a budget to derive one from
/// — an older server, or a run of polls that all landed somewhere without
/// the job. Generous on purpose: this can only make the client wait longer
/// than necessary, never give up on a job that is still going. Mirrors
/// `FALLBACK_MS`.
const Duration fallbackBudget = Duration(minutes: 25);

/// The state a server reports for "there is no such job here". The one
/// string in this file that names a value rather than a structure, and a
/// guess that costs nothing if wrong: an unrecognised state that is not
/// "running" simply ends the watch, which is the same thing that happens
/// for any other terminal state. Mirrors `STATE_NONE`.
const String stateNone = 'none';

/// Mirrors `STATE_RUNNING`.
const String stateRunning = 'running';

/// The document an action was invoked from — mirrors the `origin` argument
/// (`{ entity, href }`) to `start` in live.js.
@immutable
class Origin {
  final Entity entity;
  final String href;
  const Origin({required this.entity, required this.href});
}

/// What an action's response looked like, as far as deciding whether to
/// watch it is concerned — mirrors the `result` argument (`{ status,
/// entity }`) to `start` in live.js.
@immutable
class ActionOutcome {
  final int status;
  final Entity entity;
  const ActionOutcome({required this.status, required this.entity});
}

/// Callbacks a watch reports through — mirrors the `handlers` argument
/// (`onProgress`, `onDone`, `onGiveUp`) to `start` in live.js.
class LiveHandlers {
  final void Function(Entity entity) onProgress;
  final void Function(Entity entity) onDone;
  final void Function() onGiveUp;

  const LiveHandlers({
    required this.onProgress,
    required this.onDone,
    required this.onGiveUp,
  });
}

/// The state of one in-progress watch — mirrors the `watching` object in
/// live.js. Not exposed: everything a caller needs is read through
/// [LiveService]'s own methods and the handlers it was started with.
class _Watch {
  _Watch({
    required this.backend,
    required this.href,
    required this.id,
    required this.budget,
    required this.startedAt,
    required this.handlers,
  });

  final Backend backend;
  final String href;

  /// The job id this watch started with, or null when the entity carried
  /// none. Compared against later replies by [LiveService._relevant].
  final dynamic id;

  /// The current deadline budget, re-derived by [LiveService._checkDeadline]
  /// on every poll that carries one. Null means "no budget seen yet, use
  /// [fallbackBudget]".
  Duration? budget;

  final DateTime startedAt;
  final LiveHandlers handlers;
}

/// Follows work that outlives the request that started it.
///
/// [HttpService] is injected exactly as `nav_service.dart` does. The clock
/// ([now]) and timer factory ([createTimer]) are injected too, so a test can
/// drive a 24-second server-side job (see `pebble/tools/fake-siren-server.py`)
/// or a 25-minute fallback deadline without an actual wall-clock wait —
/// mirrors the fake `setTimeout`/`Date.now` `test-live.js` installs before
/// requiring live.js.
class LiveService {
  LiveService({
    required HttpService http,
    DateTime Function()? now,
    Timer Function(Duration duration, void Function() callback)? createTimer,
    void Function(String message)? log,
  }) : _http = http,
       _now = now ?? DateTime.now,
       _createTimer = createTimer ?? ((duration, callback) => Timer(duration, callback)),
       _log = log ?? debugPrint;

  final HttpService _http;
  final DateTime Function() _now;
  final Timer Function(Duration duration, void Function() callback) _createTimer;
  final void Function(String message) _log;

  Timer? _timer;
  _Watch? _watching;

  /// Whether a watch is currently running. Mirrors `isLive`.
  bool get isLive => _watching != null;

  /// Decides whether an action's response is something to follow. Either
  /// signal is enough: the status code, or the entity's own account.
  /// Mirrors `shouldWatch`.
  static bool shouldWatch(ActionOutcome result) =>
      result.status == 202 || _running(result.entity);

  /// Picks the resource to watch: a link on the origin document whose rel
  /// matches a class of the thing the action returned. Mirrors
  /// `pollTarget`.
  static String? pollTarget(Entity originEntity, Entity resultEntity) {
    for (final className in resultEntity.classes) {
      final href = originEntity.follow(className);
      if (href != null) {
        return href;
      }
    }
    // No matching link. There is then nothing to follow, and saying so is
    // the only honest answer -- see the note on start().
    return null;
  }

  /// Reports whether an entity says it is still in progress. Mirrors
  /// `running`.
  static bool _running(Entity entity) => entity.properties['state'] == stateRunning;

  /// Reports whether an entity says anything at all about progress. One
  /// that does not is not a job resource, and treating its silence as
  /// completion is exactly the lie this file exists to prevent. Mirrors
  /// `judgeable`.
  static bool _judgeable(Entity entity) => entity.properties.containsKey('state');

  /// The server's own staleness budget, with [budgetBuffer] slack added, or
  /// null when the entity carried none. Mirrors `budgetMs`.
  static Duration? _budgetFor(Entity entity) {
    final stale = entity.properties['staleAfterSeconds'];
    if (stale is num && stale > 0) {
      return Duration(milliseconds: (stale * 1000).round()) + budgetBuffer;
    }
    return null;
  }

  /// Begins watching, if there is anything to watch. Returns true when a
  /// watch was started, so the caller can tell "in progress" from
  /// "finished". Mirrors `start`.
  bool start(Backend backend, Origin origin, ActionOutcome result, LiveHandlers handlers) {
    stop();
    if (!shouldWatch(result)) {
      return false;
    }

    final href = pollTarget(origin.entity, result.entity);
    if (href == null) {
      _log('live: nothing the server offers tracks this, not watching');
      return false;
    }

    final values = result.entity.properties;
    _watching = _Watch(
      backend: backend,
      href: href,
      id: values['id'],
      budget: _budgetFor(result.entity),
      startedAt: _now(),
      handlers: handlers,
    );
    final id = values['id'];
    final budget = _watching!.budget;
    _log(
      'live: watching $href id ${id ?? '(none)'}, '
      'budget ${budget == null ? 'default' : '${budget.inMilliseconds}ms'}',
    );
    _schedule();
    return true;
  }

  /// Stops watching, if anything was. Mirrors `stop`. Idempotent, same as
  /// the original: called both to tear down an old watch before [start]
  /// begins a new one, and directly by a caller that navigated away.
  void stop() {
    _timer?.cancel();
    _timer = null;
    _watching = null;
  }

  void _schedule() {
    _timer = _createTimer(pollInterval, _poll);
  }

  /// Fires one poll. Mirrors `poll`.
  Future<void> _poll() async {
    _timer = null;
    final current = _watching;
    if (current == null) {
      return;
    }

    final result = await _http.get(current.backend, current.href);
    // A watch that was stopped -- or replaced by a new one -- while the
    // request was in flight must not resurrect itself.
    if (!identical(_watching, current)) {
      return;
    }

    switch (result) {
      case Err(failure: final failure):
        // A failed poll is not news about the job, only about the network.
        // Keep asking until the deadline; that is the whole reason there is
        // one.
        _log('live: poll failed (${failure.kind}), still watching');
        _checkDeadline(null);
      case Ok(value: final response):
        _handle(Entity.fromJson(response.entity));
    }
  }

  /// Gives up when the server's own budget has run out. Returns true when
  /// the watch has ended. Mirrors `checkDeadline`.
  bool _checkDeadline(Entity? entity) {
    final watching = _watching;
    if (watching == null) {
      return true;
    }
    if (entity != null) {
      final fresh = _budgetFor(entity);
      if (fresh != null) {
        watching.budget = fresh;
      }
    }
    final budget = watching.budget ?? fallbackBudget;
    if (_now().difference(watching.startedAt) > budget) {
      final handlers = watching.handlers;
      _log('live: gave up after ${budget.inMilliseconds}ms');
      stop();
      handlers.onGiveUp();
      return true;
    }
    _schedule();
    return false;
  }

  /// Filters out answers that are not about our job. Both cases mean "ask
  /// again", not "it finished". Mirrors `relevant`.
  bool _relevant(Entity entity) {
    final watching = _watching!;
    final values = entity.properties;
    if (values['state'] == stateNone) {
      return false;
    }
    final wantedId = watching.id;
    final gotId = values['id'];
    if (wantedId != null && gotId != null && gotId != wantedId) {
      return false;
    }
    return true;
  }

  /// Applies one poll reply. Mirrors `handle`.
  void _handle(Entity entity) {
    if (!_relevant(entity)) {
      _log('live: answer is about a different job, or none — asking again');
      _checkDeadline(entity);
      return;
    }
    if (!_judgeable(entity)) {
      // Whatever we are polling does not report progress, so it cannot
      // tell us the job ended. Stop watching and say we stopped -- the
      // alternative is reading silence as completion, which is how a
      // client reports machines as up while they are still booting.
      final handlers = _watching!.handlers;
      _log('live: nothing here reports progress, stopping');
      stop();
      handlers.onGiveUp();
      return;
    }
    if (!_running(entity)) {
      _finish(entity);
      return;
    }
    _watching!.handlers.onProgress(entity);
    _checkDeadline(entity);
  }

  void _finish(Entity entity) {
    final handlers = _watching!.handlers;
    _log('live: finished (${entity.properties['state']})');
    stop();
    handlers.onDone(entity);
  }
}
