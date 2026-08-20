package live

import (
	"time"

	"github.com/snonux/restforge/cli/internal/siren"
)

// verdict is what a poll reply says about w's job, before any deadline
// check is applied. Mirrors the three-way branch poll.go's handle and
// waitforlive.go's blockPoll used to each compose from relevant, judgeable
// and running independently -- see decide's doc comment.
type verdict int

const (
	// verdictAskAgain: the reply was not about our job, or said "none".
	// Ask again, subject to the deadline -- there is no progress to
	// report.
	verdictAskAgain verdict = iota
	// verdictRunning: our job, still going. Ask again, subject to the
	// deadline -- and there is progress to report.
	verdictRunning
	// verdictDone: our job, no longer running.
	verdictDone
	// verdictUnwatchable: nothing here reports progress at all. Give up
	// unconditionally -- the deadline does not enter into it, since no
	// future reply from this resource will ever be worth reading either.
	verdictUnwatchable
)

// decide reads one poll reply and says what it means for w: relevant,
// judgeable and running composed into a single verdict. This is the one
// place that composition lives -- extracted from poll.go's handle and
// waitforlive.go's blockHandle, which used to build the identical
// four-way branch by hand, independently of each other (task m31, an OCP
// finding: any change to watch-termination policy -- a new terminal
// condition, a change to precedence -- had to be made twice to stay
// consistent, a drift risk waitforlive.go's own doc comment already called
// out). poll.go and waitforlive.go now each wrap this same verdict with
// their own scheduling mechanics (timer + Handlers vs. sleep + return) and
// their own logging, but neither composes relevant/judgeable/running
// itself any more.
//
// Deliberately silent on the deadline: "what did the reply say" and "is
// there still time to ask again" are two different questions, answered
// separately (by deadlineExceeded) so a failed poll -- which has no reply
// to decide on -- can still ask the second question through the same
// function poll.go and waitforlive.go otherwise call after this one.
func decide(w *watch, entity siren.Entity) verdict {
	if !relevant(w, entity) {
		return verdictAskAgain
	}
	if !judgeable(entity) {
		return verdictUnwatchable
	}
	if !running(entity) {
		return verdictDone
	}
	return verdictRunning
}

// deadlineExceeded re-derives w.budget from entity (when non-nil -- there
// was a reply to derive one from) and reports whether the server's own
// budget, or FallbackBudget when none has ever been seen, has run out as
// of now. now is a parameter rather than read from a clock so poll.go's
// callback-driven watch can pass its injected clock and
// waitforlive.go's blocking watch can pass the wall clock, both through the
// same function -- mirrors what poll.go's former checkDeadline and
// waitforlive.go's former blockDeadlineDecision/blockDeadlineExceeded used
// to each compute on their own.
func deadlineExceeded(w *watch, entity *siren.Entity, now time.Time) bool {
	if entity != nil {
		if fresh := budgetFor(*entity); fresh != nil {
			w.budget = fresh
		}
	}
	return now.Sub(w.startedAt) > effectiveBudget(w)
}

// effectiveBudget is w's current deadline budget: its own, when one has
// been seen, or FallbackBudget otherwise. Shared by deadlineExceeded's
// comparison and poll.go's "gave up" log line, which needs the same
// number.
func effectiveBudget(w *watch) time.Duration {
	if w.budget != nil {
		return *w.budget
	}
	return FallbackBudget
}
