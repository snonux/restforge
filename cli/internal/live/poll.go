package live

import (
	"fmt"
	"reflect"
	"time"

	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/siren"
)

// poll fires one poll. Mirrors poll in live.js/live_service.dart. Runs on
// whatever goroutine the injected timer calls its callback from (a fresh
// one per firing, for the production time.AfterFunc-backed timer).
func (l *Live) poll() {
	l.mu.Lock()
	l.timer = nil
	current := l.current
	l.mu.Unlock()
	if current == nil {
		return
	}

	// The HTTP round trip happens with the lock released -- Start/Stop must
	// stay usable from another goroutine for the whole time this is in
	// flight.
	resp, err := l.http.Get(current.backend, current.href)
	if !l.isCurrent(current) {
		// A watch that was stopped -- or replaced by a new one -- while the
		// request was in flight must not resurrect itself.
		return
	}

	if err != nil {
		// A failed poll is not news about the job, only about the network.
		// Keep asking until the deadline; that is the whole reason there is
		// one.
		l.log(fmt.Sprintf("live: poll failed (%s), still watching", failureKind(err)))
		l.checkDeadline(current, nil)
		return
	}
	l.handle(current, siren.EntityFromJSON(resp.Entity))
}

// handle applies one poll reply. Mirrors handle.
func (l *Live) handle(w *watch, entity siren.Entity) {
	if !relevant(w, entity) {
		l.log("live: answer is about a different job, or none — asking again")
		l.checkDeadline(w, &entity)
		return
	}
	if !judgeable(entity) {
		// Whatever we are polling does not report progress, so it cannot
		// tell us the job ended. Stop watching and say we stopped -- the
		// alternative is reading silence as completion, which is how a
		// client reports machines as up while they are still booting.
		l.log("live: nothing here reports progress, stopping")
		if l.stopIfCurrent(w) {
			w.handlers.OnGiveUp()
		}
		return
	}
	if !running(entity) {
		l.finish(w, entity)
		return
	}
	if l.isCurrent(w) {
		w.handlers.OnProgress(entity)
	}
	l.checkDeadline(w, &entity)
}

// finish reports a watch as done. Mirrors finish.
func (l *Live) finish(w *watch, entity siren.Entity) {
	l.log(fmt.Sprintf("live: finished (%v)", entity.Properties["state"]))
	if l.stopIfCurrent(w) {
		w.handlers.OnDone(entity)
	}
}

// checkDeadline gives up when the server's own budget has run out. Returns
// true when the watch has ended. Mirrors checkDeadline.
func (l *Live) checkDeadline(w *watch, entity *siren.Entity) bool {
	if entity != nil {
		if fresh := budgetFor(*entity); fresh != nil {
			w.budget = fresh
		}
	}
	budget := FallbackBudget
	if w.budget != nil {
		budget = *w.budget
	}
	if l.now().Sub(w.startedAt) > budget {
		l.log(fmt.Sprintf("live: gave up after %dms", budget.Milliseconds()))
		if l.stopIfCurrent(w) {
			w.handlers.OnGiveUp()
		}
		return true
	}
	l.scheduleIfCurrent(w)
	return false
}

// relevant filters out answers that are not about our job. Both cases mean
// "ask again", not "it finished". Mirrors relevant.
func relevant(w *watch, entity siren.Entity) bool {
	values := entity.Properties
	if values["state"] == StateNone {
		return false
	}
	wantedID, gotID := w.id, values["id"]
	if wantedID != nil && gotID != nil && !idsEqual(gotID, wantedID) {
		return false
	}
	return true
}

// running reports whether an entity says it is still in progress. Mirrors
// running.
func running(entity siren.Entity) bool {
	return entity.Properties["state"] == StateRunning
}

// judgeable reports whether an entity says anything at all about progress.
// One that does not is not a job resource, and treating its silence as
// completion is exactly the lie this package exists to prevent. Mirrors
// judgeable.
func judgeable(entity siren.Entity) bool {
	_, ok := entity.Properties["state"]
	return ok
}

// budgetFor is the server's own staleness budget, with BudgetBuffer slack
// added, or nil when the entity carried none. Mirrors budgetMs.
func budgetFor(entity siren.Entity) *time.Duration {
	stale, ok := entity.Properties["staleAfterSeconds"].(float64)
	if !ok || stale <= 0 {
		return nil
	}
	d := time.Duration(stale*float64(time.Second)).Round(time.Millisecond) + BudgetBuffer
	return &d
}

// idsEqual compares two job ids decoded off JSON, safely: reflect.DeepEqual
// never panics regardless of the dynamic type on either side, unlike a bare
// != between two `any` values, which panics if either side's dynamic type
// turns out to be uncomparable (e.g. a decoded JSON array or object stood
// in as an id).
func idsEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// failureKind names why a poll failed, for the log line -- mirrors reading
// error.kind in live.js/live_service.dart. Coerces err through the shared
// failure.From rather than hand-rolling the type assertion, falling back
// to Kind: Config on anything that is not a *failure.Failure, which
// httpGetter.Get always returns on error in production; the fallback only
// exists so a hand-rolled test double cannot panic this package by
// returning some other error type -- the same fallback action and nav use
// for their own equivalent seams, see failure.From's doc comment for why
// that is one conscious choice rather than three independent ones.
func failureKind(err error) string {
	return failure.From(err, failure.Config).Kind.String()
}

// isCurrent reports whether w is still the watch Live is driving.
func (l *Live) isCurrent(w *watch) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current == w
}

// stopIfCurrent stops the watch and reports true, but only if w is still
// the one Live is driving -- guarding against a concurrent Start or Stop
// (from a caller's goroutine) having already ended or replaced it while
// this poll was in flight or deciding what to do with its answer. Returns
// false when someone else got there first, in which case the caller must
// not fire a terminal handler: whoever changed current already owns
// reporting that.
func (l *Live) stopIfCurrent(w *watch) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current != w {
		return false
	}
	l.clearLocked()
	return true
}

// scheduleIfCurrent schedules the next poll for w, but only if w is still
// the current watch -- see stopIfCurrent.
func (l *Live) scheduleIfCurrent(w *watch) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current != w {
		return
	}
	l.timer = l.createTimer(PollInterval, l.poll)
}
