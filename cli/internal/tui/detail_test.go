package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// detailFixture builds a Model wrapping a root document with one property
// long enough that its row's Sublabel would truncate on screen -- exactly
// the case render.DetailTarget exists for (see render/rows.go's
// propertyRows, which sets Target on every property row regardless of
// length). Mirrors questionFixture (confirm_test.go) in shape: a fakeClient
// wired through session.New, wrapped in a Model, with the backend already
// open so Session has a document to activate a row on.
func detailFixture() Model {
	client := &fakeClient{routes: map[string]map[string]any{
		testBaseURL: {
			"class": []any{"root"},
			"title": "Root",
			"properties": map[string]any{
				"description": strings.Repeat("line\n", 20) + "the end",
			},
		},
	}}
	sess := session.New(nav.New(client), action.New(client), live.New(client))
	m := New(sess)
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.base = screenDocument
	m.document = m.document.syncRows(m.session.Document())
	return m
}

// --- detailModel.syncFromSession -------------------------------------------

func TestDetailSyncFromSessionSetsContentOnFirstAppearance(t *testing.T) {
	d := newDetailModel().resize(80, 24)
	detail := &session.DetailView{Heading: "description", Body: "the full value"}

	d = d.syncFromSession(detail)

	if !strings.Contains(d.viewport.View(), "the full value") {
		t.Errorf("viewport.View() = %q, want it to contain the body", d.viewport.View())
	}
}

// TestDetailSyncFromSessionPreservesScrollPositionOnUnchangedRedraw pins
// syncFromSession's whole reason for existing: an unrelated redraw (a
// resize, toggling help) re-delivers the very same *session.DetailView
// pointer, and must not reset scroll back to the top -- mirrors
// valuePromptModel.syncFromSession preserving typed text for the same
// reason.
func TestDetailSyncFromSessionPreservesScrollPositionOnUnchangedRedraw(t *testing.T) {
	detail := &session.DetailView{Heading: "h", Body: strings.Repeat("line\n", 40)}
	d := newDetailModel().resize(80, 5)
	d = d.syncFromSession(detail)
	d.viewport.LineDown(3)
	offset := d.viewport.YOffset

	d = d.syncFromSession(detail)

	if d.viewport.YOffset != offset {
		t.Errorf("YOffset = %d, want %d preserved across an unchanged resync", d.viewport.YOffset, offset)
	}
}

// TestDetailSyncFromSessionResetsScrollWhenDetailChanges checks that a new
// DetailView -- even one arriving right after the previous one, as a fresh
// property row activation always does since Session.Activate allocates a
// new *DetailView every time (session.go) -- always starts back at the top,
// never inheriting the previous reading's scroll position.
func TestDetailSyncFromSessionResetsScrollWhenDetailChanges(t *testing.T) {
	first := &session.DetailView{Heading: "a", Body: strings.Repeat("line\n", 40)}
	d := newDetailModel().resize(80, 5)
	d = d.syncFromSession(first)
	d.viewport.LineDown(3)
	if d.viewport.YOffset == 0 {
		t.Fatal("test setup: expected LineDown to move YOffset off 0")
	}

	second := &session.DetailView{Heading: "b", Body: strings.Repeat("line\n", 40)}
	d = d.syncFromSession(second)

	if d.viewport.YOffset != 0 {
		t.Errorf("YOffset = %d, want 0 after a new DetailView replaced the previous one", d.viewport.YOffset)
	}
}

// TestDetailSyncFromSessionClearsOnDismiss checks the nil transition
// (Session.DismissDetail) is handled without panicking and forgets what was
// shown, so a later DetailView with the same pointer value could never
// happen anyway -- but the transition itself must be a safe no-op on the
// viewport's own content.
func TestDetailSyncFromSessionClearsOnDismiss(t *testing.T) {
	detail := &session.DetailView{Heading: "h", Body: "body"}
	d := newDetailModel().resize(80, 24)
	d = d.syncFromSession(detail)

	d = d.syncFromSession(nil)

	if d.shown != nil {
		t.Error("shown was not cleared by syncFromSession(nil)")
	}
}

// --- View -------------------------------------------------------------------

func TestDetailViewShowsHeadingAndBody(t *testing.T) {
	d := newDetailModel().resize(80, 24)
	d = d.syncFromSession(&session.DetailView{Heading: "description", Body: "the full value"})

	view := d.View(session.DetailView{Heading: "description", Body: "the full value"})

	if !strings.Contains(view, "description") {
		t.Errorf("View() = %q, want it to contain the heading", view)
	}
	if !strings.Contains(view, "the full value") {
		t.Errorf("View() = %q, want it to contain the body", view)
	}
}

// --- updateDetail -------------------------------------------------------

// TestUpdateDetailForwardsScrollKeysToViewport checks a scroll key reaches
// bubbles/viewport.Model's own scrolling behaviour once it does get here --
// mirrors TestUpdateValuePromptForwardsOtherKeysToInput's same check for
// ValuePrompt's text input.
func TestUpdateDetailForwardsScrollKeysToViewport(t *testing.T) {
	m := detailFixture()
	m.detail = m.detail.resize(80, 5)
	m.detail = m.detail.syncFromSession(&session.DetailView{Heading: "h", Body: strings.Repeat("line\n", 40)})

	next, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	nm := next.(Model)

	if nm.detail.viewport.YOffset == 0 {
		t.Error("updateDetail did not forward 'j' to the viewport's own scroll-down binding")
	}
}

// --- integration: a row press reaches Detail, Back dismisses it ------------

// TestModelIntegrationActivatePropertyRowOpensDetail exercises the whole
// path a real keypress takes: pressing Enter on a property row activates it
// through activateDocumentSelection (document_update.go), landing on a
// render.DetailTarget Session.Activate turns straight into Session.Detail()
// -- and the resulting sessionUpdatedMsg is enough for deriveScreen to
// switch the shell to Detail. Mirrors
// TestModelIntegrationActivateActionRowRaisesConfirm (confirm_test.go) for
// an action row.
func TestModelIntegrationActivatePropertyRowOpensDetail(t *testing.T) {
	m := detailFixture()
	if len(m.document.rows.Items()) != 1 {
		t.Fatalf("test setup: rows.Items() len = %d, want 1", len(m.document.rows.Items()))
	}

	next, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.currentScreen() != screenDetail {
		t.Fatalf("currentScreen() = %v, want screenDetail after activating the property row", m.currentScreen())
	}
	if m.session.Detail() == nil {
		t.Fatal("Session.Detail() is nil after activating a render.DetailTarget row")
	}
	view := m.currentScreenView()
	if !strings.Contains(view, "description") {
		t.Errorf("currentScreenView() = %q, want it to contain the property's heading", view)
	}
}

// TestModelIntegrationBackDismissesDetailReturnsToDocument checks the
// shell-level dismissal path: the global Esc/backspace key, routed through
// Model.handleBack, calls Session.DismissDetail and falls back to the
// Document screen underneath -- mirrors detail_screen.dart's DetailViewHost
// popping its route once SessionService.detail goes back to null.
func TestModelIntegrationBackDismissesDetailReturnsToDocument(t *testing.T) {
	m := detailFixture()
	m.session.Activate(render.DetailTarget{Heading: "description", Body: "the full value"})
	m.document = m.document.syncRows(m.session.Document())
	m.detail = m.detail.syncFromSession(m.session.Detail())
	if m.currentScreen() != screenDetail {
		t.Fatalf("test setup: currentScreen() = %v, want screenDetail", m.currentScreen())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Error("dismissing Detail via the global Back key should not need a tea.Cmd")
	}
	nm := next.(Model)

	if nm.session.Detail() != nil {
		t.Error("esc did not dismiss the open Detail")
	}
	if nm.currentScreen() != screenDocument {
		t.Errorf("currentScreen() = %v, want screenDocument once Detail is dismissed", nm.currentScreen())
	}
}

// TestModelIntegrationBackResyncsDetailModelNotJustSession is j31's
// regression test: it was added when Model.handleKey's global-Back branch
// was found re-implementing resyncDocumentScreen's three-line body inline
// instead of calling it (SOLID audit finding j31), which left the two
// "resync all overlays" bodies free to drift out of sync by hand. Session
// state alone (checked by
// TestModelIntegrationBackDismissesDetailReturnsToDocument above) cannot
// catch that drift, since Session.Detail() going nil is true regardless of
// whether m.detail itself was ever told about it -- so this asserts
// directly on detailModel.shown, the field syncFromSession(nil) clears,
// which would stay stale (pointing at the just-dismissed DetailView) if a
// future overlay resync were dropped from one of the two copies again.
func TestModelIntegrationBackResyncsDetailModelNotJustSession(t *testing.T) {
	m := detailFixture()
	m.session.Activate(render.DetailTarget{Heading: "description", Body: "the full value"})
	m.document = m.document.syncRows(m.session.Document())
	m.detail = m.detail.syncFromSession(m.session.Detail())
	if m.detail.shown == nil {
		t.Fatal("test setup: expected detailModel.shown to be set after syncFromSession with an open Detail")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	nm := next.(Model)

	if nm.detail.shown != nil {
		t.Error("the global Back key dismissed Session.Detail() but left detailModel.shown pointing at the stale DetailView -- handleKey's Back branch must resync detailModel, not just Session")
	}
}
