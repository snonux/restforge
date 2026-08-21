package action

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/siren"
)

// AskOutcome is what asking about an action found out. A caller switches
// over this exhaustively rather than checking a status flag that could
// silently fail to match.
//
// This is Go's nearest equivalent to the sealed AskOutcome hierarchy in
// action_service.dart: an unexported marker method closes the interface to
// the three types in this file, so nothing outside this package can add a
// fourth. Go has no "sealed" keyword and no compiler check that a type
// switch over an interface covers every implementation the way Dart's
// analyzer checks a switch over a sealed class -- see
// internal/render/target.go for the convention this package follows: every
// switch over AskOutcome, InvokeOutcome or FieldFillOutcome anywhere in
// this codebase must carry a default case that panics with an
// unreachable-style message, turning a forgotten case for a later addition
// into a loud failure the first time that code path runs, instead of a
// silently-wrong fallthrough.
type AskOutcome interface {
	isAskOutcome()
}

// ActionNotOffered means the server does not currently offer an action by
// this name. A real answer -- it means this cannot be done right now --
// not something to route around by inventing a request. Mirrors the
// withdrawn-action branch of askAction() in actions.js.
type ActionNotOffered struct {
	Name string
}

func (ActionNotOffered) isAskOutcome() {}

// ConfirmationRequired means the method is unsafe, so nothing has been
// sent yet. Heading and Body are exactly what a caller shows on screen and
// nothing more -- see the package comment on why the href behind this
// question never appears here.
type ConfirmationRequired struct {
	Heading string
	Body    string
}

func (ConfirmationRequired) isAskOutcome() {}

// ActionInvoked means the method was safe (IsSafeMethod), so it went
// straight through without asking -- Siren allows a GET action, and
// stopping to confirm a read would be noise. Outcome is exactly what
// invoking it produced.
type ActionInvoked struct {
	Outcome InvokeOutcome
}

func (ActionInvoked) isAskOutcome() {}

// Ask looks up name on entity and decides whether it needs confirming.
// Mirrors askAction() in actions.js / ActionService.ask in
// action_service.dart.
//
// A safe method's pendingAction is never published to a.pending at all
// (r31): nothing needs to await a confirmation for it -- invoke below is
// called with it directly, and only invoke's own FieldValueMissing branch
// ever needs to make it visible as a.pending (via casPending), for a
// caller to answer with AnswerValue. Leaving it unpublished here also
// closes the concurrency gap this task found: two Ask calls for the same
// safe action racing (e.g. a double keypress before the first's HTTP round
// trip returns -- see internal/tui/cmd.go's package comment) no longer
// have a window where a.pending briefly names one goroutine's action while
// a concurrent CancelPending or second Ask can see and clobber it.
func (a *Action) Ask(be backend.Backend, entity siren.Entity, name string) AskOutcome {
	act := entity.ActionByName(name)
	if act == nil {
		// The server does not offer this right now. That is a real
		// answer, so it is reported rather than routed around by
		// inventing a request.
		a.log(fmt.Sprintf("action %q is no longer offered", name))
		return ActionNotOffered{Name: name}
	}

	p := &pendingAction{name: name, href: act.Href, method: act.Method}
	if IsSafeMethod(act.Method) {
		return ActionInvoked{Outcome: a.invoke(be, entity, true, nil, p)}
	}

	// Unsafe: nothing is sent until Answer/AnswerValue claims this exact
	// p (takePendingIf) -- see action.go's field comment on pending.
	a.mu.Lock()
	a.pending = p
	a.mu.Unlock()
	return ConfirmationRequired{
		Heading: act.Label(),
		Body:    ConfirmationText(*act),
	}
}
