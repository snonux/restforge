// Part of the document_screen library (task d21): the state-banner widgets (_FailureBanner, _NoticeBanner, _watchingBanner, _outcomeBanner, _Banner, _FullScreenFailure),
// split out so document_screen.dart is the Scaffold + state-dispatch shell.
//
part of 'document_screen.dart';

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
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: foreground,
                ),
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
