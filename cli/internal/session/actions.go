package session

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// askAction looks name up on the current document and decides whether it
// needs confirming. Mirrors askAction() in actions.js. A no-op if nothing is
// open yet -- there is nothing to look the action up on.
func (s *Session) askAction(name string) {
	entity, ok := s.currentEntity()
	if !ok {
		return
	}
	be := s.nav.Backend()
	label := actionLabel(entity, name)
	s.mu.Lock()
	s.pendingActionLabel = label
	s.mu.Unlock()

	// s.actions.Ask does I/O for a confirmable action's own request (it may
	// pre-check state); it runs with s.mu released, same as every other
	// call into nav/actions/live in this file -- see nav's fetch for why.
	outcome := s.actions.Ask(be, entity, name)
	switch o := outcome.(type) {
	case action.ActionNotOffered:
		s.mu.Lock()
		s.question = nil
		s.notice = ActionWithdrawn{Heading: o.Name}
		s.mu.Unlock()
	case action.ConfirmationRequired:
		s.mu.Lock()
		s.question = ConfirmQuestion{Heading: o.Heading, Body: o.Body}
		s.mu.Unlock()
	case action.ActionInvoked:
		s.applyInvokeOutcome(o.Outcome, entity, be)
	default:
		panic(fmt.Sprintf("session: unreachable AskOutcome type %T", outcome))
	}
}

// actionLabel is the on-screen label of the action named name on entity, or
// name itself when the server does not offer it -- mirrors
// entity.actionByName(name)?.label ?? name in session.dart, used to thread
// the action's own wording through to the notice set once the round trip
// completes.
func actionLabel(entity siren.Entity, name string) string {
	if act := entity.ActionByName(name); act != nil {
		return act.Label()
	}
	return name
}

// Answer handles the reply to a ConfirmQuestion. Mirrors the confirmed half
// of answer() in actions.js -- the "what value" half is AnswerValue, so a
// caller's two questions stay two distinct, statically typed calls (see
// internal/action's package comment on Action.Answer for the same reasoning
// one layer down).
func (s *Session) Answer(confirmed bool) {
	entity, ok := s.currentEntity()
	if !ok {
		s.mu.Lock()
		s.question = nil
		s.mu.Unlock()
		return
	}
	be := s.nav.Backend()
	outcome := s.actions.Answer(confirmed, be, entity)
	s.mu.Lock()
	s.question = nil
	s.mu.Unlock()
	if outcome == nil {
		// Declined, or the question was already superseded -- both are
		// "nothing to do", not an error, mirroring action.Answer's own
		// contract.
		return
	}
	s.applyInvokeOutcome(outcome, entity, be)
}

// AnswerValue handles the reply to a ValueQuestion. Mirrors answerSpoken()
// in actions.js.
func (s *Session) AnswerValue(text string) {
	entity, ok := s.currentEntity()
	if !ok {
		s.mu.Lock()
		s.question = nil
		s.mu.Unlock()
		return
	}
	be := s.nav.Backend()
	outcome := s.actions.AnswerValue(text, be, entity)
	s.mu.Lock()
	s.question = nil
	s.mu.Unlock()
	if outcome == nil {
		return
	}
	s.applyInvokeOutcome(outcome, entity, be)
}

// applyInvokeOutcome turns an action.InvokeOutcome into Notice/Question and,
// where the contract requires it, a re-fetch of the document the action
// came from -- mirrors afterAction/afterActionSuccess/afterActionError in
// actions.js, the wiring internal/action's package comment leaves to this
// one.
func (s *Session) applyInvokeOutcome(outcome action.InvokeOutcome, originEntity siren.Entity, be backend.Backend) {
	switch o := outcome.(type) {
	case action.InvokeRefused:
		s.mu.Lock()
		s.notice = ActionRefused{Heading: s.pendingActionLabel, Reason: o.Reason}
		s.mu.Unlock()
	case action.InvokeNeedsValue:
		// Still pending -- now awaiting a value instead of a yes/no.
		s.mu.Lock()
		s.question = ValueQuestion{Label: o.Label}
		s.mu.Unlock()
	case action.InvokeSucceeded:
		s.handleSuccess(o.Response, originEntity, be)
	case action.InvokeFailed:
		s.handleFailure(o.Failure)
	default:
		panic(fmt.Sprintf("session: unreachable InvokeOutcome type %T", outcome))
	}
}

// handleSuccess is the request-was-sent-and-answered branch. Mirrors
// afterActionSuccess: hands the response to internal/live first, and only
// re-fetches immediately when nothing picked it up to watch --
// docs/DESIGN.md's "never carry a document across an action", weighed
// against "do not claim a job finished" when it plainly has not.
func (s *Session) handleSuccess(response httpclient.HTTPResponse, originEntity siren.Entity, be backend.Backend) {
	resultEntity := siren.EntityFromJSON(response.Entity)

	s.mu.Lock()
	label := s.pendingActionLabel
	s.notice = ActionOutcomeReported{
		Heading: label,
		Message: live.ResultText(resultEntity, response.Status),
		Body:    describeResult(resultEntity, response.Status),
	}
	s.mu.Unlock()

	started := s.live.Start(be,
		live.Origin{Entity: originEntity, Href: s.nav.Href()},
		live.ActionOutcome{Status: response.Status, Entity: resultEntity},
		live.Handlers{
			OnProgress: func(entity siren.Entity) { s.onLiveProgress(label, entity) },
			OnDone:     func(entity siren.Entity) { s.onLiveDone(label, entity) },
			OnGiveUp:   func() { s.onLiveGiveUp(label) },
		},
	)
	if started {
		// Still running: the job, not the document, changed. There is
		// nothing new to re-fetch until the watch above says so.
		return
	}
	s.nav.Refresh()
}

// handleFailure is the request-failed branch. Mirrors afterActionError: a
// conflict is re-fetched (the state acted on was stale; the remedy is to look
// again -- the one retry the contract allows already happened inside
// internal/action before this outcome came back, see InvokeFailed's doc
// comment). Anything else -- auth, unreachable, server, client, parse --
// tells us nothing new about the document, and re-fetching would only fail
// the same way or silently paper over a question that is still real.
func (s *Session) handleFailure(f *failure.Failure) {
	s.mu.Lock()
	s.notice = ActionFailed{Heading: s.pendingActionLabel, Failure: f}
	s.mu.Unlock()
	if f.Kind == failure.Conflict {
		s.nav.Refresh()
	}
}

// onLiveProgress is a step reported while still watching. Mirrors
// liveHandlers.onProgress in actions.js. The text comes from
// live.ProgressText, which owns the step/state property names -- this
// coordinator just sets the notice kind + string, so a property-name change
// on the server drifts against the watch logic in one place, not two.
func (s *Session) onLiveProgress(label string, entity siren.Entity) {
	s.mu.Lock()
	s.notice = ActionProgress{Heading: label, Step: live.ProgressText(entity)}
	s.mu.Unlock()
}

// onLiveDone is the job-stopped branch. Mirrors liveHandlers.onDone: the
// document it acted on is worth re-reading now, because that is where the
// effect shows. The message comes from live.DoneText and the body from
// render.Describe -- both owned by the live/render layers (this coordinator
// just sets the notice), so neither the job-state vocabulary nor the
// property rendering lives here.
func (s *Session) onLiveDone(label string, entity siren.Entity) {
	s.mu.Lock()
	s.notice = ActionOutcomeReported{
		Heading: label,
		Message: live.DoneText(entity),
		Body:    render.Describe(entity),
	}
	s.mu.Unlock()
	s.nav.Refresh()
}

// onLiveGiveUp is the watching-was-abandoned branch. Mirrors
// liveHandlers.onGiveUp: not a claim the job failed, only that this app
// stopped asking -- and the document is still re-fetched, because whatever
// the action changed before this app gave up watching is still worth seeing.
func (s *Session) onLiveGiveUp(label string) {
	s.mu.Lock()
	s.notice = ActionGaveUp{Heading: label}
	s.mu.Unlock()
	s.nav.Refresh()
}

// describeResult is the body of an action-outcome notice: the response's
// properties, rendered generically through render.Describe so nothing the
// server sent is thrown away, falling back to the bare status when the
// response carried no properties at all. The property rendering lives in
// internal/render (rendering is that package's job, not a coordinator's --
// docs/DESIGN.md, "Rendering does not interpret"); the status fallback is
// this package's because it is the action-response context only the
// coordinator holds. Mirrors describeResult() in actions.js.
func describeResult(entity siren.Entity, status int) string {
	described := render.Describe(entity)
	if described != "" {
		return described
	}
	return fmt.Sprintf("HTTP %d", status)
}
