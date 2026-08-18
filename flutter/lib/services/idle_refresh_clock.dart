/// The idle-refresh clock, extracted from `NavService` (task a21).
///
/// Re-reads the document on top of `NavService`'s stack every
/// [idleRefreshInterval] when nothing else is going on — mirrors `nav.js`'s
/// `scheduleIdle`/`idleRefresh`/`idleRefreshable`. It is a *collaborator* of
/// [NavService], not part of it: `NavService` keeps only the navigation stack
/// and fetching (and stays pure-Dart, framework-free, per AGENTS.md section 5),
/// and this clock listens to it — `nav.addListener(reconsider)` — so every
/// navigation event reconsiders whether to arm, exactly as `nav.js`'s
/// `send()` re-ran `scheduleIdle()` on every frame. A fetch starting,
/// landing, failing, a stack push or pop all count, because each can change
/// what [_refreshable] depends on (there may now be a document with an address
/// to refresh, or there may no longer be one).
///
/// Two things hold it off, mirroring `nav.js`'s two `idleRefreshable()`
/// checks beyond "there is a document with an address to refresh":
///
///  - **An outstanding action question.** `nav.js` weighs two flags for this
///    — `overlayShowing` (an open confirmation) and the `actionPending()`
///    hook (still true in the gap after the watch dismisses the confirmation
///    without answering it, which clearing `overlayShowing` alone does not
///    cover). This port folds both into one: `action_service.dart`'s
///    `hasPending` already stays true across exactly that gap — it is cleared
///    only by answering or cancelling the pending action, never by a dialog
///    merely closing — so a single hook, set with [setActionPendingCheck],
///    covers what `nav.js` needed two flags for. It is a hook rather than a
///    held `ActionService` reference for the same reason `nav.js`'s is:
///    `action_service.dart` needs nothing from this clock, and requiring it
///    here would couple two modules for no reason either would use.
///    `SessionService` wires this hook at construction time, exactly as
///    `session.js` runs `setActionPendingCheck`.
///
///  - **The app not being in the foreground.** New in this port — a watch app
///    has no equivalent, because a Pebble app does not keep running against
///    someone else's API once the wearer stops looking at it the way a
///    backgrounded phone app could. [setInForeground] is how a *framework-bound*
///    lifecycle watcher (a `WidgetsBindingObserver` living in the widget
///    layer, e.g. `DocumentScreen`) tells this clock the app went to the
///    background or came back, translating `AppLifecycleState` into this
///    pure-Dart bool. This clock and `NavService` never import the widgets
///    framework; the framework coupling lives in the widget layer where it
///    belongs — the point of the extraction (AGENTS.md section 5: services are
///    pure-Dart testable).
///
/// A failed idle refresh goes through [NavService.refresh] → the same `_fetch`
/// every other refresh uses, so it is already held to the invariant
/// `NavService` exists to protect (`pebble/docs/DESIGN.md`, "A failed request
/// is not an answer"): the document on screen is never replaced by an empty
/// one, and a background refresh nobody asked for is the last place that
/// should ever blank it.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import 'nav_service.dart';

/// How often to re-read the visible document when nothing else is going on.
/// A screen someone is looking at should not be showing minute-old state.
/// Mirrors `IDLE_MS` in `nav.js`.
const Duration idleRefreshInterval = Duration(seconds: 60);

/// Owns the idle-refresh timer, the pending-action hook, and the foreground
/// gate for one [NavService] — see the module comment. Listens to [NavService]
/// and reconsiders the timer on every notification; fires [NavService.refresh]
/// when the interval elapses.
///
/// The timer factory ([createTimer]) is injected, exactly as
/// `live_service.dart` injects its own — a test drives
/// [idleRefreshInterval] without an actual 60-second wait (see
/// `test/services/idle_refresh_clock_test.dart`'s `FakeTimers`).
class IdleRefreshClock {
  IdleRefreshClock({
    required NavService nav,
    Timer Function(Duration duration, void Function() callback)? createTimer,
  }) : _nav = nav,
       _createTimer =
           createTimer ?? ((duration, callback) => Timer(duration, callback)) {
    _nav.addListener(_reconsider);
  }

  final NavService _nav;
  final Timer Function(Duration duration, void Function() callback)
  _createTimer;

  /// Whether an action confirmation is still awaiting an answer — consulted
  /// by [_refreshable]. Defaults to "never pending", the same unwired default
  /// `actionPending` has in `nav.js` before `session.js` runs
  /// `setActionPendingCheck`. See the module comment for why this is a hook
  /// rather than a held `ActionService` reference.
  bool Function() _actionPending = () => false;

  /// The app's current foreground phase, as last reported through
  /// [setInForeground]. Starts true: nothing has told this clock otherwise
  /// yet, and a freshly-launched app is in the foreground — mirrors starting
  /// from "nothing pending" for the action hook above, for the same reason
  /// (the unwired default should not itself suppress the clock).
  bool _inForeground = true;

  Timer? _timer;

  /// Wires this clock to whichever state governs a pending action
  /// confirmation, without holding a reference to whatever owns it — mirrors
  /// `setActionPendingCheck` in `nav.js`; see the module comment for why a
  /// hook and not a reference. [check] replaces whatever was wired before,
  /// including the unwired default of "never pending", then reconsiders so a
  /// pending state takes hold immediately.
  void setActionPendingCheck(bool Function() check) {
    _actionPending = check;
    _reconsider();
  }

  /// Called by the framework-bound lifecycle watcher (a
  /// `WidgetsBindingObserver` in a widget) to report the app's foreground
  /// phase — see the module comment. Leaving the foreground cancels the
  /// timer outright rather than letting it fire once more and discover it
  /// should not have: the point is not to wake the radio and hit a server for
  /// a screen nobody is looking at, and a timer already in flight when the
  /// app backgrounds would do exactly that. Returning to the foreground
  /// reconsiders, mirroring `dismissed()` in `nav.js` re-running
  /// `scheduleIdle()` once whatever was holding the clock off no longer does.
  /// A no-op if the phase has not actually changed.
  void setInForeground(bool inForeground) {
    if (inForeground == _inForeground) {
      return;
    }
    _inForeground = inForeground;
    if (inForeground) {
      _reconsider();
    } else {
      _cancel();
    }
  }

  /// Mirrors `idleRefreshable()` in `nav.js`, minus the `live.isLive()` and
  /// `overlayShowing` checks (out of scope — see the module comment), plus
  /// the foreground gate, which has no equivalent there. `_nav.href` is null
  /// both before any document has arrived and for an embedded document with
  /// no address of its own, so it covers `nav.js`'s "there is a document with
  /// an address" check in one read.
  bool get _refreshable =>
      _nav.href != null && _inForeground && !_actionPending();

  /// Mirrors `scheduleIdle()` in `nav.js`: cancels whatever was pending and,
  /// if [_refreshable] holds right now, arms a fresh [idleRefreshInterval]
  /// timer. Called on every [NavService] notification (a fetch starting,
  /// landing, failing, a push, a pop, an idle refresh itself) and on a
  /// foreground change, so a reason to hold off that comes and goes between
  /// those events is picked up without anything else having to remember to
  /// ask.
  void _reconsider() {
    _cancel();
    if (_refreshable) {
      _timer = _createTimer(idleRefreshInterval, _fire);
    } else {
      // Worth a line, same as nav.js's comment on the equivalent branch: a
      // background refresh that quietly stops happening looks like nothing
      // at all, which is how the overlay-dismiss bug there went unnoticed
      // until a live test caught it.
      debugPrint(
        'idle: not scheduled (foreground=$_inForeground '
        'pending=${_actionPending()} hasHref=${_nav.href != null})',
      );
    }
  }

  void _cancel() {
    _timer?.cancel();
    _timer = null;
  }

  /// Mirrors `idleRefresh()` in `nav.js`: re-checks [_refreshable] at fire
  /// time (state may have changed in the [idleRefreshInterval] since this was
  /// armed) and, if it still holds, re-fetches the document on top of the
  /// stack through [NavService.refresh] — so a failure here is already covered
  /// by the invariant `NavService.refresh` itself protects, and success or
  /// failure alike reschedules the clock via `NavService`'s
  /// `notifyListeners` → this listener, without this method doing it directly.
  Future<void> _fire() async {
    _timer = null;
    if (!_refreshable) {
      return;
    }
    await _nav.refresh();
  }

  /// Stops listening to [NavService] and cancels any pending timer. Called by
  /// whoever owns this clock (SessionService) when it is itself disposed.
  void dispose() {
    _cancel();
    _nav.removeListener(_reconsider);
  }
}
