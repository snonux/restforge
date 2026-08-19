// Part of the Confirm/ValuePrompt overlay (task 631): the "what value"
// follow-up for a session.ValueQuestion, the terminal counterpart to
// flutter/lib/screens/confirmation_sheet.dart's _ValueBody. Unlike
// ConfirmQuestion (confirm.go), a ValueQuestion needs somewhere to hold what
// has been typed between renders, so -- exactly like settingsForm
// (settings_form.go) holding bubbles/textinput.Models for the Settings
// screen -- that state lives on Model as its own valuePromptModel rather
// than being rebuilt fresh on every View call. Key handling
// (valueprompt_update.go) lives beside it, the same split confirm.go/
// confirm_update.go and document.go/document_update.go use.
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/snonux/restforge/cli/internal/session"
)

// valuePromptModel is the ValuePrompt screen's own state: one
// bubbles/textinput.Model for whatever is being typed, plus enough
// bookkeeping (label, shownForLabel) to tell "the same question redrawn"
// apart from "a new question just like it" -- see syncFromSession.
type valuePromptModel struct {
	input textinput.Model

	// label is the session.ValueQuestion.Label the input was last
	// (re)initialised for -- see syncFromSession.
	label string

	// shownForLabel is whether the input has already been (re)initialised
	// for label. Needed alongside label itself because "" is a valid label
	// (a required field with no server-supplied title, falling back to its
	// bare field name is still possible to be empty in principle) -- without
	// this flag, a fresh valuePromptModel (whose zero-value label is also
	// "") could not be told apart from one already shown for an
	// empty-labelled question.
	shownForLabel bool
}

// newValuePromptModel builds an empty, unfocused text input -- syncFromSession
// clears and focuses it the first time a session.ValueQuestion actually
// appears, the same lazy-initialisation newSettingsModel's own doc comment
// describes for its list.Model.
func newValuePromptModel() valuePromptModel {
	in := textinput.New()
	in.Prompt = "> "
	return valuePromptModel{input: in}
}

// syncFromSession (re)initialises the text input whenever q is a
// session.ValueQuestion this model has not already been shown for -- cleared
// and focused fresh, so a stale answer from a previous question never
// lingers into the next one. Mirrors confirmation_sheet.dart's own
// _ConfirmationSheetState._controller: "by the time a ValueQuestion replaces
// [a ConfirmQuestion] in-place the controller is still empty, exactly as if
// it were fresh" (see that file's module comment) -- this port reaches the
// same end state (an empty, focused field) for every distinct ValueQuestion,
// whether it followed a ConfirmQuestion, another ValueQuestion, or nothing
// at all.
//
// A no-op once already shown for the same label, so this model's own text
// and cursor position survive an unrelated redraw (a resize, toggling help)
// the same way documentModel.syncRows preserves the row list's cursor --
// called from Model.Update everywhere documentModel.syncRows already is
// (model.go).
func (v valuePromptModel) syncFromSession(q session.SessionQuestion) valuePromptModel {
	vq, ok := q.(session.ValueQuestion)
	if !ok {
		// Not a ValueQuestion right now (nil, or a ConfirmQuestion): forget
		// shownForLabel, so the next ValueQuestion -- even one with the same
		// label as before -- is treated as new rather than mistaken for a
		// redraw of a question this screen already handled.
		v.shownForLabel = false
		return v
	}
	if v.shownForLabel && v.label == vq.Label {
		return v
	}
	v.label = vq.Label
	v.shownForLabel = true
	v.input.SetValue("")
	v.input.Focus()
	return v
}

// View renders the ValuePrompt modal for q: its label and the text input,
// framed in the same OverlayBorderStyle confirmView uses.
func (v valuePromptModel) View(q session.ValueQuestion) string {
	body := TitleStyle.Render(q.Label) + "\n\n" +
		v.input.View() + "\n\n" +
		MutedStyle.Render("enter send · esc cancel")
	return OverlayBorderStyle.Render(body)
}
