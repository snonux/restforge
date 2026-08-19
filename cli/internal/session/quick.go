package session

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/quick"
	"github.com/snonux/restforge/cli/internal/render"
)

// SaveQuick saves row as a shortcut, or explains why it cannot be one -- see
// QuickSaveOutcome. Mirrors saveQuick() in session.js: what is stored is
// what the server offered, not a URL of this app's own -- an action by name
// plus Href (the document's address, never the action's own -- see
// internal/quick's package comment), a link by the href it carried. A
// property (render.DetailTarget) opens a reading view, not a place to return
// to, and an already-embedded sub-entity (render.EmbeddedTarget) has no
// address of its own either -- both are refused rather than saved as
// something that would not resolve to anything next time.
//
// The error return is non-nil only when quick.Add's own storage write
// failed (a full disk, or the config path could not be resolved); a full
// shortcut list or an unsaveable row is a real, reportable outcome returned
// as the QuickSaveOutcome value with a nil error, never silently dropped.
func (s *Session) SaveQuick(row render.Row) (QuickSaveOutcome, error) {
	be := s.nav.Backend()
	if be.BaseURL == "" {
		// Nothing is open: there is no backend to remember the shortcut
		// against. Mirrors session.dart's `if (backend == null)` guard.
		return QuickSaveNotSaveable, nil
	}

	switch t := row.Target.(type) {
	case render.ActionTarget:
		holder := s.nav.Href()
		if holder == "" {
			// The document offering this action arrived embedded, not linked
			// -- nowhere to look the action up again next time.
			return QuickSaveNotSaveable, nil
		}
		item := quick.QuickItem{
			Label:       row.Label,
			BackendName: be.Name,
			BaseURL:     be.BaseURL,
			Kind:        quick.KindAction,
			Holder:      holder,
			Name:        t.Name,
		}
		return s.saveQuickItem(item)
	case render.FetchTarget:
		item := quick.QuickItem{
			Label:       row.Label,
			BackendName: be.Name,
			BaseURL:     be.BaseURL,
			Kind:        quick.KindDocument,
			Href:        t.Href,
		}
		return s.saveQuickItem(item)
	case render.DetailTarget, render.EmbeddedTarget:
		return QuickSaveNotSaveable, nil
	default:
		panic(fmt.Sprintf("session: unreachable RowTarget type %T", row.Target))
	}
}

// saveQuickItem stores item through quick.Add and maps the result onto a
// QuickSaveOutcome. Split out of SaveQuick so each target branch stays the
// shape of "build the item, then hand off", and so the saved/full decision
// has one home rather than being repeated per branch.
func (s *Session) saveQuickItem(item quick.QuickItem) (QuickSaveOutcome, error) {
	saved, err := quick.Add(item)
	if err != nil {
		return 0, err
	}
	if saved == nil {
		// quick.Add refuses (rather than errors) when the item is incomplete
		// or the list is already at quick.MaxQuick. SaveQuick has already
		// built a complete item by the time it gets here, so a nil result
		// means the cap was reached.
		return QuickSaveFull, nil
	}
	return QuickSaveSaved, nil
}

// RunQuick follows a saved shortcut -- see QuickRunOutcome. Mirrors
// runQuick() in session.js: adopt the backend the shortcut points at (never
// fetch its root first -- nav.Adopt's doc comment), then either fetch the
// saved address (a document shortcut) or fetch the holder document and look
// the action up by name in whatever comes back (an action shortcut) -- the
// exact same askAction a hand-pressed render.ActionTarget goes through, so a
// withdrawn action is reported as ActionWithdrawn and a confirmable one
// still asks, precisely as if this had been walked to by hand rather than
// jumped to.
//
// The error return is non-nil only when quick.BackendFor's own config read
// failed; a shortcut whose backend has since been deleted resolves to nil
// with a nil error and is reported as QuickRunBackendMissing.
func (s *Session) RunQuick(item quick.QuickItem) (QuickRunOutcome, error) {
	be, err := quick.BackendFor(item)
	if err != nil {
		return 0, err
	}
	if be == nil {
		return QuickRunBackendMissing, nil
	}

	s.live.Stop()
	s.clearTransient()
	s.nav.Adopt(*be)

	if item.Kind == quick.KindDocument {
		s.nav.Fetch(item.Href, item.Label)
		return QuickRunOpened, nil
	}

	s.nav.Fetch(item.Holder, item.Label)
	if s.nav.State() == nav.StateOK {
		// Only ask if the holder itself was actually fetched -- a failed
		// fetch already left state/failure set for the caller to render;
		// asking about an action on a document that never arrived would be
		// inventing a document nobody sent (mirrors openQuickHolder's error
		// branch in nav.js, which never calls its then callback).
		s.askAction(item.Name)
	}
	return QuickRunOpened, nil
}
