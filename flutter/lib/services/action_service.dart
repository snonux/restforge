/// Siren action policy: the safe/unsafe method split, filling an action's
/// fields, phrasing the confirmation the user is asked for, and invoking it.
///
/// This is the Dart port of the parts of `pebble/src/pkjs/actions.js` this
/// task owns — see that file's header for the full reasoning, most of which
/// carries over unchanged. Left to their own tasks, exactly as
/// `flutter/AGENTS.md` section 4 maps them: the one retry the hypermedia
/// contract allows on a `409` (`m11`), and asking out loud for a required
/// field with no default (`n11`, the `pending.awaiting`/`spoken` branch of
/// `actions.js`). Until `n11` lands, a field with neither a checkbox nor a
/// server-supplied default is simply left unfilled — see [fieldValues] — the
/// "or refused" half of `pebble/docs/DESIGN.md`'s "Do not invent a value",
/// not the "asked for out loud" half.
///
/// **Ask before acting** (`pebble/docs/DESIGN.md`): anything whose method is
/// not in [safeMethods] gets a confirmation before anything is sent.
/// [safeMethods] is RFC 9110's safe/unsafe division, not a blocklist of
/// scary-sounding action names — an action called "detonate" over `GET` is
/// still safe by this rule, and one called "refresh" over `POST` still asks.
///
/// **The pending action never reaches the UI.** [ask] and [answer] hand back
/// a rendered [ConfirmationRequired] — a heading and a body sentence, both
/// plain strings — never the href or method behind it. Those stay in
/// [ActionService]'s own private state ([_PendingAction]) for the same
/// reason the watch never learns an href in the Pebble build: the UI layer
/// only has to be trusted with what it shows on screen, not with an address
/// it could otherwise be tricked into hitting.
///
/// **What this file does not own.** Sending the request is the last thing
/// this service does with it — deciding what happens next (re-fetching the
/// document the action was invoked from, watching a `202`/still-running
/// reply with `LiveService`) is a coordinator's job, mirroring `session.js`
/// wiring `nav.js` and `actions.js` together on the watch side. That
/// coordinator (`lib/services/session.dart` in the module map) does not
/// exist yet, so [InvokeOutcome] simply reports what came back and leaves
/// the next step to whoever calls this service.
library;

import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/result.dart';
import '../models/siren.dart';
import 'http_service.dart';
import 'settings_service.dart';

/// HTTP methods that only read. RFC 9110 calls these safe, and the division
/// is the whole basis for asking before acting: anything outside this set is
/// a request to change something. Carried over unchanged from `SAFE_METHODS`
/// in `actions.js`.
const Set<String> safeMethods = {'GET', 'HEAD', 'OPTIONS', 'TRACE'};

/// Whether [method] is one of [safeMethods]. Case-insensitive even though
/// `Action.method` (`siren.dart`) is already normalised to uppercase, so a
/// caller never has to know that to ask the question safely.
bool isSafeMethod(String method) => safeMethods.contains(method.toUpperCase());

/// What asking about an action found out. A caller `switch`es over this
/// exhaustively rather than checking a status flag that could silently fail
/// to match.
sealed class AskOutcome {
  const AskOutcome();
}

/// The server does not currently offer an action by this name. A real
/// answer — it means this cannot be done right now — not something to route
/// around by inventing a request. Mirrors the withdrawn-action branch of
/// `askAction()` in actions.js.
class ActionNotOffered extends AskOutcome {
  final String name;
  const ActionNotOffered(this.name);
}

/// The method is unsafe, so nothing has been sent yet. [heading] and [body]
/// are exactly what a caller shows on screen and nothing more — see the
/// module comment on why the href behind this question never appears here.
class ConfirmationRequired extends AskOutcome {
  final String heading;
  final String body;
  const ConfirmationRequired({required this.heading, required this.body});
}

/// The method was safe ([isSafeMethod]), so it went straight through without
/// asking — Siren allows a `GET` action, and stopping to confirm a read
/// would be noise. [outcome] is exactly what invoking it produced.
class ActionInvoked extends AskOutcome {
  final InvokeOutcome outcome;
  const ActionInvoked(this.outcome);
}

/// What sending a (confirmed, or safe) action produced.
sealed class InvokeOutcome {
  const InvokeOutcome();
}

/// Nothing was sent: the action named by a pending question is no longer
/// offered on the document passed to [ActionService.answer] — it may have
/// been withdrawn, or the document may have moved on since [ActionService.ask]
/// was called. That is a real answer, not a bug to route around, mirroring
/// `invoke()`'s re-check of `nav.top().entity` in actions.js.
class InvokeRefused extends InvokeOutcome {
  final String reason;
  const InvokeRefused(this.reason);
}

/// The request was sent and the server answered without a transport
/// failure — a `202` included, same as [HttpService]. What that means (a
/// job still running, a document to re-fetch) is for the caller to decide;
/// see the module comment.
class InvokeSucceeded extends InvokeOutcome {
  final HttpResponse response;
  const InvokeSucceeded(this.response);
}

/// The request failed — a `409` included. This port does not retry a
/// conflict: that is the one bounded exception the contract allows, and it
/// is its own task (`m11`). Every failure, conflict or otherwise, is
/// reported here the same way; deciding whether to re-fetch is left to the
/// caller, exactly as `pebble/docs/DESIGN.md`'s "never carry a document
/// across an action" describes.
class InvokeFailed extends InvokeOutcome {
  final Failure failure;
  const InvokeFailed(this.failure);
}

/// The action awaiting confirmation. Carries the href and method a UI must
/// never see — see the module comment — so it is deliberately private and
/// never returned from any public method of [ActionService].
class _PendingAction {
  const _PendingAction({
    required this.name,
    required this.href,
    required this.method,
  });
  final String name;
  final String href;
  final String method;
}

/// Fills an action's fields generically. Nothing here knows what any field
/// means:
///
///  - a checkbox carries the user's confirmation, because a required
///    checkbox *is* the confirmation this app already asked for;
///  - anything else takes the default the server declared in `value`.
///
/// A field with neither — no checkbox, no default — is left out of the
/// result rather than guessed at, because `siren.dart`'s [Field] does not
/// yet model which fields the server marked required (that lands with the
/// required-field prompt, `n11`) — there is nothing here to safely guess
/// with. Mirrors `fieldValues()` in actions.js, minus the `spoken`/`missing`
/// paths that task also owns.
Map<String, String> fieldValues(Action action, {required bool confirmed}) {
  final values = <String, String>{};
  for (final field in action.fields) {
    if (field.name.isEmpty) {
      continue;
    }
    if (field.type == 'checkbox') {
      values[field.name] = confirmed ? 'true' : 'false';
    } else if (field.value != null) {
      values[field.name] = field.value.toString();
    }
  }
  return values;
}

/// What the user reads before confirming an unsafe action. A checkbox
/// field's title is preferred over everything else, because that sentence
/// is the server explaining the consequence — it is written for exactly
/// this moment and nothing this app could add would improve it. Mirrors
/// `confirmationText()` in actions.js; the original prefers a checkbox only
/// when it is also marked required, a qualifier `siren.dart`'s [Field]
/// cannot express yet (see [fieldValues]), so here any checkbox carrying a
/// title is treated as the server's own confirmation wording.
String confirmationText(Action action) {
  for (final field in action.fields) {
    if (field.type == 'checkbox' &&
        field.title != null &&
        field.title!.isNotEmpty) {
      return field.title!;
    }
  }
  return '${action.label}?  ${action.method} to this server.';
}

/// Fills an action's fields, phrases its confirmation, and sends it.
///
/// [HttpService] is injected, exactly as `nav_service.dart` and
/// `live_service.dart` are composed with injected collaborators — a test
/// wires an [HttpService] built on `package:http`'s `MockClient`, never a
/// real socket. [Backend] and the current [Entity] are passed into [ask] and
/// [answer] by the caller rather than read from a held reference to
/// `NavService`: this service has no navigation state of its own (mirroring
/// `actions.js` sitting on top of `nav.js` rather than reaching into it) and
/// no coordinator wiring the two together exists yet — see the module
/// comment.
class ActionService {
  ActionService({required HttpService http, void Function(String message)? log})
    : _http = http,
      _log = log ?? debugPrint;

  final HttpService _http;
  final void Function(String message) _log;

  _PendingAction? _pending;

  /// Whether a question is currently awaiting an answer. Mirrors
  /// `hasPending()` in actions.js — a future idle-refresh timer needs to
  /// know this before it re-fetches out from under a question nobody has
  /// answered yet, the same reason `nav.js`'s idle refresh consults it there.
  bool get hasPending => _pending != null;

  /// Abandons whatever question is pending, without sending anything.
  /// Mirrors `cancelPending()` in actions.js — called when the screen an
  /// action was offered on is left before it is answered, so the question
  /// does not outlive the document it was about.
  void cancelPending() {
    _pending = null;
  }

  /// Looks up [name] on [entity] and decides whether it needs confirming.
  /// Mirrors `askAction()` in actions.js.
  Future<AskOutcome> ask(Backend backend, Entity entity, String name) async {
    final action = entity.actionByName(name);
    if (action == null) {
      // The server does not offer this right now. That is a real answer, so
      // it is reported rather than routed around by inventing a request.
      _log('action "$name" is no longer offered');
      return ActionNotOffered(name);
    }
    _pending = _PendingAction(
      name: name,
      href: action.href,
      method: action.method,
    );
    if (isSafeMethod(action.method)) {
      return ActionInvoked(await _invoke(backend, entity, confirmed: true));
    }
    return ConfirmationRequired(
      heading: action.label,
      body: confirmationText(action),
    );
  }

  /// Handles the reply to a confirmation [ask] raised. Mirrors the confirmed
  /// half of `answer()` in actions.js — the `pending.awaiting`/`spoken` half,
  /// for a value asked for out loud, is a separate task (`n11`) and has no
  /// counterpart here yet.
  ///
  /// Returns null when nothing was sent: either [confirmed] is false, or the
  /// question had already been superseded (answered or cancelled) before
  /// this call — both are "nothing to do", not an error.
  ///
  /// [entity] is the document currently on screen, looked up by name again
  /// rather than trusting whatever [Action] was found when [ask] was called
  /// — mirrors `invoke()` re-reading `nav.top().entity` in actions.js, in
  /// case the document moved on while the question was still on screen.
  Future<InvokeOutcome?> answer(
    bool confirmed,
    Backend backend,
    Entity entity,
  ) async {
    if (_pending == null) {
      return null;
    }
    if (!confirmed) {
      _log('declined "${_pending!.name}"');
      _pending = null;
      return null;
    }
    return _invoke(backend, entity, confirmed: true);
  }

  /// Sends the pending action, if it is still offered on [entity]. Shared by
  /// the safe-method branch of [ask] and by [answer] — mirrors `invoke()` in
  /// actions.js.
  Future<InvokeOutcome> _invoke(
    Backend backend,
    Entity entity, {
    required bool confirmed,
  }) async {
    final name = _pending!.name;
    final action = entity.actionByName(name);
    if (action == null) {
      _pending = null;
      return const InvokeRefused('not offered');
    }

    final values = fieldValues(action, confirmed: confirmed);
    final href = _pending!.href;
    final method = _pending!.method;
    _pending = null;

    _log('invoking "$name" ($method)');
    final result = await _http.request(backend, href, method, values);
    return switch (result) {
      Ok(value: final response) => InvokeSucceeded(response),
      Err(failure: final failure) => InvokeFailed(failure),
    };
  }
}
