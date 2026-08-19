package live_test

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/siren"
)

// WaitForLive's blocking loop is driven by real time (time.Sleep), so the
// cases that would actually wait -- a still-running job polled until done
// or until the deadline -- are not unit-tested here; they would need a real
// wall-clock wait. The cases that return before the first sleep are pinned
// instead: nothing to watch, nothing to follow, an immediate done, and an
// immediate give-up (nothing reports progress). Each completes in one poll
// or none, so none of them ever reaches time.Sleep.

func TestWaitForLiveNothingToWatchReturnsImmediately(t *testing.T) {
	e := newEnv()
	// 200 with a state that is not "running": not something to follow.
	_, done, err := e.live.WaitForLive(testBackend(), testOrigin(), job(map[string]any{"state": "done"}, 200), nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if done {
		t.Error("done = true, want false: nothing was watched")
	}
	if len(e.getter.polls) != 0 {
		t.Errorf("polled %v, want no polls", e.getter.polls)
	}
}

func TestWaitForLiveNothingToFollowReturnsImmediately(t *testing.T) {
	e := newEnv()
	// A 202 with a running job, but an origin whose links do not match the
	// job's class -- there is nothing to poll.
	origin := live.Origin{
		Entity: siren.Entity{Links: []siren.Link{{Rel: []string{"self"}, Href: "/"}}},
	}
	_, done, err := e.live.WaitForLive(testBackend(), origin, job(map[string]any{"state": "running"}, 202), nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if done {
		t.Error("done = true, want false: nothing was followed")
	}
	if len(e.getter.polls) != 0 {
		t.Errorf("polled %v, want no polls", e.getter.polls)
	}
}

func TestWaitForLiveDoneOnFirstPoll(t *testing.T) {
	e := newEnv()
	e.getter.replyBody = jobBody(map[string]any{"state": "done", "id": float64(7)})

	entity, done, err := e.live.WaitForLive(
		testBackend(),
		testOrigin(),
		job(map[string]any{"state": "running", "id": float64(7)}, 202),
		nil,
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !done {
		t.Error("done = false, want true: the server said the job finished")
	}
	if got, _ := entity.Properties["state"].(string); got != "done" {
		t.Errorf("entity state = %q, want %q", got, "done")
	}
	if len(e.getter.polls) != 1 {
		t.Errorf("polled %v, want exactly one poll", e.getter.polls)
	}
}

func TestWaitForLiveGivesUpWhenNothingReportsProgress(t *testing.T) {
	e := newEnv()
	// A reply with no "state" property: not judgeable, so the watch stops
	// rather than reading silence as completion -- the same rule poll.go's
	// !judgeable branch follows.
	e.getter.replyBody = jobBody(map[string]any{"id": float64(7)})

	_, done, err := e.live.WaitForLive(
		testBackend(),
		testOrigin(),
		job(map[string]any{"state": "running", "id": float64(7)}, 202),
		nil,
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if done {
		t.Error("done = true, want false: nothing reported progress, so this is a give-up, not a finish")
	}
	if len(e.getter.polls) != 1 {
		t.Errorf("polled %v, want exactly one poll", e.getter.polls)
	}
}
