package tui

import "github.com/snonux/restforge/cli/internal/session"

// screenSource is the subset of *internal/session.Session deriveScreen
// reads: just the two overlay accessors. Declared as its own interface
// (rather than taking *session.Session directly) so a test can drive
// deriveScreen with a plain struct instead of a full Session wired to real
// internal/nav, internal/action and internal/live instances -- the
// dependency-inversion seam nav/action/live's own httpGetter/requester
// interfaces already use for the same reason, one layer down.
type screenSource interface {
	Detail() *session.DetailView
	Question() session.SessionQuestion
}

// *session.Session satisfies screenSource structurally; asserted here so a
// future signature change to either method is caught at compile time,
// rather than surfacing only when Model.currentScreen is called.
var _ screenSource = (*session.Session)(nil)

// deriveScreen decides which screen is on top right now. Session's own
// overlay state always wins over base (the screen last explicitly
// navigated to -- Home, Document or Settings): Detail first, since it
// reads as a full-screen view laid over everything else once a value is
// open for reading, then Question, split into Confirm or ValuePrompt by
// its concrete type. Mirrors how document_screen.dart layers detail,
// question and notice over whatever document is on screen (see that
// file's top-of-file comment) -- translated here from "all three always
// present, conditionally rendered" to "exactly one screen is current",
// since Bubble Tea redraws whole-screen on every message rather than
// diffing a persistent widget tree.
//
// notice (session.SessionNotice) has no screen of its own on purpose: it
// is a banner over the Document screen (task 831), never a full-screen
// state -- see SessionNotice's own doc comment in internal/session.
func deriveScreen(src screenSource, base screen) screen {
	if src.Detail() != nil {
		return screenDetail
	}
	switch src.Question().(type) {
	case session.ConfirmQuestion:
		return screenConfirm
	case session.ValueQuestion:
		return screenValuePrompt
	}
	return base
}
