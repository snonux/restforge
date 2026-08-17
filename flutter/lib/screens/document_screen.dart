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
/// **What this file deliberately does not build**, each already a separate
/// task depending on this one: opening a property
/// ([render.DetailTarget]) sets [SessionService.detail], but the full,
/// scrollable reading view for it is task t11's job
/// (`pebble/src/c/win_detail.c`'s equivalent); confirming or supplying a
/// value for an action ([SessionService.question]) is task u11's; watching a
/// job's progress ([SessionService.isLive], [SessionService.notice] for a
/// running action) is task v11's. This screen calls
/// [SessionService.activate] for every row exactly the same way regardless
/// of which of those follows, so none of those tasks has to change how a row
/// is pressed — only what appears once [SessionService] reacts to it.
library;

import 'package:flutter/material.dart';

import '../models/failure.dart';
import '../services/nav_service.dart' show DocumentState;
import '../services/render_service.dart' as render;
import '../services/session.dart';

class DocumentScreen extends StatelessWidget {
  const DocumentScreen({super.key, required this.session});

  /// The coordinator this screen renders and dispatches every row press to.
  /// Always supplied by whoever pushes this screen with a [SessionService]
  /// that has already had [SessionService.openBackend] called on it — this
  /// screen itself never opens a backend, it only ever renders whatever is
  /// already current. Nothing pushes it yet (see the module comment on this
  /// task's scope: wiring it into `home_screen.dart`'s picker is a separate
  /// concern), so today this is exercised only by
  /// `test/screens/document_screen_test.dart`.
  final SessionService session;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: session,
      builder: (context, _) {
        return PopScope(
          // Whenever there is somewhere in SessionService's own stack to pop
          // to, this route itself must not close — see the module comment.
          canPop: !session.canGoBack,
          onPopInvokedWithResult: (didPop, result) {
            if (didPop) {
              // canPop was already true: the platform popped this route (to
              // the backend picker, say), and there is nothing left here to
              // unwind first.
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
          Expanded(child: _RowList(document: document, session: session)),
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
        );
      },
    );
  }
}

/// Picks the row widget for [render.Row.kind] — see the module comment on
/// why each kind gets its own look and feel rather than one tile styled by a
/// switch on a colour.
class _RowTile extends StatelessWidget {
  const _RowTile({super.key, required this.row, required this.onTap});

  final render.Row row;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    switch (row.kind) {
      case render.RowKind.property:
        return _PropertyRow(row: row, onTap: onTap);
      case render.RowKind.entity:
        return _EntityRow(row: row, onTap: onTap);
      case render.RowKind.link:
        return _LinkRow(row: row, onTap: onTap);
      case render.RowKind.action:
        return _ActionRow(row: row, onTap: onTap);
    }
  }
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
  const _LinkRow({required this.row, required this.onTap});

  final render.Row row;
  final VoidCallback onTap;

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
  const _ActionRow({required this.row, required this.onTap});

  final render.Row row;
  final VoidCallback onTap;

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
