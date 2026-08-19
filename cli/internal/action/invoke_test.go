package action_test

// Ported from action_service_test.dart's "answering", "failures" and "the
// bounded 409 retry" groups.

import (
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/siren"
)

func TestDecliningSendsNothingAndClearsTheQuestion(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	outcome := e.action.Answer(false, e.backend, rootEntity(nil))

	if outcome != nil {
		t.Errorf("outcome = %#v, want nil", outcome)
	}
	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false")
	}
}

func TestConfirmingPostsToTheHrefWithItsFields(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.requestedKeys(); len(got) != 1 || got[0] != "POST /brew" {
		t.Errorf("requested = %v, want [POST /brew]", got)
	}
	succeeded, ok := outcome.(action.InvokeSucceeded)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeSucceeded", outcome)
	}
	if succeeded.Response.Status != 200 {
		t.Errorf("Status = %d, want 200", succeeded.Response.Status)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false")
	}
}

func TestRequiredCheckboxFillsFromTheConfirmationGiven(t *testing.T) {
	e := newEnv(t, nil, nil)
	ask := e.action.Ask(e.backend, rootEntity(nil), "cool")
	confirm, ok := ask.(action.ConfirmationRequired)
	if !ok {
		t.Fatalf("ask outcome = %#v, want ConfirmationRequired", ask)
	}
	if confirm.Body != "The kettle is still hot. Cool it anyway?" {
		t.Errorf("Body = %q, want the checkbox title", confirm.Body)
	}

	e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.sentBodies(); len(got) != 1 || got[0] != "confirm=true" {
		t.Errorf("sent bodies = %v, want [confirm=true]", got)
	}
}

func TestAnsweringWithNothingPendingSendsNothing(t *testing.T) {
	e := newEnv(t, nil, nil)
	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if outcome != nil {
		t.Errorf("outcome = %#v, want nil", outcome)
	}
	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
}

func TestAnsweringAgainstWithdrawnActionRefusesNotInvents(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	// The document underneath the question moved on -- e.g. an idle
	// refresh landed while the confirmation was on screen -- and no
	// longer offers "brew".
	outcome := e.action.Answer(true, e.backend, rootEntity([]siren.Action{}))

	if _, ok := outcome.(action.InvokeRefused); !ok {
		t.Fatalf("outcome = %#v, want InvokeRefused", outcome)
	}
	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false")
	}
}

func TestConflictIsReportedLikeAnyOtherFailureAndNotRetried(t *testing.T) {
	e := newEnv(t, map[string]route{
		"POST /brew": {status: 409, body: `{"properties":{"message":"a brew is already running"}}`},
	}, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 1 {
		t.Errorf("POST count = %d, want 1: \"brew\" has no required checkbox, so nothing "+
			"was confirmed in the sense the bounded retry covers", got)
	}
	failed, ok := outcome.(action.InvokeFailed)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeFailed", outcome)
	}
	if failed.Failure.Kind != failure.Conflict {
		t.Errorf("Kind = %v, want Conflict", failed.Failure.Kind)
	}
}

func TestAuthFailureIsReported(t *testing.T) {
	e := newEnv(t, map[string]route{
		"POST /brew": {status: 401, body: `{"properties":{"message":"API key rejected"}}`},
	}, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 1 {
		t.Errorf("POST count = %d, want 1", got)
	}
	failed, ok := outcome.(action.InvokeFailed)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeFailed", outcome)
	}
	if failed.Failure.Kind != failure.Auth {
		t.Errorf("Kind = %v, want Auth", failed.Failure.Kind)
	}
}

// --- the bounded 409 retry ------------------------------------------
//
// The one case where repeating a request is right: the user ticked a
// required checkbox, and the server judged the same request twice on
// budgets that changed in between. Mirrors action_service_test.dart's own
// "the bounded 409 retry" group -- "cool" is the fixture action with the
// required checkbox, same as TestRequiredCheckboxFillsFromTheConfirmationGiven
// above.

func TestConfirmedCheckboxIsRetriedOnceAndTheRetrySucceedingIsReported(t *testing.T) {
	e := newEnv(t, nil, map[string][]route{
		"POST /cool": {
			{status: 409, body: `{"properties":{"message":"needs confirmation"}}`},
			{status: 200, body: `{"properties":{"state":"done"}}`},
		},
	})
	e.action.Ask(e.backend, rootEntity(nil), "cool")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 2 {
		t.Errorf("POST count = %d, want 2: a confirmed action is retried exactly once", got)
	}
	if got := e.sentBodies(); len(got) != 2 || got[0] != "confirm=true" || got[1] != "confirm=true" {
		t.Errorf("sent bodies = %v, want both attempts to carry the confirmation", got)
	}
	if _, ok := outcome.(action.InvokeSucceeded); !ok {
		t.Fatalf("outcome = %#v, want InvokeSucceeded: the retry succeeding is what is reported", outcome)
	}
}

func TestARetryThatAlsoConflictsIsNotRetriedAgain(t *testing.T) {
	e := newEnv(t, map[string]route{
		"POST /cool": {status: 409, body: `{"properties":{"message":"still no"}}`},
	}, nil)
	e.action.Ask(e.backend, rootEntity(nil), "cool")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 2 {
		t.Errorf("POST count = %d, want 2: the retry itself is never retried, however it comes back", got)
	}
	failed, ok := outcome.(action.InvokeFailed)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeFailed", outcome)
	}
	if failed.Failure.Kind != failure.Conflict {
		t.Errorf("Kind = %v, want Conflict", failed.Failure.Kind)
	}
}

func TestConflictAfterTheConfirmationIsAMinuteOldIsNotRetried(t *testing.T) {
	e := newEnv(t, map[string]route{
		// The response itself is what carries the clock forward, so the
		// confirmation is already stale by the time the 409 is in hand --
		// exactly the case the TTL exists to reject.
		"POST /cool": {status: 409, body: `{"properties":{"message":"needs confirmation"}}`, advanceClockBy: time.Minute},
	}, nil)
	e.action.Ask(e.backend, rootEntity(nil), "cool")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 1 {
		t.Errorf("POST count = %d, want 1: a confirmation older than the TTL is not spent on a retry", got)
	}
	failed, ok := outcome.(action.InvokeFailed)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeFailed", outcome)
	}
	if failed.Failure.Kind != failure.Conflict {
		t.Errorf("Kind = %v, want Conflict", failed.Failure.Kind)
	}
}

func TestConflictJustInsideTheConfirmationTTLIsStillRetriedOnce(t *testing.T) {
	e := newEnv(t, nil, map[string][]route{
		"POST /cool": {
			{status: 409, body: `{"properties":{"message":"needs confirmation"}}`, advanceClockBy: 59 * time.Second},
			{status: 200, body: `{"properties":{"state":"done"}}`},
		},
	})
	e.action.Ask(e.backend, rootEntity(nil), "cool")

	outcome := e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 2 {
		t.Errorf("POST count = %d, want 2: a minute has not yet passed, so the retry is still taken", got)
	}
	if _, ok := outcome.(action.InvokeSucceeded); !ok {
		t.Fatalf("outcome = %#v, want InvokeSucceeded", outcome)
	}
}

func TestActionWithNoRequiredCheckboxIsNeverRetriedEvenWhenConfirmed(t *testing.T) {
	// "brew" has no checkbox at all: confirming it ticks nothing, so
	// there is no confirmation for the retry exception to apply to.
	e := newEnv(t, map[string]route{
		"POST /brew": {status: 409, body: `{"properties":{"message":"a brew is already running"}}`},
	}, nil)
	e.action.Ask(e.backend, rootEntity(nil), "brew")

	e.action.Answer(true, e.backend, rootEntity(nil))

	if got := e.postCount(); got != 1 {
		t.Errorf("POST count = %d, want 1", got)
	}
}
