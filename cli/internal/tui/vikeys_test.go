package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/render"
)

func hKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")} }
func lKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")} }

// --- remapViKey ------------------------------------------------------------

func TestRemapViKeyMapsHToEscAndLToEnter(t *testing.T) {
	m, _ := newTestModel()

	got := m.remapViKey(hKey())
	if got.Type != tea.KeyEsc {
		t.Errorf("remapViKey(h).Type = %v, want KeyEsc", got.Type)
	}

	got = m.remapViKey(lKey())
	if got.Type != tea.KeyEnter {
		t.Errorf("remapViKey(l).Type = %v, want KeyEnter", got.Type)
	}
}

// TestRemapViKeyLeavesEverythingElseAlone checks a key that is neither h
// nor l passes through completely unchanged.
func TestRemapViKeyLeavesEverythingElseAlone(t *testing.T) {
	m, _ := newTestModel()

	j := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	if got := m.remapViKey(j); got.String() != "j" {
		t.Errorf("remapViKey(j) = %q, want unchanged %q", got.String(), "j")
	}

	esc := tea.KeyMsg{Type: tea.KeyEsc}
	if got := m.remapViKey(esc); got.Type != tea.KeyEsc {
		t.Errorf("remapViKey(esc) = %v, want unchanged KeyEsc", got.Type)
	}
}

// --- isTextEntryScreen -------------------------------------------------

// TestIsTextEntryScreenTrueForValuePrompt puts Session into a real pending
// ValueQuestion (via questionFixture's "safe-needs-note" action, the same
// fixture valueprompt_test.go's own tests use) rather than faking
// currentScreen's answer directly, so this exercises the real
// deriveScreen -> isTextEntryScreen path a keystroke actually takes.
func TestIsTextEntryScreenTrueForValuePrompt(t *testing.T) {
	m := openQuestionFixture(nil)
	m.session.Activate(render.ActionTarget{Name: "safe-needs-note"})
	if m.currentScreen() != screenValuePrompt {
		t.Fatalf("test setup: currentScreen() = %v, want screenValuePrompt", m.currentScreen())
	}

	if !m.isTextEntryScreen() {
		t.Error("isTextEntryScreen() = false while a ValueQuestion is pending, want true")
	}
	if got := m.remapViKey(hKey()); got.String() != "h" {
		t.Errorf("remapViKey(h) during ValuePrompt = %q, want unchanged %q (a literal h must stay typable)", got.String(), "h")
	}
	if got := m.remapViKey(lKey()); got.String() != "l" {
		t.Errorf("remapViKey(l) during ValuePrompt = %q, want unchanged %q (a literal l must stay typable)", got.String(), "l")
	}
}

func TestIsTextEntryScreenTrueForSettingsEditMode(t *testing.T) {
	m, _ := newTestModel()
	m = m.openSettings()
	m.settings = m.settings.startAdd()

	if !m.isTextEntryScreen() {
		t.Error("isTextEntryScreen() = false in settingsModeEdit, want true")
	}
	if got := m.remapViKey(lKey()); got.String() != "l" {
		t.Errorf("remapViKey(l) during Settings edit mode = %q, want unchanged %q", got.String(), "l")
	}
}

func TestIsTextEntryScreenFalseForSettingsListMode(t *testing.T) {
	m, _ := newTestModel()
	m = m.openSettings()

	if m.isTextEntryScreen() {
		t.Error("isTextEntryScreen() = true in settingsModeList, want false")
	}
}

func TestIsTextEntryScreenFalseForHomeAndDocument(t *testing.T) {
	m, _ := newTestModel()
	if m.isTextEntryScreen() {
		t.Error("isTextEntryScreen() = true on Home, want false")
	}

	m.base = screenDocument
	if m.isTextEntryScreen() {
		t.Error("isTextEntryScreen() = true on Document, want false")
	}
}

// --- currentFilterState -----------------------------------------------

func TestCurrentFilterStateUnfilteredByDefault(t *testing.T) {
	m, _ := newTestModel()
	if got := m.currentFilterState(); got != list.Unfiltered {
		t.Errorf("currentFilterState() = %v, want Unfiltered", got)
	}
}

func TestCurrentFilterStateFollowsHomeFocus(t *testing.T) {
	m, _ := newTestModel()
	m.home.backends.SetFilterState(list.Filtering)
	m.home.shortcuts.SetFilterState(list.FilterApplied)

	m.home.focus = homeFocusBackends
	if got := m.currentFilterState(); got != list.Filtering {
		t.Errorf("currentFilterState() with backends focused = %v, want Filtering", got)
	}

	m.home.focus = homeFocusQuick
	if got := m.currentFilterState(); got != list.FilterApplied {
		t.Errorf("currentFilterState() with shortcuts focused = %v, want FilterApplied", got)
	}
}

func TestCurrentFilterStateForDocument(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenDocument
	m.document.rows.SetFilterState(list.Filtering)

	if got := m.currentFilterState(); got != list.Filtering {
		t.Errorf("currentFilterState() on Document = %v, want Filtering", got)
	}
}

func TestCurrentFilterStateForSettingsListModeOnly(t *testing.T) {
	m, _ := newTestModel()
	m = m.openSettings()
	m.settings.list.SetFilterState(list.Filtering)

	if got := m.currentFilterState(); got != list.Filtering {
		t.Errorf("currentFilterState() in settingsModeList = %v, want Filtering", got)
	}

	m.settings = m.settings.startAdd()
	if got := m.currentFilterState(); got != list.Unfiltered {
		t.Errorf("currentFilterState() in settingsModeEdit = %v, want Unfiltered (no list is current)", got)
	}
}

// --- handleKey's filter-deferral cases ----------------------------------

// TestHandleKeyDefersToListWhileFiltering checks that once a list is
// list.Filtering, a key that would otherwise be the shell's global Quit
// ('q') instead reaches the list (and so its own FilterInput), never
// quitting the program -- the concrete case vikeys.go's own doc comment
// describes: "q" must stay typable in a search query.
func TestHandleKeyDefersToListWhileFiltering(t *testing.T) {
	m, _ := newTestModel()
	m.home.backends.SetFilterState(list.Filtering)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	nm := next.(Model)

	if nm.quitting {
		t.Error("'q' quit the program while a filter was being typed; it should have reached the filter input instead")
	}
}

// TestHandleKeyDefersEscToClearAppliedFilter checks that once a filter is
// FilterApplied (confirmed, not being edited), Esc still defers to the
// list -- list.Model's own ClearFilter binding -- rather than the shell's
// global Back navigating away, matching handleKey's own doc comment.
func TestHandleKeyDefersEscToClearAppliedFilter(t *testing.T) {
	m, _ := newTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.session.Activate(render.FetchTarget{Href: "/child"})
	m.base = screenDocument
	m.document.rows.SetFilterState(list.FilterApplied)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	nm := next.(Model)

	// A real navigation Back would have popped the stack; deferring to
	// list.Model's ClearFilter must not.
	if !nm.session.CanGoBack() {
		t.Error("esc while a filter was applied popped Session's navigation stack; it should have cleared the filter instead")
	}
}

// --- Model-level integration: h/l actually drive navigation/selection ---

// TestHKeyNavigatesBackOnDocument presses the shell's own vi-back key on
// the Document screen after pushing a child document, and checks it pops
// Session's navigation stack exactly like a literal Esc would (see
// TestModelUpdateBackPopsWithoutIO, model_test.go) -- the end-to-end proof
// that remapViKey's output reaches Model.handleBack through the ordinary
// key.Matches(m.keys.Back) path.
func TestHKeyNavigatesBackOnDocument(t *testing.T) {
	m, _ := newTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	m.session.Activate(render.FetchTarget{Href: "/child"})
	if !m.session.CanGoBack() {
		t.Fatal("test setup: expected CanGoBack after opening a child document")
	}

	next, cmd := m.Update(hKey())
	nm := next.(Model)

	if cmd != nil {
		t.Error("h (back) returned a non-nil tea.Cmd; Back performs no I/O and should not need one")
	}
	if nm.session.CanGoBack() {
		t.Error("h did not pop Session's navigation stack the way Esc does")
	}
}

// TestLKeyActivatesTheSelectedDocumentRow presses 'l' on a Document row and
// checks it follows the link exactly like a literal Enter would (see
// TestModelIntegrationOpenBackendThenActivateLink, document_test.go).
func TestLKeyActivatesTheSelectedDocumentRow(t *testing.T) {
	m := newLinkedTestModel()
	m.session.OpenBackend(backend.Backend{Name: "test", BaseURL: testBaseURL})
	next, _ := m.Update(homeOpenedMsg{})
	m = next.(Model)
	m.document.rows.Select(0)

	next, cmd := m.Update(lKey())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("l (select) on a link row returned a nil tea.Cmd, want sessionCmd's fetch")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.session.Href() != "/child" {
		t.Errorf("Session.Href() = %q after l, want %q (the followed link's href)", m.session.Href(), "/child")
	}
}
