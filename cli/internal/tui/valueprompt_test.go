package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// --- syncFromSession ------------------------------------------------------

func TestSyncFromSessionFocusesAndClearsOnFirstAppearance(t *testing.T) {
	v := newValuePromptModel()

	v = v.syncFromSession(session.ValueQuestion{Label: "A note"})

	if v.input.Value() != "" {
		t.Errorf("input.Value() = %q, want empty on first appearance", v.input.Value())
	}
	if !v.input.Focused() {
		t.Error("input is not focused after syncFromSession raised a new ValueQuestion")
	}
	if v.label != "A note" {
		t.Errorf("label = %q, want %q", v.label, "A note")
	}
}

// TestSyncFromSessionPreservesTypedTextOnUnchangedRedraw pins
// syncFromSession's whole reason for existing: an unrelated redraw (a
// resize, toggling help) re-delivers the very same ValueQuestion, and must
// not wipe out whatever the user has typed so far -- mirrors
// documentModel.syncRows preserving the row list's cursor for the same
// reason.
func TestSyncFromSessionPreservesTypedTextOnUnchangedRedraw(t *testing.T) {
	v := newValuePromptModel().syncFromSession(session.ValueQuestion{Label: "A note"})
	v.input.SetValue("hello")

	v = v.syncFromSession(session.ValueQuestion{Label: "A note"})

	if v.input.Value() != "hello" {
		t.Errorf("input.Value() = %q, want %q preserved across an unchanged resync", v.input.Value(), "hello")
	}
}

// TestSyncFromSessionResetsWhenLabelChanges checks that a different
// ValueQuestion -- even one arriving directly, with no intervening
// non-ValueQuestion state -- is never mistaken for a redraw of the one
// before it.
func TestSyncFromSessionResetsWhenLabelChanges(t *testing.T) {
	v := newValuePromptModel().syncFromSession(session.ValueQuestion{Label: "A"})
	v.input.SetValue("hello")

	v = v.syncFromSession(session.ValueQuestion{Label: "B"})

	if v.input.Value() != "" {
		t.Errorf("input.Value() = %q, want empty once the label changed", v.input.Value())
	}
	if v.label != "B" {
		t.Errorf("label = %q, want %q", v.label, "B")
	}
}

// TestSyncFromSessionResetsOnReappearanceAfterAnotherQuestion checks the
// case a bare label comparison would miss: the same label appearing again
// after something else (nil, or a ConfirmQuestion) was current in between
// must still be treated as a fresh question, not a redraw of the earlier
// one this model already handled -- see the shownForLabel field's own doc
// comment on why label alone cannot tell these apart.
func TestSyncFromSessionResetsOnReappearanceAfterAnotherQuestion(t *testing.T) {
	v := newValuePromptModel().syncFromSession(session.ValueQuestion{Label: "A"})
	v.input.SetValue("hello")

	v = v.syncFromSession(nil)
	v = v.syncFromSession(session.ValueQuestion{Label: "A"})

	if v.input.Value() != "" {
		t.Errorf("input.Value() = %q, want empty: the same label reappearing after another state must reset", v.input.Value())
	}
}

// --- View -----------------------------------------------------------------

func TestValuePromptViewShowsLabelAndInput(t *testing.T) {
	v := newValuePromptModel().syncFromSession(session.ValueQuestion{Label: "A note"})

	view := v.View(session.ValueQuestion{Label: "A note"})

	if !strings.Contains(view, "A note") {
		t.Errorf("View() = %q, want it to contain the question's label", view)
	}
}

// --- updateValuePrompt ------------------------------------------------

// TestUpdateValuePromptEnterSubmitsTypedText types "hello" through
// updateValuePrompt's own forwarding to bubbles/textinput.Model, then
// submits with Enter, and checks the field actually sent carries what was
// typed -- exercising the "safe-needs-note" fixture, which lands directly
// on a ValueQuestion with no ConfirmQuestion first (a safe method invokes
// straight through).
func TestUpdateValuePromptEnterSubmitsTypedText(t *testing.T) {
	var gotFields map[string]string
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		gotFields = fields
		return httpclient.HTTPResponse{Status: 200, Entity: map[string]any{}}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "safe-needs-note"})
	vq, ok := m.session.Question().(session.ValueQuestion)
	if !ok {
		t.Fatalf("test setup: want a ValueQuestion pending, got %T", m.session.Question())
	}
	m.valuePrompt = m.valuePrompt.syncFromSession(vq)

	for _, r := range "hello" {
		next, _ := m.updateValuePrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if m.valuePrompt.input.Value() != "hello" {
		t.Fatalf("test setup: input.Value() = %q, want %q typed in", m.valuePrompt.input.Value(), "hello")
	}

	next, cmd := m.updateValuePrompt(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("updateValuePrompt(Enter) returned a nil cmd, want sessionCmd wrapping Session.AnswerValue")
	}
	msg := cmd()
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Fatalf("cmd() = %T, want sessionUpdatedMsg", msg)
	}
	nm := next.(Model)
	if nm.session.Question() != nil {
		t.Error("Question() still pending after Enter")
	}
	if gotFields["note"] != "hello" {
		t.Errorf(`Request called with fields["note"] = %q, want %q`, gotFields["note"], "hello")
	}
}

// TestUpdateValuePromptEnterWithEmptyTextRefusesWithoutSending is this
// task's own named requirement: an empty submission must be treated as
// InvokeRefused (nothing sent), never sent as an empty string -- matching
// action_service.dart's answerValue guard, ported to
// internal/action.AnswerValue (invoke.go) and relied on rather than
// re-implemented here (see updateValuePrompt's own doc comment).
func TestUpdateValuePromptEnterWithEmptyTextRefusesWithoutSending(t *testing.T) {
	called := false
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		called = true
		return httpclient.HTTPResponse{}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "safe-needs-note"})
	m.valuePrompt = m.valuePrompt.syncFromSession(m.session.Question())

	next, cmd := m.updateValuePrompt(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("updateValuePrompt(Enter) returned a nil cmd even with an empty input")
	}
	next, _ = (next.(Model)).Update(cmd())
	nm := next.(Model)

	if called {
		t.Error("an empty submission must never reach the network")
	}
	if nm.session.Question() != nil {
		t.Error("Question() still pending after an empty submission")
	}
}

// --- global Back declines a pending ValueQuestion ------------------------

func TestHandleBackDeclinesPendingValueQuestion(t *testing.T) {
	called := false
	m := openQuestionFixture(func(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
		called = true
		return httpclient.HTTPResponse{}, nil
	})
	m.session.Activate(render.ActionTarget{Name: "safe-needs-note"})
	m.valuePrompt = m.valuePrompt.syncFromSession(m.session.Question())
	m.valuePrompt.input.SetValue("half-typed")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Error("declining a question via the global Back key should not need a tea.Cmd")
	}
	nm := next.(Model)
	if nm.session.Question() != nil {
		t.Error("esc did not decline the pending ValueQuestion")
	}
	if called {
		t.Error("declining via the global Back key must not send anything")
	}
}

// TestUpdateValuePromptForwardsOtherKeysToInput checks that a key which is
// neither Enter nor intercepted by the shell's global bindings (backspace
// included -- see valuePromptSubmitBinding's own doc comment) still reaches
// bubbles/textinput.Model's own editing behaviour once it does get here.
func TestUpdateValuePromptForwardsOtherKeysToInput(t *testing.T) {
	m := openQuestionFixture(nil)
	m.session.Activate(render.ActionTarget{Name: "safe-needs-note"})
	m.valuePrompt = m.valuePrompt.syncFromSession(m.session.Question())

	// ctrl+h is textinput.Model's DefaultKeyMap alternate for
	// DeleteCharacterBackward -- reachable here because the shell only
	// claims the literal "backspace" key for its own global Back binding
	// (keys.go), the same convention updateSettingsEdit's own doc comment
	// documents for the Settings screen's text fields.
	next, _ := m.updateValuePrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	next, _ = m.updateValuePrompt(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = next.(Model)

	if m.valuePrompt.input.Value() != "" {
		t.Errorf("input.Value() = %q, want empty after typing 'a' then ctrl+h deleted it", m.valuePrompt.input.Value())
	}
}
