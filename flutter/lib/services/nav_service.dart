/// The navigation stack, document fetching and the four document states.
///
/// This is the navigation-and-fetching half of `pebble/src/pkjs/nav.js` —
/// see that file's header for the full reasoning, most of which carries over
/// unchanged. The short version: a client following hypermedia links has one
/// address book, the stack of documents it walked through to get here, and
/// one question worth asking on every fetch: did the last one land, and if
/// not, does that mean anything about the document already on screen?
///
/// [NavService] is a [ChangeNotifier] — the state-management default from
/// AGENTS.md section 5 — because "which document is on screen, and what
/// happened to the last request for it" is exactly the shape that section
/// describes.
///
/// **The invariant this file exists to protect**
/// (`pebble/docs/DESIGN.md`, "A failed request is not an answer"): a
/// failure never replaces the document on screen with an empty one. This
/// port keeps that even more literally than the watch does. `nav.js` can
/// only ever have one frame in flight to the watch at a time, so a fetch
/// necessarily *starts* by sending a `Loading` frame that blanks the screen
/// before the failure (if any) arrives and restores the old document
/// underneath it. Here there is no such wire protocol: [document] always
/// reads the last stack frame that actually arrived, and [state]/[failure]
/// are separate, layered signals about what is happening *to* it — a
/// loading fetch or a failed one never touches [document] at all, so a
/// caller that keeps rendering the last [document] while [state] is
/// [DocumentState.loading] or an error kind never has a moment where the
/// screen goes blank.
///
/// **The four states this module owns**: [DocumentState.ok],
/// [DocumentState.loading], [DocumentState.error] and
/// [DocumentState.unreachable] — [stateFor] maps every [FailureKind] onto
/// one of the latter two. `nav.js` has a fifth, `STATE_NEEDS_CONFIG`, whose
/// only purpose is to route the watch to a dedicated "open the Pebble app"
/// screen when a request cannot even be built (a bad auth header name) or
/// no backend is configured at all. Neither concern belongs to this module
/// here: an unconfigured backend never reaches [NavService] because there is
/// nothing to open yet (`home_screen.dart` owns that empty state), and a
/// local configuration failure is still, from this module's point of view,
/// "the fetch did not produce a document" — so it is folded into
/// [DocumentState.error] rather than kept as a fifth state with no screen of
/// its own to route to.
///
/// **What this module does not own**, left to their own tasks exactly as
/// `flutter/AGENTS.md` section 4 maps them: the action policy — filling an
/// action's fields, confirming it, retrying a `409` — and everything to do
/// with a notice or an overlay laid on top of a frame by an action's outcome
/// (`nav.js`'s `setNotice`/`applyNotice`/`overlay`), following work that
/// outlives its request (`live.js`), and the backend picker and saved
/// shortcuts (`quick.js`, already `home_screen.dart`'s job on this port —
/// see that file's module comment). A coordinator wiring this module to
/// those (`nav.js`'s `session.js`) is its own future task too.
///
/// **The idle-refresh clock** (`nav.js`'s `scheduleIdle`/`idleRefresh`/
/// `idleRefreshable`) *is* owned here, and re-reads the document on top of
/// the stack every [idleRefreshInterval] when nothing else is going on. Two
/// things hold it off, mirroring the two `idleRefreshable()` checks beyond
/// "there is a document with an address to refresh":
///
///  - **An outstanding action question.** `nav.js` weighs two flags for
///    this — `overlayShowing` (an open confirmation) and the
///    `actionPending()` hook (still true in the gap after the watch
///    dismisses the confirmation without answering it, which clearing
///    `overlayShowing` alone does not cover). This port folds both into one:
///    `action_service.dart`'s `hasPending` already stays true across exactly
///    that gap — it is cleared only by answering or cancelling the pending
///    action, never by a dialog merely closing — so a single hook, set with
///    [setActionPendingCheck], covers what `nav.js` needed two flags for.
///    Mirrors `setActionPendingCheck` in `nav.js`, including the reasoning
///    in its comment there for why this is a hook and not a held
///    `ActionService` reference: `action_service.dart` needs nothing from
///    this module, and requiring it here would let two modules require
///    each other for no reason either would use.
///  - **The app not being in the foreground.** New in this port — a watch
///    app has no equivalent, because a Pebble app does not keep running
///    against someone else's API once the wearer stops looking at it the
///    way a backgrounded phone app could. [didChangeAppLifecycleState]
///    (`WidgetsBindingObserver`'s method — a caller registers this service
///    with `WidgetsBinding.instance.addObserver` once one exists to do the
///    registering) is how a screen tells this module the app went to the
///    background or came back, so idle refresh does not poll a server, and
///    spend battery, for a screen nobody is reading.
///
/// A failed idle refresh goes through the same [_fetch] every other refresh
/// does, so it is already held to the invariant this file exists to protect
/// — see above: [document] keeps reading the last stack frame that actually
/// arrived, and a background refresh nobody asked for is the last place that
/// should ever be allowed to blank the screen.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart'
    show AppLifecycleState, WidgetsBindingObserver;

import '../models/failure.dart';
import '../models/result.dart';
import '../models/siren.dart';
import 'http_service.dart';
import 'render_service.dart' as render;
import 'settings_service.dart';

/// How often to re-read the visible document when nothing else is going on.
/// A screen someone is looking at should not be showing minute-old state.
/// Mirrors `IDLE_MS` in `nav.js`.
const Duration idleRefreshInterval = Duration(seconds: 60);

/// What is on screen right now, independent of *which* document it is.
///
/// Mirrors the `STATE_*` constants in `nav.js` minus `STATE_NEEDS_CONFIG` —
/// see the module comment for why that one does not carry over here.
enum DocumentState {
  /// The document in [NavService.document] is exactly what the server last
  /// sent for it.
  ok,

  /// A fetch is in flight. [NavService.document] still holds whatever was
  /// there before — see the module comment on why this port does not blank
  /// the screen the way `nav.js`'s `sendLoading` does.
  loading,

  /// The last fetch failed for a reason that says nothing about whether the
  /// document in [NavService.document] is still accurate, but is not a
  /// pure connectivity problem either (auth, conflict, server, client,
  /// parse or config — see [FailureKind]). [NavService.failure] carries the
  /// reason.
  error,

  /// The last fetch never got an answer at all — [FailureKind.unreachable]
  /// or [FailureKind.timeout]. Kept distinct from [error] because "I could
  /// not ask" and "the answer was no" are different facts, and only one of
  /// them is about the server (`pebble/docs/DESIGN.md`).
  unreachable,
}

/// Maps a fetch failure onto a [DocumentState] — mirrors `stateFor` in
/// `nav.js`. Every [FailureKind] other than [FailureKind.unreachable]/
/// [FailureKind.timeout] becomes [DocumentState.error]; see the module
/// comment for why [FailureKind.config] does not get a state of its own
/// here the way it does on the watch.
DocumentState stateFor(FailureKind kind) {
  if (kind == FailureKind.unreachable || kind == FailureKind.timeout) {
    return DocumentState.unreachable;
  }
  return DocumentState.error;
}

/// One entry on the navigation stack: a document, the href it can be
/// re-fetched from, and the title it is shown under.
///
/// [href] is null for a sub-entity that arrived embedded rather than linked
/// (see [NavService.openEmbedded]) — Siren allows either, and an embedded
/// entity simply has no address of its own to re-fetch from. Mirrors the
/// `{ entity, href, title }` stack frame shape in `nav.js`.
@immutable
class _StackFrame {
  const _StackFrame({required this.entity, this.href, this.title = ''});

  final Entity entity;
  final String? href;
  final String title;
}

/// Owns the navigation stack, every fetch that changes it, and the
/// idle-refresh clock — see the module comment.
///
/// [HttpService] is injected, exactly as `render_service.dart` and
/// `http_service.dart` themselves are composed with injected collaborators
/// — a test wires a [HttpService] built on `package:http`'s `MockClient`
/// (see `test/services/http_service_test.dart`), never a real socket. The
/// idle timer's factory is injected too, exactly as `live_service.dart`
/// injects its own — a test drives [idleRefreshInterval] without an actual
/// 60-second wait (see `test/services/nav_service_test.dart`'s `FakeTimers`).
class NavService extends ChangeNotifier with WidgetsBindingObserver {
  NavService({
    required HttpService http,
    Timer Function(Duration duration, void Function() callback)? createTimer,
  }) : _http = http,
       _createTimer =
           createTimer ?? ((duration, callback) => Timer(duration, callback));

  final HttpService _http;
  final Timer Function(Duration duration, void Function() callback)
  _createTimer;

  final List<_StackFrame> _stack = [];

  Backend? _backend;
  DocumentState _state = DocumentState.ok;
  Failure? _failure;

  /// Whether an action confirmation is still awaiting an answer — consulted
  /// by [_idleRefreshable]. Defaults to "never pending", the same unwired
  /// default `actionPending` has in `nav.js` before `session.js` runs its
  /// `setActionPendingCheck`. See the module comment for why this is a hook
  /// rather than a held `ActionService` reference.
  bool Function() _actionPending = () => false;

  /// The app's current lifecycle phase, as last reported through
  /// [didChangeAppLifecycleState]. Starts [AppLifecycleState.resumed]:
  /// nothing has told this service otherwise yet, and a freshly-launched app
  /// is in the foreground — mirrors starting from "nothing pending" for the
  /// action hook above, for the same reason (the unwired default should not
  /// itself suppress the clock).
  AppLifecycleState _lifecycleState = AppLifecycleState.resumed;

  Timer? _idleTimer;

  /// The backend currently open, or null before [openRoot] has ever been
  /// called. A live accessor rather than a value handed out once — mirrors
  /// `currentBackend` in `nav.js`, kept for the same reason: a future
  /// caller (the action pipeline) needs whichever backend is current at the
  /// moment it sends a request, not whichever one was current when it first
  /// asked.
  Backend? get backend => _backend;

  /// What is on screen right now. See the module comment for why this never
  /// changes for a [loading] fetch or a failed one — only [state] and
  /// [failure] do.
  DocumentState get state => _state;

  /// Why [state] is [DocumentState.error] or [DocumentState.unreachable].
  /// Null whenever [state] is [DocumentState.ok] or [DocumentState.loading].
  Failure? get failure => _failure;

  /// The document to render: the last entity that was actually fetched or
  /// opened, turned into rows. Null only before the first document has ever
  /// arrived, or right after [openRoot] has reset the stack for a new
  /// backend and before its root has landed — there is nothing to keep
  /// showing at that point because nothing has been shown yet.
  render.RenderedDocument? get document => _stack.isEmpty
      ? null
      : render.document(_stack.last.entity, _stack.last.title);

  /// The raw document on top of the stack, before [document] turns it into
  /// rows — what `action_service.dart` needs to look an action up by name
  /// ([Entity.actionByName]) and what `live_service.dart` needs to match a
  /// poll target against ([LiveService.pollTarget]). Null under the same
  /// conditions [document] is. Added for `session.dart` (task p11), the one
  /// module allowed to compose this service with those two — see the module
  /// comment on what this file does not own.
  Entity? get entity => _stack.isEmpty ? null : _stack.last.entity;

  /// The href [entity] can be re-fetched from, or null for a sub-entity that
  /// arrived embedded rather than linked — same nullability as
  /// [_StackFrame.href], and added for the same reason as [entity].
  String? get href => _stack.isEmpty ? null : _stack.last.href;

  /// True once there is a document below the one on screen to pop back to
  /// with [back]. False for the backend's root document — what "back" means
  /// from there (closing this backend, say) is a decision for whatever
  /// screen holds this service, not this module; mirrors `nav.js`'s `back()`
  /// falling through to the picker frame at that point, minus the picker
  /// itself (out of scope here — see the module comment).
  bool get canGoBack => _stack.length > 1;

  /// Opens [backend] at its base URL, replacing whatever backend and stack
  /// were previously open, then follows [Backend.startRel] if it has one.
  /// Mirrors `openBackend` + `fetchRoot` + `followStart` in `nav.js`.
  ///
  /// Nothing is carried over from the previous backend: keeping its
  /// document on screen while this one loads would be showing one server's
  /// state under another server's name — mirrors the same reasoning in
  /// `nav.js`'s `openBackend`.
  Future<void> openRoot(Backend backend) async {
    _stack.clear();
    _backend = backend;
    _state = DocumentState.loading;
    _failure = null;
    _notify();

    final result = await _http.get(backend, backend.baseUrl);
    switch (result) {
      case Ok(value: final response):
        final entity = Entity.fromJson(response.entity);
        // Checked before anything is pushed: a server speaking a version
        // this app was not written against may have changed the meaning of
        // something it would otherwise display confidently and wrongly —
        // mirrors the `siren.versionProblem` check in `fetchRoot`.
        final problem = entity.versionProblem;
        if (problem != null) {
          _state = DocumentState.error;
          _failure = Failure(kind: FailureKind.client, message: problem);
          _notify();
          return;
        }
        _push(entity, href: backend.baseUrl, title: backend.name);
        _notify();
        await _followStart(backend, entity);
      case Err(failure: final failure):
        _state = stateFor(failure.kind);
        _failure = failure;
        _notify();
    }
  }

  /// Follows the link with rel [Backend.startRel] on the freshly-fetched
  /// root, if the backend configured one and the root actually offers it.
  /// The rel is the only server-specific string anywhere in this app, and
  /// the user typed it into the settings screen themselves — mirrors
  /// `followStart` in `nav.js`. A missing rel is not a failure: the server
  /// may simply not offer it right now, which is a legitimate answer, not
  /// something to route around.
  Future<void> _followStart(Backend backend, Entity root) async {
    if (backend.startRel.isEmpty) {
      return;
    }
    final href = root.follow(backend.startRel);
    if (href == null) {
      debugPrint('nav: no link with rel "${backend.startRel}" on the root');
      return;
    }
    await _fetch(href, title: backend.startRel, replace: false);
  }

  /// Follows [href] and pushes the result on top of the stack — mirrors
  /// `nav.js`'s `fetch(href, title, false)`, the case a link row or a saved
  /// shortcut uses. On failure the stack, and so [document], is untouched;
  /// only [state]/[failure] change.
  Future<void> fetch(String href, {String title = ''}) =>
      _fetch(href, title: title, replace: false);

  /// Re-fetches the document on top of the stack and replaces it in place.
  /// Mirrors `refresh` in `nav.js`. An embedded document has no address of
  /// its own to re-fetch from — re-rendering what is already in hand is the
  /// honest option there, same as `nav.js`'s comment on the same case: it is
  /// not a failure, so it does not touch [state]/[failure] either, it is
  /// simply a no-op past clearing whatever was already on screen.
  Future<void> refresh() async {
    if (_stack.isEmpty) {
      return;
    }
    final here = _stack.last;
    final href = here.href;
    if (href == null) {
      _state = DocumentState.ok;
      _failure = null;
      _notify();
      return;
    }
    await _fetch(href, title: here.title, replace: true);
  }

  /// Opens a sub-entity that arrived embedded inside the document already on
  /// screen. Nothing is fetched: it is already in hand, and asking the
  /// server for it again could legitimately return something different —
  /// mirrors `openEmbedded` in `nav.js`. Out-of-range or before anything is
  /// open, [index] is simply ignored: there is nothing there to open.
  void openEmbedded(int index) {
    if (_stack.isEmpty) {
      return;
    }
    final entities = _stack.last.entity.entities;
    if (index < 0 || index >= entities.length) {
      return;
    }
    final child = entities[index];
    _push(child, href: child.follow('self'), title: child.label);
    _notify();
  }

  /// Pops one document. A no-op at the backend's root — see [canGoBack].
  /// Mirrors the stack-popping half of `back` in `nav.js`; the half that
  /// falls through to the picker below the root does not carry over here
  /// (out of scope — see the module comment).
  void back() {
    if (!canGoBack) {
      return;
    }
    _stack.removeLast();
    _state = DocumentState.ok;
    _failure = null;
    _notify();
  }

  /// Performs one fetch and applies its outcome to the stack. Shared by
  /// [fetch] (push), [refresh] (replace) and [_followStart] (push) — mirrors
  /// `nav.js`'s single `fetch(href, title, replace)`.
  ///
  /// A failure never touches the stack: [document] keeps reading whatever
  /// was there before this call, which is precisely the invariant this
  /// module exists to protect — see the module comment.
  Future<void> _fetch(
    String href, {
    required String title,
    required bool replace,
  }) async {
    final backend = _backend;
    if (backend == null) {
      return;
    }
    _state = DocumentState.loading;
    _failure = null;
    _notify();

    final result = await _http.get(backend, href);
    switch (result) {
      case Ok(value: final response):
        final entity = Entity.fromJson(response.entity);
        if (replace && _stack.isNotEmpty) {
          _stack[_stack.length - 1] = _StackFrame(
            entity: entity,
            href: href,
            title: title,
          );
        } else {
          _push(entity, href: href, title: title);
        }
        _state = DocumentState.ok;
        _failure = null;
      case Err(failure: final failure):
        _state = stateFor(failure.kind);
        _failure = failure;
    }
    _notify();
  }

  void _push(Entity entity, {String? href, String title = ''}) {
    _stack.add(_StackFrame(entity: entity, href: href, title: title));
    _state = DocumentState.ok;
    _failure = null;
  }

  /// Tells listeners something changed, then re-evaluates the idle timer
  /// against the new state. Every place in this class that used to call
  /// `notifyListeners()` directly calls this instead, so the idle clock is
  /// reconsidered on every navigation event exactly as `nav.js`'s `send()`
  /// reconsiders it (via `scheduleIdle()`) on every frame — a fetch starting,
  /// landing, failing, a stack push or pop all count, because each one can
  /// change what [_idleRefreshable] depends on (there may now be a document
  /// with an address to refresh, or there may no longer be one).
  void _notify() {
    notifyListeners();
    _scheduleIdle();
  }

  /// --- idle refresh ---------------------------------------------------
  ///
  /// Quiet on purpose, same as `nav.js`: no loading state is surfaced for an
  /// idle refresh (the document already on screen is what stays up while it
  /// runs, exactly as any other fetch), and a failure sets [failure] rather
  /// than doing anything more intrusive. A refresh nobody asked for should
  /// never take the screen away from them — see the module comment.

  /// Wires this service to whichever state governs a pending action
  /// confirmation, without holding a reference to whatever owns it — mirrors
  /// `setActionPendingCheck` in `nav.js`; see the module comment for why a
  /// hook and not a reference. [check] replaces whatever was wired before,
  /// including the unwired default of "never pending".
  void setActionPendingCheck(bool Function() check) {
    _actionPending = check;
  }

  /// Called by whatever registers this service with
  /// `WidgetsBinding.instance.addObserver` — see the module comment. Leaving
  /// the foreground cancels the idle timer outright rather than letting it
  /// fire once more and discover it should not have: the point is not to
  /// wake the radio and hit a server for a screen nobody is looking at, and
  /// a timer already in flight when the app backgrounds would do exactly
  /// that. Returning to the foreground resumes it, mirroring `dismissed()`
  /// in `nav.js` re-running `scheduleIdle()` once whatever was holding the
  /// clock off no longer does.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _lifecycleState = state;
    if (_inForeground) {
      _scheduleIdle();
    } else {
      _idleTimer?.cancel();
      _idleTimer = null;
    }
  }

  bool get _inForeground => _lifecycleState == AppLifecycleState.resumed;

  /// Mirrors `idleRefreshable()` in `nav.js`, minus the `live.isLive()` and
  /// `overlayShowing` checks — out of scope here, see the module comment —
  /// plus [_inForeground], which has no equivalent there.
  bool get _idleRefreshable =>
      _stack.isNotEmpty &&
      _stack.last.href != null &&
      _inForeground &&
      !_actionPending();

  /// Mirrors `scheduleIdle()` in `nav.js`: cancels whatever was pending and,
  /// if [_idleRefreshable] holds right now, arms a fresh [idleRefreshInterval]
  /// timer. Called from [_notify] after every navigation event and from
  /// [didChangeAppLifecycleState] on returning to the foreground, so a
  /// reason to hold off that comes and goes between those events is picked
  /// up without anything else having to remember to ask.
  void _scheduleIdle() {
    _idleTimer?.cancel();
    _idleTimer = null;
    if (_idleRefreshable) {
      _idleTimer = _createTimer(idleRefreshInterval, _idleRefresh);
    } else {
      // Worth a line, same as nav.js's comment on the equivalent branch: a
      // background refresh that quietly stops happening looks like nothing
      // at all, which is how the overlay-dismiss bug there went unnoticed
      // until a live test caught it.
      debugPrint(
        'nav: idle refresh not scheduled (foreground=$_inForeground '
        'pending=${_actionPending()} depth=${_stack.length})',
      );
    }
  }

  /// Mirrors `idleRefresh()` in `nav.js`: re-checks [_idleRefreshable] at
  /// fire time (state may have changed in the [idleRefreshInterval] since
  /// this was scheduled) and, if it still holds, re-fetches the document on
  /// top of the stack through the same [_fetch] every other refresh uses —
  /// so a failure here is already covered by the invariant [_fetch] itself
  /// protects, and success or failure alike reschedules the clock via
  /// [_notify] without this method having to do it directly.
  Future<void> _idleRefresh() async {
    _idleTimer = null;
    if (!_idleRefreshable) {
      return;
    }
    final here = _stack.last;
    await _fetch(here.href!, title: here.title, replace: true);
  }

  @override
  void dispose() {
    _idleTimer?.cancel();
    _idleTimer = null;
    super.dispose();
  }
}
