package action_test

// Ported from action_service_test.dart's "hasPending" and "asking about an
// action" groups.

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/siren"
)

func TestHasPendingTracksAskAction(t *testing.T) {
	e := newEnv(t, nil, nil)
	if e.action.HasPending() {
		t.Fatal("HasPending before Ask = true, want false")
	}

	outcome := e.action.Ask(e.backend, rootEntity(nil), "brew")
	if _, ok := outcome.(action.ConfirmationRequired); !ok {
		t.Fatalf("Ask outcome = %#v, want ConfirmationRequired", outcome)
	}
	if !e.action.HasPending() {
		t.Fatal("HasPending after Ask = false, want true")
	}

	e.action.CancelPending()
	if e.action.HasPending() {
		t.Error("HasPending after CancelPending = true, want false")
	}
	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none: nothing was sent by asking then cancelling", got)
	}
}

func TestUnsafeActionAsksFirst(t *testing.T) {
	e := newEnv(t, nil, nil)
	outcome := e.action.Ask(e.backend, rootEntity(nil), "brew")

	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
	confirm, ok := outcome.(action.ConfirmationRequired)
	if !ok {
		t.Fatalf("outcome = %#v, want ConfirmationRequired", outcome)
	}
	if confirm.Heading != "Brew a pot of tea" {
		t.Errorf("Heading = %q, want the server's wording", confirm.Heading)
	}
}

func TestSafeActionGoesStraightThrough(t *testing.T) {
	e := newEnv(t, nil, nil)
	outcome := e.action.Ask(e.backend, rootEntity(nil), "peek")

	if got := e.requestedKeys(); len(got) != 1 || got[0] != "GET /peek" {
		t.Errorf("requested = %v, want [GET /peek]", got)
	}
	invoked, ok := outcome.(action.ActionInvoked)
	if !ok {
		t.Fatalf("outcome = %#v, want ActionInvoked", outcome)
	}
	if _, ok := invoked.Outcome.(action.InvokeSucceeded); !ok {
		t.Errorf("inner outcome = %#v, want InvokeSucceeded", invoked.Outcome)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false: nothing is left pending once it went straight through")
	}
}

func TestWithdrawnActionIsReportedNotInvented(t *testing.T) {
	e := newEnv(t, nil, nil)
	outcome := e.action.Ask(e.backend, rootEntity([]siren.Action{}), "brew")

	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
	notOffered, ok := outcome.(action.ActionNotOffered)
	if !ok {
		t.Fatalf("outcome = %#v, want ActionNotOffered", outcome)
	}
	if notOffered.Name != "brew" {
		t.Errorf("Name = %q, want brew", notOffered.Name)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false")
	}
}
