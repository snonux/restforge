/// Siren action policy: the safe/unsafe method split, filling an action's
/// fields, phrasing the confirmation the user is asked for, and invoking it.
///
/// This is the Dart port of the parts of `pebble/src/pkjs/actions.js` this
/// task owns — see that file's header for the full reasoning, most of which
/// carries over unchanged.
///
/// **Never carry a document across an action** (`pebble/docs/DESIGN.md`,
/// same section): sending a request is the last thing this service does
/// with the document it was invoked from — [InvokeSucceeded] carries the
/// raw [HttpResponse], never a parsed, ready-to-render [Entity], so nothing
/// here can tempt a caller into showing the action's own response in place
/// of re-fetching. The one exception the contract allows for a `409` is a
/// required checkbox the user actually ticked, re-sent once, within a
/// minute — implemented entirely inside [ActionService._invoke] (see
/// `_ConfirmedRetry` and `ActionService._retryable`), so the
/// retry never reaches a caller as a separate step: by the time
/// [InvokeSucceeded] or [InvokeFailed] comes back, the one retry the
/// contract allows has already happened, and that outcome is final. A `409`
/// that is not eligible for it is reported the same as any other failure —
/// re-fetching what it means is still the caller's decision (see below),
/// but retrying the exact same request is never on the table again.
///
/// **Do not invent a value** (`pebble/docs/DESIGN.md`): a required field
/// with no default and no confirmation to stand in for it is asked for out
/// loud, or the action is refused — never guessed at. [fillFields] is where
/// that decision is made: a checkbox fills itself from the confirmation
/// already given, a field with a server-supplied default takes it as-is,
/// and a required field with neither becomes [FieldValueMissing] (the first
/// one) or [FieldsRefused] (a second one — dictating several fields one at a
/// time is worse than saying plainly this cannot be done without asking).
/// [ActionService.answerValue] is the reply to that question, mirroring
/// `answerSpoken()` in actions.js. Only [Field.required] (`siren.dart`)
/// makes any of this possible to tell apart from an optional field with no
/// value — see that file for how the server signals it.
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

/// Nothing was sent, with [reason] explaining why — never a bug to route
/// around by inventing a request. Three cases end up here, all mirrored
/// from actions.js:
///
///  - the action named by a pending question is no longer offered on the
///    document passed to [ActionService.answer] or [ActionService.answerValue]
///    — it may have been withdrawn, or the document may have moved on since
///    [ActionService.ask] was called (`invoke()`'s re-check of
///    `nav.top().entity`);
///  - more than one required field has no default and no confirmation to
///    fill it — asking out loud for one is fine, dictating several one at a
///    time is not (the `problem` branch of `fieldValues()`);
///  - a value asked for out loud via [InvokeNeedsValue] came back empty
///    (`answerSpoken()`'s "nothing was heard" case).
class InvokeRefused extends InvokeOutcome {
  final String reason;
  const InvokeRefused(this.reason);
}

/// A required field on the pending action has no default and no
/// confirmation to stand in for it (`pebble/docs/DESIGN.md`, "Do not invent
/// a value"), so it must be asked for out loud rather than guessed at.
/// [fieldName] identifies the field for the eventual
/// [ActionService.answerValue] call; [label] is what to show on screen —
/// the server's own wording ([Field.title]) when it gave one, else the
/// field's name. Mirrors `invokeAskForValue()` in actions.js.
class InvokeNeedsValue extends InvokeOutcome {
  final String fieldName;
  final String label;
  const InvokeNeedsValue({required this.fieldName, required this.label});
}

/// The request was sent and the server answered without a transport
/// failure — a `202` included, same as [HttpService]. What that means (a
/// job still running, a document to re-fetch) is for the caller to decide;
/// see the module comment.
class InvokeSucceeded extends InvokeOutcome {
  final HttpResponse response;
  const InvokeSucceeded(this.response);
}

/// The request failed — a `409` included. By the time this is returned,
/// the one bounded retry the contract allows (a confirmed required
/// checkbox, re-sent once, within a minute — see [ActionService._invoke])
/// has already been taken if it applied, and this [failure] is what came
/// back after that, not before it. Every failure, conflict or otherwise, is
/// reported here the same way; deciding whether to re-fetch is left to the
/// caller, exactly as `pebble/docs/DESIGN.md`'s "never carry a document
/// across an action" describes — a `409` here still means "go look", never
/// "try again".
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
    this.awaitingField,
  });
  final String name;
  final String href;
  final String method;

  /// Set once a required field has been asked for out loud and the pending
  /// question has changed from "yes/no" to "what value" — mirrors
  /// `pending.awaiting` in actions.js. Null the rest of the time.
  final String? awaitingField;

  /// A copy with [awaitingField] set, once [fillFields] finds exactly one
  /// field to ask for out loud.
  _PendingAction awaiting(String fieldName) => _PendingAction(
    name: name,
    href: href,
    method: method,
    awaitingField: fieldName,
  );
}

/// A value given for one field by voice/text, matched to it by name.
/// Mirrors the `spoken` parameter threaded through `fieldValues()`/
/// `invoke()` in actions.js.
class FieldAnswer {
  final String name;
  final String text;
  const FieldAnswer({required this.name, required this.text});
}

/// Fills an action's fields generically. Nothing here knows what any field
/// means:
///
///  - a checkbox carries the user's confirmation, because a required
///    checkbox *is* the confirmation this app already asked for;
///  - a value matching [spoken]'s field name is what the user just said,
///    asked for by [fillFields] on an earlier call;
///  - anything else takes the default the server declared in `value`.
///
/// A required field with none of those is left out of the result here —
/// this function only fills what it safely can. Deciding whether that
/// omission means asking out loud or refusing the action is [fillFields]'s
/// job, not this one, so a direct caller (see the `fieldValues` group in
/// the test file) always gets a plain map back. Mirrors the field-filling
/// loop in `fieldValues()` in actions.js.
Map<String, String> fieldValues(
  Action action, {
  required bool confirmed,
  FieldAnswer? spoken,
}) {
  final values = <String, String>{};
  for (final field in action.fields) {
    if (field.name.isEmpty) {
      continue;
    }
    if (field.type == 'checkbox') {
      values[field.name] = confirmed ? 'true' : 'false';
    } else if (spoken != null && spoken.name == field.name) {
      values[field.name] = spoken.text;
    } else if (field.value != null) {
      values[field.name] = field.value.toString();
    }
  }
  return values;
}

/// What [fillFields] decided: either every field got a value, or a required
/// one didn't and filling stopped there. A caller `switch`es over this
/// exhaustively, same as every other outcome type in this file.
sealed class FieldFillOutcome {
  const FieldFillOutcome();
}

/// Every field — required or not — has a value, [values] included.
class FieldsFilled extends FieldFillOutcome {
  final Map<String, String> values;
  const FieldsFilled(this.values);
}

/// Exactly one required field has neither a checkbox nor a default nor a
/// matching [FieldAnswer] — the one case this app can ask out loud for
/// rather than refuse.
class FieldValueMissing extends FieldFillOutcome {
  final Field field;
  const FieldValueMissing(this.field);
}

/// More than one required field is missing a value. Asking out loud for one
/// field at a time is fine; dictating several is worse than plainly
/// refusing, so the whole action is refused rather than asking for the
/// first and silently dropping the rest.
class FieldsRefused extends FieldFillOutcome {
  final String reason;
  const FieldsRefused(this.reason);
}

/// Decides whether [action]'s fields can all be sent as-is, whether exactly
/// one required field needs asking for out loud, or whether the action must
/// be refused outright (`pebble/docs/DESIGN.md`, "Do not invent a value").
/// Built on top of [fieldValues] rather than duplicating its fill logic:
/// this function only adds the question "is anything required still
/// missing", scanning [Action.fields] once more against the map
/// [fieldValues] already produced. Mirrors the `missing`/`problem` branches
/// of `fieldValues()` in actions.js.
FieldFillOutcome fillFields(
  Action action, {
  required bool confirmed,
  FieldAnswer? spoken,
}) {
  final values = fieldValues(action, confirmed: confirmed, spoken: spoken);
  Field? missing;
  for (final field in action.fields) {
    if (field.name.isEmpty ||
        !field.required ||
        values.containsKey(field.name)) {
      continue;
    }
    if (missing == null) {
      // Asked for by voice rather than refused -- but only the first one.
      missing = field;
    } else {
      return const FieldsRefused(
        'needs values for more than one field, which cannot be asked for '
        'one at a time',
      );
    }
  }
  if (missing != null) {
    return FieldValueMissing(missing);
  }
  return FieldsFilled(values);
}

/// What the user reads before confirming an unsafe action. A *required*
/// checkbox's title is preferred over everything else, because that
/// sentence is the server explaining the consequence — it is written for
/// exactly this moment and nothing this app could add would improve it.
/// Mirrors `confirmationText()` in actions.js: a checkbox that merely
/// exists but isn't required does not stand in for the generic fallback
/// sentence, the same distinction [fillFields] draws when deciding what is
/// safe to send without asking.
String confirmationText(Action action) {
  for (final field in action.fields) {
    if (field.type == 'checkbox' &&
        field.required &&
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
  ActionService({
    required HttpService http,
    void Function(String message)? log,
    DateTime Function()? now,
  }) : _http = http,
       _log = log ?? debugPrint,
       _now = now ?? DateTime.now;

  final HttpService _http;
  final void Function(String message) _log;

  /// Injected the same way `live_service.dart`'s clock is (see
  /// `test/services/live_service_test.dart`'s `FakeClock`), so a test can
  /// place a confirmation's age exactly on either side of
  /// [confirmationRetryTtl] without a real minute passing.
  final DateTime Function() _now;

  _PendingAction? _pending;

  /// A confirmation the user gave, kept just long enough to answer a server
  /// that comes back `409` after it — see [_ConfirmedRetry] and
  /// [_retryable].
  _ConfirmedRetry? _confirmedRetry;

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

  /// The narrow question the contract permits: did the user confirm *this*
  /// action, by ticking a required checkbox, within the last
  /// [confirmationRetryTtl]? Anything else -- no confirmation remembered, a
  /// confirmation for a different action, or one old enough to have
  /// expired -- is a no. Mirrors `retryable()` in actions.js.
  bool _retryable(String name) {
    final retry = _confirmedRetry;
    if (retry == null || retry.name != name) {
      return false;
    }
    return _now().difference(retry.at) < confirmationRetryTtl;
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
  /// for a value asked for out loud, is [answerValue] instead of a second
  /// parameter here, so a caller's two questions ("yes/no" vs. "what
  /// value") stay two distinct, statically-typed calls rather than one
  /// dynamically-dispatched one.
  ///
  /// Returns null when nothing was sent: either [confirmed] is false, or the
  /// question had already been superseded (answered or cancelled) before
  /// this call — both are "nothing to do", not an error. May also return
  /// [InvokeNeedsValue] — see [fillFields] — in which case nothing has been
  /// sent yet and the question is still pending, now awaiting [answerValue]
  /// instead.
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

  /// Handles the reply to the "say a value" prompt [InvokeNeedsValue]
  /// raised. Mirrors `answerSpoken()` in actions.js.
  ///
  /// An empty [text] is not an answer: "confirmed but nothing said" is
  /// reported as [InvokeRefused] rather than sent as an empty string, the
  /// same guard `answerSpoken()` has ("Nothing was heard, so nothing was
  /// sent"). Returns null when there is no value currently being asked for
  /// — the question was answered or cancelled already, or [ask]/[answer]
  /// never raised [InvokeNeedsValue] in the first place — the same
  /// "nothing to do" contract [answer] uses.
  Future<InvokeOutcome?> answerValue(
    String text,
    Backend backend,
    Entity entity,
  ) async {
    final pending = _pending;
    if (pending == null || pending.awaitingField == null) {
      return null;
    }
    if (text.isEmpty) {
      _pending = null;
      return const InvokeRefused('Nothing was heard, so nothing was sent.');
    }
    final spoken = FieldAnswer(name: pending.awaitingField!, text: text);
    return _invoke(backend, entity, confirmed: true, spoken: spoken);
  }

  /// Sends the pending action, if it is still offered on [entity] and every
  /// required field can be filled. Shared by the safe-method branch of
  /// [ask], by [answer] and by [answerValue] — mirrors `invoke()` in
  /// actions.js.
  Future<InvokeOutcome> _invoke(
    Backend backend,
    Entity entity, {
    required bool confirmed,
    FieldAnswer? spoken,
  }) async {
    final name = _pending!.name;
    final action = entity.actionByName(name);
    if (action == null) {
      _pending = null;
      return const InvokeRefused('not offered');
    }

    final filled = fillFields(action, confirmed: confirmed, spoken: spoken);
    switch (filled) {
      case FieldsRefused(:final reason):
        _pending = null;
        return InvokeRefused(reason);
      case FieldValueMissing(:final field):
        // Keep the question pending -- now awaiting a value for this field
        // rather than a yes/no -- instead of clearing it as every other
        // branch does; see the module comment and answerValue.
        _pending = _pending!.awaiting(field.name);
        return InvokeNeedsValue(fieldName: field.name, label: field.label);
      case FieldsFilled(:final values):
        final href = _pending!.href;
        final method = _pending!.method;
        _pending = null;
        _log('invoking "$name" ($method)');
        if (confirmed && _hasRequiredCheckbox(action)) {
          // Remembered for the one retry the contract allows -- see
          // _ConfirmedRetry and _retryable. Unconditionally overwrites
          // whatever was remembered before, mirroring `confirmedAt = ...`
          // in actions.js: a fresh confirmation replaces a stale one rather
          // than accumulating alongside it.
          _confirmedRetry = _ConfirmedRetry(
            name: name,
            href: href,
            method: method,
            values: values,
            at: _now(),
          );
        }
        return _send(backend, href, method, values, name);
    }
  }

  /// Sends the request. Mirrors `invokeSend()`'s call into `http.request()`
  /// in actions.js; what happens to the answer is [_afterSend]'s job, split
  /// out so this function stays the shape of a single request.
  Future<InvokeOutcome> _send(
    Backend backend,
    String href,
    String method,
    Map<String, String> values,
    String name,
  ) async {
    final result = await _http.request(backend, href, method, values);
    return _afterSend(backend, href, method, values, name, result);
  }

  /// Turns a response into the outcome a caller sees, taking the contract's
  /// one bounded retry first if it applies. Mirrors `afterAction()` and the
  /// retry branch of `afterActionError()` in actions.js, collapsed into one
  /// place here because the retry must never reach the caller as a visible,
  /// separate step -- by the time this returns, whatever the contract
  /// allowed has already happened, and the [InvokeOutcome] it returns is
  /// final.
  Future<InvokeOutcome> _afterSend(
    Backend backend,
    String href,
    String method,
    Map<String, String> values,
    String name,
    Result<HttpResponse> result,
  ) async {
    switch (result) {
      case Ok(value: final response):
        // The confirmation was spent, successfully -- see afterActionSuccess
        // in actions.js clearing confirmedAt the same way.
        _confirmedRetry = null;
        return InvokeSucceeded(response);
      case Err(failure: final failure):
        if (failure.kind != FailureKind.conflict || !_retryable(name)) {
          return InvokeFailed(failure);
        }
        // The one exception: a required checkbox the user actually ticked,
        // re-sent once, within a minute. Cleared *before* sending the retry
        // so whatever this second attempt returns -- success, the same
        // conflict again, or anything else -- is final; retryable(name)
        // will say no to a second attempt at the same action regardless of
        // how much of the TTL is left.
        _confirmedRetry = null;
        _log('conflict on a confirmed action: re-sending it once');
        final retried = await _http.request(backend, href, method, values);
        return switch (retried) {
          Ok(value: final response) => InvokeSucceeded(response),
          Err(failure: final retryFailure) => InvokeFailed(retryFailure),
        };
    }
  }
}

/// Whether [action] has a required checkbox -- the one field type whose
/// value *is* the user's confirmation (see [fieldValues]), and therefore the
/// only case the bounded `409` retry applies to. A checkbox that exists but
/// isn't required does not count, same distinction [confirmationText] draws.
/// Mirrors `hasRequiredCheckbox()` in actions.js.
bool _hasRequiredCheckbox(Action action) =>
    action.fields.any((field) => field.type == 'checkbox' && field.required);

/// How long a confirmation stays eligible for the one retry the contract
/// allows. Mirrors `CONFIRMATION_TTL_MS` in actions.js.
const Duration confirmationRetryTtl = Duration(minutes: 1);

/// A confirmation the user gave, kept just long enough to answer a server
/// that asks for it after the fact.
///
/// The contract allows this narrow case: an action whose required checkbox
/// the user explicitly ticked can still come back `409`, because the server
/// judges the request twice against budgets that can change in between.
/// Re-sending the same confirmed field once is legitimate; synthesising a
/// confirmation nobody gave is not, which is why this records only what was
/// actually confirmed ([values], filled by [fillFields] with [_hasRequiredCheckbox]
/// already true), and only briefly ([confirmationRetryTtl], checked by
/// [ActionService._retryable]). Mirrors `confirmedAt` in actions.js.
class _ConfirmedRetry {
  const _ConfirmedRetry({
    required this.name,
    required this.href,
    required this.method,
    required this.values,
    required this.at,
  });
  final String name;
  final String href;
  final String method;
  final Map<String, String> values;
  final DateTime at;
}
