package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/quick"
	"github.com/snonux/restforge/cli/internal/session"
)

// errLoad is a sentinel error standing in for a real load failure (e.g.
// config.Path's own UserConfigDir error) in the tests that only need to
// check applyLoaded/applyQuickRan report *something*, not any specific
// underlying error.
var errLoad = errors.New("boom")

// withHomeConfig points config.ConfigEnvVar at a fresh path inside
// t.TempDir(), seeded with backends -- the same isolation
// internal/config/store_test.go and internal/quick/quick_test.go use, so
// homeInitCmd's real config.LoadBackends/quick.Load calls never touch a
// real $HOME or $XDG_CONFIG_HOME.
func withHomeConfig(t *testing.T, backends []backend.Backend) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(config.ConfigEnvVar, path)
	if len(backends) > 0 {
		if err := config.SaveBackends(backends); err != nil {
			t.Fatalf("SaveBackends() error = %v", err)
		}
	}
}

func oneBackend() backend.Backend {
	return backend.Backend{Name: "alpha", BaseURL: testBaseURL, Secret: "k"}
}

// --- applyLoaded -----------------------------------------------------------

func TestHomeApplyLoadedFocusesBackendsWhenPresent(t *testing.T) {
	h := newHomeModel()
	h = h.applyLoaded(homeLoadedMsg{backends: []backend.Backend{oneBackend()}})

	if h.focus != homeFocusBackends {
		t.Errorf("focus = %v, want homeFocusBackends", h.focus)
	}
	if len(h.backends.Items()) != 1 {
		t.Errorf("backends.Items() len = %d, want 1", len(h.backends.Items()))
	}
}

func TestHomeApplyLoadedFocusesQuickWhenNoBackends(t *testing.T) {
	h := newHomeModel()
	row := homeQuickRow{item: quick.QuickItem{Label: "Root"}}
	h = h.applyLoaded(homeLoadedMsg{rows: []homeQuickRow{row}})

	if h.focus != homeFocusQuick {
		t.Errorf("focus = %v, want homeFocusQuick", h.focus)
	}
	if !h.hasShortcuts {
		t.Error("hasShortcuts = false, want true")
	}
}

func TestHomeApplyLoadedNoShortcutsWhenRowsEmpty(t *testing.T) {
	h := newHomeModel()
	h.hasShortcuts = true // prove applyLoaded resets this, not just leaves it
	h = h.applyLoaded(homeLoadedMsg{backends: []backend.Backend{oneBackend()}})

	if h.hasShortcuts {
		t.Error("hasShortcuts = true, want false for an empty rows slice")
	}
}

func TestHomeApplyLoadedReportsLoadError(t *testing.T) {
	h := newHomeModel()
	h = h.applyLoaded(homeLoadedMsg{err: errLoad})

	if h.notice == "" {
		t.Error("notice is empty, want a report of the load error")
	}
	if !strings.Contains(h.notice, "boom") {
		t.Errorf("notice = %q, want it to mention the underlying error", h.notice)
	}
}

// --- View --------------------------------------------------------------

func TestHomeViewShowsEmptyStateWhenNoBackends(t *testing.T) {
	h := newHomeModel()
	h = h.resize(80, 24)
	h = h.applyLoaded(homeLoadedMsg{})

	view := h.View()
	if !strings.Contains(view, "No backends configured") {
		t.Errorf("View() = %q, want it to contain the empty state", view)
	}
	if !strings.Contains(view, "restforge backends add") {
		t.Errorf("View() = %q, want it to point at the CLI command", view)
	}
}

func TestHomeViewShowsBackendRowWhenPresent(t *testing.T) {
	h := newHomeModel()
	h = h.resize(80, 24)
	h = h.applyLoaded(homeLoadedMsg{backends: []backend.Backend{oneBackend()}})

	view := h.View()
	if strings.Contains(view, "No backends configured") {
		t.Errorf("View() = %q, should not show the empty state once a backend is configured", view)
	}
	if !strings.Contains(view, "alpha") {
		t.Errorf("View() = %q, want it to contain the backend's name", view)
	}
	if !strings.Contains(view, testBaseURL) {
		t.Errorf("View() = %q, want it to contain the backend's base URL", view)
	}
}

func TestHomeViewShowsBackendRemovedForAnOrphanedShortcut(t *testing.T) {
	h := newHomeModel()
	h = h.resize(80, 24)
	row := homeQuickRow{item: quick.QuickItem{Label: "Orphan"}, backend: nil}
	h = h.applyLoaded(homeLoadedMsg{rows: []homeQuickRow{row}})

	view := h.View()
	if !strings.Contains(view, "Backend removed") {
		t.Errorf("View() = %q, want it to report the shortcut's missing backend", view)
	}
}

// --- selection -----------------------------------------------------------

func TestHomeSelectedBackendAndQuick(t *testing.T) {
	h := newHomeModel()
	h = h.resize(80, 24)
	be := oneBackend()
	row := homeQuickRow{item: quick.QuickItem{Label: "Root"}, backend: &be}
	h = h.applyLoaded(homeLoadedMsg{backends: []backend.Backend{be}, rows: []homeQuickRow{row}})

	gotBackend, ok := h.selectedBackend()
	if !ok || gotBackend.Name != "alpha" {
		t.Errorf("selectedBackend() = %v, %v, want alpha, true", gotBackend, ok)
	}

	gotQuick, ok := h.selectedQuick()
	if !ok || gotQuick.Label != "Root" {
		t.Errorf("selectedQuick() = %v, %v, want Root, true", gotQuick, ok)
	}
}

func TestHomeSelectedBackendFalseWhenListEmpty(t *testing.T) {
	h := newHomeModel()
	if _, ok := h.selectedBackend(); ok {
		t.Error("selectedBackend() ok = true for an empty list, want false")
	}
	if _, ok := h.selectedQuick(); ok {
		t.Error("selectedQuick() ok = true for an empty list, want false")
	}
}

// --- updateHome: tab and enter ---------------------------------------------

func TestUpdateHomeTabTogglesFocusWhenBothSectionsPresent(t *testing.T) {
	m, _ := newTestModel()
	be := oneBackend()
	row := homeQuickRow{item: quick.QuickItem{Label: "Root"}, backend: &be}
	m.home = m.home.applyLoaded(homeLoadedMsg{backends: []backend.Backend{be}, rows: []homeQuickRow{row}})

	next, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyTab})
	nm := next.(Model)

	if nm.home.focus != homeFocusQuick {
		t.Errorf("focus after tab = %v, want homeFocusQuick", nm.home.focus)
	}
}

func TestUpdateHomeTabNoopWithOnlyBackends(t *testing.T) {
	m, _ := newTestModel()
	m.home = m.home.applyLoaded(homeLoadedMsg{backends: []backend.Backend{oneBackend()}})

	next, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyTab})
	nm := next.(Model)

	if nm.home.focus != homeFocusBackends {
		t.Errorf("focus after tab with no shortcuts = %v, want homeFocusBackends unchanged", nm.home.focus)
	}
}

func TestActivateHomeSelectionNoopWhenNothingSelected(t *testing.T) {
	m, _ := newTestModel()

	next, cmd := m.activateHomeSelection()
	nm := next.(Model)

	if cmd != nil {
		t.Error("activateHomeSelection() returned a non-nil cmd for an empty list")
	}
	if nm.base != screenHome {
		t.Errorf("base = %v, want screenHome unchanged", nm.base)
	}
}

func TestActivateHomeSelectionOpensTheSelectedBackend(t *testing.T) {
	m, _ := newTestModel()
	be := backend.Backend{Name: "test", BaseURL: testBaseURL, Secret: "k"}
	m.home = m.home.applyLoaded(homeLoadedMsg{backends: []backend.Backend{be}})

	_, cmd := m.activateHomeSelection()
	if cmd == nil {
		t.Fatal("activateHomeSelection() returned a nil cmd, want openBackendCmd")
	}
	msg := cmd()
	if _, ok := msg.(homeOpenedMsg); !ok {
		t.Fatalf("cmd() = %T, want homeOpenedMsg", msg)
	}
	if m.session.Backend().BaseURL != testBaseURL {
		t.Errorf("session.Backend().BaseURL = %q, want %q", m.session.Backend().BaseURL, testBaseURL)
	}
}

func TestActivateHomeSelectionRunsTheSelectedShortcut(t *testing.T) {
	m, _ := newTestModel()
	withHomeConfig(t, []backend.Backend{{Name: "test", BaseURL: testBaseURL, Secret: "k"}})

	item := quick.QuickItem{Label: "Root", BaseURL: testBaseURL, Kind: quick.KindDocument, Href: testBaseURL}
	be := backend.Backend{Name: "test", BaseURL: testBaseURL, Secret: "k"}
	m.home = m.home.applyLoaded(homeLoadedMsg{rows: []homeQuickRow{{item: item, backend: &be}}})

	_, cmd := m.activateHomeSelection()
	if cmd == nil {
		t.Fatal("activateHomeSelection() returned a nil cmd, want runQuickCmd")
	}
	msg := cmd()
	ran, ok := msg.(homeQuickRanMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want homeQuickRanMsg", msg)
	}
	if ran.err != nil {
		t.Fatalf("homeQuickRanMsg.err = %v, want nil", ran.err)
	}
	if ran.outcome != session.QuickRunOpened {
		t.Errorf("homeQuickRanMsg.outcome = %v, want QuickRunOpened", ran.outcome)
	}
}

// --- applyQuickRan -----------------------------------------------------

func TestApplyQuickRanSwitchesToDocumentOnOpened(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenHome

	got := m.applyQuickRan(homeQuickRanMsg{outcome: session.QuickRunOpened, label: "Root"})

	if got.base != screenDocument {
		t.Errorf("base = %v, want screenDocument", got.base)
	}
}

func TestApplyQuickRanSetsNoticeOnBackendMissing(t *testing.T) {
	m, _ := newTestModel()

	got := m.applyQuickRan(homeQuickRanMsg{outcome: session.QuickRunBackendMissing, label: "Root"})

	if got.base != screenHome {
		t.Errorf("base = %v, want screenHome unchanged", got.base)
	}
	if !strings.Contains(got.home.notice, "Root") {
		t.Errorf("home.notice = %q, want it to mention the shortcut's label", got.home.notice)
	}
}

func TestApplyQuickRanSetsNoticeOnError(t *testing.T) {
	m, _ := newTestModel()

	got := m.applyQuickRan(homeQuickRanMsg{err: errLoad, label: "Root"})

	if got.base != screenHome {
		t.Errorf("base = %v, want screenHome unchanged", got.base)
	}
	if got.home.notice == "" {
		t.Error("home.notice is empty, want a report of the error")
	}
}

// --- deleteSelectedQuick / applyQuickDeleted (add/remove shortcuts) ------

func TestDeleteSelectedQuickNoopWhenNothingSelected(t *testing.T) {
	m, _ := newTestModel()

	next, cmd := m.deleteSelectedQuick()
	nm := next.(Model)

	if cmd != nil {
		t.Error("deleteSelectedQuick() returned a non-nil cmd for an empty Quick list")
	}
	if nm.base != screenHome {
		t.Errorf("base = %v, want screenHome unchanged", nm.base)
	}
}

// TestDeleteSelectedQuickRemovesTheSelectedShortcut drives the whole
// delete-a-shortcut round trip: seed one stored shortcut, select it on
// Home's Quick list, press the key ('d', via updateHome -- see
// TestUpdateHomeDeleteQuickKeyRemovesTheSelectedShortcut for that half),
// run the returned cmd, and check it is actually gone from storage once
// applyQuickDeleted has folded the result back in.
func TestDeleteSelectedQuickRemovesTheSelectedShortcut(t *testing.T) {
	withHomeConfig(t, []backend.Backend{{Name: "test", BaseURL: testBaseURL, Secret: "k"}})
	saved, err := quick.Add(quick.QuickItem{Label: "Root", BaseURL: testBaseURL, Kind: quick.KindDocument, Href: testBaseURL})
	if err != nil {
		t.Fatalf("quick.Add() error = %v", err)
	}

	m, _ := newTestModel()
	be := backend.Backend{Name: "test", BaseURL: testBaseURL, Secret: "k"}
	m.home = m.home.applyLoaded(homeLoadedMsg{rows: []homeQuickRow{{item: *saved, backend: &be}}})

	next, cmd := m.deleteSelectedQuick()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("deleteSelectedQuick() returned a nil cmd, want deleteQuickCmd")
	}
	msg := cmd()
	deleted, ok := msg.(homeQuickDeletedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want homeQuickDeletedMsg", msg)
	}
	if deleted.err != nil {
		t.Fatalf("homeQuickDeletedMsg.err = %v, want nil", deleted.err)
	}
	if !deleted.ok {
		t.Error("homeQuickDeletedMsg.ok = false, want true")
	}

	remaining, err := quick.Load()
	if err != nil {
		t.Fatalf("quick.Load() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("quick.Load() after delete = %v, want empty", remaining)
	}
}

func TestApplyQuickDeletedReloadsHomeOnSuccess(t *testing.T) {
	withHomeConfig(t, []backend.Backend{{Name: "test", BaseURL: testBaseURL, Secret: "k"}})
	m, _ := newTestModel()

	next, cmd := m.applyQuickDeleted(homeQuickDeletedMsg{label: "Root", ok: true})
	nm := next.(Model)

	if nm.home.notice != "" {
		t.Errorf("home.notice = %q, want empty on success", nm.home.notice)
	}
	if cmd == nil {
		t.Fatal("applyQuickDeleted() returned a nil cmd on success, want homeInitCmd")
	}
	if _, ok := cmd().(homeLoadedMsg); !ok {
		t.Errorf("cmd() = %T, want homeLoadedMsg (homeInitCmd)", cmd())
	}
}

func TestApplyQuickDeletedSetsNoticeOnError(t *testing.T) {
	m, _ := newTestModel()

	next, _ := m.applyQuickDeleted(homeQuickDeletedMsg{label: "Root", err: errLoad})
	nm := next.(Model)

	if nm.home.notice == "" {
		t.Error("home.notice is empty, want a report of the error")
	}
	if !strings.Contains(nm.home.notice, "Root") {
		t.Errorf("home.notice = %q, want it to mention the shortcut's label", nm.home.notice)
	}
}

func TestApplyQuickDeletedSetsNoticeWhenAlreadyGone(t *testing.T) {
	withHomeConfig(t, nil)
	m, _ := newTestModel()

	next, cmd := m.applyQuickDeleted(homeQuickDeletedMsg{label: "Root", ok: false})
	nm := next.(Model)

	if nm.home.notice == "" {
		t.Error("home.notice is empty, want a report that nothing was removed")
	}
	if cmd == nil {
		t.Fatal("applyQuickDeleted() returned a nil cmd when ok=false, want homeInitCmd to still reload")
	}
}

// TestUpdateHomeDeleteQuickKeyRemovesTheSelectedShortcut checks 'd' only
// deletes while the Quick list has focus -- pressing it with the Backends
// list focused must reach updateHomeList (bubbles/list's own handling)
// instead, never deleteSelectedQuick.
func TestUpdateHomeDeleteQuickKeyRemovesTheSelectedShortcut(t *testing.T) {
	withHomeConfig(t, []backend.Backend{{Name: "test", BaseURL: testBaseURL, Secret: "k"}})
	saved, err := quick.Add(quick.QuickItem{Label: "Root", BaseURL: testBaseURL, Kind: quick.KindDocument, Href: testBaseURL})
	if err != nil {
		t.Fatalf("quick.Add() error = %v", err)
	}
	m, _ := newTestModel()
	be := backend.Backend{Name: "test", BaseURL: testBaseURL, Secret: "k"}
	m.home = m.home.applyLoaded(homeLoadedMsg{
		backends: []backend.Backend{be},
		rows:     []homeQuickRow{{item: *saved, backend: &be}},
	})
	m.home.focus = homeFocusBackends

	next, cmd := m.updateHome(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("'d' with the Backends list focused returned a non-nil cmd, want it forwarded to the list instead")
	}

	m.home.focus = homeFocusQuick
	next, cmd = m.updateHome(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("'d' with the Quick list focused returned a nil cmd, want deleteQuickCmd")
	}
	if _, ok := cmd().(homeQuickDeletedMsg); !ok {
		t.Errorf("cmd() = %T, want homeQuickDeletedMsg", cmd())
	}
}

// --- homeInitCmd ---------------------------------------------------------

func TestHomeInitCmdLoadsBackendsAndQuick(t *testing.T) {
	withHomeConfig(t, []backend.Backend{{Name: "alpha", BaseURL: testBaseURL, Secret: "k"}})
	if _, err := quick.Add(quick.QuickItem{
		Label: "Root", BaseURL: testBaseURL, Kind: quick.KindDocument, Href: testBaseURL,
	}); err != nil {
		t.Fatalf("quick.Add() error = %v", err)
	}

	msg := homeInitCmd()()
	loaded, ok := msg.(homeLoadedMsg)
	if !ok {
		t.Fatalf("homeInitCmd()() = %T, want homeLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("homeLoadedMsg.err = %v, want nil", loaded.err)
	}
	if len(loaded.backends) != 1 || loaded.backends[0].Name != "alpha" {
		t.Errorf("homeLoadedMsg.backends = %v, want one backend named alpha", loaded.backends)
	}
	if len(loaded.rows) != 1 || loaded.rows[0].backend == nil || loaded.rows[0].backend.Name != "alpha" {
		t.Errorf("homeLoadedMsg.rows = %v, want one row resolved to alpha", loaded.rows)
	}
}

func TestHomeInitCmdWithNoConfigYieldsEmptyHome(t *testing.T) {
	withHomeConfig(t, nil)

	msg := homeInitCmd()()
	loaded, ok := msg.(homeLoadedMsg)
	if !ok {
		t.Fatalf("homeInitCmd()() = %T, want homeLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("homeLoadedMsg.err = %v, want nil", loaded.err)
	}
	if len(loaded.backends) != 0 {
		t.Errorf("homeLoadedMsg.backends = %v, want none", loaded.backends)
	}
	if len(loaded.rows) != 0 {
		t.Errorf("homeLoadedMsg.rows = %v, want none", loaded.rows)
	}
}

// --- filtering: matches a row's value, not just its label -----------------

// TestHomeBackendFilterMatchesOnBaseURLNotJustName mirrors
// TestDocumentFilterMatchesOnValueNotJustLabel (document_test.go) for
// Home's own backend list.
func TestHomeBackendFilterMatchesOnBaseURLNotJustName(t *testing.T) {
	h := newHomeModel().resize(80, 24)
	h = h.applyLoaded(homeLoadedMsg{backends: []backend.Backend{
		{Name: "alpha", BaseURL: "https://alpha.example/", Secret: "k"},
		{Name: "beta", BaseURL: "https://distinctivehost.example/", Secret: "k"},
	}})

	h.backends.SetFilterText("distinctivehost")

	visible := h.backends.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("VisibleItems() len = %d after filtering on a base-URL-only term, want 1", len(visible))
	}
	item, ok := visible[0].(backendItem)
	if !ok || item.backend.Name != "beta" {
		t.Errorf("VisibleItems()[0] = %#v, want the backend whose BaseURL matched", visible[0])
	}
}

func TestHomeListsFilteringIsEnabled(t *testing.T) {
	h := newHomeModel()
	if !h.backends.FilteringEnabled() {
		t.Error("backends.FilteringEnabled() = false, want true")
	}
	if !h.shortcuts.FilteringEnabled() {
		t.Error("shortcuts.FilteringEnabled() = false, want true")
	}
}
