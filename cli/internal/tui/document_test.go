package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// newLinkedTestModel builds a Model like newTestModel (cmd_test.go), but
// with the root document linking to /child by an ordinary rel rather than
// "self", so a row-activation test can tell "still on the same document"
// apart from "navigated to a different one" by title alone -- newTestModel's
// own root only ever links back to itself, which cmd_test.go's tests reach
// past by calling Session.Activate directly rather than through a row.
func newLinkedTestModel() Model {
	client := &fakeClient{routes: map[string]map[string]any{
		testBaseURL: {
			"class": []any{"root"},
			"title": "Root",
			"links": []any{
				map[string]any{"rel": []any{"child"}, "href": "/child"},
			},
		},
		"/child": {
			"class": []any{"child"},
			"title": "Child",
		},
	}}
	sess := session.New(nav.New(client), action.New(client), live.New(client))
	return New(sess)
}

// fakeDocumentSource is documentModel's test double for documentSource --
// see that interface's own doc comment for why documentModel.View takes it
// rather than *session.Session directly.
type fakeDocumentSource struct {
	doc     *render.RenderedDocument
	state   nav.DocumentState
	failure *failure.Failure
}

func (f fakeDocumentSource) Document() *render.RenderedDocument { return f.doc }
func (f fakeDocumentSource) State() nav.DocumentState           { return f.state }
func (f fakeDocumentSource) Failure() *failure.Failure          { return f.failure }

func twoRowDoc() *render.RenderedDocument {
	return &render.RenderedDocument{
		Title: "Root",
		Rows: []render.Row{
			{Label: "a", Kind: render.RowKindProperty},
			{Label: "b", Kind: render.RowKindLink},
		},
	}
}

// --- syncRows ---------------------------------------------------------

func TestDocumentSyncRowsBuildsItemsFromDocument(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	if len(d.rows.Items()) != 2 {
		t.Errorf("rows.Items() len = %d, want 2", len(d.rows.Items()))
	}
}

func TestDocumentSyncRowsHandlesNilDocument(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	d = d.syncRows(nil)

	if len(d.rows.Items()) != 0 {
		t.Errorf("rows.Items() len = %d, want 0 after syncRows(nil)", len(d.rows.Items()))
	}
}

// TestDocumentSyncRowsPreservesCursorWhenUnchanged pins syncRows' whole
// reason for existing: Session.Document rebuilds a brand new
// *render.RenderedDocument on every call (nav.Nav.Document's own doc
// comment), so a fresh pointer with identical content must not reset the
// list's cursor -- otherwise every unrelated redraw (a resize, toggling
// help) would snap the cursor back to the top.
func TestDocumentSyncRowsPreservesCursorWhenUnchanged(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	d.rows.Select(1)

	d = d.syncRows(twoRowDoc()) // a different pointer, identical content

	if d.rows.Index() != 1 {
		t.Errorf("Index() = %d, want 1 (an unchanged resync must not move the cursor)", d.rows.Index())
	}
}

func TestDocumentSyncRowsResetsCursorWhenDocumentChanges(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	d.rows.Select(1)

	changed := &render.RenderedDocument{
		Title: "Child",
		Rows:  []render.Row{{Label: "c", Kind: render.RowKindEntity}},
	}
	d = d.syncRows(changed)

	if d.rows.Index() != 0 {
		t.Errorf("Index() = %d, want 0 after navigating to a different document", d.rows.Index())
	}
	if len(d.rows.Items()) != 1 {
		t.Errorf("rows.Items() len = %d, want 1", len(d.rows.Items()))
	}
}

func TestDocumentSyncRowsClearsDismissedFailureOnNavigation(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	f := &failure.Failure{Kind: failure.Server, Message: "boom"}
	d.dismissedFailure = f

	changed := &render.RenderedDocument{Title: "Child"}
	d = d.syncRows(changed)

	if d.dismissedFailure != nil {
		t.Error("dismissedFailure was not cleared by navigating to a different document")
	}
}

// --- View ---------------------------------------------------------------

func TestDocumentViewShowsTitleAndRows(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateOK})

	if !strings.Contains(view, "Root") {
		t.Errorf("View() = %q, want it to contain the document title", view)
	}
	if !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Errorf("View() = %q, want it to contain both row labels", view)
	}
}

func TestDocumentViewShowsLoadingLineWithoutDroppingRows(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateLoading})

	if !strings.Contains(view, "Loading") {
		t.Errorf("View() = %q, want a loading indicator while State is StateLoading", view)
	}
	if !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Errorf("View() = %q, want the last-good rows to stay on screen while loading", view)
	}
}

// TestDocumentViewKeepsRowsOnFailure pins the core invariant this task's
// own description names: a failed refresh overlays a banner on top of the
// last-good document, it does not replace the rows with the failure.
func TestDocumentViewKeepsRowsOnFailure(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	f := &failure.Failure{Kind: failure.Server, Message: "server exploded"}

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateError, failure: f})

	if !strings.Contains(view, "server exploded") {
		t.Errorf("View() = %q, want the failure message in a banner", view)
	}
	if !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Errorf("View() = %q, want the previous rows to stay on screen despite the failure", view)
	}
}

func TestDocumentViewUnreachableWordsDifferentlyFromOtherFailures(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	f := &failure.Failure{Kind: failure.Unreachable, Message: "connection refused"}

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateUnreachable, failure: f})

	if !strings.Contains(view, "Unreachable") {
		t.Errorf("View() = %q, want the unreachable wording, not the generic failed-request one", view)
	}
}

func TestDocumentViewEmptyRowsShowsNote(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	empty := &render.RenderedDocument{Title: "Empty"}
	d = d.syncRows(empty)

	view := d.View(fakeDocumentSource{doc: empty, state: nav.StateOK})

	if !strings.Contains(view, "nothing to show") {
		t.Errorf("View() = %q, want a note that this document has no rows", view)
	}
}

func TestDocumentViewDismissedFailureIsHidden(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())
	f := &failure.Failure{Kind: failure.Server, Message: "server exploded"}
	d.dismissedFailure = f

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateError, failure: f})

	if strings.Contains(view, "server exploded") {
		t.Errorf("View() = %q, want a dismissed failure to no longer show its banner", view)
	}
}

// --- emptyView ------------------------------------------------------------

func TestDocumentEmptyViewBeforeAnyFetch(t *testing.T) {
	d := newDocumentModel().resize(80, 24)

	view := d.View(fakeDocumentSource{state: nav.StateOK})

	if !strings.Contains(view, "Nothing to show yet") {
		t.Errorf("View() = %q, want the before-first-fetch note", view)
	}
}

func TestDocumentEmptyViewFullScreenFailureWithNoDocument(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	f := &failure.Failure{Kind: failure.Server, Message: "root fetch failed"}

	view := d.View(fakeDocumentSource{state: nav.StateError, failure: f})

	if !strings.Contains(view, "root fetch failed") {
		t.Errorf("View() = %q, want the failure message on a full-screen failure", view)
	}
}

func TestDocumentEmptyViewLoading(t *testing.T) {
	d := newDocumentModel().resize(80, 24)

	view := d.View(fakeDocumentSource{state: nav.StateLoading})

	if !strings.Contains(view, "Loading") {
		t.Errorf("View() = %q, want a loading indicator before anything has been fetched", view)
	}
}

// --- updateDocument / activateDocumentSelection ----------------------------

// TestActivateDocumentSelectionCallsSessionActivate drives the whole path
// through Model: open a backend, land on Document, press Enter on the
// selected row, and check the cmd it returns actually calls
// Session.Activate with that row's target -- following a render.FetchTarget
// to the linked child, exactly the way a link row's Enter press is meant
// to behave. Running the cmd is deferred to Bubble Tea's own goroutine in
// production (see cmd.go's doc comment); calling it directly here is enough
// to check the contract without a running Program.
func TestActivateDocumentSelectionCallsSessionActivate(t *testing.T) {
	m := newLinkedTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.base = screenDocument
	m.document = m.document.syncRows(m.session.Document())

	// The root document's rows: propertyRows/entityRows/linkRows/actionRows
	// in Siren order -- the root fixture above has a single "child" link,
	// so it is the first (and only) row.
	if len(m.document.rows.Items()) != 1 {
		t.Fatalf("test setup: rows.Items() len = %d, want 1", len(m.document.rows.Items()))
	}
	m.document.rows.Select(0)

	next, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("updateDocument(Enter) returned a nil cmd, want sessionCmd wrapping Session.Activate")
	}
	msg := cmd()
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Fatalf("cmd() = %T, want sessionUpdatedMsg", msg)
	}
	nm := next.(Model)
	if nm.session.Document().Title != "Child" {
		t.Errorf("session.Document().Title = %q, want %q after Activate follows the link", nm.session.Document().Title, "Child")
	}
}

// TestUpdateDocumentEnterNoopWhenNothingSelected checks the empty-document
// guard in activateDocumentSelection.
func TestUpdateDocumentEnterNoopWhenNothingSelected(t *testing.T) {
	m, _ := newTestModel()

	_, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Error("updateDocument(Enter) returned a non-nil cmd with no row selected")
	}
}

// TestUpdateDocumentDismissClearsFailureBanner checks the 'd' key this port
// adds beyond document_banners.dart's own _FailureBanner (see
// documentModel.dismissedFailure's doc comment).
func TestUpdateDocumentDismissClearsFailureBanner(t *testing.T) {
	m, _ := newTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: "https://unknown.invalid/"})
	if m.session.Failure() == nil {
		t.Fatal("test setup: expected OpenBackend against an unrouted URL to fail")
	}

	next, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd != nil {
		t.Error("dismissing the failure banner should not need a tea.Cmd")
	}
	nm := next.(Model)
	if nm.document.dismissedFailure != nm.session.Failure() {
		t.Error("'d' did not record the current failure as dismissed")
	}
}

// TestModelIntegrationOpenBackendThenActivateLink exercises the whole
// wiring through Model.Update end to end, the way the real tea.Program
// loop drives it: opening a backend and delivering homeOpenedMsg switches
// to Document with the root's rows synced, and activating the link row
// (Enter, then delivering the resulting sessionUpdatedMsg) navigates to the
// child, with the row list resynced to the child's own rows.
func TestModelIntegrationOpenBackendThenActivateLink(t *testing.T) {
	m := newLinkedTestModel()

	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	next, _ := m.Update(homeOpenedMsg{})
	m = next.(Model)

	if m.currentScreen() != screenDocument {
		t.Fatalf("currentScreen() = %v, want screenDocument after homeOpenedMsg", m.currentScreen())
	}
	if len(m.document.rows.Items()) != 1 {
		t.Fatalf("document rows were not synced after homeOpenedMsg: len = %d, want 1", len(m.document.rows.Items()))
	}

	m.document.rows.Select(0)
	next, cmd := m.updateDocument(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	view := m.currentScreenView()
	if !strings.Contains(view, "Child") {
		t.Errorf("currentScreenView() = %q, want the child document's title after following its link", view)
	}
}
