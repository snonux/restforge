/// The coordinator: the only service the UI layer talks to.
///
/// This is the Dart port of `pebble/src/pkjs/session.js` — see that file's
/// header for the full reasoning, most of which carries over unchanged. It
/// used to be one 800-line module sharing globals between five concerns;
/// split along those concerns, what is left is what genuinely needs more
/// than one of them:
///
///  - **[activate]**, because a row's [render.RowTarget] can mean either "go
///    somewhere" ([NavService]) or "ask about doing something"
///    ([ActionService]) — deciding which is the one thing that needs to know
///    about both. A [render.DetailTarget] is neither; it is a value already
///    in hand, and — exactly as `session.js`'s `activate()` handles it
///    inline rather than asking `nav.js` to decide — this file just opens it
///    (see [detail]).
///  - **Wiring the idle-refresh clock to `action_service.dart`'s pending
///    question.** The clock (`IdleRefreshClock`, a collaborator of
///    [NavService] since task a21 — see `idle_refresh_clock.dart`) must not
///    depend on [ActionService], and [ActionService] already sits on top of
///    navigation-adjacent concepts (a [Backend], an [Entity]). This file is
///    the one place allowed to know about both, so the constructor creates
///    the clock and runs `clock.setActionPendingCheck(() =>
///    actions.hasPending)` once, exactly as `session.js` runs the equivalent
///    `nav.js` call. This coordinator also owns the clock's lifetime and
///    exposes [setAppForeground] for the framework-bound lifecycle watcher
///    (`DocumentScreen`) that gates idle refresh on the app being in the
///    foreground.
///  - **Everything `action_service.dart`'s own module comment defers to a
///    coordinator**: re-fetching the document an action was invoked from
///    (`pebble/docs/DESIGN.md`, "Never carry a document across an action"),
///    and handing a still-running response to [LiveService] instead of
///    reporting it as finished. This mirrors `actions.js`'s
///    `afterActionSuccess`/`afterActionError`/`liveHandlers` — in the
///    watchapp those lived inside `actions.js` because it could reach `nav.js`
///    and `live.js` directly; here `action_service.dart` and
///    `live_service.dart` deliberately hold no reference to either
///    `nav_service.dart` or each other (see their own module comments), so
///    the wiring moves to this file, the coordinator.
///  - **[saveQuick]/[runQuick]**, for the same reason: `quick_service.dart`
///    only stores and resolves a shortcut (its own module comment is explicit
///    that composing that with a fetch or an action is deliberately left to
///    this file), and [NavService]/[ActionService] each know nothing of the
///    other or of [QuickService]. Saving needs [NavService]'s notion of "the
///    current backend and document" to turn a pressed row into a
///    [QuickItem]; running needs [QuickService.backendFor] plus
///    [NavService.adopt]/[NavService.fetch] plus, for an action shortcut,
///    the same [_askAction] a hand-pressed [render.ActionTarget] goes
///    through — so a shortcut gets exactly the confirmation a hand-reached
///    action would, never a silent invoke.
///
/// **State management.** [SessionService] is the top-level [ChangeNotifier]
/// the UI listens to (AGENTS.md section 5) — but it forwards, rather than
/// duplicates, [NavService]'s state (`document`, `state`, `failure`,
/// `backend`, `canGoBack` are plain passthrough getters, and every
/// [NavService] notification is relayed as this service's own). What it
/// does hold itself is state neither composed service has anywhere to put:
/// a [DetailView] opened for reading, a [SessionQuestion] awaiting an
/// answer, and a [SessionNotice] reporting what the last action (or the job
/// it started) produced. `nav.js` could fold the equivalent of the last two
/// onto the very next frame it sent (`setNotice`/`applyNotice`,
/// `overlay`) because every navigation event rebuilt the frame from
/// scratch; this port's fields persist until told otherwise, so
/// [_clearTransient] is this file's replacement for "a fresh frame has
/// nothing on top of it" — called on every navigation event that changes
/// which document is on screen, deliberately *not* called by the refresh an
/// action's own outcome triggers, since that refresh is exactly the moment
/// the notice it just set is meant to be seen next to.
///
/// The three overlay states stay on this one [ChangeNotifier] rather than
/// splitting into per-concern notifiers (a DetailNotifier/QuestionNotifier/
/// NoticeNotifier) to scope rebuilds — an ISP tension that was weighed and
/// held. The subtree under `DocumentScreen`'s `ListenableBuilder`
/// rebuilds on every notification (it listens to this service, and
/// `DetailViewHost`/`ConfirmationSheetHost` depend on the whole service for
/// one slice each), but the rebuild is cheap — the row list is a lazy `ListView.builder` and
/// the banners are a handful of one-line widgets, so re-creating the widget
/// description to diff is sub-millisecond — and there is no measured jank,
/// no large list, and no screen whose rebuild is genuinely expensive.
/// Splitting would add a second state vocabulary (three notifiers and their
/// wiring) for a scoped-rebuild benefit with no buyer, the exact coupling
/// cost AGENTS.md section 5 warns against. Revisit when a screen's rebuild
/// is demonstrably the cost (a large non-lazy list, heavy per-row work, or
/// measured jank); until then one notifier fits this single-document browser.
///
/// **What does not carry over**, beyond the AppMessage/PebbleKit layer this
/// whole port has no equivalent of (`flutter/AGENTS.md` section 4):
///
///  - `removeQuick` and the backend picker itself (`quick.js`, `session.js`'s
///    `listBackends`/`openBackend` passthrough). Removing a shortcut is pure
///    storage — `QuickService.remove` — with nothing to compose, so
///    `home_screen.dart` calls it directly rather than through this file;
///    [saveQuick] and [runQuick] *do* carry over (see the module comment
///    above), since both need more than one service.
///  - `noteInbox`, `setSeq`, `dismissed` — purely about the watch's overlay/
///    sequence protocol over AppMessage, which has no analogue on a single
///    device with no separate window stack. The one behavioural gap
///    `dismissed()` existed to close (a confirm overlay the watch closed by
///    itself without answering must still hold the idle clock off) already
///    holds here for free: [ActionService.hasPending] stays true across
///    exactly that gap regardless of whether a Flutter screen is still
///    showing the sheet — see [NavService]'s module comment.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/siren.dart';
import 'action_service.dart';
import 'http_service.dart';
import 'idle_refresh_clock.dart';
import 'live_service.dart';
import 'nav_service.dart';
import 'quick_service.dart';
import 'render_service.dart' as render;
import 'settings_service.dart';

/// A value opened for full reading — the result of [SessionService.activate]
/// on a [render.DetailTarget] row. Carries the value itself, not just "open
/// something": `render_service.dart`'s [render.DetailTarget] already carries
/// the full text, so there is nothing further to look up.
@immutable
class DetailView {
  final String heading;
  final String body;
  const DetailView({required this.heading, required this.body});
}

/// What the user is currently being asked, on top of whatever
/// [SessionService.document] shows — the rendered form of an
/// [ActionService] [AskOutcome]/[InvokeOutcome] a screen can put on screen
/// without ever seeing the href or method behind it (see
/// `action_service.dart`'s module comment on why those never leave that
/// file). A caller `switch`es over this exhaustively, same as every sealed
/// outcome type elsewhere in this app.
sealed class SessionQuestion {
  const SessionQuestion();
}

/// A yes/no confirmation before an unsafe action is sent. Mirrors
/// [ConfirmationRequired]. Answered with [SessionService.answer].
class ConfirmQuestion extends SessionQuestion {
  final String heading;
  final String body;
  const ConfirmQuestion({required this.heading, required this.body});
}

/// A value asked for out loud, for a required field with no default and no
/// confirmation to stand in for it (`pebble/docs/DESIGN.md`, "Do not invent
/// a value"). Mirrors [InvokeNeedsValue]. Answered with
/// [SessionService.answerValue].
class ValueQuestion extends SessionQuestion {
  final String label;
  const ValueQuestion(this.label);
}

/// A one-shot report of what the last action — or the job it started —
/// produced, laid over [SessionService.document] until the next navigation
/// clears it. See the module comment on why this lives here rather than in
/// `nav_service.dart`.
sealed class SessionNotice {
  final String heading;
  const SessionNotice(this.heading);
}

/// The server no longer offers the action a row promised. A real answer —
/// it cannot be done right now — not a bug to route around by inventing a
/// request. Mirrors the withdrawn-action branch of `askAction()` in
/// actions.js. [heading] is the action's name: the server never offered it,
/// so there is no [Action.label] to prefer over it.
class ActionWithdrawn extends SessionNotice {
  const ActionWithdrawn(super.heading);
}

/// [SessionService.answer]/[SessionService.answerValue] found a problem
/// [ActionService.fillFields] would rather not guess around: more than one
/// required field with nothing to fill it, or a value asked for out loud
/// that came back empty. Mirrors [InvokeRefused].
class ActionRefused extends SessionNotice {
  final String reason;
  const ActionRefused(super.heading, this.reason);
}

/// The action was sent and answered, or the job it started has finished.
/// [message] is the server's own word for the outcome (a `state` property,
/// or "Accepted"/"Done" when it did not send one); [body] is the full
/// response rendered generically. Mirrors `resultBanner`/`describeResult` in
/// actions.js, reused for a live job's `onDone` the same way that file does.
class ActionOutcomeReported extends SessionNotice {
  final String message;
  final String body;
  const ActionOutcomeReported(
    super.heading, {
    required this.message,
    required this.body,
  });
}

/// A step reported while a job is still being watched. Mirrors `onProgress`
/// in actions.js's `liveHandlers` — simpler than [ActionOutcomeReported] on
/// purpose: a step is a running commentary, not a final account.
class ActionProgress extends SessionNotice {
  final String step;
  const ActionProgress(super.heading, this.step);
}

/// Watching a job was abandoned before the server ever said it was done —
/// not a claim the job failed, only that this app stopped asking. Mirrors
/// `onGiveUp`. The document is re-fetched anyway (see
/// [SessionService._onLiveGiveUp]): whatever the action changed before this
/// app gave up watching is still worth seeing.
class ActionGaveUp extends SessionNotice {
  const ActionGaveUp(super.heading);
}

/// The action, or the one retry `action_service.dart` allows, failed.
/// Mirrors `afterActionError` in actions.js. Whether the document was
/// re-fetched after this is not part of the notice itself — see
/// [SessionService._handleFailure] for the conflict-only rule
/// `pebble/docs/DESIGN.md` requires.
class ActionFailed extends SessionNotice {
  final Failure failure;
  const ActionFailed(super.heading, this.failure);
}

/// What [SessionService.saveQuick] did with a pressed row. Not a
/// [SessionNotice]: the row being saved is already on screen (this is
/// `document_screen.dart`'s long-press/overflow affordance), so the caller
/// reports the outcome directly (a `SnackBar`, say) rather than laying
/// something over [SessionService.document] that the very next navigation
/// would clear before anyone read it. Mirrors the three outcomes
/// `saveQuick()` sends as a frame message in session.js
/// (`'Saved'`/`'Not saved'`/`'Cannot save'`/`'Cannot save that'`), collapsed
/// to one enum since this port has a typed return value to switch on instead
/// of a string to read.
enum QuickSaveOutcome {
  /// Stored — new, or replacing an identical existing shortcut (see
  /// [QuickService.add]'s idempotence).
  saved,

  /// [QuickService.maxQuick] shortcuts are already stored. Mirrors
  /// `'Not saved'`; must be surfaced, never silently dropped.
  full,

  /// [row] cannot be a shortcut at all: a property or an already-embedded
  /// sub-entity has nothing to look up or re-fetch later
  /// ([render.DetailTarget]/[render.EmbeddedTarget]), or the row was an
  /// action on a document that has no address of its own to remember as the
  /// holder (an embedded document — [NavService.href] is null). Mirrors
  /// `'Cannot save'`/`'Cannot save that'`.
  notSaveable,
}

/// What [SessionService.runQuick] did with a saved shortcut. Also not a
/// [SessionNotice], and for the same reason [QuickSaveOutcome] is not one:
/// this is called from wherever shortcuts are listed (`home_screen.dart`),
/// before any [SessionService.document] exists to lay a notice over.
/// Mirrors the two outcomes `runQuick()` in session.js can produce before it
/// ever gets as far as fetching anything.
enum QuickRunOutcome {
  /// The backend was resolved and adopted, and the fetch that follows —
  /// [render.FetchTarget]'s href for a document shortcut, [QuickItem.holder]
  /// for an action one — is already under way or has already landed (or
  /// failed; see [NavService.adopt]'s doc comment on why a shortcut still
  /// navigates on a failed fetch rather than reporting nothing at all). The
  /// caller should now show [SessionService] on screen (push
  /// `DocumentScreen`) — whatever it has to show, including a failure or a
  /// [ActionWithdrawn] notice, belongs there, not on the screen the
  /// shortcut was pressed from.
  opened,

  /// [QuickItem.baseUrl] no longer matches a configured backend. Nothing was
  /// adopted or fetched; report this on the screen the shortcut was pressed
  /// from — mirrors session.js's `runQuick` showing an overlay on the
  /// *picker* frame rather than adopting one, since there is nothing to show
  /// past this point.
  backendMissing,
}

/// Composes [NavService], [ActionService] and [LiveService] into the single
/// service the UI layer talks to — see the module comment for what belongs
/// here and why.
///
/// [HttpService] is injected the same way every other service in this app
/// is; [nav]/[actions]/[live]/[quick] are injected too, and default to plain
/// instances (built on [http] for the first three), so a test can substitute
/// any of them without reaching inside this class. The idle-refresh clock's
/// timer factory ([createTimer]) is injected too, so a test drives
/// [idleRefreshInterval] without a real 60s wait (mirrors
/// `idle_refresh_clock_test.dart`'s `FakeTimers`; the same fake is shared
/// with [LiveService]'s poll timer in `session_test.dart`); a [LiveService]
/// with a fake clock (mirrors `live_service_test.dart`'s `FakeClock`).
/// [QuickService] needs no [http] — it never does its own I/O (see
/// its module comment) — so it is optional on its own, not derived from it.
class SessionService extends ChangeNotifier {
  SessionService({
    required HttpService http,
    NavService? nav,
    ActionService? actions,
    LiveService? live,
    QuickService? quick,
    Timer Function(Duration duration, void Function() callback)? createTimer,
  }) : _nav = nav ?? NavService(http: http),
       _actions = actions ?? ActionService(http: http),
       _live = live ?? LiveService(http: http),
       _quick = quick ?? QuickService(),
       _ownsNav = nav == null {
    // The idle-refresh clock is a collaborator of NavService (task a21),
    // extracted so NavService stays pure-Dart. It owns the timer (with the
    // injected [createTimer] a test drives without a real 60s wait), the
    // pending-action hook, and the foreground gate.
    _clock = IdleRefreshClock(nav: _nav, createTimer: createTimer);
    // The one piece of cross-module wiring neither nav_service.dart nor
    // action_service.dart can do to itself — see the module comment.
    _clock.setActionPendingCheck(() => _actions.hasPending);
    // Forwarded, not duplicated: every navigation event nav_service.dart
    // already tracks (a fetch starting, landing or failing; a stack push,
    // pop or idle refresh) is exactly a change this coordinator's own
    // listeners need to hear about too.
    _nav.addListener(notifyListeners);
  }

  final NavService _nav;
  final ActionService _actions;
  final LiveService _live;
  final QuickService _quick;

  /// The idle-refresh clock this coordinator owns — see `idle_refresh_clock.dart`.
  /// `late final` because it is created in the constructor body (it needs
  /// [_nav], built in the initializer list).
  late final IdleRefreshClock _clock;

  /// Whether this service constructed [_nav] itself (and so owns disposing
  /// it) or was handed one built elsewhere (a test's, most often) — a
  /// service this file did not create is not this file's to tear down.
  final bool _ownsNav;

  /// True once [dispose] has run. A live poll or an action outcome that
  /// completes after the user has backed away from the document (so the
  /// coordinator is disposed) must not [notifyListeners] on a disposed
  /// [ChangeNotifier] — see task 821. [notifyListeners] is overridden to a
  /// no-op once this is set, so [_onLiveDone]/[_onLiveGiveUp]/[_handleSuccess]
  /// (and any other async tail) cannot trip the "used after dispose" assert.
  bool _disposed = false;

  DetailView? _detail;
  SessionQuestion? _question;
  SessionNotice? _notice;

  /// The action's own label, remembered across the round trip from
  /// [activate] asking a question to [answer]/[answerValue] resolving it —
  /// mirrors `siren.label(action)` being threaded through `invoke()` in
  /// actions.js. Needed because by the time a question is answered, the
  /// [SessionQuestion] on screen may be a [ValueQuestion] carrying a
  /// *field's* label instead.
  String _pendingActionLabel = '';

  // --- forwarded nav_service.dart state -----------------------------------

  /// The backend currently open. See [NavService.backend].
  Backend? get backend => _nav.backend;

  /// What is on screen right now. See [NavService.state].
  DocumentState get state => _nav.state;

  /// Why [state] is not [DocumentState.ok]. See [NavService.failure].
  Failure? get failure => _nav.failure;

  /// The document to render. See [NavService.document].
  render.RenderedDocument? get document => _nav.document;

  /// The href [document] can be re-fetched from, or null for a sub-entity
  /// that arrived embedded rather than linked. See [NavService.href]. Added
  /// for [saveQuick]: an action row is saved as (this href, the action's
  /// name), never as the action's own href — see `quick_service.dart`'s
  /// module comment.
  String? get href => _nav.href;

  /// True once there is a document below the one on screen. See
  /// [NavService.canGoBack].
  bool get canGoBack => _nav.canGoBack;

  // --- state this file owns itself ----------------------------------------

  /// A value opened for full reading by [activate], or null. See
  /// [dismissDetail].
  DetailView? get detail => _detail;

  /// The question currently awaiting [answer] or [answerValue], or null.
  SessionQuestion? get question => _question;

  /// What the last action (or the job it started) produced, or null. See
  /// [dismissNotice].
  SessionNotice? get notice => _notice;

  /// Whether a job is currently being watched. See [LiveService.isLive].
  bool get isLive => _live.isLive;

  // --- navigation ----------------------------------------------------------

  /// Opens [backend] at its root, replacing whatever was open before.
  /// Mirrors `nav.openBackend` — passed straight through in session.js, but
  /// this port also stops any live watch and clears whatever was laid over
  /// the previous backend's document, since neither has any business
  /// surviving a switch to a different server. See the module comment on
  /// [_clearTransient].
  Future<void> openBackend(Backend backend) async {
    _live.stop();
    _clearTransient();
    await _nav.openRoot(backend);
  }

  /// Turns a pressed row back into what it means: [render.FetchTarget] and
  /// [render.EmbeddedTarget] go to [NavService], [render.ActionTarget] goes
  /// to [ActionService] via [_askAction], and [render.DetailTarget] is
  /// handled right here — see the module comment. Mirrors `activate()` in
  /// session.js.
  Future<void> activate(render.RowTarget target) async {
    switch (target) {
      case render.DetailTarget(:final heading, :final body):
        _detail = DetailView(heading: heading, body: body);
        notifyListeners();
      case render.FetchTarget(:final href):
        // Mirrors nav.js's fetch(href, title, false): navigating somewhere
        // new means whatever was being watched belonged to the screen being
        // left.
        _live.stop();
        _clearTransient();
        await _nav.fetch(href);
      case render.EmbeddedTarget(:final index):
        // Mirrors nav.js's openEmbedded, which does not stop a live watch —
        // opening a sub-entity already in hand is not "leaving" the
        // document the watch is tied to the way following a link is.
        _clearTransient();
        _nav.openEmbedded(index);
      case render.ActionTarget(:final name):
        await _askAction(name);
    }
  }

  /// Pops one document. Mirrors `back()` in session.js: abandons whatever
  /// action question was pending (leaving the screen it was offered on
  /// abandons the question with it) and stops any live watch, since both
  /// belonged to the document being left.
  void back() {
    _live.stop();
    _clearTransient();
    _nav.back();
  }

  /// Re-fetches the document on screen. Mirrors `session.js`'s `refresh:
  /// nav.refresh` — deliberately does *not* stop a live watch or clear
  /// [notice]/[detail]: mirrors `nav.js`'s own `fetch(href, title, true)`
  /// skipping `live.stop()` when replacing rather than pushing, since a
  /// manual refresh of the very document a watch is tied to is not
  /// "leaving" it.
  Future<void> refresh() => _nav.refresh();

  /// Closes [detail] without otherwise touching navigation. Nothing in
  /// session.js needs an equivalent — the watch's reading window is
  /// dismissed by the watch itself, off this module entirely — but a
  /// Flutter screen showing [detail] as a modal needs an explicit way to
  /// close it that is not also a [back] press.
  void dismissDetail() {
    _detail = null;
    notifyListeners();
  }

  /// Clears [notice] once a screen has shown it. See [dismissDetail]'s
  /// comment; the same reasoning applies.
  void dismissNotice() {
    _notice = null;
    notifyListeners();
  }

  /// Clears whatever is laid on top of [document] — [detail], [question]
  /// and [notice] — and abandons any question [ActionService] is still
  /// holding open. Called by every navigation event that changes which
  /// document is on screen — see the module comment for why this port
  /// needs an explicit call where session.js's frame-per-navigation model
  /// did not, and for why this is deliberately never called from the
  /// refresh an action's own outcome triggers.
  void _clearTransient() {
    _actions.cancelPending();
    _detail = null;
    _question = null;
    _notice = null;
    _pendingActionLabel = '';
  }

  // --- saved shortcuts -------------------------------------------------

  /// Saves [row] as a shortcut, or explains why it cannot be one — see
  /// [QuickSaveOutcome]. Mirrors `saveQuick()` in session.js: what is stored
  /// is what the server offered, not a URL of this app's own — an action by
  /// name plus [href] (the *document's* address, never the action's own —
  /// `quick_service.dart`'s module comment), a link by the href it carried.
  /// A property ([render.DetailTarget]) opens a reading view, not a place to
  /// return to, and an already-embedded sub-entity ([render.EmbeddedTarget])
  /// has no address of its own either — both are refused rather than saved
  /// as something that would not resolve to anything next time.
  Future<QuickSaveOutcome> saveQuick(render.Row row) async {
    final backend = _nav.backend;
    if (backend == null) {
      return QuickSaveOutcome.notSaveable;
    }

    QuickItem item;
    switch (row.target) {
      case render.ActionTarget(:final name):
        final holder = _nav.href;
        if (holder == null || holder.isEmpty) {
          // The document offering this action arrived embedded, not linked
          // — nowhere to look the action up again next time.
          return QuickSaveOutcome.notSaveable;
        }
        item = QuickItem(
          label: row.label,
          backendName: backend.name,
          baseUrl: backend.baseUrl,
          kind: QuickKind.action,
          holder: holder,
          name: name,
        );
      case render.FetchTarget(:final href):
        item = QuickItem(
          label: row.label,
          backendName: backend.name,
          baseUrl: backend.baseUrl,
          kind: QuickKind.document,
          href: href,
        );
      case render.DetailTarget():
      case render.EmbeddedTarget():
        return QuickSaveOutcome.notSaveable;
    }

    final saved = await _quick.add(item);
    return saved != null ? QuickSaveOutcome.saved : QuickSaveOutcome.full;
  }

  /// Follows a saved shortcut — see [QuickRunOutcome]. Mirrors `runQuick()`
  /// in session.js: adopt the backend the shortcut points at (never fetch
  /// its root first — [NavService.adopt]'s doc comment), then either fetch
  /// the saved address (a document shortcut) or fetch the holder document
  /// and look the action up by name in whatever comes back (an action
  /// shortcut) — the exact same [_askAction] a hand-pressed
  /// [render.ActionTarget] goes through, so a withdrawn action is reported
  /// as [ActionWithdrawn] and a confirmable one still asks, precisely as if
  /// this had been walked to by hand rather than jumped to.
  Future<QuickRunOutcome> runQuick(QuickItem item) async {
    final backend = await _quick.backendFor(item);
    if (backend == null) {
      return QuickRunOutcome.backendMissing;
    }

    _live.stop();
    _clearTransient();
    _nav.adopt(backend);

    if (item.kind == QuickKind.document) {
      await _nav.fetch(item.href, title: item.label);
      return QuickRunOutcome.opened;
    }

    await _nav.fetch(item.holder, title: item.label);
    if (_nav.state == DocumentState.ok) {
      // Only ask if the holder itself was actually fetched — a failed fetch
      // already left state/failure set for the caller to render; asking
      // about an action on a document that never arrived would be
      // inventing a document nobody sent (mirrors `openQuickHolder`'s
      // error branch in nav.js, which never calls its `then` callback).
      await _askAction(item.name);
    }
    return QuickRunOutcome.opened;
  }

  // --- actions ---------------------------------------------------------

  /// Looks [name] up on the current document and decides whether it needs
  /// confirming. Mirrors `askAction()` in actions.js. A no-op if nothing is
  /// open yet — there is nothing to look the action up on.
  Future<void> _askAction(String name) async {
    final backend = _nav.backend;
    final entity = _nav.entity;
    if (backend == null || entity == null) {
      return;
    }
    _pendingActionLabel = entity.actionByName(name)?.label ?? name;

    final outcome = await _actions.ask(backend, entity, name);
    switch (outcome) {
      case ActionNotOffered(:final name):
        _question = null;
        _notice = ActionWithdrawn(name);
        notifyListeners();
      case ConfirmationRequired(:final heading, :final body):
        _question = ConfirmQuestion(heading: heading, body: body);
        notifyListeners();
      case ActionInvoked(:final outcome):
        await _applyInvokeOutcome(outcome, entity, backend);
    }
  }

  /// Handles the reply to a [ConfirmQuestion]. Mirrors the confirmed half of
  /// `answer()` in actions.js — the "what value" half is [answerValue]
  /// instead, so a caller's two questions stay two distinct, statically
  /// typed calls (see `action_service.dart`'s module comment on
  /// [ActionService.answer] for the same reasoning one layer down).
  Future<void> answer(bool confirmed) async {
    final backend = _nav.backend;
    final entity = _nav.entity;
    if (backend == null || entity == null) {
      _question = null;
      notifyListeners();
      return;
    }
    final outcome = await _actions.answer(confirmed, backend, entity);
    _question = null;
    if (outcome == null) {
      // Declined, or the question was already superseded — both are
      // "nothing to do", not an error, mirroring ActionService.answer's own
      // contract.
      notifyListeners();
      return;
    }
    await _applyInvokeOutcome(outcome, entity, backend);
  }

  /// Handles the reply to a [ValueQuestion]. Mirrors `answerSpoken()` in
  /// actions.js.
  Future<void> answerValue(String text) async {
    final backend = _nav.backend;
    final entity = _nav.entity;
    if (backend == null || entity == null) {
      _question = null;
      notifyListeners();
      return;
    }
    final outcome = await _actions.answerValue(text, backend, entity);
    _question = null;
    if (outcome == null) {
      notifyListeners();
      return;
    }
    await _applyInvokeOutcome(outcome, entity, backend);
  }

  /// Turns an [InvokeOutcome] into [notice]/[question] and, where the
  /// contract requires it, a re-fetch of the document the action came
  /// from — mirrors `afterAction`/`afterActionSuccess`/`afterActionError`
  /// in actions.js, the wiring `action_service.dart`'s module comment
  /// leaves to this file.
  Future<void> _applyInvokeOutcome(
    InvokeOutcome outcome,
    Entity originEntity,
    Backend backend,
  ) async {
    switch (outcome) {
      case InvokeRefused(:final reason):
        _notice = ActionRefused(_pendingActionLabel, reason);
        notifyListeners();
      case InvokeNeedsValue(label: final fieldLabel):
        // Still pending — now awaiting a value instead of a yes/no.
        _question = ValueQuestion(fieldLabel);
        notifyListeners();
      case InvokeSucceeded(:final response):
        await _handleSuccess(response, originEntity, backend);
      case InvokeFailed(:final failure):
        await _handleFailure(failure);
    }
  }

  /// The request was sent and the server answered without a transport
  /// failure. Mirrors `afterActionSuccess`: hands the response to
  /// [LiveService] first, and only re-fetches immediately when nothing
  /// picked it up to watch — `pebble/docs/DESIGN.md`'s "never carry a
  /// document across an action", weighed against "do not claim a job
  /// finished" when it plainly has not.
  Future<void> _handleSuccess(
    HttpResponse response,
    Entity originEntity,
    Backend backend,
  ) async {
    final resultEntity = Entity.fromJson(response.entity);
    _notice = ActionOutcomeReported(
      _pendingActionLabel,
      message: LiveService.resultText(resultEntity, response.status),
      body: _describeResult(resultEntity, response.status),
    );

    final label = _pendingActionLabel;
    final started = _live.start(
      backend,
      Origin(entity: originEntity, href: _nav.href ?? ''),
      ActionOutcome(status: response.status, entity: resultEntity),
      LiveHandlers(
        onProgress: (entity) => _onLiveProgress(label, entity),
        onDone: (entity) => _onLiveDone(label, entity),
        onGiveUp: () => _onLiveGiveUp(label),
      ),
    );
    if (started) {
      // Still running: the job, not the document, changed. There is
      // nothing new to re-fetch until the watch above says so.
      notifyListeners();
      return;
    }
    await _nav.refresh();
    notifyListeners();
  }

  /// The request failed. Mirrors `afterActionError`: a conflict is
  /// re-fetched (the state acted on was stale; the remedy is to look
  /// again — the one retry the contract allows already happened inside
  /// `action_service.dart` before this outcome came back, see
  /// [InvokeFailed]'s doc comment). Anything else — auth, unreachable,
  /// server, client, parse — tells us nothing new about the document, and
  /// re-fetching would only fail the same way or silently paper over a
  /// question that is still real.
  Future<void> _handleFailure(Failure failure) async {
    _notice = ActionFailed(_pendingActionLabel, failure);
    if (failure.kind == FailureKind.conflict) {
      await _nav.refresh();
    }
    notifyListeners();
  }

  /// A step reported while still watching. Mirrors `liveHandlers.onProgress`
  /// in actions.js. The text comes from [LiveService.progressText], which owns
  /// the `step`/`state` property names — this coordinator just sets the
  /// notice kind + string, so a property-name change on the server drifts
  /// against the watch logic in one place, not two.
  void _onLiveProgress(String label, Entity entity) {
    _notice = ActionProgress(label, LiveService.progressText(entity));
    notifyListeners();
  }

  /// The job stopped. Mirrors `liveHandlers.onDone`: the document it acted
  /// on is worth re-reading now, because that is where the effect shows. The
  /// message comes from [LiveService.doneText] and the body from
  /// `render_service.describe` — both owned by the live/render layers (this
  /// coordinator just sets the notice), so neither the job-state vocabulary
  /// nor the property rendering lives here.
  Future<void> _onLiveDone(String label, Entity entity) async {
    _notice = ActionOutcomeReported(
      label,
      message: LiveService.doneText(entity),
      body: render.describe(entity),
    );
    await _nav.refresh();
    notifyListeners();
  }

  /// Watching was abandoned. Mirrors `liveHandlers.onGiveUp`: not a claim
  /// the job failed, only that this app stopped asking — and the document
  /// is still re-fetched, because whatever the action changed before this
  /// app gave up watching is still worth seeing.
  Future<void> _onLiveGiveUp(String label) async {
    _notice = ActionGaveUp(label);
    await _nav.refresh();
    notifyListeners();
  }

  /// A no-op once [dispose] has run, so an async tail — a live poll's
  /// [_onLiveDone]/[_onLiveGiveUp], or an action outcome's
  /// [_handleSuccess]/[_handleFailure], whose `await` completed after the
  /// coordinator was disposed — cannot trip [ChangeNotifier]'s "used after
  /// dispose" assert. See [_disposed].
  @override
  void notifyListeners() {
    if (_disposed) {
      return;
    }
    super.notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _nav.removeListener(notifyListeners);
    _clock.dispose();
    _live.stop();
    if (_ownsNav) {
      _nav.dispose();
    }
    super.dispose();
  }

  /// Tells this coordinator's idle-refresh clock whether the app is in the
  /// foreground — called by a framework-bound lifecycle watcher (a
  /// `WidgetsBindingObserver` in a widget, e.g. `DocumentScreen`) that
  /// translates `AppLifecycleState` into this pure-Dart bool. See
  /// `idle_refresh_clock.dart`'s module comment for why the framework coupling
  /// lives in the widget layer, not here: this service and `NavService` never
  /// import the widgets framework.
  void setAppForeground(bool inForeground) =>
      _clock.setInForeground(inForeground);
}

/// The body of an action-outcome notice: the response's properties, rendered
/// generically through `render_service.describe` so nothing the server sent
/// is thrown away, falling back to the bare status when the response carried
/// no properties at all. The property rendering lives in `render_service.dart`
/// (rendering is that module's job, not a coordinator's —
/// `pebble/docs/DESIGN.md`, "Rendering does not interpret"); the status
/// fallback is this file's because it is the action-response context only
/// the coordinator holds. Mirrors `describeResult()` in `actions.js`.
String _describeResult(Entity entity, int status) {
  final described = render.describe(entity);
  return described.isNotEmpty ? described : 'HTTP $status';
}
