/// The backend editor: add, edit, reorder and delete the Siren API backends
/// RESTForge can browse.
///
/// This is the Dart port of the Pebble phone app's settings page,
/// `pebble/src/pkjs/configpage.js` — see `pebble/tools/test-configpage.js`
/// for the cases `test/screens/settings_screen_test.dart` mirrors: add and
/// validate, reorder keeps unsaved edits, delete removes the right row, the
/// base-URL rules, cancel leaves storage alone.
///
/// One thing deliberately does not carry over. configpage.js embeds a
/// backend's current secret back into its input field, reveal toggle and
/// all, because a data: URI page has no server-side session to keep it out
/// of, and an unreadable key is one you cannot spot a typo in. That trade
/// isn't available for free here: this widget tree is inspectable in ways a
/// throwaway webview page is not (DevTools, a screenshot, a future test that
/// dumps the tree), so a loaded secret is kept in memory but never placed in
/// a [TextEditingController] — see [_BackendRow]. Editing an existing
/// backend starts with the secret field blank; leaving it blank at Save
/// keeps the stored secret, typing into it replaces it.
///
/// Validation and bounds (the name/URL rules, `maxBackends`, `maxFieldLength`)
/// live in [SettingsService] and are never duplicated here — this screen
/// only displays whatever [SettingsService.validate] says, and only ever
/// caps the "Add" button at [SettingsService.maxBackends] rather than
/// re-deriving that number.
library;

import 'package:flutter/material.dart';

import '../models/result.dart';
import '../services/settings_service.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, this.settingsService});

  /// Overrides the service this screen talks to. Left null in production so
  /// the constructor above stays const and the caller (the backend picker)
  /// never has to know a [SettingsService] exists; a widget test passes one
  /// wired to an in-memory secret fake instead of the real Keystore-backed
  /// store, which needs a platform channel `flutter test` does not have.
  final SettingsService? settingsService;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final SettingsService _settings =
      widget.settingsService ?? SettingsService();

  final List<_BackendRow> _rows = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final row in _rows) {
      row.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    final backends = await _settings.loadBackends();
    if (!mounted) {
      return;
    }
    setState(() {
      _rows.addAll(backends.map(_BackendRow.fromBackend));
      _loading = false;
    });
  }

  void _addRow() {
    if (_rows.length >= SettingsService.maxBackends) {
      return;
    }
    setState(() {
      _rows.add(_BackendRow.blank());
      _error = null;
    });
  }

  void _moveRow(int index, int delta) {
    final to = index + delta;
    if (to < 0 || to >= _rows.length) {
      return;
    }
    setState(() {
      final row = _rows.removeAt(index);
      _rows.insert(to, row);
    });
  }

  void _deleteRow(int index) {
    setState(() {
      _rows.removeAt(index).dispose();
      _error = null;
    });
  }

  void _cancel() {
    // An empty response in configpage.js is index.js's signal to leave
    // stored config alone; here that's just "never call saveBackends".
    if (Navigator.of(context).canPop()) {
      Navigator.of(context).pop(false);
    }
  }

  /// Validates every row through [SettingsService], stopping at the first
  /// problem exactly like configpage.js's save handler does, then persists
  /// the whole list through [SettingsService.saveBackends] — which is also
  /// what turns a deleted row into a deleted secret, see the module comment.
  Future<void> _save() async {
    final backends = <Backend>[];
    for (var i = 0; i < _rows.length; i++) {
      final backend = SettingsService.normalise(_rows[i].toRawMap());
      final problem = SettingsService.validate(backend);
      if (problem != null) {
        setState(() => _error = 'Backend ${i + 1}: $problem');
        return;
      }
      backends.add(backend);
    }

    final result = await _settings.saveBackends(backends);
    if (!mounted) {
      return;
    }
    switch (result) {
      case Ok():
        setState(() => _error = null);
        if (Navigator.of(context).canPop()) {
          Navigator.of(context).pop(true);
        }
      case Err(:final failure):
        setState(() => _error = failure.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Backends')),
      body: Column(
        children: [
          Expanded(
            child: _rows.isEmpty
                ? const Center(child: Text('No backends yet.'))
                : ListView.builder(
                    itemCount: _rows.length,
                    itemBuilder: (context, index) => _BackendCard(
                      key: ValueKey('card-$index'),
                      row: _rows[index],
                      index: index,
                      canMoveUp: index > 0,
                      canMoveDown: index < _rows.length - 1,
                      onMoveUp: () => _moveRow(index, -1),
                      onMoveDown: () => _moveRow(index, 1),
                      onDelete: () => _deleteRow(index),
                    ),
                  ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: OutlinedButton(
              key: const Key('add'),
              onPressed: _rows.length >= SettingsService.maxBackends
                  ? null
                  : _addRow,
              child: const Text('+ Add backend'),
            ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
              child: Text(
                _error!,
                key: const Key('error'),
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    key: const Key('cancel'),
                    onPressed: _cancel,
                    child: const Text('Cancel'),
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: FilledButton(
                    key: const Key('save'),
                    onPressed: _save,
                    child: const Text('Save'),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// One editable backend row: five [TextEditingController]s plus the secret
/// this row started with. [originalSecret] is never written into a
/// controller — see the module comment above — it exists only to be
/// substituted back in at save time when the secret field was left blank.
class _BackendRow {
  _BackendRow({
    String name = '',
    String baseUrl = '',
    String authHeader = '',
    String startRel = '',
    this.originalSecret = '',
  }) : nameController = TextEditingController(text: name),
       baseUrlController = TextEditingController(text: baseUrl),
       authHeaderController = TextEditingController(text: authHeader),
       startRelController = TextEditingController(text: startRel),
       secretController = TextEditingController();

  factory _BackendRow.fromBackend(Backend backend) => _BackendRow(
    name: backend.name,
    baseUrl: backend.baseUrl,
    authHeader: backend.authHeader,
    startRel: backend.startRel,
    originalSecret: backend.secret,
  );

  /// Mirrors configpage.js's "add" handler, which fills the new row's auth
  /// header in with the default rather than leaving it as a placeholder —
  /// there's nothing secret about it, so pre-filling costs nothing.
  factory _BackendRow.blank() =>
      _BackendRow(authHeader: SettingsService.defaultAuthHeader);

  final String originalSecret;

  final TextEditingController nameController;
  final TextEditingController baseUrlController;
  final TextEditingController authHeaderController;
  final TextEditingController secretController;
  final TextEditingController startRelController;

  /// The raw map [SettingsService.normalise] expects. A blank secret field
  /// resolves to [originalSecret] — leaving it blank means "keep the
  /// existing secret", not "clear it".
  Map<String, dynamic> toRawMap() => {
    'name': nameController.text,
    'baseUrl': baseUrlController.text,
    'authHeader': authHeaderController.text,
    'secret': secretController.text.isEmpty
        ? originalSecret
        : secretController.text,
    'startRel': startRelController.text,
  };

  void dispose() {
    nameController.dispose();
    baseUrlController.dispose();
    authHeaderController.dispose();
    secretController.dispose();
    startRelController.dispose();
  }
}

/// One backend's fields plus its reorder/delete tools — the widget
/// equivalent of configpage.js's `cardNode` + `toolsNode`.
class _BackendCard extends StatelessWidget {
  const _BackendCard({
    required super.key,
    required this.row,
    required this.index,
    required this.canMoveUp,
    required this.canMoveDown,
    required this.onMoveUp,
    required this.onMoveDown,
    required this.onDelete,
  });

  final _BackendRow row;
  final int index;
  final bool canMoveUp;
  final bool canMoveDown;
  final VoidCallback onMoveUp;
  final VoidCallback onMoveDown;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Backend ${index + 1}',
              style: Theme.of(context).textTheme.labelLarge,
            ),
            const SizedBox(height: 8),
            TextField(
              key: ValueKey('name-$index'),
              controller: row.nameController,
              decoration: const InputDecoration(
                labelText: 'Name',
                helperText: 'Shown on the backend picker',
              ),
            ),
            TextField(
              key: ValueKey('baseUrl-$index'),
              controller: row.baseUrlController,
              keyboardType: TextInputType.url,
              decoration: const InputDecoration(
                labelText: 'Base URL',
                helperText:
                    'The Siren API root. A trailing / is added if you omit it.',
              ),
            ),
            TextField(
              key: ValueKey('authHeader-$index'),
              controller: row.authHeaderController,
              decoration: const InputDecoration(
                labelText: 'Auth header',
                helperText:
                    'The secret is sent in this header, never in the URL.',
              ),
            ),
            TextField(
              key: ValueKey('secret-$index'),
              controller: row.secretController,
              obscureText: true,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: 'Secret',
                helperText: row.originalSecret.isEmpty
                    ? 'Kept on this device.'
                    : 'Leave blank to keep the existing secret.',
              ),
            ),
            TextField(
              key: ValueKey('startRel-$index'),
              controller: row.startRelController,
              decoration: const InputDecoration(
                labelText: 'Start at rel',
                helperText:
                    'Optional. A link rel to open straight after the root.',
              ),
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                IconButton(
                  key: ValueKey('up-$index'),
                  onPressed: canMoveUp ? onMoveUp : null,
                  icon: const Icon(Icons.arrow_upward),
                  tooltip: 'Move up',
                ),
                IconButton(
                  key: ValueKey('down-$index'),
                  onPressed: canMoveDown ? onMoveDown : null,
                  icon: const Icon(Icons.arrow_downward),
                  tooltip: 'Move down',
                ),
                const Spacer(),
                IconButton(
                  key: ValueKey('delete-$index'),
                  onPressed: onDelete,
                  icon: const Icon(Icons.delete_outline),
                  tooltip: 'Delete',
                  color: Theme.of(context).colorScheme.error,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
