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
/// `flutter/AGENTS.md` section 4 maps them: the idle-refresh clock
/// (`nav.js`'s `scheduleIdle`/`idleRefresh`), the action policy — filling an
/// action's fields, confirming it, retrying a `409` — and everything to do
/// with a notice or an overlay laid on top of a frame by an action's outcome
/// (`nav.js`'s `setNotice`/`applyNotice`/`overlay`), following work that
/// outlives its request (`live.js`), and the backend picker and saved
/// shortcuts (`quick.js`, already `home_screen.dart`'s job on this port —
/// see that file's module comment). A coordinator wiring this module to
/// those (`nav.js`'s `session.js`) is its own future task too.
library;

import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/result.dart';
import '../models/siren.dart';
import 'http_service.dart';
import 'render_service.dart' as render;
import 'settings_service.dart';

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

/// Owns the navigation stack and every fetch that changes it.
///
/// [HttpService] is injected, exactly as `render_service.dart` and
/// `http_service.dart` themselves are composed with injected collaborators
/// — a test wires a [HttpService] built on `package:http`'s `MockClient`
/// (see `test/services/http_service_test.dart`), never a real socket.
class NavService extends ChangeNotifier {
  NavService({required HttpService http}) : _http = http;

  final HttpService _http;

  final List<_StackFrame> _stack = [];

  Backend? _backend;
  DocumentState _state = DocumentState.ok;
  Failure? _failure;

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
  render.RenderedDocument? get document =>
      _stack.isEmpty ? null : render.document(_stack.last.entity, _stack.last.title);

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
    notifyListeners();

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
          notifyListeners();
          return;
        }
        _push(entity, href: backend.baseUrl, title: backend.name);
        notifyListeners();
        await _followStart(backend, entity);
      case Err(failure: final failure):
        _state = stateFor(failure.kind);
        _failure = failure;
        notifyListeners();
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
      notifyListeners();
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
    notifyListeners();
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
    notifyListeners();
  }

  /// Performs one fetch and applies its outcome to the stack. Shared by
  /// [fetch] (push), [refresh] (replace) and [_followStart] (push) — mirrors
  /// `nav.js`'s single `fetch(href, title, replace)`.
  ///
  /// A failure never touches the stack: [document] keeps reading whatever
  /// was there before this call, which is precisely the invariant this
  /// module exists to protect — see the module comment.
  Future<void> _fetch(String href, {required String title, required bool replace}) async {
    final backend = _backend;
    if (backend == null) {
      return;
    }
    _state = DocumentState.loading;
    _failure = null;
    notifyListeners();

    final result = await _http.get(backend, href);
    switch (result) {
      case Ok(value: final response):
        final entity = Entity.fromJson(response.entity);
        if (replace && _stack.isNotEmpty) {
          _stack[_stack.length - 1] = _StackFrame(entity: entity, href: href, title: title);
        } else {
          _push(entity, href: href, title: title);
        }
        _state = DocumentState.ok;
        _failure = null;
      case Err(failure: final failure):
        _state = stateFor(failure.kind);
        _failure = failure;
    }
    notifyListeners();
  }

  void _push(Entity entity, {String? href, String title = ''}) {
    _stack.add(_StackFrame(entity: entity, href: href, title: title));
    _state = DocumentState.ok;
    _failure = null;
  }
}
