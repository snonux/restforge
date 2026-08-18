/// The full-screen reading view for a single value.
///
/// This is the Dart port of `pebble/src/c/win_detail.c` and
/// `win_scroll_text.c` — see `pebble/docs/DESIGN.md`'s "Layout on two
/// screens" for the requirement it served: when a value is too long for its
/// row, the whole thing opens in a scrollable view rather than being
/// truncated, because "the whole value is legible, and nothing is silently
/// cut off" is the property that makes a clipped `TextOverflow.ellipsis`
/// honest rather than a quiet lie. None of the Pebble layout arithmetic
/// carries over — no fixed cell heights, no runtime font measurement, no
/// chord-derived widths (flutter/AGENTS.md section 4's do-not-port list) —
/// because none of it is needed on a screen that can scroll and wrap. What
/// carries over is the requirement, not the mechanism.
///
/// [SessionService.activate] on a property row ([render.DetailTarget]) sets
/// [SessionService.detail] to a [DetailView] carrying the value's heading
/// and its full body. This file is the screen that renders it: a
/// full-screen route pushed by [DetailViewHost] (which listens to
/// [SessionService.detail] the same way `confirmation_sheet.dart`'s
/// `ConfirmationSheetHost` listens to [SessionService.question] — see that
/// file's module comment for the host-listens-and-pushes-a-route pattern
/// and why the route, not an in-place overlay, is the right vehicle). The
/// body is shown with [SelectableText] inside a [SingleChildScrollView], so
/// it wraps rather than truncating, scrolls when it still does not fit, and
/// is selectable so a value like a URL or an id can be copied out rather
/// than read off the screen.
///
/// Dismissing the route (back button, system back, or a tap on the close
/// action) calls [SessionService.dismissDetail] through [DetailViewHost]'s
/// post-await — the single place this file reaches back into the session,
/// mirroring `ConfirmationSheetHost._show` calling `answer(false)` on close.
library;

import 'package:flutter/material.dart';

import '../services/session.dart';

/// Wraps [child] and pushes a full-screen [DetailScreen] whenever
/// [SessionService.detail] becomes non-null, popping it when it goes back
/// to null. Mirrors `ConfirmationSheetHost` in `confirmation_sheet.dart`:
/// the host is the only piece that knows about [SessionService.detail], so
/// the screen it wraps (`document_screen.dart`) keeps rendering rows and
/// dispatching presses exactly as before, and the reading view is one
/// declarative `Listenable` edge away rather than a route push threaded
/// through every call site that might set a detail.
class DetailViewHost extends StatefulWidget {
  const DetailViewHost({super.key, required this.session, required this.child});

  final SessionService session;
  final Widget child;

  @override
  State<DetailViewHost> createState() => _DetailViewHostState();
}

class _DetailViewHostState extends State<DetailViewHost> {
  /// Whether this host currently has the reading route open — guards the
  /// edge into "a detail is showing" the same way
  /// `_ConfirmationSheetHostState._showing` guards the sheet, so repeated
  /// notifications while a detail is already up do not push a second route.
  bool _showing = false;

  @override
  void initState() {
    super.initState();
    widget.session.addListener(_onSessionChanged);
    // Same reason as _ConfirmationSheetHostState: a detail may already be
    // pending when this host mounts (set before DocumentScreen was pushed),
    // and the listener only fires on *changes*. No current caller sets a
    // detail pre-mount, but the guard keeps this host honest if one ever
    // does, mirroring the confirmation host exactly.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        return;
      }
      _onSessionChanged();
    });
  }

  @override
  void didUpdateWidget(covariant DetailViewHost oldWidget) {
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
  /// two edges that matter: a detail appearing opens the reading route; a
  /// detail already showing clearing to null (the session clearing
  /// transient state on navigation — see `session.dart`'s
  /// `_clearTransient`) closes it. Closing for a back press is [_show]'s
  /// job once its pushed future completes.
  void _onSessionChanged() {
    final hasDetail = widget.session.detail != null;
    if (hasDetail && !_showing) {
      _showing = true;
      _show();
    } else if (!hasDetail && _showing) {
      Navigator.of(context).maybePop();
    }
  }

  /// Pushes the reading route and, once it closes for *any* reason, clears
  /// the detail — the single place [SessionService.dismissDetail] is
  /// called on this screen's behalf, whether the close was a back press, a
  /// tap on the close action, or the session itself clearing the detail
  /// (which the `maybePop` above would have already handled).
  Future<void> _show() async {
    final session = widget.session;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => DetailScreen(session: session)),
    );
    _showing = false;
    if (session.detail != null) {
      session.dismissDetail();
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// The reading view itself: the value's heading as the AppBar title and its
/// full body below, wrapped and scrollable and selectable — see the module
/// comment. A pure function of [SessionService.detail]; it renders nothing
/// if the detail is already null (a transient frame on the way to the host
/// popping the route, not a state this widget has to handle).
class DetailScreen extends StatelessWidget {
  const DetailScreen({super.key, required this.session});

  final SessionService session;

  @override
  Widget build(BuildContext context) {
    final detail = session.detail;
    if (detail == null) {
      // The host is about to pop this route; show nothing in the meantime
      // rather than a flash of stale content.
      return const Scaffold(body: SizedBox.shrink());
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(
          detail.heading,
          // A long heading (a property name can be a sentence) wraps to two
          // lines, ellipsising only a heading too long even for that — the
          // body below is what the "never silently cut off" invariant is
          // actually for, and it neither wraps-limits nor ellipsises.
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
        ),
        // An explicit close action alongside the back button: a reading
        // view is a dead end (nothing to navigate to from here), so an
        // unambiguous "done" is worth the AppBar slot.
        actions: const [CloseButton()],
      ),
      body: SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
          child: SelectableText(
            detail.body,
            // The body type, a touch larger than the row it came from, so
            // the opened value reads as the thing worth a screen of its
            // own rather than the same line that almost fit.
            style: Theme.of(context).textTheme.bodyLarge,
            // SelectableText wraps by default at the screen width — the
            // whole point of this view — and the SingleChildScrollView
            // above takes anything that still overflows past the bottom.
          ),
        ),
      ),
    );
  }
}
