package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// questionFixture builds a root document offering three actions, one for
// each shape a Confirm/ValuePrompt test needs -- shared by this file and
// valueprompt_test.go, the same way model_test.go's fakeClient/newTestModel
// are shared across every screen's own test file:
//
//   - "delete-thing" is unsafe (DELETE) with no fields at all -- Activate
//     raises a plain ConfirmQuestion, and confirming it sends the request
//     straight away with nothing further to ask.
//   - "unsafe-needs-note" is unsafe (POST) with one required, non-checkbox
//     field -- Activate raises a ConfirmQuestion first (same as
//     delete-thing), but confirming it turns the very same pending question
//     into a ValueQuestion instead of sending anything
//     (action.InvokeNeedsValue) -- exercising the Confirm -> ValuePrompt
//     hand-off valuePromptModel.syncFromSession has to get right.
//   - "safe-needs-note" is safe (GET) with the same required field --
//     Activate invokes it straight through (a safe method never confirms)
//     and lands directly on a ValueQuestion with no ConfirmQuestion in
//     between, covering the other way a ValueQuestion can appear.
//
// Mirrors newTestModel/newLinkedTestModel (model_test.go, document_test.go)
// in shape: a fakeClient wired through session.New, wrapped in a Model.
func questionFixture() (Model, *fakeClient) {
	client := &fakeClient{routes: map[string]map[string]any{
		testBaseURL: {
			"class": []any{"root"},
			"title": "Root",
			"actions": []any{
				map[string]any{
					"name":   "delete-thing",
					"method": "DELETE",
					"href":   testBaseURL + "delete",
					"title":  "Delete it",
				},
				map[string]any{
					"name":   "unsafe-needs-note",
					"method": "POST",
					"href":   testBaseURL + "unsafe-needs-note",
					"title":  "Do the unsafe thing",
					"fields": []any{
						map[string]any{"name": "note", "required": true, "title": "A note"},
					},
				},
				map[string]any{
					"name":   "safe-needs-note",
					"method": "GET",
					"href":   testBaseURL + "safe-needs-note",
					"title":  "Do the safe thing",
					"fields": []any{
						map[string]any{"name": "note", "required": true, "title": "A note"},
					},
				},
			},
		},
	}}
	sess := session.New(nav.New(client), action.New(client), live.New(client))
	return New(sess), client
}

// openQuestionFixture builds questionFixture, points its fakeClient's
// Request at requestFn (nil when a test expects Request to never be
// called), and opens the backend so Session has a current entity to look
// actions up on -- everything a Confirm/ValuePrompt test needs before it
// calls Session.Activate itself.
func openQuestionFixture(requestFn func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error)) Model {
	m, client := questionFixture()
	client.requestFn = requestFn
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.base = screenDocument
	m.document = m.document.syncRows(m.session.Document())
	return m
}

// --- confirmView ------------------------------------------------------

func TestConfirmViewShowsHeadingAndBody(t *testing.T) {
	view := confirmView(session.ConfirmQuestion{Heading: "Delete it", Body: "DELETE to this server."})

	if !strings.Contains(view, "Delete it") {
		t.Errorf("confirmView() = %q, want it to contain the heading", view)
	}
	if !strings.Contains(view, "DELETE to this server.") {
		t.Errorf("confirmView() = %q, want it to contain the body", view)
	}
}

// --- Activate raising ConfirmQuestion ----------------------------------

func TestActivateUnsafeActionRaisesConfirmQuestion(t *testing.T) {
	m := openQuestionFixture(nil)
	m.session.Activate(render.ActionTarget{Name: "delete-thing"})

	if m.currentScreen() != screenConfirm {
		t.Fatalf("currentScreen() = %v, want screenConfirm", m.currentScreen())
	}
	q, ok := m.session.Question().(session.ConfirmQuestion)
	if !ok {
		t.Fatalf("Question() = %T, want session.ConfirmQuestion", m.session.Question())
	}
	if q.Heading != "Delete it" {
		t.Errorf("Question().Heading = %q, want %q", q.Heading, "Delete it")
	}
}

// --- updateConfirm ------------------------------------------------------

// TestUpdateConfirmYesSendsRequestAndClearsQuestion drives 'y' through
// updateConfirm and checks the sessionCmd it returns actually reaches
// Session.Answer(true), which sends the request action.Ask remembered
// (href/method never exposed past internal/action -- see that package's
// own doc comment) and clears the pending question.
func TestUpdateConfirmYesSendsRequestAndClearsQuestion(t *testing.T) {
	var gotHref, gotMethod string
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		gotHref, gotMethod = href, method
		return httpclient.HTTPResponse{Status: 200, Entity: map[string]any{}}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "delete-thing"})

	next, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("updateConfirm('y') returned a nil cmd, want sessionCmd wrapping Session.Answer(true)")
	}
	msg := cmd()
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Fatalf("cmd() = %T, want sessionUpdatedMsg", msg)
	}
	nm := next.(Model)
	if nm.session.Question() != nil {
		t.Error("Question() still pending after 'y'")
	}
	if gotHref != testBaseURL+"delete" || gotMethod != "DELETE" {
		t.Errorf("Request called with (href, method) = (%q, %q), want (%q, %q)", gotHref, gotMethod, testBaseURL+"delete", "DELETE")
	}
}

// TestUpdateConfirmNoDeclinesWithoutSendingRequest checks the 'n' key
// declines the same way handleBack's Esc/backspace path does: nothing is
// sent, and updateConfirm needs no tea.Cmd for it (action.Answer(false)'s
// own contract: declining performs no I/O).
func TestUpdateConfirmNoDeclinesWithoutSendingRequest(t *testing.T) {
	called := false
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		called = true
		return httpclient.HTTPResponse{}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "delete-thing"})

	next, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		t.Error("updateConfirm('n') returned a non-nil cmd; declining performs no I/O and should not need one")
	}
	nm := next.(Model)
	if nm.session.Question() != nil {
		t.Error("Question() still pending after 'n'")
	}
	if called {
		t.Error("'n' must not send the request")
	}
}

// --- global Back declines a pending question ----------------------------

// TestHandleBackDeclinesPendingConfirmQuestion checks the shell-level path:
// the global Esc/backspace key, routed through Model.handleBack, declines a
// pending ConfirmQuestion exactly as updateConfirm's own 'n' does --
// mirrors confirmation_sheet.dart's ConfirmationSheetHost answering false
// on every dismissal that is not the sheet's own Confirm button.
func TestHandleBackDeclinesPendingConfirmQuestion(t *testing.T) {
	called := false
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		called = true
		return httpclient.HTTPResponse{}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "delete-thing"})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Error("declining a question via the global Back key should not need a tea.Cmd")
	}
	nm := next.(Model)
	if nm.session.Question() != nil {
		t.Error("esc did not decline the pending ConfirmQuestion")
	}
	if nm.currentScreen() != screenDocument {
		t.Errorf("currentScreen() = %v, want screenDocument once the question is declined", nm.currentScreen())
	}
	if called {
		t.Error("declining via the global Back key must not send the request")
	}
}

// --- Confirm -> ValuePrompt hand-off -------------------------------------

// TestConfirmYesTransitionsToValueQuestionWhenFieldMissing drives the
// unsafe-needs-note fixture's whole round trip through Model.Update, the
// way the real tea.Program loop drives it: confirming an action that turns
// out to need a value it was not given switches the shell straight to
// ValuePrompt, without ever sending a request (action.InvokeNeedsValue) --
// see internal/action/invoke.go's own doc comment on why this is not a bug
// to route around.
func TestConfirmYesTransitionsToValueQuestionWhenFieldMissing(t *testing.T) {
	called := false
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		called = true
		return httpclient.HTTPResponse{}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "unsafe-needs-note"})
	if _, ok := m.session.Question().(session.ConfirmQuestion); !ok {
		t.Fatalf("test setup: want a ConfirmQuestion pending, got %T", m.session.Question())
	}

	next, cmd := m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	vq, ok := m.session.Question().(session.ValueQuestion)
	if !ok {
		t.Fatalf("Question() = %T, want session.ValueQuestion", m.session.Question())
	}
	if vq.Label != "A note" {
		t.Errorf("Question().Label = %q, want %q", vq.Label, "A note")
	}
	if m.currentScreen() != screenValuePrompt {
		t.Errorf("currentScreen() = %v, want screenValuePrompt", m.currentScreen())
	}
	if called {
		t.Error("confirming must not send the request before the missing field is filled")
	}
}

// --- integration: a row press reaches Confirm ----------------------------

// TestModelIntegrationActivateActionRowRaisesConfirm exercises the whole
// path a real keypress takes: pressing Enter on the action row activates it
// through activateDocumentSelection (document_update.go), and the resulting
// sessionUpdatedMsg is enough for deriveScreen to switch the shell to
// Confirm -- mirrors TestModelIntegrationOpenBackendThenActivateLink
// (document_test.go) for a link row.
func TestModelIntegrationActivateActionRowRaisesConfirm(t *testing.T) {
	m := openQuestionFixture(nil)

	// The root fixture's rows: no properties, no sub-entities, no links,
	// then the three actions in declaration order (render.actionRows) --
	// "delete-thing" is first.
	if len(m.document.rows.Items()) != 3 {
		t.Fatalf("test setup: rows.Items() len = %d, want 3", len(m.document.rows.Items()))
	}
	m.document.rows.Select(0)

	next, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.currentScreen() != screenConfirm {
		t.Fatalf("currentScreen() = %v, want screenConfirm after activating the action row", m.currentScreen())
	}
	view := m.currentScreenView()
	if !strings.Contains(view, "Delete it") {
		t.Errorf("currentScreenView() = %q, want it to contain the action's heading", view)
	}
}
