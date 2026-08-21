package live

import (
	"context"
	"fmt"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/siren"
)

// WaitForLive is the blocking variant of a watch, for the one-shot CLI: it
// polls synchronously until the job is done, gives up, or turns out not to
// be watchable, and returns the outcome rather than reporting it through
// Handlers. Sits alongside Start (callback-based, non-blocking, the one a
// Bubble Tea program adapts) and Stop, so the two callers
// internal/session's package comment names each have the shape that fits
// them: the TUI wants Start's callbacks (it forwards them as tea.Msg onto
// its single-threaded Update loop -- see the package comment), while a
// one-shot CLI has no event loop to forward onto and wants to block until
// there is a final answer.
//
// done is true when the server said the job finished (the OnDone case of
// the callback-based watch); false covers everything else -- gave up, the
// response was not watchable, or nothing the server offers tracks the job
// (the OnGiveUp / "not watching" cases). entity is the last poll's reply
// when there was one, or the zero value when nothing was ever watched; a
// caller that needs to re-fetch the origin document afterwards should do
// so regardless of done, exactly as the callback-based onDone/onGiveUp
// both do.
//
// onProgress, when non-nil, is called with each poll reply that keeps the
// watch going (decide's verdictAskAgain and verdictRunning cases -- an
// answer about someone else's job counts too, the same as it always has),
// so a one-shot CLI can print each progress step to stderr as it arrives --
// the blocking analogue of Start's OnProgress callback. It is never called
// for the terminal polls (done or give-up), since those return instead of
// looping.
//
// err is currently always nil. A failed poll is news about the network,
// not the job, and is never an error -- the watch keeps asking until the
// deadline, the same rule poll.go follows. The return value is reserved so
// a future unrecoverable condition (none exists today) can be signalled
// without changing the signature every caller already switched on.
//
// Unlike Start, WaitForLive is driven by real time (time.Now / time.Sleep)
// rather than the injected now/createTimer pair: the injected clock exists
// so the callback-based watch is testable without a wall-clock wait, but a
// blocking call that waited on the fake timer would deadlock -- the fake
// only fires when a test's tick() drives it, and the goroutine that would
// call tick() is the one blocked here. So this method is not covered by
// the fake-clock tests that cover Start; the cases it adds that are not
// about waiting (nothing to watch, nothing to follow, an immediate done,
// an immediate give-up) are pinned in waitforlive_test.go instead, all of
// which return before the first time.Sleep.
//
// WaitForLive does not register its watch against Live.current the way
// Start does, so IsLive reports false while a blocking watch is in
// progress and a concurrent Stop cannot cancel it. That is deliberate for
// the single-threaded one-shot CLI this exists for; a future caller that
// needs cancellation or a live-status read alongside a blocking watch
// would need that wiring added first.
func (l *Live) WaitForLive(be backend.Backend, origin Origin, result ActionOutcome, onProgress func(siren.Entity)) (siren.Entity, bool, error) {
	if !ShouldWatch(result) {
		return siren.Entity{}, false, nil
	}
	href := PollTarget(origin.Entity, result.Entity)
	if href == "" {
		// Only a document the server pointed us at will do -- see the
		// package comment for why falling back to the origin is not an
		// option, the same reasoning Start applies.
		l.log("live: nothing the server offers tracks this, not watching")
		return siren.Entity{}, false, nil
	}

	w := &watch{
		backend:   be,
		href:      href,
		id:        result.Entity.Properties["id"],
		budget:    budgetFor(result.Entity),
		startedAt: time.Now(),
	}
	l.logStart(href, w)
	return l.blockPoll(be, w, onProgress)
}

// blockPoll is WaitForLive's synchronous poll loop, split out so
// WaitForLive itself stays the shape of "decide whether to watch, then
// hand off". It reads each poll reply through the same shared decide/
// deadlineExceeded pair poll.go's handle/checkDeadline are built on
// (decide.go), so a reply means the same thing here as it does to the
// callback-based watch; only the scheduling differs, this one sleeping in
// real time rather than rescheduling a timer, and returning instead of
// dispatching Handlers. Used to have its own blockHandle/
// blockDeadlineDecision/blockDeadlineExceeded trio recomputing that same
// verdict independently -- task m31 folded the two computations into one,
// since keeping them consistent by hand was exactly the drift risk this
// package's own comments used to warn about.
func (l *Live) blockPoll(be backend.Backend, w *watch, onProgress func(siren.Entity)) (siren.Entity, bool, error) {
	for {
		// context.Background(), not w.ctx (which is nil here -- see watch.ctx's
		// doc comment): this blocking loop is not registered against
		// Live.current, so nothing else could ever cancel a per-watch context
		// even if one existed -- see WaitForLive's own doc comment on why a
		// concurrent Stop cannot reach a blocking watch. Threading GetContext
		// through here (q31) is still worth doing over calling Get directly:
		// it keeps httpGetter to one method (see the interface's own doc
		// comment) rather than requiring implementers to satisfy two
		// equivalent seams for the same underlying call.
		resp, err := l.http.GetContext(context.Background(), be, w.href)
		if err != nil {
			// A failed poll is not news about the job -- see the package
			// comment. Keep asking until the deadline. No budget is
			// re-derived here: the failure carries no entity, the same as
			// poll.go's checkDeadline(current, nil) on a failed poll.
			l.log(fmt.Sprintf("live: poll failed (%s), still watching", failureKind(err)))
			if deadlineExceeded(w, nil, time.Now()) {
				return siren.Entity{}, false, nil
			}
			time.Sleep(PollInterval)
			continue
		}

		entity := siren.EntityFromJSON(resp.Entity)
		switch decide(w, entity) {
		case verdictUnwatchable:
			// Nothing here reports progress -- stop, the same way
			// poll.go's handle does rather than read silence as
			// completion.
			l.log("live: nothing here reports progress, stopping")
			return entity, false, nil
		case verdictDone:
			l.log(fmt.Sprintf("live: finished (%v)", entity.Properties["state"]))
			return entity, true, nil
		default:
			// verdictAskAgain or verdictRunning: still eligible to
			// continue, subject to the deadline. The budget IS re-derived
			// from this reply first (matching poll.go's
			// checkDeadline(w, &entity)), so even a !relevant answer that
			// carries a fresh staleAfterSeconds tightens the deadline.
			if deadlineExceeded(w, &entity, time.Now()) {
				return siren.Entity{}, false, nil
			}
			if onProgress != nil {
				onProgress(entity)
			}
			time.Sleep(PollInterval)
		}
	}
}
