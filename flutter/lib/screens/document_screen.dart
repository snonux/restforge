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
/// **The full-screen reading view** for a property's value
/// ([render.DetailTarget] → [SessionService.detail]) is built by
/// [DetailViewHost] wrapping [build] — see `detail_screen.dart`'s module
/// comment (the port of `pebble/src/c/win_detail.c`). This screen calls
/// [SessionService.activate] for every row exactly the same way regardless
/// of what follows, so the reading view did not have to change how a row is
/// pressed — only what appears once [SessionService] reacts to it.
///
/// **Confirming or supplying a value for an action**
/// ([SessionService.question]) is task u11's, and with the reading view
/// above is one of the two exceptions to "this file only decides how a row
/// looks": [build] wraps the whole screen in [ConfirmationSheetHost],
/// which owns showing/hiding the modal sheet — see that file's module
/// comment for why that wiring is one line here and everything else lives
/// there.
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
import 'detail_screen.dart';

part 'document_banners.dart';
part 'document_rows.dart';

class DocumentScreen extends StatefulWidget {
  const DocumentScreen({super.key, required this.session});

  /// The coordinator this screen renders and dispatches every row press to.
  /// Always supplied by whoever pushes this screen with a [SessionService]
  /// that has already had [SessionService.openBackend] called on it — this
  /// screen itself never opens a backend, it only ever renders whatever is
  /// already current. Pushed by `home_screen.dart`'s `_openBackend`/`_runShortcut`
  /// once a backend or a saved shortcut is opened, and also exercised by
  /// `test/screens/document_screen_test.dart`.
  ///
  /// This screen is also the framework-bound half of the idle-refresh
  /// foreground gate (task a21): it registers a `WidgetsBindingObserver`
  /// for its own lifetime that translates `AppLifecycleState` into the
  /// pure-Dart bool the coordinator's clock reads — see
  /// `_DocumentScreenState` and `_AppLifecycleObserver`.
  final SessionService session;

  @override
  State<DocumentScreen> createState() => _DocumentScreenState();
}

class _DocumentScreenState extends State<DocumentScreen> {
  _AppLifecycleObserver? _lifecycle;

  @override
  void initState() {
    super.initState();
    // The framework-bound half of the idle-refresh gate (task a21): a
    // WidgetsBindingObserver that translates AppLifecycleState into the
    // pure-Dart bool the SessionService's clock reads. Registered for this
    // screen's lifetime — the only time idle refresh is relevant is while
    // a document is on screen, and this screen is on screen for exactly
    // that, so the watcher's lifetime matches the session's.
    _lifecycle = _AppLifecycleObserver(widget.session);
    WidgetsBinding.instance.addObserver(_lifecycle!);
  }

  @override
  void dispose() {
    if (_lifecycle != null) {
      WidgetsBinding.instance.removeObserver(_lifecycle!);
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final session = widget.session;
    // DetailViewHost (task t11) is the only piece of this file that knows
    // about SessionService.detail — wrapping the rest of the screen in it
    // is that task's wiring here (see detail_screen.dart's module comment),
    // the same shape as ConfirmationSheetHost below it for
    // SessionService.question.
    return DetailViewHost(
      session: session,
      child: ConfirmationSheetHost(
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
      ),
    );
  }
}

/// The framework-bound lifecycle watcher for one [SessionService]'s
/// idle-refresh gate — see `_DocumentScreenState.initState`. Pure
/// translation of `AppLifecycleState` into the `bool`
/// [SessionService.setAppForeground] takes; this is the only place in the
/// document screen that touches the widgets-framework lifecycle API,
/// keeping the services pure-Dart (the point of task a21's extraction).
class _AppLifecycleObserver extends WidgetsBindingObserver {
  _AppLifecycleObserver(this._session);

  final SessionService _session;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _session.setAppForeground(state == AppLifecycleState.resumed);
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
