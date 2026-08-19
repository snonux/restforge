package tui

import (
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/session"
)

// Run builds the production internal/session.Session -- one
// *httpclient.Client composing internal/nav, internal/action and
// internal/live, exactly the trio internal/cli's one-shot commands build
// around the same client (see internal/cli/act.go's runAct) -- and runs it
// as a Bubble Tea program until the user quits.
//
// No backend is opened here: Session starts with nothing fetched, and
// deciding which backend to open (or which saved shortcut to run) is the
// Home screen's job (task 431), reading internal/config directly when it
// mounts. Run's job stops at wiring Session into the program.
//
// tea.WithAltScreen switches the terminal to its alternate screen buffer
// for the duration of the program, the same way a full-screen editor does,
// so restforge's own output does not scroll the user's shell history off
// screen and is cleanly restored on exit.
//
// Run needs a terminal: Bubble Tea opens the controlling TTY for input,
// and Program.Run returns an error when there is none (a piped stdin, no
// /dev/tty, or a headless CI run). That error is reported as a general
// failure by internal/cli's dispatch layer, not worked around here -- a
// TUI that cannot reach a terminal genuinely cannot run.
func Run() error {
	// The live watch's createTimer (live_cmd.go) needs the running
	// *tea.Program to post liveTickMsgs onto, but the Program is built from
	// the Model, which is built from the Session, whose *live.Live holds the
	// createTimer -- a cycle. programSender breaks it: it is captured by the
	// createTimer before the Program exists, and pointed at the Program once
	// Run has one; the first live timer only fires once p.Run is underway, so
	// the pointer is always set by the time Send is read. atomic.Pointer
	// keeps that read/write race-free across the timer's own goroutine and
	// this one.
	sender := &programSender{}
	p := tea.NewProgram(New(newProductionSession(sender)), tea.WithAltScreen())
	sender.store(p)
	_, err := p.Run()
	return err
}

// programSender is the production msgSender (live_cmd.go) newLiveCreateTimer
// posts liveTickMsgs through: a thin, race-free holder for the running
// *tea.Program, pointed at the Program the moment Run has built it. A nil
// program (before that point) makes Send a no-op rather than a panic; once
// the Program has quit, tea.Program.Send is itself a no-op on the cancelled
// context, so a late live timer firing after quit is harmless either way.
type programSender struct {
	program atomic.Pointer[tea.Program]
}

func (s *programSender) Send(msg tea.Msg) {
	if p := s.program.Load(); p != nil {
		p.Send(msg)
	}
}

func (s *programSender) store(p *tea.Program) { s.program.Store(p) }

// newProductionSession builds a Session on a fresh *httpclient.Client with
// every default (timeouts, logging) client, action and live construction
// already use for the one-shot CLI -- see internal/cli/act.go's runAct and
// watchAndPrint for the same construction, minus the options runAct needs
// only for the one-shot path (WithUserValues, WithLog for a silent live
// watch), which have no equivalent yet: the field values an action needs
// come from the interactive ValuePrompt overlay (task 631), not from a
// one-shot --field flag, and live's log line has nowhere to go in a
// full-screen TUI until a screen surfaces it.
//
// sender is the live watch's msgSender: internal/live's poll timer posts a
// liveTickMsg through it instead of mutating Session on a bare timer
// goroutine, so the poll runs inside a sessionCmd on a goroutine Bubble
// Tea's Update loop scheduled -- see live_cmd.go's own package comment for
// the data-race this is what closes.
func newProductionSession(sender msgSender) *session.Session {
	client := httpclient.New()
	return session.New(
		nav.New(client),
		action.New(client),
		live.New(client, live.WithCreateTimer(newLiveCreateTimer(sender))),
	)
}
