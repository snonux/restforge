// Part of the document_screen library (task d21): the row widgets (_RowList, _RowTile, the four row kinds, and _saveQuickShortcut),
// split out so document_screen.dart is the Scaffold + state-dispatch shell.
//
part of 'document_screen.dart';

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
