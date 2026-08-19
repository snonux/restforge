// Part of the Confirm/ValuePrompt overlay (task 631): the yes/no modal for a
// session.ConfirmQuestion, the terminal counterpart to
// flutter/lib/screens/confirmation_sheet.dart's _ConfirmBody. Split from
// valueprompt.go (its ValueQuestion sibling) because a ConfirmQuestion needs
// no state of its own to hold between renders -- Heading and Body are
// exactly what internal/action's ConfirmationRequired/ConfirmationText
// already composed, shown verbatim (docs/DESIGN.md, "Rendering does not
// interpret" -- written about documents, but the reasoning carries over
// unchanged: this app does not get to improve on wording the server, or a
// required checkbox's own title, already wrote for this exact moment). So
// confirmView is a pure function of session.Question(), not a model kept on
// Model the way valuePromptModel's textinput.Model has to be. Key handling
// (confirm_update.go) lives beside it, the same split document.go/
// document_update.go and settings.go/settings_update.go use.
package tui

import "github.com/snonux/restforge/cli/internal/session"

// confirmView renders the yes/no modal for q, framed in OverlayBorderStyle
// so it reads as the same kind of thing ValuePrompt and (once task 731
// lands) Detail render -- mirrors document_screen.dart layering its own
// overlays over the document underneath, translated here to "this screen
// replaces Document for as long as the question is pending" per
// deriveScreen's own doc comment (derive.go), since Bubble Tea redraws
// whole-screen on every message rather than compositing a persistent widget
// tree.
func confirmView(q session.ConfirmQuestion) string {
	body := TitleStyle.Render(q.Heading) + "\n\n" +
		q.Body + "\n\n" +
		MutedStyle.Render("y confirm · n/esc cancel")
	return OverlayBorderStyle.Render(body)
}
