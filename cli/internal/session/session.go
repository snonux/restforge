package session

import (
	"fmt"
	"sync"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// Session composes nav.Nav, action.Action and live.Live into the single
// coordinator a caller talks to -- see the package comment for what belongs
// here and why. internal/quick is composed too, but as package-level
// functions rather than a held instance: quick has no state of its own (its
// storage lives in internal/config), so there is nothing to inject or hold.
//
// nav, actions and live are injected (a test substitutes any of them, built
// on fakes), exactly as session.dart injects NavService/ActionService/
// LiveService. A caller that needs the blocking live watch for the one-shot
// CLI keeps a reference to the *live.Live it passes here and calls
// live.WaitForLive on it -- see the package comment.
type Session struct {
	nav     *nav.Nav
	actions *action.Action
	live    *live.Live

	// mu guards detail/question/notice/pendingActionLabel below -- the
	// overlay state this package owns itself, as opposed to nav's own
	// State/Document/Failure (which nav.Nav's own mutex already guards) or
	// live's IsLive (which live.Live's own mutex already guards). It exists
	// for the same reason nav.Nav's mu does: a caller's cmd goroutine (see
	// internal/tui/cmd.go's sessionCmd) mutates these fields while that
	// caller's render goroutine may be reading them through Detail/
	// Question/Notice at the same time.
	mu sync.RWMutex

	detail   *DetailView
	question SessionQuestion
	notice   SessionNotice

	// pendingActionLabel is the action's own label, remembered across the
	// round trip from Activate asking a question to Answer/AnswerValue
	// resolving it -- mirrors siren.label(action) being threaded through
	// invoke() in actions.js. Needed because by the time a question is
	// answered, the SessionQuestion on screen may be a ValueQuestion
	// carrying a field's label instead.
	pendingActionLabel string
}

// New builds a Session composing the given nav, actions and live. Each is
// injected rather than constructed here so a test can substitute a fake for
// any of them, the same seam session.dart's constructor offers; production
// code builds the three on a shared *httpclient.Client and passes them in.
func New(n *nav.Nav, actions *action.Action, live *live.Live) *Session {
	return &Session{nav: n, actions: actions, live: live}
}

// --- forwarded nav state --------------------------------------------------

// Backend is the backend currently open. See nav.Nav.Backend.
func (s *Session) Backend() backend.Backend { return s.nav.Backend() }

// State is what is on screen right now. See nav.Nav.State.
func (s *Session) State() nav.DocumentState { return s.nav.State() }

// Failure is why State is not nav.StateOK. See nav.Nav.Failure.
func (s *Session) Failure() *failure.Failure { return s.nav.Failure() }

// Document is the document to render. See nav.Nav.Document.
func (s *Session) Document() *render.RenderedDocument { return s.nav.Document() }

// Href is the href Document can be re-fetched from, or "" for a sub-entity
// that arrived embedded rather than linked (or before anything is open).
// See nav.Nav.Href. Added for SaveQuick: an action row is saved as (this
// href, the action's name), never as the action's own href -- see
// internal/quick's package comment.
func (s *Session) Href() string { return s.nav.Href() }

// CanGoBack is true once there is a document below the one on screen. See
// nav.Nav.CanGoBack.
func (s *Session) CanGoBack() bool { return s.nav.CanGoBack() }

// --- state this package owns itself ---------------------------------------

// Detail is a value opened for full reading by Activate, or nil. See
// DismissDetail.
func (s *Session) Detail() *DetailView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.detail
}

// Question is the question currently awaiting Answer or AnswerValue, or
// nil.
func (s *Session) Question() SessionQuestion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.question
}

// Notice is what the last action (or the job it started) produced, or nil.
// See DismissNotice.
func (s *Session) Notice() SessionNotice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notice
}

// IsLive reports whether a job is currently being watched. See
// live.Live.IsLive.
func (s *Session) IsLive() bool { return s.live.IsLive() }

// --- navigation -----------------------------------------------------------

// OpenBackend opens be at its root, replacing whatever was open before.
// Mirrors nav.openBackend -- passed straight through in session.js, but this
// port also stops any live watch and clears whatever was laid over the
// previous backend's document, since neither has any business surviving a
// switch to a different server. See the package comment on clearTransient.
func (s *Session) OpenBackend(be backend.Backend) {
	s.live.Stop()
	s.clearTransient()
	s.nav.OpenRoot(be)
}

// Activate turns a pressed row back into what it means:
// render.FetchTarget and render.EmbeddedTarget go to nav, render.ActionTarget
// goes to action via askAction, and render.DetailTarget is handled right
// here -- see the package comment. Mirrors activate() in session.js.
func (s *Session) Activate(target render.RowTarget) {
	switch t := target.(type) {
	case render.DetailTarget:
		s.mu.Lock()
		s.detail = &DetailView{Heading: t.Heading, Body: t.Body}
		s.mu.Unlock()
	case render.FetchTarget:
		// Mirrors nav.js's fetch(href, title, false): navigating somewhere
		// new means whatever was being watched belonged to the screen being
		// left. t.Backend -- the backend this target's document was
		// rendered against, not whatever nav.Nav.Backend() happens to
		// return right now -- pins the fetch to the right server even under
		// the goroutine-scheduling unfairness p31 closes; see
		// render.FetchTarget's and nav.Nav.Fetch's own doc comments.
		s.live.Stop()
		s.clearTransient()
		s.nav.Fetch(t.Backend, t.Href, "")
	case render.EmbeddedTarget:
		// Mirrors nav.js's openEmbedded, which does not stop a live watch
		// -- opening a sub-entity already in hand is not "leaving" the
		// document the watch is tied to the way following a link is.
		s.clearTransient()
		s.nav.OpenEmbedded(t.Index)
	case render.ActionTarget:
		s.askAction(t.Name)
	default:
		panic(fmt.Sprintf("session: unreachable RowTarget type %T", target))
	}
}

// Back pops one document. Mirrors back() in session.js: abandons whatever
// action question was pending (leaving the screen it was offered on
// abandons the question with it) and stops any live watch, since both
// belonged to the document being left.
func (s *Session) Back() {
	s.live.Stop()
	s.clearTransient()
	s.nav.Back()
}

// Refresh re-fetches the document on screen. Mirrors session.js's
// refresh: nav.refresh -- deliberately does NOT stop a live watch or clear
// Notice/Detail: mirrors nav.js's own fetch(href, title, true) skipping
// live.stop() when replacing rather than pushing, since a manual refresh of
// the very document a watch is tied to is not "leaving" it.
func (s *Session) Refresh() {
	s.nav.Refresh()
}

// DismissDetail closes Detail without otherwise touching navigation.
// Nothing in session.js needs an equivalent -- the watch's reading window
// is dismissed by the watch itself, off that module entirely -- but a
// caller showing Detail as a modal needs an explicit way to close it that
// is not also a Back.
func (s *Session) DismissDetail() {
	s.mu.Lock()
	s.detail = nil
	s.mu.Unlock()
}

// DismissNotice clears Notice once a caller has shown it. See DismissDetail;
// the same reasoning applies.
func (s *Session) DismissNotice() {
	s.mu.Lock()
	s.notice = nil
	s.mu.Unlock()
}

// clearTransient clears whatever is laid on top of Document -- Detail,
// Question and Notice -- and abandons any question action.Action is still
// holding open. Called by every navigation event that changes which document
// is on screen -- see the package comment for why this port needs an
// explicit call where session.js's frame-per-navigation model did not, and
// for why this is deliberately never called from the refresh an action's own
// outcome triggers.
func (s *Session) clearTransient() {
	s.actions.CancelPending()
	s.mu.Lock()
	s.detail = nil
	s.question = nil
	s.notice = nil
	s.pendingActionLabel = ""
	s.mu.Unlock()
}

// currentEntity returns the document on screen as a non-nil value plus
// true, or the zero entity plus false when nothing is open yet. askAction
// and Answer/AnswerValue need both the entity (to look an action up by
// name) and a "nothing open" gate; nav.Entity already returns the pointer
// and nil, this just unpacks it to the value those methods pass on.
func (s *Session) currentEntity() (siren.Entity, bool) {
	e := s.nav.Entity()
	if e == nil {
		return siren.Entity{}, false
	}
	return *e, true
}
