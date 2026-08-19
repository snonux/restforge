package tui

// screen is the closed set of top-level views the root Model switches
// between -- the Screen enum the tui shell task calls for. A closed
// interface (one unexported marker method, the pattern render.RowTarget
// and session.SessionQuestion already use in this codebase) was the other
// option; an enum was chosen instead because every one of these states is
// interchangeable data to the shell -- deriveScreen and Model.View just
// need to know *which* one is current, never anything it carries, so a
// plain comparable value is the simpler fit and needs no default-panics
// switch to stay closed (an int const set is closed by construction: it is
// declared once, right here).
type screen int

const (
	// screenHome is the opening screen: the configured-backend and
	// saved-quick-shortcut picker, rendered by homeModel (home.go). The
	// root Model starts on this screen.
	screenHome screen = iota

	// screenDocument renders Session.Document's rows for whichever
	// backend/href is currently open, with Session.State/Session.Failure
	// overlaid on top without replacing the last-good document -- mirrors
	// flutter/lib/screens/document_screen.dart. Filled in by task 531.
	screenDocument

	// screenConfirm is the yes/no modal for a session.ConfirmQuestion.
	// Entered automatically whenever Session.Question() holds one -- see
	// deriveScreen -- never navigated to directly. Filled in by task 631.
	screenConfirm

	// screenValuePrompt is the modal asking for a value out loud, for a
	// session.ValueQuestion. Entered automatically -- see screenConfirm.
	// Filled in by task 631.
	screenValuePrompt

	// screenDetail is the full-text reading view for Session.Detail().
	// Entered automatically whenever Session.Detail() is non-nil -- see
	// deriveScreen. Filled in by task 731.
	screenDetail

	// screenSettings is the backend editor, reached explicitly from Home
	// (never derived from Session state the way Confirm/ValuePrompt/Detail
	// are -- there is nothing in Session itself that means "edit a
	// backend"). Filled in by task 931.
	screenSettings
)

// String names a screen for logs and test failure messages -- mirrors
// nav.DocumentState.String's reasoning for existing at all.
func (s screen) String() string {
	switch s {
	case screenHome:
		return "Home"
	case screenDocument:
		return "Document"
	case screenConfirm:
		return "Confirm"
	case screenValuePrompt:
		return "Value prompt"
	case screenDetail:
		return "Detail"
	case screenSettings:
		return "Settings"
	default:
		return "Unknown"
	}
}
