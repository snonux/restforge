// Part of the Detail overlay (task 731): the full-text, scrollable reading
// view for a session.DetailView, the terminal counterpart to
// flutter/lib/screens/detail_screen.dart's DetailScreen. Opened automatically
// whenever Session.Detail() is non-nil (deriveScreen, derive.go) -- for a
// property row too long for its own line (render.DetailTarget, resolved by
// Session.Activate) -- so the whole value is legible and nothing is silently
// cut off, mirroring the requirement detail_screen.dart's own module comment
// traces back to the Pebble original's win_detail.c/win_scroll_text.c.
//
// Built on bubbles/viewport.Model, the component this task's own description
// names for a full-text, scrollable reading view -- the first screen in this
// package to need one, since Confirm and ValuePrompt's bodies are never long
// enough to need scrolling of their own. Unlike ConfirmQuestion (confirm.go),
// a DetailView needs somewhere to hold scroll position between renders, so --
// exactly like valuePromptModel holding a bubbles/textinput.Model -- that
// state lives on Model as its own detailModel rather than being rebuilt
// fresh on every View call. Key handling (detail_update.go) lives beside it,
// the same split every other screen in this package uses.
package tui

import (
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/snonux/restforge/cli/internal/session"
)

// detailModel is the Detail screen's own state: one bubbles/viewport.Model
// holding the value's full body, plus enough bookkeeping (shown) to tell
// "the same DetailView redrawn" apart from "a new one just opened" -- see
// syncFromSession.
type detailModel struct {
	viewport viewport.Model

	// shown is the *session.DetailView the viewport's content was last set
	// from, compared by pointer identity. Unlike documentModel.syncRows
	// (Session.Document rebuilds a fresh *render.RenderedDocument on every
	// call, so pointer identity cannot be used there -- see
	// documentFingerprint's own doc comment), Session.Detail returns
	// whatever *DetailView is currently stored on Session, unchanged across
	// calls until Activate or DismissDetail next touches it (session.go) --
	// so pointer identity is enough here and needs no fingerprint.
	shown *session.DetailView
}

// newDetailModel builds an empty, zero-size viewport -- resize gives it its
// real size once the terminal's own size is known, the same lazy-sizing
// newDocumentModel/newHomeModel use.
func newDetailModel() detailModel {
	return detailModel{viewport: viewport.New(0, 0)}
}

// resize gives the viewport its share of the available body height, after
// setting aside room for the heading above it and the hint line below --
// mirrors documentModel.resize's own reasoning, generous rather than exact
// for the same stated reason.
func (d detailModel) resize(width, height int) detailModel {
	const chrome = 6
	avail := height - chrome
	if avail < 3 {
		avail = 3
	}
	d.viewport.Width = width
	d.viewport.Height = avail
	return d
}

// syncFromSession (re)initialises the viewport's content whenever detail is
// a *session.DetailView this model has not already been shown for --
// scrolled back to the top, so a previous reading's scroll position never
// leaks into an unrelated value. A no-op once already shown for the same
// pointer, so this model's own scroll position survives an unrelated redraw
// (a resize, toggling help) the same way valuePromptModel.syncFromSession
// preserves typed text -- called from Model.Update everywhere
// documentModel.syncRows/valuePromptModel.syncFromSession already are
// (model.go).
func (d detailModel) syncFromSession(detail *session.DetailView) detailModel {
	if detail == d.shown {
		return d
	}
	d.shown = detail
	if detail == nil {
		// Dismissed, or superseded by clearTransient (session.go) -- there
		// is nothing left to show; the next DetailView to appear will set
		// fresh content of its own.
		return d
	}
	d.viewport.SetContent(detail.Body)
	d.viewport.GotoTop()
	return d
}

// View renders the Detail modal for detail: its heading and the viewport's
// own scrolled window onto the full body, framed in the same
// OverlayBorderStyle Confirm and ValuePrompt use -- mirrors
// detail_screen.dart's AppBar title plus scrollable body, translated here to
// "this screen replaces Document for as long as a value is open for
// reading" per deriveScreen's own doc comment, since a terminal has no
// separate AppBar/CloseButton chrome of its own to reach for.
func (d detailModel) View(detail session.DetailView) string {
	body := TitleStyle.Render(detail.Heading) + "\n\n" +
		d.viewport.View() + "\n\n" +
		MutedStyle.Render(d.hintLine())
	return OverlayBorderStyle.Render(body)
}

// hintLine is the Detail screen's own short key hint: scrolling, plus a
// reminder that the shell's global Back key (keys.go) is what dismisses this
// overlay -- see Model.handleBack, which calls Session.DismissDetail before
// this screen's own key handling (updateDetail, detail_update.go) ever runs.
func (d detailModel) hintLine() string {
	return "↑/↓ scroll · esc close"
}
