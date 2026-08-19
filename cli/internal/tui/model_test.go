package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// fakeClient is a route-table test double satisfying nav's httpGetter,
// action's requester and live's httpGetter seams at once -- all three are
// structural interfaces over a *httpclient.Client (see each package's own
// httpGetter/requester doc comment), so one small fake standing in for the
// client covers everything session.New needs. These tests only drive
// navigation (OpenBackend, Activate on a FetchTarget, Back), never an
// action, so Request is never actually exercised -- it exists only to
// satisfy action.New's parameter type.
type fakeClient struct {
	routes map[string]map[string]any
}

func (f *fakeClient) Get(be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	doc, ok := f.routes[href]
	if !ok {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Client, Message: "fakeClient: no route for " + href}
	}
	return httpclient.HTTPResponse{Status: 200, URL: href, Entity: doc}, nil
}

func (f *fakeClient) Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
	return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Client, Message: "fakeClient: Request not used by these tests"}
}

const testBaseURL = "https://example.test/"

// newTestModel builds a Model wrapping a real *session.Session on top of
// fakeClient, with a root document and one linked child so tests can push
// a second frame onto Session's navigation stack (CanGoBack) without a
// real backend.
func newTestModel() (Model, *fakeClient) {
	client := &fakeClient{routes: map[string]map[string]any{
		testBaseURL: {
			"class": []any{"root"},
			"title": "Root",
			"links": []any{
				map[string]any{"rel": []any{"self"}, "href": testBaseURL},
			},
		},
		"/child": {
			"class": []any{"child"},
			"title": "Child",
			"links": []any{
				map[string]any{"rel": []any{"self"}, "href": "/child"},
			},
		},
	}}
	sess := session.New(nav.New(client), action.New(client), live.New(client))
	return New(sess), client
}

func TestModelUpdateQuit(t *testing.T) {
	m, _ := newTestModel()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	nm := next.(Model)

	if !nm.quitting {
		t.Error("Update did not set quitting on 'q'")
	}
	if cmd == nil {
		t.Fatal("Update returned a nil tea.Cmd for 'q', want tea.Quit")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("Update's cmd() = %#v, want tea.Quit()", msg)
	}
}

func TestModelUpdateHelpToggles(t *testing.T) {
	m, _ := newTestModel()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	nm := next.(Model)
	if !nm.showHelp {
		t.Fatal("Update did not set showHelp on '?'")
	}

	next, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	nm = next.(Model)
	if nm.showHelp {
		t.Error("a second '?' did not toggle showHelp back off")
	}
}

// TestModelUpdateBackPopsWithoutIO opens the root, then pushes a child via
// Activate(FetchTarget), then checks the global back key pops the stack --
// through Session.Back, never through sessionCmd -- matching handleBack's
// own doc comment that Back performs no I/O (nav.Nav.Back only pops an
// already-fetched frame).
func TestModelUpdateBackPopsWithoutIO(t *testing.T) {
	m, _ := newTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.session.Activate(render.FetchTarget{Href: "/child"})

	if !m.session.CanGoBack() {
		t.Fatal("test setup: expected CanGoBack after opening a child document")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	nm := next.(Model)

	if cmd != nil {
		t.Error("the back key returned a non-nil tea.Cmd; Back performs no I/O and should not need one")
	}
	if nm.session.CanGoBack() {
		t.Error("the back key did not pop Session's navigation stack")
	}
}

// TestModelUpdateBackAtRootReturnsHome checks the fallback branch of
// handleBack: nothing to pop and no Detail open goes to Home.
func TestModelUpdateBackAtRootReturnsHome(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	nm := next.(Model)

	if nm.currentScreen() != screenHome {
		t.Errorf("currentScreen() = %v, want screenHome", nm.currentScreen())
	}
}
