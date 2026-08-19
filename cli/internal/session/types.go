package session

import "github.com/snonux/restforge/cli/internal/failure"

// DetailView is a value opened for full reading -- the result of
// Session.Activate on a render.DetailTarget row. Carries the value itself,
// not just "open something": render.DetailTarget already carries the full
// text, so there is nothing further to look up. Mirrors DetailView in
// session.dart.
type DetailView struct {
	Heading string
	Body    string
}

// SessionQuestion is what the user is currently being asked, on top of
// whatever Session.Document shows -- the rendered form of an
// internal/action AskOutcome/InvokeOutcome a screen can put on screen
// without ever seeing the href or method behind it (see internal/action's
// package comment on why those never leave that package). A caller type
// switches over this exhaustively, the same convention every other closed
// interface in this codebase follows -- see render.RowTarget's doc comment
// on the default-panics-as-canary rule every such switch must carry.
//
// This is Go's nearest equivalent to the sealed SessionQuestion hierarchy in
// session.dart: an unexported marker method closes the interface to the two
// types in this file, so nothing outside this package can add a third.
type SessionQuestion interface {
	isSessionQuestion()
}

// ConfirmQuestion is a yes/no confirmation before an unsafe action is sent.
// Mirrors ConfirmationRequired. Answered with Session.Answer.
type ConfirmQuestion struct {
	Heading string
	Body    string
}

func (ConfirmQuestion) isSessionQuestion() {}

// ValueQuestion is a value asked for out loud, for a required field with no
// default and no confirmation to stand in for it (docs/DESIGN.md, "Do not
// invent a value"). Mirrors InvokeNeedsValue. Answered with
// Session.AnswerValue.
type ValueQuestion struct {
	Label string
}

func (ValueQuestion) isSessionQuestion() {}

// SessionNotice is a one-shot report of what the last action -- or the job
// it started -- produced, laid over Session.Document until the next
// navigation clears it. See the package comment on why this lives here
// rather than in internal/nav. Closed the same way SessionQuestion is.
//
// Each notice carries the action's own label as Heading, threaded through
// from Activate asking a question to Answer/AnswerValue resolving it --
// mirrors siren.label(action) being threaded through invoke() in
// actions.js -- because by the time a question is answered, the
// SessionQuestion on screen may be a ValueQuestion carrying a field's label
// instead.
type SessionNotice interface {
	isSessionNotice()
}

// ActionWithdrawn means the server no longer offers the action a row
// promised. A real answer -- it cannot be done right now -- not a bug to
// route around by inventing a request. Mirrors the withdrawn-action branch
// of askAction() in actions.js. Heading is the action's name: the server
// never offered it, so there is no siren.Action.Label to prefer over it.
type ActionWithdrawn struct {
	Heading string
}

func (ActionWithdrawn) isSessionNotice() {}

// ActionRefused means Session.Answer/Session.AnswerValue found a problem
// internal/action would rather not guess around: more than one required
// field with nothing to fill it, or a value asked for out loud that came
// back empty. Mirrors InvokeRefused.
type ActionRefused struct {
	Heading string
	Reason  string
}

func (ActionRefused) isSessionNotice() {}

// ActionOutcomeReported means the action was sent and answered, or the job
// it started has finished. Message is the server's own word for the outcome
// (a "state" property, or "Accepted"/"Done" when it did not send one); Body
// is the full response rendered generically. Mirrors resultBanner/
// describeResult in actions.js, reused for a live job's onDone the same way
// that file does.
type ActionOutcomeReported struct {
	Heading string
	Message string
	Body    string
}

func (ActionOutcomeReported) isSessionNotice() {}

// ActionProgress is a step reported while a job is still being watched.
// Mirrors onProgress in actions.js's liveHandlers -- simpler than
// ActionOutcomeReported on purpose: a step is a running commentary, not a
// final account.
type ActionProgress struct {
	Heading string
	Step    string
}

func (ActionProgress) isSessionNotice() {}

// ActionGaveUp means watching a job was abandoned before the server ever
// said it was done -- not a claim the job failed, only that this app stopped
// asking. Mirrors onGiveUp. The document is re-fetched anyway (see
// Session.onLiveGiveUp): whatever the action changed before this app gave up
// watching is still worth seeing.
type ActionGaveUp struct {
	Heading string
}

func (ActionGaveUp) isSessionNotice() {}

// ActionFailed means the action, or the one retry internal/action allows,
// failed. Mirrors afterActionError in actions.js. Whether the document was
// re-fetched after this is not part of the notice itself -- see
// Session.handleFailure for the conflict-only rule docs/DESIGN.md requires.
type ActionFailed struct {
	Heading string
	Failure *failure.Failure
}

func (ActionFailed) isSessionNotice() {}

// QuickSaveOutcome is what Session.SaveQuick did with a pressed row. Not a
// SessionNotice: the row being saved is already on screen, so the caller
// reports the outcome directly rather than laying something over
// Session.Document that the very next navigation would clear before anyone
// read it. Mirrors the three outcomes saveQuick() sends as a frame message
// in session.js, collapsed to one enum since this port has a typed return
// value to switch on instead of a string to read.
type QuickSaveOutcome int

const (
	// QuickSaveSaved: stored -- new, or replacing an identical existing
	// shortcut (see quick.Add's idempotence).
	QuickSaveSaved QuickSaveOutcome = iota

	// QuickSaveFull: quick.MaxQuick shortcuts are already stored. Must be
	// surfaced, never silently dropped.
	QuickSaveFull

	// QuickSaveNotSaveable: the row cannot be a shortcut at all -- a
	// property or an already-embedded sub-entity has nothing to look up or
	// re-fetch later (render.DetailTarget/render.EmbeddedTarget), or the
	// row was an action on a document that has no address of its own to
	// remember as the holder (an embedded document -- Session.Href is
	// empty).
	QuickSaveNotSaveable
)

// QuickRunOutcome is what Session.RunQuick did with a saved shortcut. Also
// not a SessionNotice, and for the same reason QuickSaveOutcome is not one:
// this is called from wherever shortcuts are listed, before any
// Session.Document exists to lay a notice over. Mirrors the two outcomes
// runQuick() in session.js can produce before it ever gets as far as
// fetching anything.
type QuickRunOutcome int

const (
	// QuickRunOpened: the backend was resolved and adopted, and the fetch
	// that follows -- render.FetchTarget's href for a document shortcut,
	// quick.QuickItem.Holder for an action one -- is already under way or
	// has already landed (or failed; see nav.Adopt's doc comment on why a
	// shortcut still navigates on a failed fetch rather than reporting
	// nothing at all). The caller should now show Session on screen --
	// whatever it has to show, including a failure or an ActionWithdrawn
	// notice, belongs there, not on the screen the shortcut was pressed
	// from.
	QuickRunOpened QuickRunOutcome = iota

	// QuickRunBackendMissing: quick.QuickItem.BaseURL no longer matches a
	// configured backend. Nothing was adopted or fetched; report this on
	// the screen the shortcut was pressed from -- mirrors session.js's
	// runQuick showing an overlay on the picker frame rather than adopting
	// one, since there is nothing to show past this point.
	QuickRunBackendMissing
)
