// Tests for the live-progress spinner and the live-watch threading fix
// (live_cmd.go, task 831): startSpinnerCmd/handleSpinnerTick (the spinner's
// tick loop), handleLiveTick (funnelling a poll through sessionCmd),
// postLiveTick/newLiveCreateTimer (the createTimer that posts a liveTickMsg
// instead of mutating Session on a bare timer goroutine), and an
// end-to-end run of a watchable action -> poll -> notice through Model.Update.
package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// --- a wall-clock-free clock + timer for internal/live -------------------

// liveTestClock is a fake now/createTimer pair handed to live.New so a
// watch is driven without a real wait -- mirrors internal/live's own
// fakeClock (live_test.go), kept here because that one lives in the live
// package's test binary and is not reachable from here. CreateTimer records
// the callback internal/live handed it (always *live.Live.poll in
// production) so a test can fire it through a liveTickMsg the way
// newLiveCreateTimer would in production.
type liveTestClock struct {
	now          time.Time
	lastCallback func()
}

func newLiveTestClock() *liveTestClock { return &liveTestClock{now: time.UnixMilli(500000)} }

func (c *liveTestClock) Now() time.Time { return c.now }

func (c *liveTestClock) CreateTimer(d time.Duration, callback func()) live.Timer {
	c.lastCallback = callback
	return &liveTestTimer{active: true}
}

// liveTestTimer satisfies live.Timer. Stop just flips active; no clock to
// remove from, since these tests never advance the clock -- the only poll
// that runs is the one a test fires explicitly through a liveTickMsg.
type liveTestTimer struct{ active bool }

func (t *liveTestTimer) Stop() bool {
	if !t.active {
		return false
	}
	t.active = false
	return true
}

// --- a fake msgSender for postLiveTick/newLiveCreateTimer ----------------

// fakeSender records what it was asked to Send -- the test double for the
// msgSender newLiveCreateTimer posts liveTickMsgs through (the production
// type is programSender, run.go).
type fakeSender struct {
	sent []tea.Msg
}

func (s *fakeSender) Send(msg tea.Msg) { s.sent = append(s.sent, msg) }

// --- the watchable-action fixture ----------------------------------------

// rootWithBrewAction is a root offering one safe (GET) action "brew" whose
// 202/running response starts a live watch, plus the kettle-job link the
// watch polls against -- the same shape internal/session's own session_test
// uses (see jobBody there), minimised to what these tests need.
func rootWithBrewAction() map[string]any {
	return map[string]any{
		"class": []any{"root"},
		"title": "Root",
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": testBaseURL},
			map[string]any{"rel": []any{"kettle-job"}, "href": "/job"},
		},
		"actions": []any{
			map[string]any{"name": "brew", "method": "GET", "href": "/brew", "title": "Brew"},
		},
	}
}

// jobBody is a poll reply's decoded body -- state and step parameterised so
// the progress and done cases share one shape.
func jobBody(step, state string) map[string]any {
	return map[string]any{
		"class": []any{"kettle-job"},
		"properties": map[string]any{
			"state": state, "id": float64(7), "step": step,
			"staleAfterSeconds": float64(120),
		},
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/job"}},
	}
}

// newLiveFixture builds a Model on a real Session whose *live.Live is wired
// to a fake clock/timer (so a watch is driven synchronously) and a fake
// client whose "brew" action responds 202/running and whose "/job" route
// replies jobReply, then opens the backend so Activate("brew") can start a
// watch. Returns the Model, the fake clock (whose lastCallback is the
// watch's poll once a watch has started) and the client (so a test can
// retarget "/job" between progress and done).
func newLiveFixture(t *testing.T, jobReply map[string]any) (Model, *liveTestClock, *fakeClient) {
	t.Helper()
	client := &fakeClient{
		routes: map[string]map[string]any{
			testBaseURL: rootWithBrewAction(),
			"/job":      jobReply,
		},
		requestFn: func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
			if href == "/brew" {
				return httpclient.HTTPResponse{Status: 202, Entity: jobReply}, nil
			}
			return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Client, Message: "newLiveFixture: no route for " + href}
		},
	}
	clock := newLiveTestClock()
	sess := session.New(
		nav.New(client),
		action.New(client),
		live.New(client, live.WithNow(clock.Now), live.WithCreateTimer(clock.CreateTimer)),
	)
	m := New(sess)
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.base = screenDocument
	return m, clock, client
}

// update is the one-line type-asserting wrapper every test in this file
// shares -- Model.Update returns tea.Model, the cases here all want Model.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// startWatch activates the fixture's "brew" action and resyncs, leaving the
// Model with a live watch running (IsLive true) and the watch's poll
// callback captured on clock.lastCallback -- the shared setup for every
// test that drives a poll.
func startWatch(t *testing.T, m Model, clock *liveTestClock) Model {
	t.Helper()
	m.session.Activate(render.ActionTarget{Name: "brew"})
	if !m.session.IsLive() {
		t.Fatal("test setup: Activate(\"brew\") did not start a live watch")
	}
	m, _ = update(m, sessionUpdatedMsg{}) // resync + start the spinner
	return m
}

// --- postLiveTick / newLiveCreateTimer -----------------------------------

func TestPostLiveTickSendsLiveTickMsgCarryingTheCallback(t *testing.T) {
	sender := &fakeSender{}
	called := false
	cb := func() { called = true }

	postLiveTick(sender, cb)()

	if len(sender.sent) != 1 {
		t.Fatalf("Send called %d times, want 1", len(sender.sent))
	}
	lt, ok := sender.sent[0].(liveTickMsg)
	if !ok {
		t.Fatalf("sent %T, want liveTickMsg", sender.sent[0])
	}
	if called {
		t.Error("postLiveTick invoked the callback itself; it should only carry it for Update to run")
	}
	if lt.fn == nil {
		t.Fatal("liveTickMsg.fn = nil, want the carried callback")
	}
	lt.fn()
	if !called {
		t.Error("the carried callback did not run when invoked")
	}
}

func TestNewLiveCreateTimerReturnsStoppableTimer(t *testing.T) {
	sender := &fakeSender{}
	create := newLiveCreateTimer(sender)

	// A long enough duration that the timer has not fired by the time Stop
	// runs a few microseconds later -- this is only checking the Timer
	// contract (Stop on an unfired timer returns true), not the posting.
	timer := create(time.Second, func() {})
	if timer == nil {
		t.Fatal("createTimer returned nil, want a live.Timer")
	}
	if !timer.Stop() {
		t.Error("Stop() = false on a fresh unfired timer, want true")
	}
}

// --- startSpinnerCmd / handleSpinnerTick ---------------------------------

func TestStartSpinnerCmdNilWhenNotLive(t *testing.T) {
	m, _ := newTestModel()
	if cmd := m.startSpinnerCmd(); cmd != nil {
		t.Error("startSpinnerCmd() = non-nil with no watch running, want nil")
	}
}

func TestStartSpinnerCmdStartsWhenLive(t *testing.T) {
	m, clock, _ := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)

	if cmd := m.startSpinnerCmd(); cmd == nil {
		t.Error("startSpinnerCmd() = nil while IsLive, want the spinner's Tick cmd")
	}
}

func TestHandleSpinnerTickStopsWhenNotLive(t *testing.T) {
	m, _ := newTestModel()
	// A spinner.TickMsg arriving once the watch has already ended: the loop
	// lets the chain die rather than rescheduling. IsLive is false here, so
	// the TickMsg is never passed to spinner.Model.Update -- the gate
	// returns first; the message is produced by a real spinner.Tick cmd so
	// its type (and tag) are honest, even though the tag is never read.
	tickMsg := newDocumentModel().spinner.Tick()
	next, cmd := update(m, tickMsg)
	if cmd != nil {
		t.Error("handleSpinnerTick returned a non-nil cmd when not live, want nil (chain ends)")
	}
	if next.startSpinnerCmd() != nil {
		t.Error("next startSpinnerCmd non-nil, want nil (still not live)")
	}
}

func TestHandleSpinnerTickReschedulesWhileLive(t *testing.T) {
	m, clock, _ := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)
	tick := m.startSpinnerCmd()
	if tick == nil {
		t.Fatal("startSpinnerCmd() = nil, want a Tick cmd to drive")
	}
	tickMsg := tick()

	next, reschedule := update(m, tickMsg)
	if reschedule == nil {
		t.Error("handleSpinnerTick returned nil while live, want a reschedule cmd")
	}
	if !next.session.IsLive() {
		t.Error("IsLive flipped false just from a spinner tick, want still true")
	}
}

// --- handleLiveTick: funnelling a poll through sessionCmd ----------------

func TestHandleLiveTickWrapsCallbackInSessionCmd(t *testing.T) {
	m, _ := newTestModel()
	called := false
	fn := func() { called = true }

	next, cmd := update(m, liveTickMsg{fn: fn})
	if cmd == nil {
		t.Fatal("handleLiveTick returned nil cmd, want sessionCmd(fn)")
	}
	msg := cmd()
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Fatalf("cmd() = %T, want sessionUpdatedMsg", msg)
	}
	if !called {
		t.Error("the poll callback was not run by the sessionCmd")
	}
	_ = next
}

// --- end-to-end: a watchable action -> poll -> notice through Update -----

func TestLivePollRoutesThroughUpdateAndReportsProgress(t *testing.T) {
	m, clock, _ := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)

	// Fire the watch's poll the way newLiveCreateTimer would in production:
	// as a liveTickMsg carrying the poll callback, delivered to Update.
	next, cmd := update(m, liveTickMsg{fn: clock.lastCallback})
	if cmd == nil {
		t.Fatal("Update(liveTickMsg) returned nil cmd, want sessionCmd wrapping the poll")
	}
	msg := cmd() // runs the poll synchronously here in the test
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Fatalf("poll cmd() = %T, want sessionUpdatedMsg", msg)
	}

	notice, ok := next.session.Notice().(session.ActionProgress)
	if !ok {
		t.Fatalf("Notice = %T, want ActionProgress after a running poll", next.session.Notice())
	}
	if notice.Step != "heating" {
		t.Errorf("ActionProgress.Step = %q, want heating (the server's own step)", notice.Step)
	}
	if !next.session.IsLive() {
		t.Error("IsLive = false after a running poll, want true: the watch is still going")
	}
}

func TestLivePollDoneClearsLiveAndReportsOutcome(t *testing.T) {
	m, clock, client := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)
	// Retarget the poll route to "done" so the next poll finishes the watch.
	client.routes["/job"] = jobBody("", "done")

	next, cmd := update(m, liveTickMsg{fn: clock.lastCallback})
	if cmd == nil {
		t.Fatal("Update(liveTickMsg) returned nil cmd, want sessionCmd wrapping the poll")
	}
	cmd() // run the poll -> onLiveDone -> notice + nav.Refresh

	notice, ok := next.session.Notice().(session.ActionOutcomeReported)
	if !ok {
		t.Fatalf("Notice = %T, want ActionOutcomeReported after a done poll", next.session.Notice())
	}
	if notice.Message != "done" {
		t.Errorf("Message = %q, want done", notice.Message)
	}
	if next.session.IsLive() {
		t.Error("IsLive = true after a done poll, want false: the watch is over")
	}
	// And the spinner loop now ends: startSpinnerCmd is a no-op once IsLive
	// is false, so the next resync does not reschedule it.
	if cmd := next.startSpinnerCmd(); cmd != nil {
		t.Error("startSpinnerCmd() = non-nil after the watch ended, want nil (loop ends)")
	}
}

// TestLivePollGiveUpClearsLiveAndReportsGaveUp exercises the give-up path
// end-to-end (not just the rendering, which document_notice_test.go covers):
// a poll reply that carries no "state" property is not judgeable, so
// internal/live's !judgeable branch gives up rather than reading silence as
// completion -- OnGiveUp -> ActionGaveUp, and IsLive goes false. This is the
// "Do not claim a job finished" branch the design cares most about, and the
// one the running/done tests above do not reach.
func TestLivePollGiveUpClearsLiveAndReportsGaveUp(t *testing.T) {
	m, clock, client := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)
	// A reply with the matching id but no "state" property: relevant (the id
	// matches) but not judgeable (nothing reports progress) -> give up.
	client.routes["/job"] = map[string]any{
		"class":      []any{"kettle-job"},
		"properties": map[string]any{"id": float64(7)},
		"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/job"}},
	}

	next, cmd := update(m, liveTickMsg{fn: clock.lastCallback})
	if cmd == nil {
		t.Fatal("Update(liveTickMsg) returned nil cmd, want sessionCmd wrapping the poll")
	}
	cmd() // run the poll -> onLiveGiveUp -> ActionGaveUp + nav.Refresh

	if _, ok := next.session.Notice().(session.ActionGaveUp); !ok {
		t.Fatalf("Notice = %T, want ActionGaveUp after a non-judgeable poll", next.session.Notice())
	}
	if next.session.IsLive() {
		t.Error("IsLive = true after a give-up poll, want false: the watch is over")
	}
	if cmd := next.startSpinnerCmd(); cmd != nil {
		t.Error("startSpinnerCmd() = non-nil after the watch gave up, want nil (loop ends)")
	}
}

// --- dismiss 'd' clears a finished notice --------------------------------

func TestDismissKeyClearsAFinishedNotice(t *testing.T) {
	m, clock, client := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)
	client.routes["/job"] = jobBody("", "done")
	_, cmd := update(m, liveTickMsg{fn: clock.lastCallback})
	cmd() // finish the watch -> ActionOutcomeReported, IsLive false
	if m.session.Notice() == nil {
		t.Fatal("test setup: no notice after a done poll")
	}

	next, _ := update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	if next.session.Notice() != nil {
		t.Errorf("Notice = %T after 'd', want nil (DismissNotice)", next.session.Notice())
	}
}

func TestDismissKeyDoesNothingWhileLive(t *testing.T) {
	m, clock, _ := newLiveFixture(t, jobBody("heating", "running"))
	m = startWatch(t, m, clock)
	// The watching banner is not dismissible (see dismissibleNoticeShowing),
	// so 'd' must not call DismissNotice -- the job keeps running underneath
	// regardless of the banner.
	before := m.session.Notice()

	next, cmd := update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd != nil {
		t.Error("'d' while live returned a non-nil cmd, want no-op")
	}
	if next.session.Notice() != before {
		t.Error("'d' while live changed the notice, want it left alone")
	}
}

// (spinnerTickMsg helpers removed: a real TickMsg is produced inline via
// spinner.Model.Tick() in the one test that needs one, so its tag stays
// honest rather than hand-constructed against an unexported field.)
