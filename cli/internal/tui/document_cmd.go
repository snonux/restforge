package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// documentQuickSavedMsg reports that Document's call to Session.SaveQuick
// (saveSelectedQuick, document_update.go) has returned, carrying the
// outcome SaveQuick returns directly (session.QuickSaveOutcome) -- mirrors
// homeQuickRanMsg's own reasoning (home_cmd.go) for why this defines its own
// Msg rather than reusing sessionCmd's payload-free sessionUpdatedMsg.
type documentQuickSavedMsg struct {
	label   string
	outcome session.QuickSaveOutcome
	err     error
}

// saveQuickCmd saves row as a shortcut via Session.SaveQuick, off Update's
// own goroutine -- a TOML file write, same "never block Update" reasoning
// deleteQuickCmd's own doc comment gives (home_cmd.go), even though this is
// local disk rather than network.
func saveQuickCmd(sess *session.Session, row render.Row) tea.Cmd {
	return func() tea.Msg {
		outcome, err := sess.SaveQuick(row)
		return documentQuickSavedMsg{label: row.Label, outcome: outcome, err: err}
	}
}
