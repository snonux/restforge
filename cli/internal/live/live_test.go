// Ported from flutter/test/services/live_service_test.dart (itself ported
// from pebble/tools/test-live.js) -- see live.go's package comment and that
// Dart file's header for the three details these tests exist to pin: where
// to poll, which answer is ours, and how long to wait.
//
// Matched from test-live.js: testWhatStartsAWatch, testPollTarget,
// testNothingToFollowMeansNoWatch, testSilenceIsNotCompletion,
// testProgressThenDone, testAnswerAboutAnotherJob, testNoJobHere,
// testFailedPollIsNotNews, testDeadlineFromTheServer, testFallbackDeadline,
// testBudgetIsReDerived, testStopIsFinal.
//
// live_service_test.dart drives FakeClock/tick() as an injected
// now/createTimer pair rather than monkey-patched globals, and each Dart
// test gets its own Env and its own LiveService since LiveService is a
// plain instance with no module-level state to reset by hand. This file
// does the same: fakeClock (this file) is handed to live.New via
// live.WithNow/live.WithCreateTimer, and every test builds its own env.
//
// Unlike LiveService's async, single-threaded _poll (which needs a few
// event-loop ticks after each fired timer for the awaited HTTP call to
// resolve -- see FakeClock.tick's comment in the Dart file), Live.poll runs
// to completion synchronously against fakeGetter (no real socket, no
// goroutine), so fakeClock.tick below fires each due timer and returns only
// once that timer's callback -- poll and everything it does, including any
// timer it reschedules -- has already finished.
package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/siren"
	"github.com/snonux/restforge/cli/internal/urlresolve"
)

const base = "http://host.example/"

func testBackend() backend.Backend {
	return backend.Backend{Name: "x", BaseURL: base, Secret: "s"}
}

// testOrigin is the origin document, linking to a job by a rel that
// matches the job's class -- mirrors test-live.js's ORIGIN.
func testOrigin() live.Origin {
	entity := siren.EntityFromJSON(map[string]any{
		"class": []any{"pantry"},
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": "/"},
			map[string]any{"rel": []any{"kettle-job"}, "href": "/job"},
		},
	})
	return live.Origin{Entity: entity, Href: base}
}

// job builds an ActionOutcome the way an action's response would arrive --
// mirrors test-live.js's job().
func job(props map[string]any, status int) live.ActionOutcome {
	return live.ActionOutcome{
		Status: status,
		Entity: siren.EntityFromJSON(jobBody(props)),
	}
}

// jobBody is a poll reply's decoded JSON body -- mirrors test-live.js's
// jobBody().
func jobBody(props map[string]any) map[string]any {
	return map[string]any{
		"class":      []any{"kettle-job"},
		"properties": props,
	}
}

// --- a wall-clock-free clock and timer, driven by tick -------------------

// fakeTimer is fakeClock's Timer: a pending callback the clock fires (or
// cancels) by hand rather than the wall clock.
type fakeTimer struct {
	clock    *fakeClock
	fireAt   time.Time
	callback func()
	active   bool
}

func (t *fakeTimer) Stop() bool {
	if !t.active {
		return false
	}
	t.active = false
	t.clock.remove(t)
	return true
}

// fakeClock is a fake now/createTimer pair, handed to live.New so a
// 24-second job or a 25-minute fallback deadline is driven without an
// actual wait -- the Go equivalent of live_service_test.dart's FakeClock.
type fakeClock struct {
	now     time.Time
	pending []*fakeTimer
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) CreateTimer(d time.Duration, callback func()) live.Timer {
	t := &fakeTimer{clock: c, fireAt: c.now.Add(d), callback: callback, active: true}
	c.pending = append(c.pending, t)
	return t
}

func (c *fakeClock) remove(t *fakeTimer) {
	for i, p := range c.pending {
		if p == t {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return
		}
	}
}

// tick advances the clock by d, firing every timer due at or before the new
// time, earliest first. A fired callback (always one of Live's own --
// reschedule itself, or give up) may schedule a further timer that also
// falls inside this same advance, so pending is re-scanned after each
// firing rather than snapshotted once -- mirrors test-live.js's tick() and
// live_service_test.dart's FakeClock.tick.
func (c *fakeClock) tick(d time.Duration) {
	until := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.pending {
			if !t.fireAt.After(until) && (next == nil || t.fireAt.Before(next.fireAt)) {
				next = t
			}
		}
		if next == nil {
			break
		}
		c.now = next.fireAt
		c.remove(next)
		next.callback()
	}
	c.now = until
}

// --- a fake backend, standing in for internal/httpclient.Client ----------

// fakeGetter is Live's test double for its httpGetter seam: it serves
// replyBody/replyStatus (or fails, when fail is set) to every poll, and
// logs the URLs polled -- mirrors the FakeXHR/reply/polls globals in
// test-live.js and Env's MockClient in live_service_test.dart.
type fakeGetter struct {
	polls       []string
	replyBody   map[string]any
	replyStatus int
	fail        bool

	// block, when set, is called with the request's ctx and the resolved
	// target as GetContext is entered, before replyBody/fail is consulted --
	// the hook the q31 cancellation tests use to hold a poll in flight and
	// assert on ctx.Done(), the same pattern nav_test.go's fakeClient.block
	// uses for nav's own cancellation tests.
	block func(ctx context.Context, target string)
}

func (f *fakeGetter) GetContext(ctx context.Context, be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	target := urlresolve.Resolve(href, be.BaseURL)
	f.polls = append(f.polls, target)
	if f.block != nil {
		f.block(ctx, target)
	}
	if f.fail {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Unreachable, Message: "connection refused"}
	}
	body := f.replyBody
	if body == nil {
		body = map[string]any{}
	}
	status := f.replyStatus
	if status == 0 {
		status = 200
	}
	return httpclient.HTTPResponse{Status: status, URL: target, Entity: body}, nil
}

// --- recording what a watch reported --------------------------------------

// seen records what a watch reported through its Handlers -- mirrors
// test-live.js's record() and live_service_test.dart's Seen.
type seen struct {
	progress []string
	done     string
	doneSet  bool
	gaveUp   bool
}

func handlersFor(s *seen) live.Handlers {
	return live.Handlers{
		OnProgress: func(entity siren.Entity) {
			step, _ := entity.Properties["step"].(string)
			s.progress = append(s.progress, step)
		},
		OnDone: func(entity siren.Entity) {
			state, _ := entity.Properties["state"].(string)
			s.done = state
			s.doneSet = true
		},
		OnGiveUp: func() { s.gaveUp = true },
	}
}

// --- one test's fixture ---------------------------------------------------

// env is one test's fixture: a fakeGetter, the fakeClock the live.Live under
// test is wired to, and the Live itself -- mirrors live_service_test.dart's
// Env, minus the shared mutable state Dart's own module-level watching/timer
// would otherwise need resetting by hand between tests.
type env struct {
	clock  *fakeClock
	getter *fakeGetter
	live   *live.Live
}

func newEnv() *env {
	clock := newFakeClock(time.UnixMilli(500000))
	getter := &fakeGetter{}
	l := live.New(getter,
		live.WithNow(clock.Now),
		live.WithCreateTimer(clock.CreateTimer),
		live.WithLog(func(string) {}),
	)
	return &env{clock: clock, getter: getter, live: l}
}

// begin starts a watch on a job entity with props, always via a 202 --
// mirrors test-live.js's begin() and Env.begin's default status.
func (e *env) begin(props map[string]any) (bool, *seen) {
	e.getter.polls = nil
	s := &seen{}
	started := e.live.Start(testBackend(), testOrigin(), job(props, 202), handlersFor(s))
	return started, s
}

// --- what counts as something to watch ------------------------------------

func TestA202StartsAWatch(t *testing.T) {
	if !live.ShouldWatch(job(map[string]any{"id": float64(1)}, 202)) {
		t.Error("a 202 should start a watch")
	}
}

func TestAnEntityThatSaysRunningStartsAWatch(t *testing.T) {
	if !live.ShouldWatch(job(map[string]any{"state": "running"}, 200)) {
		t.Error("an entity that says it is running should start a watch")
	}
}

// An ordinary answer is an answer. Watching it would be polling forever.
func TestAPlain200DoesNot(t *testing.T) {
	if live.ShouldWatch(job(map[string]any{"state": "done"}, 200)) {
		t.Error("a plain 200 should not start a watch")
	}
}

func TestAnEntityWithNoStateDoesNot(t *testing.T) {
	if live.ShouldWatch(job(map[string]any{}, 200)) {
		t.Error("an entity with no state should not start a watch")
	}
}

// --- the poll target --------------------------------------------------
//
// The poll target is found by matching the result's class against the
// origin's link rels -- an idiom, not a path this app knows.

func TestAMatchingRelIsTheThingToPoll(t *testing.T) {
	result := siren.EntityFromJSON(map[string]any{"class": []any{"kettle-job"}})
	if got := live.PollTarget(testOrigin().Entity, result); got != "/job" {
		t.Errorf("PollTarget() = %q, want %q", got, "/job")
	}
}

func TestAnUnmatchedClassPollsNothingInParticular(t *testing.T) {
	result := siren.EntityFromJSON(map[string]any{"class": []any{"unrelated"}})
	if got := live.PollTarget(testOrigin().Entity, result); got != "" {
		t.Errorf("PollTarget() = %q, want \"\"", got)
	}
}

// Found by looking at a real API: falling back to polling the origin
// document looks helpful and is a lie. The origin does not report the
// job's state, so the first poll would read its silence as completion and
// announce that the work finished seconds after it started.
func TestWithNothingTheServerOffersToTrackItNoWatchStarts(t *testing.T) {
	e := newEnv()
	s := &seen{}

	result := live.ActionOutcome{
		Status: 202,
		Entity: siren.EntityFromJSON(map[string]any{"class": []any{"unrelated"}}),
	}
	started := e.live.Start(testBackend(), testOrigin(), result, handlersFor(s))
	if started {
		t.Fatal("Start() = true, want false")
	}

	e.clock.tick(live.PollInterval * 3)
	if len(e.getter.polls) != 0 {
		t.Errorf("polls = %v, want none: nothing is polled", e.getter.polls)
	}
	if s.doneSet {
		t.Error("nothing should be claimed to have finished")
	}
}
