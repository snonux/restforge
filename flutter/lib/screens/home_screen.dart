/// The opening screen: the backend picker.
///
/// Lists whatever [SettingsService] has stored, with two ways into the
/// backend editor (`settings_screen.dart`, task r11): a general entry point
/// that is always on screen, and — when nothing is configured yet — an
/// empty state whose whole purpose is to lead there. Picking a backend from
/// a non-empty list opens the same editor for now, because there is nowhere
/// else to send it: fetching and rendering that backend's root document
/// needs the navigation stack and coordinator (`nav.js`/`session.js`'s
/// ports, tasks j11/p11) and the document screen (task s11), none of which
/// exist yet. Once they do, a tap should open the document instead — the
/// editor should stay reachable only from the general entry point.
///
/// Saved shortcuts (`quick.js`'s port, tasks w11/x11) are a second section
/// that belongs on this screen, below the backend list. Deliberately not
/// built here — see task x11 — but the body below is structured as a
/// [Column] of sections for exactly that reason, so adding one is additive
/// rather than a rewrite.
library;

import 'package:flutter/material.dart';

import '../services/settings_service.dart';
import 'settings_screen.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, this.settingsService});

  /// Overridden in widget tests with a [SettingsService] wired to fakes
  /// (see test/screens/home_screen_test.dart). Left null in the running
  /// app, which gets the default [SettingsService] — shared_preferences
  /// plus Keystore-backed secure storage.
  final SettingsService? settingsService;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late final SettingsService _settings =
      widget.settingsService ?? SettingsService();
  late Future<List<Backend>> _backendsFuture;

  @override
  void initState() {
    super.initState();
    _backendsFuture = _settings.loadBackends();
  }

  /// Re-reads the backend list. Called after the editor closes, since it is
  /// the only place a backend can be added, edited or removed — without
  /// this the picker would keep showing whatever it loaded at startup.
  void _reloadBackends() {
    setState(() {
      _backendsFuture = _settings.loadBackends();
    });
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
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          final backends = snapshot.data ?? const <Backend>[];
          return Column(
            children: [
              Expanded(
                child: backends.isEmpty
                    ? _EmptyState(onAddBackend: _openEditor)
                    : _BackendList(
                        backends: backends,
                        onTapBackend: (_) => _openEditor(),
                      ),
              ),
              // Saved shortcuts (task x11) go here as a second section,
              // below the backend list — not built yet, see the module
              // comment above.
            ],
          );
        },
      ),
    );
  }
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
