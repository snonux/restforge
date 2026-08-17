/// The main content screen: the current document's rows, the four document
/// states, pull-to-refresh, and back navigation through the app's own stack.
///
/// This is the screen `nav_service.dart`'s module comment describes as "a
/// caller that keeps rendering the last [SessionService.document] while
/// [SessionService.state] is [DocumentState.loading] or an error kind" — it
/// is where that promise becomes pixels. Everything shown here comes from
/// [SessionService] (`session.dart`, task p11), the coordinator this file is
/// the only screen-layer piece of: a row press goes to
/// [SessionService.activate], which already knows whether a
/// [render.RowTarget] means "go read something" or "ask about doing
/// something" (`render_service.dart`'s `RowKind`/`RowTarget`) — this screen
/// never inspects a target itself, it only decides how the *row* looks and
/// feels before that decision is made.
///
/// **Row order** is whatever `render_service.dart`'s [render.document]
/// produced — properties, sub-entities, links, actions, Siren's own order
/// (see that file's module comment on why a client does not get to reorder
/// somebody else's document). This screen lists
/// [render.RenderedDocument.rows] exactly as handed to it.
///
/// **Row affordances.** `pebble/docs/DESIGN.md` ("Ask before acting"): "the
/// row that opens a property and the row that changes the world must not act
/// the same." On four physical buttons that meant a dedicated confirm
/// screen; here it means a property/sub-entity/link row is a plain list row
/// — one tap, no visual weight beyond an icon that says what following it
/// does (read in place, open a sub-entity already in hand, or fetch a link)
/// — while an action row ([_ActionRow]) is a filled, coloured, standalone
/// tile with its own icon, deliberately shaped like a button rather than a
/// list item, so a thumb moving down the list cannot mistake one for the
/// other. See [_PropertyRow], [_EntityRow], [_LinkRow], [_ActionRow].
///
/// **The four states** ([DocumentState], `nav_service.dart`) are shown
/// exactly as that module promises: [DocumentState.ok] renders the rows with
/// nothing on top; [DocumentState.loading] adds a thin progress indicator
/// without touching them; [DocumentState.error]/[DocumentState.unreachable]
/// lay [_FailureBanner] over the *same* rows rather than replacing them —
/// this is `pebble/docs/DESIGN.md`'s "a failed request is not an answer"
/// made literal, and is the one behaviour this file has its own widget test
/// for (see `document_screen_test.dart`, "a failed refresh keeps the last
/// document on screen"). Only when nothing has ever been fetched
/// ([SessionService.document] is still null) is a failure allowed a
/// full-screen treatment ([_EmptyBody]) — there is no last good document to
/// protect yet, so a dedicated state is the honest thing to show instead of
/// an empty list pretending to be one.
///
/// **Refresh.** Pull-to-refresh calls [SessionService.refresh], which
/// re-reads the document on screen in place — never [SessionService.back] or
/// a fresh fetch, so a manual refresh behaves exactly like the idle-refresh
/// clock `nav_service.dart` already runs on its own.
///
/// **Back.** [PopScope] intercepts a pop exactly when
/// [SessionService.canGoBack] is true and calls [SessionService.back]
/// instead of letting the platform pop this route — the navigation stack
/// this screen renders is [SessionService]'s, not [Navigator]'s, and the two
/// must not be confused. Once [SessionService.canGoBack] is false (the
/// backend's root document is on screen), a pop is let through to
/// [Navigator] as normal, which is how this screen hands control back to
/// whatever pushed it (the backend picker, `home_screen.dart`) — mirrors
/// `nav.js`'s `back()` falling through to the picker frame below the root.
///
/// **Watching a job** ([_NoticeBanner]) lays [SessionService.notice] over
/// the rows, the same way [_FailureBanner] lays a transport failure over
/// them — four distinct things, shown as four distinct things, per
/// `pebble/docs/DESIGN.md`'s "Do not claim a job finished" and
/// `live_service.dart`'s own module comment, which spends most of its length
/// on exactly this failure mode:
///
///  - **Progress** ([SessionService.isLive] true, [SessionService.notice] an
///    [ActionProgress]) — [_watchingBanner] shows a spinner and the step.
///  - **No news** — a poll that came back about a different job, or with
///    nothing new, does not call any [LiveHandlers] callback
///    (`live_service.dart`'s `_relevant`/`_checkDeadline`), so
///    [SessionService] never calls `notifyListeners` for it and this
///    screen's build never runs for it either. The banner already on screen
///    — spinner still turning, text unchanged — *is* what "no news" looks
///    like: nothing new to show, so nothing changes. A separate toast or
///    banner for this would be inventing a signal `live_service.dart` does
///    not send.
///  - **Done** ([SessionService.isLive] false, [SessionService.notice] an
///    [ActionOutcomeReported]) — [_outcomeBanner] shows the server's own
///    word for the outcome, success-coloured.
///  - **Gave up** ([SessionService.isLive] false, [SessionService.notice] an
///    [ActionGaveUp]) — [_outcomeBanner] shows a distinct, neutral-coloured
///    banner that never claims the job failed or finished, only that this
///    app stopped asking.
///
/// **What this file deliberately does not build**, already a separate task
/// depending on this one: opening a property ([render.DetailTarget]) sets
/// [SessionService.detail], but the full, scrollable reading view for it is
/// task t11's job (`pebble/src/c/win_detail.c`'s equivalent). This screen
/// calls [SessionService.activate] for every row exactly the same way
/// regardless of what follows, so that task does not have to change how a
/// row is pressed — only what appears once [SessionService] reacts to it.
///
/// **Confirming or supplying a value for an action**
/// ([SessionService.question]) is task u11's, and is the one exception to
/// "this file only decides how a row looks": [build] wraps the whole screen
/// in [ConfirmationSheetHost], which owns showing/hiding the modal sheet —
/// see that file's module comment for why that wiring is one line here and
/// everything else lives there.
///
/// **Saving a shortcut** (task x11): a long-press on [_LinkRow] or
/// [_ActionRow] — never [_PropertyRow] or [_EntityRow], which
/// [SessionService.saveQuick] would refuse anyway, see its own doc comment
/// — calls [SessionService.saveQuick] and reports the outcome with a
/// [SnackBar]. This is the one row gesture besides a tap this file adds on
/// its own rather than leaving entirely to [SessionService]: a long-press
/// has nothing to do with [render.RowTarget] (activating a row and saving
/// it as a shortcut are two independent things a row supports), so, unlike
/// a tap, it is not part of the "this screen never inspects a target
/// itself" rule above — [_saveQuickShortcut] only decides *when* to call
/// [SessionService.saveQuick], never what a row means.
library;

import 'package:flutter/material.dart';

import '../models/failure.dart';
import '../services/nav_service.dart' show DocumentState;
import '../services/quick_service.dart' show QuickService;
import '../services/render_service.dart' as render;
import '../services/session.dart';
import 'confirmation_sheet.dart';

class DocumentScreen extends StatelessWidget {
  const DocumentScreen({super.key, required this.session});

  /// The coordinator this screen renders and dispatches every row press to.
  /// Always supplied by whoever pushes this screen with a [SessionService]
  /// that has already had [SessionService.openBackend] called on it — this
  /// screen itself never opens a backend, it only ever renders whatever is
  /// already current. Pushed by `home_screen.dart`'s `_openBackend`/`_runShortcut`
  /// once a backend or a saved shortcut is opened, and also exercised by
  /// `test/screens/document_screen_test.dart`.
  final SessionService session;

  @override
  Widget build(BuildContext context) {
    // ConfirmationSheetHost is the only piece of this file that knows about
    // SessionService.question — wrapping the rest of the screen in it is
    // task u11's entire wiring change here (see confirmation_sheet.dart's
    // module comment).
    return ConfirmationSheetHost(
      session: session,
      child: ListenableBuilder(
        listenable: session,
        builder: (context, _) {
          return PopScope(
            // Whenever there is somewhere in SessionService's own stack to
            // pop to, this route itself must not close — see the module
            // comment.
            canPop: !session.canGoBack,
            onPopInvokedWithResult: (didPop, result) {
              if (didPop) {
                // canPop was already true: the platform popped this route
                // (to the backend picker, say), and there is nothing left
                // here to unwind first.
                return;
              }
              session.back();
            },
            child: Scaffold(
              appBar: AppBar(
                title: Text(
                  session.document?.title ??
                      session.backend?.name ??
                      'RESTForge',
                ),
              ),
              body: _DocumentBody(session: session),
            ),
          );
        },
      ),
    );
  }
}

/// Chooses between the row list (with whatever is layered on top of it for
/// the current [DocumentState]) and [_EmptyBody] for when nothing has ever
/// been fetched — see the module comment on why those two cases render
/// differently.
class _DocumentBody extends StatelessWidget {
  const _DocumentBody({required this.session});

  final SessionService session;

  @override
  Widget build(BuildContext context) {
    final document = session.document;
    if (document == null) {
      return _EmptyBody(session: session);
    }
    final failure = session.failure;
    return RefreshIndicator(
      onRefresh: session.refresh,
      child: Column(
        children: [
          if (session.state == DocumentState.loading)
            const LinearProgressIndicator(
              key: Key('loading-indicator'),
              minHeight: 3,
            ),
          if (failure != null)
            _FailureBanner(
              failure: failure,
              unreachable: session.state == DocumentState.unreachable,
            ),
          _NoticeBanner(session: session),
          Expanded(
            child: _RowList(document: document, session: session),
          ),
        ],
      ),
    );
  }
}

/// What is on screen before the first document has ever arrived, or when the
/// very first fetch failed with nothing to fall back on — there is no last
/// good document to protect here, so [DocumentState.loading] and a failure
/// each get a plain, honest full-screen treatment instead of the
/// row-list-plus-overlay [_DocumentBody] uses once something has landed.
class _EmptyBody extends StatelessWidget {
  const _EmptyBody({required this.session});

  final SessionService session;

  @override
  Widget build(BuildContext context) {
    switch (session.state) {
      case DocumentState.loading:
        return const Center(
          child: CircularProgressIndicator(key: Key('initial-loading')),
        );
      case DocumentState.error:
      case DocumentState.unreachable:
        return _FullScreenFailure(
          failure: session.failure,
          unreachable: session.state == DocumentState.unreachable,
        );
      case DocumentState.ok:
        // Reachable only in the moment before the first fetch has even
        // started (nav_service.dart's own initial state) — not a state a
        // person should ever sit on for long, but rendering nothing at all
        // would look indistinguishable from a bug.
        return const Center(child: Text('Nothing to show yet.'));
    }
  }
}

/// A failure with no document underneath it to protect — see [_EmptyBody].
/// Distinguished from [_FailureBanner] only by taking the whole screen
/// rather than sitting on top of rows that do not exist yet.
class _FullScreenFailure extends StatelessWidget {
  const _FullScreenFailure({required this.failure, required this.unreachable});

  final Failure? failure;
  final bool unreachable;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              unreachable ? Icons.wifi_off : Icons.error_outline,
              size: 48,
              color: theme.colorScheme.error,
            ),
            const SizedBox(height: 12),
            Text(
              unreachable ? 'Could not reach the server' : 'The request failed',
              style: theme.textTheme.titleMedium,
              textAlign: TextAlign.center,
            ),
            if (failure != null) ...[
              const SizedBox(height: 8),
              Text(
                failure!.message,
                key: const Key('full-screen-failure-message'),
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// The reason [DocumentState] is not [DocumentState.ok], laid over the last
/// good document rather than replacing it — the widget this file's own
/// invariant test is about. See the module comment.
///
/// [unreachable] picks the wording and colouring: `pebble/docs/DESIGN.md`
/// ("A failed request is not an answer") treats "I could not ask" and "the
/// answer was no" as different facts, and only one of them is about the
/// server, so the two are never allowed to look the same here either.
class _FailureBanner extends StatelessWidget {
  const _FailureBanner({required this.failure, required this.unreachable});

  final Failure failure;
  final bool unreachable;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final background = unreachable
        ? theme.colorScheme.surfaceContainerHighest
        : theme.colorScheme.errorContainer;
    final foreground = unreachable
        ? theme.colorScheme.onSurfaceVariant
        : theme.colorScheme.onErrorContainer;
    return Material(
      key: const Key('failure-banner'),
      color: background,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(
              unreachable ? Icons.wifi_off : Icons.error_outline,
              color: foreground,
              size: 20,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                unreachable
                    ? 'Unreachable: ${failure.message}'
                    : 'Request failed: ${failure.message}',
                key: const Key('failure-banner-text'),
                style: TextStyle(color: foreground),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Lays [SessionService.notice] over the rows — the report of what the last
/// action, or the job it started, produced — distinct from [_FailureBanner]
/// above it: a failure is about the request that just ran, this is about
/// what the server said happened, including a job this app is still
/// watching. See the module comment for the four states this dispatch keeps
/// visually apart. [SessionService.isLive] alone decides which of the two
/// banners below applies: it is true for exactly "progress" and "no news"
/// (see [_watchingBanner]) and false for exactly "done" and "gave up" (see
/// [_outcomeBanner]) — mirrors `live_service.dart`'s own contract that
/// [LiveHandlers.onGiveUp] and [LiveHandlers.onDone] each stop the watch
/// before reporting, so the two never overlap.
class _NoticeBanner extends StatelessWidget {
  const _NoticeBanner({required this.session});

  final SessionService session;

  @override
  Widget build(BuildContext context) {
    if (session.isLive) {
      return _watchingBanner(context, session.notice);
    }
    final notice = session.notice;
    if (notice == null) {
      return const SizedBox.shrink();
    }
    return _outcomeBanner(context, notice, session.dismissNotice);
  }
}

/// "Progress" and "no news", covered by one banner on purpose — see the
/// module comment. [ActionProgress] carries a step to show; the moment
/// before the first poll lands, [SessionService.notice] is still the
/// [ActionOutcomeReported] `_handleSuccess` set from the action's own 202/
/// running reply, so that message is shown instead of a placeholder. A
/// spinner, not a bar: there is no known end point to show a fraction of.
/// Not dismissible — dismissing "something is still happening" would be a
/// lie, since the job keeps running underneath regardless of whether this
/// banner is on screen.
Widget _watchingBanner(BuildContext context, SessionNotice? notice) {
  final theme = Theme.of(context);
  final text = switch (notice) {
    ActionProgress(:final step) => step,
    ActionOutcomeReported(:final message) => message,
    _ => 'Still running',
  };
  return _Banner(
    bannerKey: const Key('live-watching-banner'),
    spinner: true,
    background: theme.colorScheme.secondaryContainer,
    foreground: theme.colorScheme.onSecondaryContainer,
    text: text,
  );
}

/// What is left once [SessionService.isLive] is false: [ActionOutcomeReported]
/// ("done", success-coloured — the job finished, or a non-watched action's
/// own reply) and [ActionGaveUp] ("gave up", neutral-coloured, worded so it
/// is never mistaken for either "done" or a failure — mirrors
/// `pebble/docs/DESIGN.md`'s "Do not claim a job finished"). The remaining
/// [SessionNotice] cases predate this task (an action that failed outright,
/// was refused, or was withdrawn) and are handled the same way for
/// exhaustiveness and a consistent look, though they are not job-watching
/// states themselves. Every case is dismissible via
/// [SessionService.dismissNotice] — unlike [_watchingBanner], each of these
/// is a finished fact, not something still changing underneath the banner.
Widget _outcomeBanner(
  BuildContext context,
  SessionNotice notice,
  VoidCallback onDismiss,
) {
  final theme = Theme.of(context);
  return switch (notice) {
    ActionOutcomeReported(:final message, :final body) => _Banner(
      bannerKey: const Key('live-done-banner'),
      icon: Icons.check_circle_outline,
      background: theme.colorScheme.primaryContainer,
      foreground: theme.colorScheme.onPrimaryContainer,
      text: '$message: $body',
      onDismiss: onDismiss,
    ),
    ActionGaveUp(:final heading) => _Banner(
      bannerKey: const Key('live-giveup-banner'),
      icon: Icons.hourglass_disabled,
      background: theme.colorScheme.surfaceContainerHighest,
      foreground: theme.colorScheme.onSurfaceVariant,
      text: 'Gave up waiting for "$heading" to finish',
      onDismiss: onDismiss,
    ),
    ActionFailed(:final heading, :final failure) => _Banner(
      bannerKey: const Key('notice-failed-banner'),
      icon: Icons.error_outline,
      background: theme.colorScheme.errorContainer,
      foreground: theme.colorScheme.onErrorContainer,
      text: '$heading failed: ${failure.message}',
      onDismiss: onDismiss,
    ),
    ActionRefused(:final heading, :final reason) => _Banner(
      bannerKey: const Key('notice-refused-banner'),
      icon: Icons.block,
      background: theme.colorScheme.errorContainer,
      foreground: theme.colorScheme.onErrorContainer,
      text: '$heading: $reason',
      onDismiss: onDismiss,
    ),
    ActionWithdrawn(:final heading) => _Banner(
      bannerKey: const Key('notice-withdrawn-banner'),
      icon: Icons.block,
      background: theme.colorScheme.surfaceContainerHighest,
      foreground: theme.colorScheme.onSurfaceVariant,
      text: '"$heading" is no longer offered',
      onDismiss: onDismiss,
    ),
    // Unreachable in practice: session.dart only ever sets an
    // ActionProgress notice while a watch is live, and _watchingBanner
    // handles isLive rather than this function — see _NoticeBanner.build.
    // Handled anyway so this switch stays exhaustive against SessionNotice
    // gaining a case later without silently mis-rendering it.
    ActionProgress(:final step) => _Banner(
      bannerKey: const Key('notice-progress-banner'),
      icon: Icons.timelapse,
      background: theme.colorScheme.surfaceContainerHighest,
      foreground: theme.colorScheme.onSurfaceVariant,
      text: step,
      onDismiss: onDismiss,
    ),
  };
}

/// The single-row, coloured-strip shape [_watchingBanner] and
/// [_outcomeBanner] both render, so the two only differ in the values they
/// pass — an icon or a spinner, a colour pair, the text, and whether
/// dismissing makes sense. [_FailureBanner] above predates this and is left
/// with its own copy of this shape rather than refactored onto this one, to
/// avoid touching a banner with its own settled tests for a task that is not
/// about it.
class _Banner extends StatelessWidget {
  const _Banner({
    required this.bannerKey,
    this.icon,
    this.spinner = false,
    required this.background,
    required this.foreground,
    required this.text,
    this.onDismiss,
  });

  final Key bannerKey;
  final IconData? icon;
  final bool spinner;
  final Color background;
  final Color foreground;
  final String text;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) {
    return Material(
      key: bannerKey,
      color: background,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            if (spinner)
              SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2, color: foreground),
              )
            else
              Icon(icon, color: foreground, size: 20),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                text,
                key: const Key('notice-banner-text'),
                style: TextStyle(color: foreground),
              ),
            ),
            if (onDismiss != null)
              IconButton(
                key: const Key('notice-dismiss'),
                icon: Icon(Icons.close, color: foreground, size: 18),
                onPressed: onDismiss,
              ),
          ],
        ),
      ),
    );
  }
}

/// The rows of [document], one tile per [render.Row], each dispatched
/// through [SessionService.activate] on tap. Wrapped in
/// [AlwaysScrollableScrollPhysics] even when short, so [RefreshIndicator]
/// above can always be pulled regardless of how many rows fit on screen.
class _RowList extends StatelessWidget {
  const _RowList({required this.document, required this.session});

  final render.RenderedDocument document;
  final SessionService session;

  @override
  Widget build(BuildContext context) {
    final rows = document.rows;
    if (rows.isEmpty) {
      return ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        children: const [
          Padding(
            padding: EdgeInsets.all(24),
            child: Center(child: Text('This document has nothing to show.')),
          ),
        ],
      );
    }
    return ListView.builder(
      physics: const AlwaysScrollableScrollPhysics(),
      itemCount: rows.length,
      itemBuilder: (context, index) {
        final row = rows[index];
        return _RowTile(
          key: ValueKey('row-${row.kind.name}-$index'),
          row: row,
          onTap: () => session.activate(row.target),
          onLongPress: () => _saveQuickShortcut(context, session, row),
        );
      },
    );
  }
}

/// Picks the row widget for [render.Row.kind] — see the module comment on
/// why each kind gets its own look and feel rather than one tile styled by a
/// switch on a colour. [onLongPress] (task x11's save-a-shortcut gesture) is
/// wired only into [_LinkRow] and [_ActionRow]: a property opens a reading
/// view and a sub-entity has no address of its own, and
/// [SessionService.saveQuick] refuses both anyway (see its doc comment), so
/// [_PropertyRow]/[_EntityRow] are left with the plain tap they already had
/// rather than offering a gesture that would always report "cannot save".
class _RowTile extends StatelessWidget {
  const _RowTile({
    super.key,
    required this.row,
    required this.onTap,
    required this.onLongPress,
  });

  final render.Row row;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    switch (row.kind) {
      case render.RowKind.property:
        return _PropertyRow(row: row, onTap: onTap);
      case render.RowKind.entity:
        return _EntityRow(row: row, onTap: onTap);
      case render.RowKind.link:
        return _LinkRow(row: row, onTap: onTap, onLongPress: onLongPress);
      case render.RowKind.action:
        return _ActionRow(row: row, onTap: onTap, onLongPress: onLongPress);
    }
  }
}

/// [SessionService.saveQuick] for [row], reported with a [SnackBar] —
/// [QuickSaveOutcome.saved]/[QuickSaveOutcome.full]/
/// [QuickSaveOutcome.notSaveable] each get their own wording so "saved",
/// "the list is full" (`QuickService.maxQuick`, `pebble/docs/DESIGN.md`-style
/// "surface the bound rather than dropping it silently") and "this can't be
/// saved" are never mistaken for one another. The [ScaffoldMessenger] is
/// looked up before the `await` (`context` is not used after it) — the
/// standard guard against using a possibly-disposed [BuildContext] once
/// [SessionService.saveQuick]'s future completes.
Future<void> _saveQuickShortcut(
  BuildContext context,
  SessionService session,
  render.Row row,
) async {
  final messenger = ScaffoldMessenger.of(context);
  final outcome = await session.saveQuick(row);
  final text = switch (outcome) {
    QuickSaveOutcome.saved => 'Saved "${row.label}" as a shortcut',
    QuickSaveOutcome.full =>
      'Already holding ${QuickService.maxQuick} shortcuts — remove one '
          'first',
    QuickSaveOutcome.notSaveable => 'This cannot be saved as a shortcut',
  };
  messenger.showSnackBar(SnackBar(content: Text(text)));
}

/// A property row: read-only, so it looks and behaves like nothing more than
/// a line of text — a plain [ListTile], no colour, no elevation. The
/// trailing "open in full" glyph (rather than a chevron) hints at what
/// pressing it does: open the whole value in place ([render.DetailTarget]),
/// not go anywhere.
class _PropertyRow extends StatelessWidget {
  const _PropertyRow({required this.row, required this.onTap});

  final render.Row row;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: const Icon(Icons.label_outline),
      title: Text(row.label),
      subtitle: row.sublabel.isEmpty
          ? null
          : Text(row.sublabel, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: const Icon(Icons.open_in_full, size: 18),
      onTap: onTap,
    );
  }
}

/// A sub-entity row: also read-only in effect (it only ever navigates), but
/// distinguished from [_LinkRow] by icon — one is already in hand
/// ([render.EmbeddedTarget]) or a reference the same as a link
/// ([render.FetchTarget]); either way this is Siren's `entities`, not
/// `links`, and the icon says so.
class _EntityRow extends StatelessWidget {
  const _EntityRow({required this.row, required this.onTap});

  final render.Row row;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: const Icon(Icons.account_tree_outlined),
      title: Text(row.label),
      subtitle: row.sublabel.isEmpty
          ? null
          : Text(row.sublabel, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}

/// A link row: follows an href ([render.FetchTarget]) exactly like a
/// referenced sub-entity does, so the gesture is the same as [_EntityRow] —
/// but a link is Siren's own separate vocabulary, so the icon still says
/// which one this is.
class _LinkRow extends StatelessWidget {
  const _LinkRow({
    required this.row,
    required this.onTap,
    required this.onLongPress,
  });

  final render.Row row;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: const Icon(Icons.link),
      title: Text(row.label),
      subtitle: row.sublabel.isEmpty
          ? null
          : Text(row.sublabel, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
      onLongPress: onLongPress,
    );
  }
}

/// An action row: changes the world, so it must never look or feel like the
/// three rows above. Rather than a flat [ListTile], this is a filled,
/// rounded, standalone tile — the shape of a button, not a list item — with
/// its own colour and a bolt icon, so a press here reads as a deliberate,
/// separate gesture even before `pebble/docs/DESIGN.md`'s "ask before
/// acting" gets a chance to show a confirmation (task u11, not built here —
/// see the module comment).
class _ActionRow extends StatelessWidget {
  const _ActionRow({
    required this.row,
    required this.onTap,
    required this.onLongPress,
  });

  final render.Row row;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Material(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(12),
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: onTap,
          onLongPress: onLongPress,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
            child: Row(
              children: [
                Icon(Icons.bolt, color: theme.colorScheme.onSecondaryContainer),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        row.label,
                        style: theme.textTheme.titleMedium?.copyWith(
                          color: theme.colorScheme.onSecondaryContainer,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      if (row.sublabel.isNotEmpty)
                        Text(
                          row.sublabel,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSecondaryContainer,
                          ),
                        ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
