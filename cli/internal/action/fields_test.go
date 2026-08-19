package action_test

// Ported from action_service_test.dart's "field-filling" and
// "confirmationText" groups. The "fieldValues" group -- exercising the
// unexported fill helper directly -- lives in fields_internal_test.go
// instead, in package action, mirroring how the Dart test reaches it
// (fieldValues is a plain top-level function in the same library, so
// nothing there is actually private to the test).

import (
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/siren"
)

func TestServerSuppliedDefaultIsUsedWithoutAsking(t *testing.T) {
	e := newEnv(t, nil, nil)
	doc := labelDoc([]siren.Field{
		{Name: "text", Type: "text", Required: true, Title: "What should the jar say?", Value: "jam"},
	})
	e.action.Ask(e.backend, doc, "label")

	outcome := e.action.Answer(true, e.backend, doc)

	if _, ok := outcome.(action.InvokeSucceeded); !ok {
		t.Fatalf("outcome = %#v, want InvokeSucceeded: required is satisfied by the server-supplied default", outcome)
	}
	if got := e.sentBodies(); len(got) != 1 || got[0] != "text=jam" {
		t.Errorf("sent bodies = %v, want [text=jam]", got)
	}
}

func TestFieldWithNoCheckboxNoDefaultAndNotRequiredIsLeftUnfilled(t *testing.T) {
	e := newEnv(t, nil, nil)
	doc := labelDoc([]siren.Field{
		{Name: "text", Type: "text", Title: "What should the jar say?"},
	})
	e.action.Ask(e.backend, doc, "label")

	e.action.Answer(true, e.backend, doc)

	if got := e.sentBodies(); len(got) != 1 || got[0] != "" {
		t.Errorf("sent bodies = %v, want ['']", got)
	}
}

func TestRequiredFieldWithNoDefaultAndNoCheckboxIsAskedForOutLoud(t *testing.T) {
	e := newEnv(t, nil, nil)
	doc := labelDoc([]siren.Field{
		{Name: "text", Type: "text", Required: true, Title: "What should the jar say?"},
	})
	e.action.Ask(e.backend, doc, "label")

	outcome := e.action.Answer(true, e.backend, doc)

	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none: nothing is sent while a value is missing", got)
	}
	needsValue, ok := outcome.(action.InvokeNeedsValue)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeNeedsValue", outcome)
	}
	if needsValue.FieldName != "text" {
		t.Errorf("FieldName = %q, want text", needsValue.FieldName)
	}
	if needsValue.Label != "What should the jar say?" {
		t.Errorf("Label = %q, want the server's wording", needsValue.Label)
	}

	answered := e.action.AnswerValue("plum jam", e.backend, doc)
	if _, ok := answered.(action.InvokeSucceeded); !ok {
		t.Fatalf("answered = %#v, want InvokeSucceeded: what was said is what is sent", answered)
	}
	if got := e.sentBodies(); len(got) != 1 || got[0] != "text=plum+jam" {
		t.Errorf("sent bodies = %v, want [text=plum+jam]", got)
	}
}

func TestEmptySpokenValueSendsNothingAndSaysSo(t *testing.T) {
	e := newEnv(t, nil, nil)
	doc := labelDoc([]siren.Field{{Name: "text", Type: "text", Required: true}})
	e.action.Ask(e.backend, doc, "label")
	e.action.Answer(true, e.backend, doc)

	outcome := e.action.AnswerValue("", e.backend, doc)

	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none: an empty transcription sends nothing", got)
	}
	refused, ok := outcome.(action.InvokeRefused)
	if !ok {
		t.Fatalf("outcome = %#v, want InvokeRefused", outcome)
	}
	if !strings.Contains(refused.Reason, "Nothing was heard") {
		t.Errorf("Reason = %q, want it to mention nothing was heard", refused.Reason)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false: an unanswerable question is not left pending forever")
	}
}

func TestMoreThanOneMissingRequiredFieldIsRefusedNotDictated(t *testing.T) {
	e := newEnv(t, nil, nil)
	doc := labelDoc([]siren.Field{
		{Name: "text", Type: "text", Required: true},
		{Name: "colour", Type: "text", Required: true},
	})
	e.action.Ask(e.backend, doc, "label")

	outcome := e.action.Answer(true, e.backend, doc)

	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none: more than one missing value is refused, not dictated", got)
	}
	if _, ok := outcome.(action.InvokeRefused); !ok {
		t.Fatalf("outcome = %#v, want InvokeRefused", outcome)
	}
	if e.action.HasPending() {
		t.Error("HasPending = true, want false: a refused action does not stay pending")
	}
}

func TestAnswerValueWithNothingAwaitingAValueSendsNothing(t *testing.T) {
	e := newEnv(t, nil, nil)
	outcome := e.action.AnswerValue("anything", e.backend, rootEntity(nil))

	if outcome != nil {
		t.Errorf("outcome = %#v, want nil", outcome)
	}
	if got := e.requestedKeys(); len(got) != 0 {
		t.Errorf("requested = %v, want none", got)
	}
}

func TestConfirmationTextChecksboxWithTitleIsTheQuestionAsked(t *testing.T) {
	act := rootEntity(nil).ActionByName("cool")
	got := action.ConfirmationText(*act)
	want := "The kettle is still hot. Cool it anyway?"
	if got != want {
		t.Errorf("ConfirmationText = %q, want %q", got, want)
	}
}

func TestConfirmationTextFallbackNamesTheActionAndItsMethod(t *testing.T) {
	act := rootEntity(nil).ActionByName("brew")
	got := action.ConfirmationText(*act)
	want := "Brew a pot of tea?  POST to this server."
	if got != want {
		t.Errorf("ConfirmationText = %q, want %q", got, want)
	}
}
