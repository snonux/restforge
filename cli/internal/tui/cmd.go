package tui

import tea "github.com/charmbracelet/bubbletea"

// sessionUpdatedMsg reports that a sessionCmd's call into Session has
// returned. It carries no payload: Session already holds whatever changed
// (Document, State, Failure, Detail, Question, Notice -- see
// internal/session's own accessors), and Update re-derives the screen from
// Session fresh on every call rather than threading a copy of its state
// through messages -- see internal/session's package comment on why no
// observer/cached-copy pattern is needed for a caller of its methods
// either.
type sessionUpdatedMsg struct{}

// sessionCmd is the one async pattern every screen task built on top of
// this shell reuses for a Session method that performs I/O -- fetching a
// document, invoking an action, or (once a screen surfaces it) following a
// blocking poll. Documented once here, since every later screen task
// depends on it:
//
//   - internal/session's own methods (Activate, Back, Refresh, Answer,
//     AnswerValue, RunQuick, SaveQuick, OpenBackend) are synchronous: each
//     one calls out over HTTP and does not return until that call has
//     landed, mutating Session's own fields in place before returning --
//     see internal/session's package comment on why Session needs no
//     observer of its own.
//   - Bubble Tea's Update must never block waiting on that: it is the one
//     goroutine driving both event handling and rendering, and a blocked
//     Update is a frozen terminal.
//   - So a screen wraps the call in a fn passed to sessionCmd, and returns
//     the resulting tea.Cmd from Update instead of calling fn directly. The
//     Bubble Tea runtime runs the returned tea.Cmd on its own goroutine;
//     fn's mutation of Session happens there, off Update's goroutine, and
//     the sessionUpdatedMsg it returns is delivered back to Update once fn
//     has returned -- Update (and so View, which only ever runs between
//     Update calls, never concurrently with one) is never blocked and
//     never touches Session while fn is still running.
//
// Typical use, once a screen has a row's target in hand:
//
//	return m, sessionCmd(func() { m.session.Activate(target) })
//
// A screen that also needs a value out of Session once fn completes
// (Session.RunQuick and Session.SaveQuick return an outcome and an error,
// rather than only mutating Session in place) defines its own Msg type
// carrying that value and its own small wrapper around this same
// goroutine-then-Msg shape, rather than forcing every caller through
// sessionUpdatedMsg's empty payload -- sessionCmd only covers the common,
// payload-free case.
//
// # What sessionCmd does not solve
//
// internal/live's watch (started inside Session.Activate/Answer/
// AnswerValue whenever an action's response is watchable) keeps running
// after the sessionCmd call that started it returns: its own background
// timer fires later, on its own goroutine, and directly mutates Session's
// notice field through the Handlers Session registered with it (see
// internal/session's package comment, "Live watches and the two callers").
// That mutation does not go through sessionCmd or arrive as a
// sessionUpdatedMsg at all -- it is a second, independent source of
// concurrent access to the same *Session this Model holds, and is exactly
// the hazard internal/session's package comment flags as unsolved by that
// package and left to internal/tui. It is still unsolved here: no screen
// in this shell yet starts a watchable action, so nothing here yet
// triggers it. Whichever screen task first does (Confirm/ValuePrompt,
// task 631, or the live-progress banner, task 831) must adapt it -- for
// example by giving internal/live's Live a createTimer that posts a
// tea.Msg to the running tea.Program instead of firing its callback
// directly on a bare timer goroutine, so the mutation is funnelled back
// through Update the same way sessionCmd funnels an ordinary request/
// response round trip.
func sessionCmd(fn func()) tea.Cmd {
	return func() tea.Msg {
		fn()
		return sessionUpdatedMsg{}
	}
}
