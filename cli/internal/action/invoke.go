package action

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// InvokeOutcome is what sending a (confirmed, or safe) action produced.
// See AskOutcome's doc comment for why every switch over this interface
// must carry a default-panics-as-canary case.
type InvokeOutcome interface {
	isInvokeOutcome()
}

// InvokeRefused means nothing was sent, with Reason explaining why --
// never a bug to route around by inventing a request. Three cases end up
// here, all mirrored from actions.js / action_service.dart:
//
//   - the action named by a pending question is no longer offered on the
//     document passed to Answer or AnswerValue -- it may have been
//     withdrawn, or the document may have moved on since Ask was called
//     (invoke()'s re-check of nav.top().entity);
//   - more than one required field has no default and no confirmation to
//     fill it -- asking out loud for one is fine, dictating several one
//     at a time is not (the "problem" branch of fieldValues());
//   - a value asked for out loud via InvokeNeedsValue came back empty
//     (answerSpoken()'s "nothing was heard" case).
type InvokeRefused struct {
	Reason string
}

func (InvokeRefused) isInvokeOutcome() {}

// InvokeNeedsValue means a required field on the pending action has no
// default and no confirmation to stand in for it (docs/DESIGN.md, "Do not
// invent a value"), so it must be asked for out loud rather than guessed
// at. FieldName identifies the field for the eventual AnswerValue call;
// Label is what to show on screen -- the server's own wording
// (siren.Field.Title) when it gave one, else the field's name. Mirrors
// invokeAskForValue() in actions.js.
type InvokeNeedsValue struct {
	FieldName string
	Label     string
}

func (InvokeNeedsValue) isInvokeOutcome() {}

// InvokeSucceeded means the request was sent and the server answered
// without a transport failure -- a 202 included, same as httpclient.
// Response carries the raw httpclient.HTTPResponse, never a parsed,
// ready-to-render siren.Entity -- see the package comment on "never carry
// a document across an action". What that means (a job still running, a
// document to re-fetch) is for the caller to decide.
type InvokeSucceeded struct {
	Response httpclient.HTTPResponse
}

func (InvokeSucceeded) isInvokeOutcome() {}

// InvokeFailed means the request failed -- a 409 included. By the time
// this is returned, the one bounded retry the contract allows (a
// confirmed required checkbox, re-sent once, within a minute -- see
// Action.retryable) has already been taken if it applied, and Failure is
// what came back after that, not before it. Every failure, conflict or
// otherwise, is reported here the same way; deciding whether to re-fetch
// is left to the caller, exactly as docs/DESIGN.md's "never carry a
// document across an action" describes -- a 409 here still means "go
// look", never "try again".
type InvokeFailed struct {
	Failure *failure.Failure
}

func (InvokeFailed) isInvokeOutcome() {}

// Answer handles the reply to a confirmation Ask raised. Mirrors the
// confirmed half of answer() in actions.js -- the pending.awaiting/spoken
// half, for a value asked for out loud, is AnswerValue instead of a
// second parameter here, so a caller's two questions ("yes/no" vs. "what
// value") stay two distinct, statically-typed calls rather than one
// dynamically-dispatched one.
//
// Returns nil when nothing was sent: either confirmed is false, or the
// question had already been superseded (answered or cancelled) before
// this call -- both are "nothing to do", not an error. May also return
// InvokeNeedsValue -- see FillFields -- in which case nothing has been
// sent yet and the question is still pending, now awaiting AnswerValue
// instead.
//
// entity is the document currently on screen, looked up by name again
// rather than trusting whatever siren.Action was found when Ask was
// called -- mirrors invoke() re-reading nav.top().entity in actions.js,
// in case the document moved on while the question was still on screen.
//
// Claims a.pending via takePendingIf (r31) rather than checking then
// clearing it as two separate steps: two overlapping calls -- a double
// press of Confirm's 'y' binding, or a 'y' racing the Back key's direct,
// synchronous Answer(false) call (internal/tui/cmd.go's package comment
// and model.go's handleBack) -- must not both see the same pendingAction
// non-nil and both go on to send it.
func (a *Action) Answer(confirmed bool, be backend.Backend, entity siren.Entity) InvokeOutcome {
	p := a.takePendingIf(func(*pendingAction) bool { return true })
	if p == nil {
		return nil
	}
	if !confirmed {
		a.log(fmt.Sprintf("declined %q", p.name))
		return nil
	}
	return a.invoke(be, entity, true, nil, p)
}

// AnswerValue handles the reply to the "say a value" prompt InvokeNeedsValue
// raised. Mirrors answerSpoken() in actions.js.
//
// An empty text is not an answer: "confirmed but nothing said" is reported
// as InvokeRefused rather than sent as an empty string, the same guard
// answerSpoken() has ("Nothing was heard, so nothing was sent"). Returns
// nil when there is no value currently being asked for -- the question
// was answered or cancelled already, or Ask/Answer never raised
// InvokeNeedsValue in the first place -- the same "nothing to do" contract
// Answer uses.
//
// Claims a.pending via takePendingIf the same way Answer does (r31),
// restricted to a pendingAction actually awaiting a value
// (awaitingField != "") -- a pending ConfirmQuestion (awaitingField == "")
// is left untouched here, exactly as the direct field checks this replaced
// did, since it belongs to Answer instead.
func (a *Action) AnswerValue(text string, be backend.Backend, entity siren.Entity) InvokeOutcome {
	p := a.takePendingIf(func(p *pendingAction) bool { return p.awaitingField != "" })
	if p == nil {
		return nil
	}
	if text == "" {
		return InvokeRefused{Reason: "Nothing was heard, so nothing was sent."}
	}
	spoken := &FieldAnswer{Name: p.awaitingField, Text: text}
	return a.invoke(be, entity, true, spoken, p)
}

// invoke sends the pending action, if it is still offered on entity and
// every required field can be filled. Shared by the safe-method branch of
// Ask, by Answer and by AnswerValue -- mirrors invoke() in actions.js.
//
// p is the pendingAction the caller already has in hand: Ask's own fresh
// one (never published to a.pending for a safe method -- see Ask's doc
// comment), or whatever Answer/AnswerValue's takePendingIf just claimed
// and cleared from a.pending. Either way, a.pending is nil by the time
// this runs (r31) -- invoke reads p directly rather than a.pending, so a
// concurrent CancelPending or a fresh Ask racing in here cannot hand this
// call a torn or superseded pendingAction out from under it; the only
// write this method itself makes back to a.pending (FieldValueMissing,
// below) goes through casPending's old-still-current check for the same
// reason.
func (a *Action) invoke(be backend.Backend, entity siren.Entity, confirmed bool, spoken *FieldAnswer, p *pendingAction) InvokeOutcome {
	name := p.name
	act := entity.ActionByName(name)
	if act == nil {
		return InvokeRefused{Reason: "not offered"}
	}

	switch filled := FillFields(*act, confirmed, spoken, a.userValues).(type) {
	case FieldsRefused:
		return InvokeRefused{Reason: filled.Reason}
	case FieldValueMissing:
		// Keep the question pending -- now awaiting a value for this
		// field rather than a yes/no -- instead of clearing it as every
		// other branch does; see the package comment and AnswerValue.
		// casPending(nil, ...) rather than a bare a.pending = &awaiting:
		// a.pending is expected still nil here (nobody has published
		// anything since p was claimed above), and if a concurrent
		// CancelPending or a fresh Ask has already moved it on to
		// something else, this call's own answer is stale and must not
		// overwrite that -- see casPending's own doc comment.
		awaiting := p.awaiting(filled.Field.Name)
		a.casPending(nil, &awaiting)
		return InvokeNeedsValue{FieldName: filled.Field.Name, Label: filled.Field.Label()}
	case FieldsFilled:
		return a.send(be, *act, confirmed, filled.Values, name, p.href, p.method)
	default:
		panic(fmt.Sprintf("action: unreachable FieldFillOutcome type %T", filled))
	}
}

// send sends the request, remembering the confirmation for the one retry
// the contract allows first if it applies. Mirrors invokeSend() in
// actions.js. href and method come from the caller's own already-claimed
// pendingAction (invoke's p) rather than a.pending -- see invoke's own doc
// comment for why nothing here reads that field directly any more.
func (a *Action) send(be backend.Backend, act siren.Action, confirmed bool, values map[string]string, name, href, method string) InvokeOutcome {
	a.log(fmt.Sprintf("invoking %q (%s)", name, method))

	if confirmed && hasRequiredCheckbox(act) {
		// Remembered for the one retry the contract allows -- see
		// confirmedRetry and Action.retryable. Unconditionally overwrites
		// whatever was remembered before, mirroring confirmedAt = ... in
		// actions.js: a fresh confirmation replaces a stale one rather
		// than accumulating alongside it. Guarded by a.mu (r31): send and
		// afterSend can, in principle, run concurrently for two different
		// actions (each with its own locally-captured href/method/values,
		// so neither's request is corrupted by the other), and both write
		// this same field -- last-writer-wins is the intended behaviour
		// here (mirrors the single-threaded overwrite above), the lock
		// only makes that overwrite itself race-free rather than torn.
		a.mu.Lock()
		a.confirmedRetry = &confirmedRetry{name: name, href: href, method: method, values: values, at: a.now()}
		a.mu.Unlock()
	}
	return a.doSend(be, href, method, values, name)
}

// doSend performs one request through http. Split out so send stays the
// shape of "fill in the values, then hand off", and so afterSend -- which
// decides what happens to the answer, including the one retry -- can call
// this exact href/method/values pair a second time. Mirrors invokeSend()'s
// call into http.request() in actions.js.
func (a *Action) doSend(be backend.Backend, href, method string, values map[string]string, name string) InvokeOutcome {
	resp, err := a.http.Request(be, href, method, values)
	return a.afterSend(be, href, method, values, name, resp, err)
}

// afterSend turns a response into the outcome a caller sees, taking the
// contract's one bounded retry first if it applies. Mirrors afterAction()
// and the retry branch of afterActionError() in actions.js, collapsed
// into one place here because the retry must never reach the caller as a
// visible, separate step -- by the time this returns, whatever the
// contract allowed has already happened, and the InvokeOutcome it returns
// is final.
func (a *Action) afterSend(be backend.Backend, href, method string, values map[string]string, name string, resp httpclient.HTTPResponse, err error) InvokeOutcome {
	if err == nil {
		// The confirmation was spent, successfully -- see
		// afterActionSuccess in actions.js clearing confirmedAt the same
		// way. Locked (r31): see send's own doc comment on why this field
		// needs a.mu even though only one goroutine ever "owns" a given
		// confirmation at a time.
		a.mu.Lock()
		a.confirmedRetry = nil
		a.mu.Unlock()
		return InvokeSucceeded{Response: resp}
	}

	f := toFailure(err)
	if f.Kind != failure.Conflict || !a.retryable(name) {
		return InvokeFailed{Failure: f}
	}

	// The one exception: a required checkbox the user actually ticked,
	// re-sent once, within a minute. Cleared *before* sending the retry
	// so whatever this second attempt returns -- success, the same
	// conflict again, or anything else -- is final; retryable(name) will
	// say no to a second attempt at the same action regardless of how
	// much of the TTL is left.
	a.mu.Lock()
	a.confirmedRetry = nil
	a.mu.Unlock()
	a.log("conflict on a confirmed action: re-sending it once")
	retriedResp, retriedErr := a.http.Request(be, href, method, values)
	if retriedErr != nil {
		return InvokeFailed{Failure: toFailure(retriedErr)}
	}
	return InvokeSucceeded{Response: retriedResp}
}

// toFailure converts an error from the injected requester into a
// *failure.Failure, via the shared failure.From -- falling back to
// Kind: Config if it is ever some other error type. *httpclient.Client
// always returns *failure.Failure on error today; this fallback only
// exists so a test double behind requester cannot panic this package by
// returning some other error type. Config is the fallback nav and live use
// too, for the same defensive case -- see failure.From's doc comment for
// why that is one conscious choice rather than three independent ones.
func toFailure(err error) *failure.Failure {
	return failure.From(err, failure.Config)
}
