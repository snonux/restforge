/// The Android equivalent of `pebble/src/c/win_prompt.c`: a modal
/// confirmation for any action whose method is not safe
/// (`action_service.dart`'s [safeMethods]), and — without closing — the
/// same sheet's follow-up prompt for a required field the server needs a
/// value for. This is a port of win_prompt.c's *intent*, not its layout: no
/// footer band, no SELECT/BACK button mapping, no dictation session — see
/// `flutter/AGENTS.md` section 4's do-not-port list (`win_*.c` is layout
/// logic tied to four physical buttons and a 200px screen). What carries
/// over is the shape of the promise that file's header states: "a
/// confirmation that does not say which press means yes is not a
/// confirmation", now spelled as two clearly distinguished on-screen
/// buttons instead of two watch buttons.
///
/// **Two widgets, two jobs.**
///
///  - [ConfirmationSheet] is a pure function of [SessionService.question] —
///    a [ConfirmQuestion] renders the yes/no confirmation
///    ([_ConfirmBody]); a [ValueQuestion] renders the text prompt
///    ([_ValueBody]); neither ever appears without the other having been
///    possible, because [SessionService.answer] can turn a [ConfirmQuestion]
///    into a [ValueQuestion] directly (`session.dart`'s
///    `_applyInvokeOutcome`, the [InvokeNeedsValue] case) — the *same*
///    sheet has to be able to show either, and switch from one to the other
///    without closing, which is exactly what listening to [SessionService]
///    and switching on the current [SessionQuestion] gets for free. This
///    widget never touches [Navigator] itself, so it is testable by pumping
///    it directly (see `test/screens/confirmation_sheet_test.dart`).
///  - [ConfirmationSheetHost] is the only piece that knows about
///    [showModalBottomSheet]: it opens the sheet the moment
///    [SessionService.question] becomes non-null, and closes it again once
///    the question resolves back to null — whether that happened because
///    [ConfirmationSheet]'s own Confirm/Send button drove it there, or
///    because nothing in this file did (see below). Wrapping
///    [DocumentScreen] in this host is task u11's entire wiring change to
///    that file.
///
/// **A required checkbox needs no extra UI.** `pebble/docs/DESIGN.md` says
/// a required checkbox's own title *is* the question
/// (`action_service.dart`'s `confirmationText`), and confirming *is* what
/// fills it in (`fieldValues`: `values[field.name] = confirmed ? 'true' :
/// 'false'`) — so by the time [ConfirmQuestion] is on screen for such an
/// action, tapping Confirm already supplies everything [fillFields] needs.
/// [InvokeNeedsValue] — the case this file's [_ValueBody] exists for — is
/// only ever raised for a *different* required field with no default and no
/// checkbox to stand in for it. Nothing here special-cases the checkbox
/// case; it falls out of composing with `action_service.dart`'s existing
/// API rather than needing one.
///
/// **Dismissing without answering must still cancel the pending action**
/// (`pebble/docs/DESIGN.md`'s "ask before acting", together with
/// `nav_service.dart`'s idle-refresh clock, which is held off for exactly
/// as long as `ActionService.hasPending` is true — see that file's module
/// comment and `session.dart`'s constructor wiring
/// `setActionPendingCheck`). Mirrors win_prompt.c's BACK handler
/// ("Declining is an answer, not a dismissal: JS is holding a pending
/// action and would otherwise keep holding it."). A modal bottom sheet has
/// three ways to close with nothing chosen — tap outside, drag down, or a
/// system back gesture — and Flutter reports all three the same way: the
/// [Future] returned by [showModalBottomSheet] completes. So rather than
/// wiring three separate handlers, [ConfirmationSheetHost] awaits that one
/// Future and, if [SessionService.question] is *still* non-null once it
/// completes (meaning neither Confirm nor Send nor Cancel already resolved
/// it), calls [SessionService.answer] with `false` — exactly what Cancel
/// itself does by simply popping the route and letting this same check
/// catch it, so there is only one place that ever declines a question, not
/// two copies of the same call to keep in sync.
library;

import 'package:flutter/material.dart';

import '../services/session.dart';

/// Wraps [child] — normally the rest of a screen's [Scaffold] — and shows
/// [ConfirmationSheet] as a modal bottom sheet whenever [session.question]
/// is non-null, closing it again once the question goes back to null. See
/// the module comment for why this is the one place that knows about
/// [Navigator]/[showModalBottomSheet] at all.
class ConfirmationSheetHost extends StatefulWidget {
  const ConfirmationSheetHost({
    super.key,
    required this.session,
    required this.child,
  });

  final SessionService session;
  final Widget child;

  @override
  State<ConfirmationSheetHost> createState() => _ConfirmationSheetHostState();
}

class _ConfirmationSheetHostState extends State<ConfirmationSheetHost> {
  /// Whether this host currently has a sheet open — guards against opening
  /// a second one while [SessionService] moves from [ConfirmQuestion] to
  /// [ValueQuestion] and back (both notify [session], but only the first
  /// transition into "some question" should push a route; the sheet itself
  /// reacts to which question it is, see [ConfirmationSheet]).
  bool _showing = false;

  @override
  void initState() {
    super.initState();
    widget.session.addListener(_onSessionChanged);
    // A question may already be pending when this host mounts: running an
    // action shortcut (home_screen's _runShortcut -> SessionService.runQuick)
    // sets session.question *before* DocumentScreen is pushed, so by the
    // time this host attaches its listener the notifyListeners has already
    // come and gone. The listener only fires on *changes*, so check the
    // initial state once the first frame is up — otherwise the user gets the
    // holder document with no confirmation sheet, the sheet a hand-walked
    // action would have got.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        return;
      }
      _onSessionChanged();
    });
  }

  @override
  void didUpdateWidget(covariant ConfirmationSheetHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.session != widget.session) {
      oldWidget.session.removeListener(_onSessionChanged);
      widget.session.addListener(_onSessionChanged);
    }
  }

  @override
  void dispose() {
    widget.session.removeListener(_onSessionChanged);
    super.dispose();
  }

  /// Reacts to every [SessionService] notification, but only acts on the
  /// two edges that matter to a sheet that isn't open yet or is: a
  /// question appearing opens one; a question already showing resolving to
  /// null (Confirm/Send drove it all the way to an outcome, with no
  /// further value needed) closes it. Closing for every *other* reason —
  /// tap-outside, drag, back gesture, or Cancel's own plain pop — is
  /// [_show]'s job once its [showModalBottomSheet] future completes; the
  /// two paths never fight over popping the same route because this
  /// branch only ever fires while [_showing] is already true and the
  /// question just became null, which [_show]'s own post-await check would
  /// otherwise have nothing left to do for.
  void _onSessionChanged() {
    final hasQuestion = widget.session.question != null;
    if (hasQuestion && !_showing) {
      _showing = true;
      _show();
    } else if (!hasQuestion && _showing) {
      Navigator.of(context).maybePop();
    }
  }

  /// Opens the sheet and, once it closes for *any* reason, cancels
  /// whatever is still pending. See the module comment: this is the single
  /// place [SessionService.answer] is ever called with `false` on this
  /// screen's behalf, whether that closing was Cancel's plain pop or a
  /// tap-outside/drag/back-gesture dismissal Flutter handled without this
  /// file's help.
  Future<void> _show() async {
    final session = widget.session;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (context) => ConfirmationSheet(session: session),
    );
    _showing = false;
    if (session.question != null) {
      await session.answer(false);
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// The sheet's content: a pure function of [session.question], with no
/// [Navigator] calls of its own — see the module comment on why
/// [ConfirmationSheetHost] is what actually opens and closes the route this
/// widget is drawn inside of. Renders nothing once [session.question] goes
/// back to null; that is a transient frame on the way to the host popping
/// the route, not a state this widget has to do anything about itself.
class ConfirmationSheet extends StatefulWidget {
  const ConfirmationSheet({super.key, required this.session});

  final SessionService session;

  @override
  State<ConfirmationSheet> createState() => _ConfirmationSheetState();
}

class _ConfirmationSheetState extends State<ConfirmationSheet> {
  /// Holds whatever has been typed for a [ValueQuestion]. One controller
  /// for the life of the sheet is enough — a [ConfirmQuestion] never shows
  /// the field it feeds, and by the time a [ValueQuestion] replaces it
  /// in-place the controller is still empty, exactly as if it were fresh.
  final TextEditingController _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: widget.session,
      builder: (context, _) {
        final question = widget.session.question;
        return switch (question) {
          ConfirmQuestion() => _ConfirmBody(
            question: question,
            onConfirm: () => widget.session.answer(true),
            onCancel: () => Navigator.of(context).maybePop(),
          ),
          ValueQuestion() => _ValueBody(
            question: question,
            controller: _controller,
            onSend: () => widget.session.answerValue(_controller.text),
            onCancel: () => Navigator.of(context).maybePop(),
          ),
          null => const SizedBox.shrink(),
        };
      },
    );
  }
}

/// The yes/no confirmation for a [ConfirmQuestion] — [question.heading] and
/// [question.body] are exactly what `action_service.dart` composed
/// ([ConfirmationRequired]/`confirmationText`), shown verbatim and never
/// reinterpreted, mirroring `pebble/docs/DESIGN.md`'s "rendering does not
/// interpret" even though that invariant was written about documents, not
/// this sentence — the reasoning is the same: this app does not get to
/// improve on wording the server (or, for a required checkbox, the field's
/// own title) already wrote for this exact moment.
class _ConfirmBody extends StatelessWidget {
  const _ConfirmBody({
    required this.question,
    required this.onConfirm,
    required this.onCancel,
  });

  final ConfirmQuestion question;
  final VoidCallback onConfirm;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 20, 20, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              question.heading,
              key: const Key('confirmation-sheet-heading'),
              style: theme.textTheme.titleLarge,
            ),
            const SizedBox(height: 8),
            Text(
              question.body,
              key: const Key('confirmation-sheet-body'),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 20),
            _ButtonRow(
              onConfirm: onConfirm,
              onCancel: onCancel,
              confirmLabel: 'Confirm',
            ),
          ],
        ),
      ),
    );
  }
}

/// The "what value" follow-up for a [ValueQuestion] — [question.label] is
/// [Field.label] (`action_service.dart`'s [InvokeNeedsValue]), the server's
/// own wording when it gave one. Mirrors win_prompt.c's dictation path
/// (`start_dictation`/`on_dictation`) in spirit only: a phone has a
/// keyboard, so a text field stands in for a microphone, and there is no
/// on-device equivalent of "dictation failed" to report separately — an
/// empty submission already reaches `action_service.dart`'s own "nothing
/// was heard, so nothing was sent" guard ([ActionService.answerValue]),
/// the same outcome win_prompt.c reports as a decline.
class _ValueBody extends StatelessWidget {
  const _ValueBody({
    required this.question,
    required this.controller,
    required this.onSend,
    required this.onCancel,
  });

  final ValueQuestion question;
  final TextEditingController controller;
  final VoidCallback onSend;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          20,
          20,
          20,
          16 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              question.label,
              key: const Key('confirmation-sheet-value-label'),
              style: theme.textTheme.titleLarge,
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('confirmation-sheet-value-field'),
              controller: controller,
              autofocus: true,
              textInputAction: TextInputAction.done,
              onSubmitted: (_) => onSend(),
            ),
            const SizedBox(height: 20),
            _ButtonRow(
              onConfirm: onSend,
              onCancel: onCancel,
              confirmLabel: 'Send',
            ),
          ],
        ),
      ),
    );
  }
}

/// Cancel and Confirm, laid out so the two can never be mistaken for each
/// other: Cancel is a plain [TextButton] — the easy, low-emphasis press,
/// nothing to accidentally lean on — while Confirm is a [FilledButton] in
/// [ColorScheme.error]/[ColorScheme.onError], the standard Material
/// pairing for a deliberate, consequential action. Both actions this sheet
/// ever offers are exactly that by construction ([ConfirmationSheetHost]
/// only shows this sheet for a method `action_service.dart` already
/// decided is unsafe), so one colouring for Confirm covers every case
/// rather than needing a per-action "how dangerous is this" judgement this
/// app has no way to make.
class _ButtonRow extends StatelessWidget {
  const _ButtonRow({
    required this.onConfirm,
    required this.onCancel,
    required this.confirmLabel,
  });

  final VoidCallback onConfirm;
  final VoidCallback onCancel;
  final String confirmLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Expanded(
          child: TextButton(
            key: const Key('confirmation-sheet-cancel'),
            onPressed: onCancel,
            child: const Text('Cancel'),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: FilledButton(
            key: const Key('confirmation-sheet-confirm'),
            style: FilledButton.styleFrom(
              backgroundColor: theme.colorScheme.error,
              foregroundColor: theme.colorScheme.onError,
            ),
            onPressed: onConfirm,
            child: Text(confirmLabel),
          ),
        ),
      ],
    );
  }
}
