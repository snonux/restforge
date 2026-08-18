/// The opening screen: the backend picker.
///
/// Lists whatever [SettingsService] has stored, with two ways into the
/// backend editor (`settings_screen.dart`, task r11): a general entry point
/// that is always on screen, and — when nothing is configured yet — an
/// empty state whose whole purpose is to lead there. Picking a backend from
/// a non-empty list opens it for browsing — [_openBackend] builds a fresh
/// [SessionService], starts its root fetch, and pushes `document_screen.dart`
/// to render whatever comes back. The editor stays reachable only from the
/// general entry point (the AppBar action and the empty state), never from a
/// backend tap, so a tap on a configured row is always "browse this", never
/// "edit this".
///
/// **Saved shortcuts** (`quick.js`'s port, tasks w11/x11) are the second
/// section, below the backend list — [_QuickSection]. Listing and removing
/// one is pure storage ([QuickService.load]/[QuickService.remove]), so this
/// screen talks to [QuickService] directly, the same way it talks to
/// [SettingsService] for the backend list; there is nothing to compose. Each
/// row also shows the shortcut's *current* backend, resolved by base URL
/// ([QuickService.backendsFor]) rather than frozen at save time — a renamed
/// or deleted backend is reflected (or reported as "backend removed") the
/// next time this screen loads, never silently dropped.
///
/// **Running a shortcut** ([_runShortcut]) is different: it needs to adopt a
/// backend, fetch, and — for an action — go through the same confirmation a
/// hand-pressed action gets, which is exactly the composition
/// `session.dart`'s module comment reserves for [SessionService]. This
/// screen builds one on demand for the run (this app has no longer-lived
/// session to reuse yet — nothing before this task ever pushed
/// `document_screen.dart`), hands it to [SessionService.runQuick], and only
/// then pushes `DocumentScreen` with it — mirrors `session.js`'s `runQuick`
/// deciding, before anything is shown, whether there is a document to show
/// at all (see [QuickRunOutcome.backendMissing]).
library;

import 'dart:async';

import 'package:flutter/material.dart';

import '../services/http_service.dart';
import '../services/quick_service.dart';
import '../services/session.dart';
import '../services/settings_service.dart';
import 'document_screen.dart';
import 'settings_screen.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({
    super.key,
    this.settingsService,
    this.quickService,
    this.httpService,
  });

  /// Overridden in widget tests with a [SettingsService] wired to fakes
  /// (see test/screens/home_screen_test.dart). Left null in the running
  /// app, which gets the default [SettingsService] — shared_preferences
  /// plus Keystore-backed secure storage.
  final SettingsService? settingsService;

  /// Overridden in widget tests the same way [settingsService] is. Left
  /// null in the running app, which gets a [QuickService] built on whatever
  /// [settingsService] resolves to — see the module comment on why the two
  /// share one [SettingsService] rather than each constructing their own.
  final QuickService? quickService;

  /// Overridden in widget tests with an [HttpService] wrapping a faked
  /// `http.Client` (see `document_screen_test.dart`'s `_Env`), so running a
  /// shortcut (task x11) can be exercised without a real network — the
  /// running app leaves this null and [_HomeScreenState._runShortcut]
  /// constructs a plain [HttpService]. Injected rather than reached into,
  /// the same way [settingsService] and [quickService] are.
  final HttpService? httpService;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late final SettingsService _settings =
      widget.settingsService ?? SettingsService();
  late final QuickService _quick =
      widget.quickService ?? QuickService(settings: _settings);
  late Future<List<Backend>> _backendsFuture;
  late Future<List<_QuickRow>> _quickFuture;

  @override
  void initState() {
    super.initState();
    _backendsFuture = _settings.loadBackends();
    _quickFuture = _loadQuickRows();
  }

  /// Re-reads the backend list. Called after the editor closes, since it is
  /// the only place a backend can be added, edited or removed — without
  /// this the picker would keep showing whatever it loaded at startup.
  /// Also reloads the shortcuts list: a shortcut's shown backend name (or
  /// "backend removed") is resolved against the *current* backend list
  /// (see [QuickService.backendsFor]), which the editor may have just
  /// changed.
  void _reloadBackends() {
    setState(() {
      _backendsFuture = _settings.loadBackends();
      _quickFuture = _loadQuickRows();
    });
  }

  /// Re-reads only the shortcuts list — used after saving, removing or
  /// running one, none of which touch the backend list itself.
  void _reloadQuick() {
    setState(() {
      _quickFuture = _loadQuickRows();
    });
  }

  /// Pairs every stored shortcut with the backend it currently resolves to
  /// (or null — see [_QuickTile]'s "backend removed" case), resolving against
  /// the backend list this screen has *already* loaded for the picker
  /// ([_backendsFuture]) via [QuickService.backendsFor] — not [backendFor],
  /// which would re-read shared_preferences and re-hydrate every secret from
  /// Keystore-backed secure storage once per shortcut. N shortcuts cost one
  /// backend load this way, not N+1, and secure storage is slow on Android.
  Future<List<_QuickRow>> _loadQuickRows() async {
    final items = await _quick.load();
    final backends = await _backendsFuture;
    final resolved = _quick.backendsFor(items, backends);
    return [
      for (var i = 0; i < items.length; i++)
        _QuickRow(item: items[i], backend: resolved[i]),
    ];
  }

  Future<void> _openEditor() async {
    await Navigator.push(
      context,
      MaterialPageRoute(builder: (_) => const SettingsScreen()),
    );
    if (!mounted) {
      return;
    }
    _reloadBackends();
  }

  /// Opens [backend] at its root for browsing — the now-wired counterpart to
  /// this screen's original skeleton behaviour of sending a backend tap to
  /// the editor because there was nowhere else to send it (see the module
  /// comment). Builds a fresh [SessionService] for the visit — this screen
  /// keeps none around between visits, the same shape as [_runShortcut] —
  /// starts the root fetch without awaiting it so [DocumentScreen] mounts
  /// straight into the loading state rather than the home screen freezing
  /// on a slow network, and pushes it. Whatever the fetch lands as — the
  /// document, a failure, an unreachable — is [DocumentScreen]'s to render,
  /// which is exactly the set of states it was written for. On return the
  /// shortcut list is re-read (a shortcut may have been saved during the
  /// visit, as with [_runShortcut]) and the session is disposed.
  Future<void> _openBackend(Backend backend) async {
    final session = SessionService(
      http: widget.httpService ?? HttpService(),
      quick: _quick,
    );
    unawaited(session.openBackend(backend));
    // Defensive: there is no `await` between the unawaited call above and
    // this check, so `mounted` cannot be false here yet — but the guard
    // matches _runShortcut's shape and stays correct if a future change
    // inserts an await before the push.
    if (!mounted) {
      session.dispose();
      return;
    }
    await Navigator.push(
      context,
      MaterialPageRoute(builder: (_) => DocumentScreen(session: session)),
    );
    if (mounted) {
      _reloadQuick();
    }
    session.dispose();
  }

  /// Drops a saved shortcut and reports it — never silent, per
  /// `pebble/docs/DESIGN.md`-style "surface it" and this task's own
  /// requirement that removing a shortcut must be, too.
  Future<void> _removeShortcut(int index) async {
    final messenger = ScaffoldMessenger.of(context);
    final removed = await _quick.remove(index);
    if (!mounted) {
      return;
    }
    _reloadQuick();
    messenger.showSnackBar(
      SnackBar(
        content: Text(removed ? 'Removed shortcut' : 'Could not remove it'),
      ),
    );
  }

  /// Runs a saved shortcut: builds a fresh [SessionService] (this screen
  /// keeps none around between visits — see the module comment), hands it
  /// to [SessionService.runQuick], and pushes `DocumentScreen` with it only
  /// once a backend was actually resolved. On
  /// [QuickRunOutcome.backendMissing] nothing is pushed — the shortcut
  /// points at a backend that no longer exists, reported here rather than
  /// navigating into a document that was never fetched.
  Future<void> _runShortcut(QuickItem item) async {
    final messenger = ScaffoldMessenger.of(context);
    final session = SessionService(
      http: widget.httpService ?? HttpService(),
      quick: _quick,
    );
    final outcome = await session.runQuick(item);
    if (!mounted) {
      session.dispose();
      return;
    }
    if (outcome == QuickRunOutcome.backendMissing) {
      session.dispose();
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            '"${item.label}": its backend is no longer configured. Remove '
            'it below.',
          ),
        ),
      );
      return;
    }
    await Navigator.push(
      context,
      MaterialPageRoute(builder: (_) => DocumentScreen(session: session)),
    );
    if (mounted) {
      // The document screen this pushed is itself a place shortcuts can be
      // saved from (its rows' long-press affordance), so the list may have
      // grown while it was open. Re-read it, exactly as `_openEditor` does
      // on its return — without this a shortcut saved during the visit
      // would be silently absent from the opening screen until something
      // else happened to reload it, the kind of silent drop this task says
      // never to let happen.
      _reloadQuick();
    }
    session.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('RESTForge'),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            tooltip: 'Backends',
            onPressed: _openEditor,
          ),
        ],
      ),
      body: FutureBuilder<List<Backend>>(
        future: _backendsFuture,
        builder: (context, backendsSnapshot) {
          if (backendsSnapshot.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          final backends = backendsSnapshot.data ?? const <Backend>[];
          return FutureBuilder<List<_QuickRow>>(
            future: _quickFuture,
            builder: (context, quickSnapshot) {
              final quickRows = quickSnapshot.data ?? const <_QuickRow>[];
              return Column(
                children: [
                  Expanded(
                    flex: 3,
                    child: backends.isEmpty
                        ? _EmptyState(onAddBackend: _openEditor)
                        : _BackendList(
                            backends: backends,
                            onTapBackend: _openBackend,
                          ),
                  ),
                  if (quickRows.isNotEmpty)
                    Expanded(
                      flex: 2,
                      child: _QuickSection(
                        rows: quickRows,
                        onRun: _runShortcut,
                        onRemove: _removeShortcut,
                      ),
                    ),
                ],
              );
            },
          );
        },
      ),
    );
  }
}

/// One saved shortcut paired with the backend it currently resolves to —
/// null once that backend has been renamed away or deleted (see
/// [QuickService.backendsFor]), which [_QuickTile] shows as "backend
/// removed" rather than dropping the row.
class _QuickRow {
  const _QuickRow({required this.item, required this.backend});

  final QuickItem item;
  final Backend? backend;
}

/// Shown when [SettingsService] has nothing stored. Its only job is to
/// explain what a backend is and lead straight to the editor that adds one.
class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.onAddBackend});

  final VoidCallback onAddBackend;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.travel_explore,
              size: 64,
              color: theme.colorScheme.primary,
            ),
            const SizedBox(height: 16),
            Text('No backends configured', style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              'A backend is an API root, an auth header and a secret. '
              'Add one and RESTForge will render whatever it offers.',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 24),
            FilledButton.icon(
              onPressed: onAddBackend,
              icon: const Icon(Icons.add),
              label: const Text('Add a backend'),
            ),
          ],
        ),
      ),
    );
  }
}

/// The configured backends, one row each. Tapping a row opens the editor —
/// see the module comment for why that, rather than browsing, is what a tap
/// does today.
class _BackendList extends StatelessWidget {
  const _BackendList({required this.backends, required this.onTapBackend});

  final List<Backend> backends;
  final ValueChanged<Backend> onTapBackend;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      itemCount: backends.length,
      separatorBuilder: (context, index) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final backend = backends[index];
        return ListTile(
          leading: const Icon(Icons.dns_outlined),
          title: Text(backend.name),
          subtitle: Text(backend.baseUrl),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => onTapBackend(backend),
        );
      },
    );
  }
}

/// The saved-shortcuts section, below the backend list — see the module
/// comment. A header (so the two sections are never mistaken for one list)
/// over a scrollable list of [_QuickTile]s, one per [QuickItem] saved from
/// `document_screen.dart`'s long-press affordance (task x11).
class _QuickSection extends StatelessWidget {
  const _QuickSection({
    required this.rows,
    required this.onRun,
    required this.onRemove,
  });

  final List<_QuickRow> rows;
  final ValueChanged<QuickItem> onRun;
  final ValueChanged<int> onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Divider(height: 1),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 4),
          child: Text('Shortcuts', style: theme.textTheme.labelLarge),
        ),
        Expanded(
          child: ListView.separated(
            itemCount: rows.length,
            separatorBuilder: (context, index) => const Divider(height: 1),
            itemBuilder: (context, index) {
              final row = rows[index];
              return _QuickTile(
                row: row,
                onTap: () => onRun(row.item),
                onRemove: () => onRemove(index),
              );
            },
          ),
        ),
      ],
    );
  }
}

/// One saved shortcut: [QuickItem.label], and the backend it currently
/// resolves to as the subtitle — or, when [_QuickRow.backend] is null, a
/// distinct "backend removed" wording rather than dropping the row (mirrors
/// `rows()` in `quick.js`: kept and marked, not silently dropped, so it can
/// be removed deliberately with [onRemove] instead of vanishing on its
/// own). Tapping the tile runs it; the trailing delete button is the only
/// way to remove a shortcut, and always reports what it did (see
/// `_HomeScreenState._removeShortcut`) — this task's "never silent" rule
/// for both bounds this file has to surface.
class _QuickTile extends StatelessWidget {
  const _QuickTile({
    required this.row,
    required this.onTap,
    required this.onRemove,
  });

  final _QuickRow row;
  final VoidCallback onTap;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final backend = row.backend;
    return ListTile(
      leading: Icon(
        row.item.kind == QuickKind.action ? Icons.bolt : Icons.link,
      ),
      title: Text(row.item.label),
      subtitle: Text(
        backend?.name ?? 'Backend removed',
        style: backend == null
            ? TextStyle(color: theme.colorScheme.error)
            : null,
      ),
      trailing: IconButton(
        icon: const Icon(Icons.delete_outline),
        tooltip: 'Remove shortcut',
        onPressed: onRemove,
      ),
      onTap: onTap,
    );
  }
}
