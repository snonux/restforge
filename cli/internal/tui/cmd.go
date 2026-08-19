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
//     Bubble Tea runtime runs the returned tea.Cmd on its own goroutine
//     (bubbletea's handleCommands does a bare `go func() { p.Send(cmd())
//     }()` per command and does not wait for it), so fn's mutation of
//     Session happens off Update's goroutine, and the sessionUpdatedMsg it
//     returns is delivered back to Update once fn has returned.
//
// # View runs concurrently with fn, not just with Update
//
// The above keeps Update itself from ever blocking, but it does NOT mean
// View is safe from fn while fn is still running. eventLoop calls View
// again on every message it processes (resize, a tick, a keypress, ...),
// and it keeps doing that on its own goroutine while a still-running
// sessionCmd goroutine is off mutating *Session (and, through it, *nav.Nav)
// on a different one -- there is no rendezvous between them until fn's
// result message arrives. A screen's View reading Session.Document/State/
// Failure/Detail/Question/Notice/IsLive is therefore a genuine data race
// against whichever cmd goroutine is currently running, for as long as any
// sessionCmd (or the liveTickMsg handling below) is in flight -- which,
// for a slow backend or a still-running watch, is a real, not
// theoretical, window. This is why internal/session.Session and
// internal/nav.Nav each guard their own fields with a sync.RWMutex --
// Update/View take the read lock through the ordinary accessors, and every
// mutating method (called from a cmd goroutine) takes the write lock only
// around the field writes themselves, never across the HTTP call that
// produces the new value -- so a render mid-fetch sees either the old,
// fully-consistent state or the new one, never a torn read, and the
// terminal keeps redrawing (a loading state, a live spinner) throughout
// the request instead of freezing for its duration.
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
// # The live-timer hazard, and how it is funnelled through this same shape
//
// internal/live's watch (started inside Session.Activate/Answer/
// AnswerValue whenever an action's response is watchable) keeps running
// after the sessionCmd call that started it returns: its own poll timer
// fires later, on its own goroutine. Left alone, that would be a second,
// independent, *unfunnelled* source of concurrent access to Session --
// exactly the hazard internal/session's package comment flags as left to
// internal/tui to solve. live_cmd.go solves it by giving internal/live's
// Live a createTimer that, instead of invoking the poll callback directly
// on the timer's own goroutine, posts a liveTickMsg to the running
// *tea.Program; Update receives that like any other message and hands the
// callback to handleLiveTick, which wraps it in this same sessionCmd --
// see live_cmd.go and run.go's programSender. So every mutation of
// Session, including a live watch's, now goes through a sessionCmd
// goroutine one way or another, and is safe against a concurrent View for
// the reason above: Session's own mutex, not the goroutine boundary alone.
func sessionCmd(fn func()) tea.Cmd {
	return func() tea.Msg {
		fn()
		return sessionUpdatedMsg{}
	}
}
