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
///  - **Wiring `nav_service.dart`'s idle-refresh clock to
///    `action_service.dart`'s pending question.** [NavService] must not
///    depend on [ActionService] (see its module comment on
///    [NavService.setActionPendingCheck]), and [ActionService] already sits
///    on top of navigation-adjacent concepts (a [Backend], an [Entity]).
///    This file is the one place allowed to know about both, so the
///    constructor runs `nav.setActionPendingCheck(() => actions.hasPending)`
///    once, exactly as `session.js` runs the equivalent `nav.js` call.
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
/// **What does not carry over**, beyond the AppMessage/PebbleKit layer this
/// whole port has no equivalent of (`flutter/AGENTS.md` section 4):
///
///  - `saveQuick`/`removeQuick`/`runQuick` and the backend picker
///    (`quick.js`, `session.js`'s `listBackends`/`openBackend` passthrough).
///    Saved shortcuts are their own future task (`quick_service.dart`, tasks
///    w11/x11) and do not exist on this port yet; until they do, the opening
///    screen talks to [SettingsService] directly (see `home_screen.dart`'s
///    module comment) and there is nothing for this file to compose.
///  - `noteInbox`, `setSeq`, `dismissed` — purely about the watch's overlay/
///    sequence protocol over AppMessage, which has no analogue on a single
///    device with no separate window stack. The one behavioural gap
///    `dismissed()` existed to close (a confirm overlay the watch closed by
///    itself without answering must still hold the idle clock off) already
///    holds here for free: [ActionService.hasPending] stays true across
///    exactly that gap regardless of whether a Flutter screen is still
///    showing the sheet — see [NavService]'s module comment.
library;

import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/siren.dart';
import 'action_service.dart';
import 'http_service.dart';
import 'live_service.dart';
import 'nav_service.dart';
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

/// Composes [NavService], [ActionService] and [LiveService] into the single
/// service the UI layer talks to — see the module comment for what belongs
/// here and why.
///
/// [HttpService] is injected the same way every other service in this app
/// is; [nav]/[actions]/[live] are injected too, and default to plain
/// instances built on [http], so a test can substitute a [NavService] with a
/// fake timer (mirrors `nav_service_test.dart`'s `FakeTimers`) or a
/// [LiveService] with a fake clock (mirrors `live_service_test.dart`'s
/// `FakeClock`) without reaching inside this class.
class SessionService extends ChangeNotifier {
  SessionService({
    required HttpService http,
    NavService? nav,
    ActionService? actions,
    LiveService? live,
  }) : _nav = nav ?? NavService(http: http),
       _actions = actions ?? ActionService(http: http),
       _live = live ?? LiveService(http: http),
       _ownsNav = nav == null {
    // The one piece of cross-module wiring neither nav_service.dart nor
    // action_service.dart can do to itself — see the module comment.
    _nav.setActionPendingCheck(() => _actions.hasPending);
    // Forwarded, not duplicated: every navigation event nav_service.dart
    // already tracks (a fetch starting, landing or failing; a stack push,
    // pop or idle refresh) is exactly a change this coordinator's own
    // listeners need to hear about too.
    _nav.addListener(notifyListeners);
  }

  final NavService _nav;
  final ActionService _actions;
  final LiveService _live;

  /// Whether this service constructed [_nav] itself (and so owns disposing
  /// it) or was handed one built elsewhere (a test's, most often) — a
  /// service this file did not create is not this file's to tear down.
  final bool _ownsNav;

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
      message: _resultBanner(resultEntity, response.status),
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
  /// in actions.js: the server's own wording for the step, falling back to
  /// its `state` when it did not send one.
  void _onLiveProgress(String label, Entity entity) {
    final step = entity.properties['step'];
    final text = (step is String && step.isNotEmpty)
        ? step
        : '${entity.properties['state']}';
    _notice = ActionProgress(label, text);
    notifyListeners();
  }

  /// The job stopped. Mirrors `liveHandlers.onDone`: the document it acted
  /// on is worth re-reading now, because that is where the effect shows.
  Future<void> _onLiveDone(String label, Entity entity) async {
    final state = entity.properties['state'];
    final message = (state is String && state.isNotEmpty) ? state : 'Done';
    _notice = ActionOutcomeReported(
      label,
      message: message,
      body: _describeEntity(entity),
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

  @override
  void dispose() {
    _nav.removeListener(notifyListeners);
    _live.stop();
    if (_ownsNav) {
      _nav.dispose();
    }
    super.dispose();
  }
}

/// The server's own word for what an action produced: its `state` property,
/// or "Accepted"/"Done" when it did not send one — the same fallback
/// `resultBanner()` uses in actions.js, since a `202` and a `200` otherwise
/// mean different things worth saying even when the body is silent about it.
String _resultBanner(Entity entity, int status) {
  final state = entity.properties['state'];
  if (state is String && state.isNotEmpty) {
    return state;
  }
  return status == 202 ? 'Accepted' : 'Done';
}

/// Renders every property of [entity] generically, so a value the server put
/// in the response — a job id, an explanation — is never thrown away.
/// Mirrors `describeEntity()` in actions.js.
String _describeEntity(Entity entity) {
  final parts = <String>[];
  entity.properties.forEach((key, value) {
    parts.add('$key: ${render.text(value)}');
  });
  return parts.join('   ');
}

/// [_describeEntity], falling back to the bare status when the response
/// carried no properties at all. Mirrors `describeResult()` in actions.js.
String _describeResult(Entity entity, int status) {
  final described = _describeEntity(entity);
  return described.isNotEmpty ? described : 'HTTP $status';
}
