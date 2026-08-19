package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/quick"
	"github.com/snonux/restforge/cli/internal/session"
)

// homeQuickRow pairs one saved shortcut with the backend it currently
// resolves to -- nil once that backend has been renamed away or deleted,
// shown as "Backend removed" by homeDelegate rather than dropping the row.
// Mirrors home_screen.dart's private _QuickRow.
type homeQuickRow struct {
	item    quick.QuickItem
	backend *backend.Backend
}

// homeLoadedMsg reports that homeInitCmd's read of internal/config and
// internal/quick has finished. Unlike sessionUpdatedMsg (cmd.go), it
// carries the values themselves rather than nothing: this is a plain
// function call into two other packages, not a Session method whose state
// Update can just re-read off *session.Session afterwards.
type homeLoadedMsg struct {
	backends []backend.Backend
	rows     []homeQuickRow
	err      error
}

// homeInitCmd reads the configured backends and saved shortcuts once, when
// Home mounts -- wired from Model.Init. Session itself does not own
// backend-listing (this task's own scope note, mirroring
// nav_service.dart's module comment that the backend picker is out of
// NavService's scope), so this goes straight to internal/config and
// internal/quick rather than through Session. Wrapped in a tea.Cmd for the
// same reason every I/O in this package is -- see cmd.go's doc comment --
// even though this is a file read rather than a network call: Update must
// never block either way.
func homeInitCmd() tea.Cmd {
	return func() tea.Msg {
		backends, err := config.LoadBackends()
		if err != nil {
			return homeLoadedMsg{err: err}
		}
		rows, err := loadQuickRows(backends)
		if err != nil {
			return homeLoadedMsg{err: err}
		}
		return homeLoadedMsg{backends: backends, rows: rows}
	}
}

// loadQuickRows pairs every stored shortcut with the backend it currently
// resolves to, against the backend list homeInitCmd already loaded --
// quick.BackendsFor, not quick.BackendFor per row, so N shortcuts cost one
// backend load, not N+1 (mirrors home_screen.dart's _loadQuickRows and its
// own doc comment on the same reasoning).
func loadQuickRows(backends []backend.Backend) ([]homeQuickRow, error) {
	items, err := quick.Load()
	if err != nil {
		return nil, err
	}
	resolved := quick.BackendsFor(items, backends)
	rows := make([]homeQuickRow, len(items))
	for i, item := range items {
		rows[i] = homeQuickRow{item: item, backend: resolved[i]}
	}
	return rows, nil
}

// homeOpenedMsg reports that Home's call to Session.OpenBackend has
// returned. Kept distinct from sessionUpdatedMsg so Update knows
// specifically to switch the shell to the Document screen -- OpenBackend
// itself returns nothing to carry that decision, and sessionUpdatedMsg's
// payload-free contract deliberately does not disambiguate which call just
// completed (see cmd.go's doc comment on why a caller needing more than
// that defines its own Msg and its own small wrapper around the same
// goroutine-then-Msg shape).
type homeOpenedMsg struct{}

// openBackendCmd opens be at its root, off Update's own goroutine -- repeats
// sessionCmd's shape (cmd.go) rather than reusing it, because the caller
// (Model.Update) needs to tell this apart from any other
// sessionUpdatedMsg-producing call.
func openBackendCmd(sess *session.Session, be backend.Backend) tea.Cmd {
	return func() tea.Msg {
		sess.OpenBackend(be)
		return homeOpenedMsg{}
	}
}

// homeQuickRanMsg reports that Home's call to Session.RunQuick has
// returned, carrying the outcome RunQuick returns directly (session.
// QuickRunOutcome) plus the shortcut's own label, needed only for the
// QuickRunBackendMissing notice Update shows on Home itself -- mirrors
// home_screen.dart's _runShortcut showing a SnackBar on this same screen
// for that one outcome, rather than navigating anywhere.
type homeQuickRanMsg struct {
	outcome session.QuickRunOutcome
	label   string
	err     error
}

// runQuickCmd runs item via Session.RunQuick, off Update's own goroutine --
// see openBackendCmd's doc comment for why this defines its own Msg rather
// than reusing sessionCmd: RunQuick also returns a value
// (session.QuickRunOutcome) sessionUpdatedMsg's payload-free shape has
// nowhere to carry.
func runQuickCmd(sess *session.Session, item quick.QuickItem) tea.Cmd {
	return func() tea.Msg {
		outcome, err := sess.RunQuick(item)
		return homeQuickRanMsg{outcome: outcome, label: item.Label, err: err}
	}
}

// homeQuickDeletedMsg reports that Home's call to quick.RemoveItem
// (deleteSelectedQuick, home_update.go) has returned. ok mirrors
// RemoveItem's own "removed, or refused" distinction (ops.go); err is
// non-nil only when the underlying Save failed.
type homeQuickDeletedMsg struct {
	label string
	ok    bool
	err   error
}

// deleteQuickCmd removes item via quick.RemoveItem, off Update's own
// goroutine -- a TOML file write, same "never block Update" reasoning as
// every other I/O call in this package (see cmd.go's doc comment), even
// though it is local disk rather than network.
func deleteQuickCmd(item quick.QuickItem) tea.Cmd {
	return func() tea.Msg {
		ok, err := quick.RemoveItem(item)
		return homeQuickDeletedMsg{label: item.Label, ok: ok, err: err}
	}
}
