package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// settingsSavedMsg reports that settingsSaveCmd's call into
// config.SaveBackends has returned. Carries the error directly (nil on
// success) rather than reusing sessionUpdatedMsg's payload-free shape --
// this call has nothing to do with *session.Session, so there is no shared
// state for Update to re-read afterwards the way sessionCmd's callers do;
// see homeLoadedMsg's own doc comment (home_cmd.go) for the same reasoning
// applied to another config-owned, non-Session load.
type settingsSavedMsg struct {
	err error
}

// settingsSaveCmd writes backends through config.SaveBackends, off Update's
// own goroutine -- the same "never block Update on I/O" contract every
// screen in this package keeps, see cmd.go's doc comment on sessionCmd for
// the fuller reasoning (this is a plain config write rather than a Session
// call, so it defines its own small wrapper the same way homeInitCmd does,
// rather than going through sessionCmd).
func settingsSaveCmd(backends []backend.Backend) tea.Cmd {
	return func() tea.Msg {
		return settingsSavedMsg{err: config.SaveBackends(backends)}
	}
}
