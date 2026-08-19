package live_test

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/live"
)

// The same failure one step later: the thing being polled turns out not to
// report progress at all.
func TestAnEntityThatReportsNoStateDoesNotEndTheWatchAsDone(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(120)})
	e.getter.replyBody = jobBody(map[string]any{"note": "no state here"})

	e.clock.tick(live.PollInterval)

	if s.doneSet {
		t.Error("done should not be reported")
	}
	if !s.gaveUp {
		t.Error("it should stop, and say it stopped")
	}
	if e.live.IsLive() {
		t.Error("IsLive() = true, want false")
	}
}

func TestProgressThenDone(t *testing.T) {
	e := newEnv()
	started, s := e.begin(map[string]any{"state": "running", "id": float64(4), "step": "one", "staleAfterSeconds": float64(120)})
	if !started {
		t.Fatal("Start() = false, want true")
	}
	if !e.live.IsLive() {
		t.Fatal("IsLive() = false, want true")
	}

	e.getter.replyBody = jobBody(map[string]any{"state": "running", "id": float64(4), "step": "two", "staleAfterSeconds": float64(120)})
	e.clock.tick(live.PollInterval)
	if want := []string{base + "job"}; len(e.getter.polls) != 1 || e.getter.polls[0] != want[0] {
		t.Errorf("polls = %v, want %v: the job link is what gets polled", e.getter.polls, want)
	}
	if want := []string{"two"}; len(s.progress) != 1 || s.progress[0] != want[0] {
		t.Errorf("progress = %v, want %v", s.progress, want)
	}

	e.getter.replyBody = jobBody(map[string]any{"state": "done", "id": float64(4), "staleAfterSeconds": float64(120)})
	e.clock.tick(live.PollInterval)
	if !s.doneSet || s.done != "done" {
		t.Errorf("done = %v (set=%v), want \"done\": completion is reported once", s.done, s.doneSet)
	}
	if e.live.IsLive() {
		t.Error("IsLive() = true, want false: the watch has ended")
	}
}

// The one that matters: a poll answered by a machine that never saw this
// job.
func TestAPollAnsweredByAMachineThatNeverSawThisJob(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(120)})

	e.getter.replyBody = jobBody(map[string]any{"state": "done", "id": float64(99), "staleAfterSeconds": float64(120)})
	e.clock.tick(live.PollInterval)
	if s.doneSet {
		t.Error("another job's completion should not be ours")
	}
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true: the watch continues")
	}

	e.getter.replyBody = jobBody(map[string]any{"state": "done", "id": float64(4), "staleAfterSeconds": float64(120)})
	e.clock.tick(live.PollInterval)
	if !s.doneSet || s.done != "done" {
		t.Errorf("done = %v (set=%v), want \"done\": our own completion still ends it", s.done, s.doneSet)
	}
}

func TestAStateOfNoneIsNotCompletion(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(120)})

	// "I have no job" is not "the job finished".
	e.getter.replyBody = jobBody(map[string]any{"state": "none", "id": float64(0)})
	e.clock.tick(live.PollInterval)

	if s.doneSet {
		t.Error("done should not be reported")
	}
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true: the watch continues")
	}
}

func TestAFailedPollSaysNothingAboutTheJob(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(120)})
	e.getter.fail = true

	e.clock.tick(live.PollInterval)

	if s.doneSet {
		t.Error("done should not be reported")
	}
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true: a failed poll does not end the watch")
	}
}

// --- the deadline -----------------------------------------------------

func TestInsideTheBudgetItKeepsGoingPastItItGivesUp(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(1)})
	e.getter.replyBody = jobBody(map[string]any{"state": "running", "id": float64(4), "step": "x", "staleAfterSeconds": float64(1)})

	e.clock.tick(live.PollInterval)
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true: inside the budget it keeps going")
	}

	e.clock.tick(live.BudgetBuffer + live.PollInterval)
	if !s.gaveUp {
		t.Error("gaveUp should be true: past the budget it gives up")
	}
	// Giving up is about this client, not about the job.
	if s.doneSet {
		t.Error("done should not be reported: giving up is not completion")
	}
}

// A server that never sends a budget gets the generous fallback, because
// abandoning a job that is still running is the worse mistake.
func TestNoAdvertisedBudgetMeansTheGenerousFallback(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4)})
	e.getter.replyBody = jobBody(map[string]any{"state": "running", "id": float64(4), "step": "x"})

	e.clock.tick(live.PollInterval * 10)
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true")
	}
	if s.gaveUp {
		t.Error("gaveUp should be false: no advertised budget means the generous default")
	}

	e.clock.tick(live.FallbackBudget)
	if !s.gaveUp {
		t.Error("gaveUp should be true: which does eventually run out")
	}
}

// The budget is re-derived every poll, so an early answer that carried none
// does not fix a short deadline for the whole run.
func TestALaterLargerBudgetReplacesTheFirstOne(t *testing.T) {
	e := newEnv()
	_, s := e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(1)})
	e.getter.replyBody = jobBody(map[string]any{
		"state":             "running",
		"id":                float64(4),
		"step":              "x",
		"staleAfterSeconds": float64(3600),
	})

	e.clock.tick(live.PollInterval)
	e.clock.tick(live.BudgetBuffer + live.PollInterval)

	if s.gaveUp {
		t.Error("gaveUp should be false")
	}
	if !e.live.IsLive() {
		t.Error("IsLive() = false, want true")
	}
}

func TestAStoppedWatchNeverPollsAgain(t *testing.T) {
	e := newEnv()
	e.begin(map[string]any{"state": "running", "id": float64(4), "staleAfterSeconds": float64(120)})
	e.live.Stop()
	e.getter.polls = nil

	e.clock.tick(live.PollInterval * 3)

	if len(e.getter.polls) != 0 {
		t.Errorf("polls = %v, want none: a stopped watch never polls again", e.getter.polls)
	}
	if e.live.IsLive() {
		t.Error("IsLive() = true, want false: and reports itself stopped")
	}
}
