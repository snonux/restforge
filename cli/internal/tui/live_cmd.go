// The threading fix task 831 exists to land: internal/live's own package
// comment ("A note for whoever wires this into a Bubble Tea program") and
// cmd.go's doc comment ("What sessionCmd does not solve") both flag the same
// hazard -- a live watch's poll timer fires on its own goroutine, and the
// internal/live.Handlers it invokes from there (wired up inside
// internal/session, out of this package's control) mutate *session.Session
// directly. Update runs on a single goroutine and touches the very same
// Session, so an unmanaged timer goroutine doing that too is a data race.
//
// The fix: internal/live.New takes a createTimer option
// (internal/live.WithCreateTimer). newLiveCreateTimer below builds one that,
// instead of invoking the poll callback straight from the timer's own
// goroutine (the package default), posts a liveTickMsg to the running
// *tea.Program. Update receives that like any other message and hands the
// callback to sessionCmd (cmd.go) -- the exact same funnel every other
// Session-mutating call in this package already goes through -- so the
// callback (internal/live's own *Live.poll, which performs the HTTP poll and
// synchronously invokes whichever Handlers callback applies) runs inside a
// tea.Cmd Update itself returned, never on a goroutine Update never
// scheduled.
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/live"
)

// liveTickMsg reports that a live watch's poll timer has fired. fn is
// exactly the callback internal/live.Live passed to the createTimer
// function this package installs (always *live.Live.poll in production --
// see internal/live's own package comment) -- see handleLiveTick for what
// happens to it.
type liveTickMsg struct {
	fn func()
}

// handleLiveTick funnels a liveTickMsg's callback through sessionCmd
// (cmd.go): fn performs the HTTP poll and, on the same call, may invoke
// whichever of internal/live.Handlers.OnProgress/OnDone/OnGiveUp applies
// (internal/session's package comment, "Live watches and the two callers"),
// mutating Session's own Notice field. Running it through sessionCmd puts
// that mutation on a tea.Cmd goroutine that reports back with a
// sessionUpdatedMsg once fn has returned, so Update (and the resync it does
// on that message -- see model.go) is never touched while fn is still
// running, exactly like every other Session-mutating call in this package.
func (m Model) handleLiveTick(msg liveTickMsg) (tea.Model, tea.Cmd) {
	return m, sessionCmd(msg.fn)
}

// msgSender is the one *tea.Program method newLiveCreateTimer needs, split
// out as its own interface so a test can drive newLiveCreateTimer against a
// fake instead of a running Program -- the same dependency-inversion seam
// documentSource/screenSource use one layer down, for the same reason.
type msgSender interface {
	Send(msg tea.Msg)
}

// *tea.Program satisfies msgSender structurally; asserted here so a future
// signature change to Send is caught at compile time, not wherever run.go
// wires this up.
var _ msgSender = (*tea.Program)(nil)

// newLiveCreateTimer builds the live.Option run.go installs on the
// production *live.Live: a createTimer that, once the requested duration
// has elapsed, posts a liveTickMsg to sender instead of invoking the poll
// callback directly on the timer's own goroutine -- see this file's own
// package comment for the hazard this closes.
//
// *time.Timer already satisfies live.Timer with no adapter needed (see
// live.go's own compile-time assertion), so the Timer returned here is
// exactly what time.AfterFunc hands back; only what runs when it fires
// changes. The "what runs" part is postLiveTick, split out so it is testable
// on its own without driving a real time.AfterFunc -- see live_cmd_test.go.
func newLiveCreateTimer(sender msgSender) func(time.Duration, func()) live.Timer {
	return func(d time.Duration, callback func()) live.Timer {
		return time.AfterFunc(d, postLiveTick(sender, callback))
	}
}

// postLiveTick wraps callback into the func the live watch's timer runs
// when it fires: post a liveTickMsg carrying callback to sender, so the
// poll (and the Session mutation it performs through internal/live's
// Handlers) is funnelled back through Update via sessionCmd rather than
// running on the timer's own goroutine. Split out of newLiveCreateTimer so
// the wrapping is unit-testable without a real time.AfterFunc wait -- the
// scheduling (time.AfterFunc) is trivially correct composition on top of
// this, and not what needs pinning.
func postLiveTick(sender msgSender, callback func()) func() {
	return func() { sender.Send(liveTickMsg{fn: callback}) }
}

// --- the live-progress spinner's tick loop ---------------------------------
//
// documentModel.spinner (document.go) only animates while Session.IsLive is
// true; startSpinnerCmd/handleSpinnerTick are the Bubble Tea side of that --
// unrelated to the threading fix above beyond sharing this file, since a
// spinner.TickMsg is Bubble Tea's own message loop working exactly as
// intended (see bubbles/spinner's own doc comment on Model.Tick), not a
// second copy of the createTimer/liveTickMsg hazard.

// startSpinnerCmd starts, or continues, the live-progress spinner's tick
// loop when Session.IsLive is true, and does nothing otherwise. Called from
// every place Model.Update resyncs the Document screen from Session (see
// model.go) -- documentModel.syncRows's own doc comment explains why that
// resync is cheap to call unconditionally at each of those sites; the same
// reasoning applies here. Safe to call even while a tick chain from an
// earlier call is already running: spinner.Model.Tick's message carries the
// spinner's current generation tag, and handleSpinnerTick's call into
// spinner.Model.Update drops any message whose tag has gone stale (see
// bubbles/spinner's own Update), so a duplicate Tick here never doubles the
// animation speed.
func (m Model) startSpinnerCmd() tea.Cmd {
	if !m.session.IsLive() {
		return nil
	}
	return m.document.spinner.Tick
}

// handleSpinnerTick advances the live-progress spinner one frame and
// reschedules the next tick -- but only while Session.IsLive is still true.
// Once a watch finishes or gives up, IsLive goes false and this simply lets
// the chain a prior startSpinnerCmd started end here rather than
// rescheduling once more, so no explicit "stop" call is needed anywhere a
// watch's terminal handler runs.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.session.IsLive() {
		return m, nil
	}
	var cmd tea.Cmd
	m.document.spinner, cmd = m.document.spinner.Update(msg)
	return m, cmd
}
