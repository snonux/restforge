package tui

import (
	"testing"
	"time"

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

// --- regression: a real keystroke-driven filter actually narrows the list

// filterMatchesWithin runs cmd and returns its result if it is (or
// produces, once tea.BatchMsg is unwrapped) a list.FilterMatchesMsg within
// timeout, discarding anything else. cmd is run on its own goroutine and
// never waited on beyond timeout: bubbles/textinput's own cursor-blink cmd
// (cursor.Model.BlinkCmd) genuinely blocks on a real context.WithTimeout
// for its full blink interval (BlinkSpeed, on the order of half a second)
// when invoked directly like this, outside Bubble Tea's own runtime that
// would normally let it run concurrently with everything else -- calling
// it synchronously in a tight per-keystroke loop is what made an earlier
// version of this test take upwards of two seconds for five characters.
// filterItems' own cmd (list.go), by contrast, is synchronous and returns
// within microseconds, so a short timeout here is enough to tell the two
// apart without waiting out a real blink.
func filterMatchesWithin(cmd tea.Cmd, timeout time.Duration) (list.FilterMatchesMsg, bool) {
	if cmd == nil {
		return nil, false
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if fm, ok := filterMatchesWithin(c, timeout); ok {
					return fm, true
				}
			}
			return nil, false
		}
		fm, ok := msg.(list.FilterMatchesMsg)
		return fm, ok
	case <-time.After(timeout):
		return nil, false
	}
}

// applyKeyAndDrainFilter sends msg through m.Update, then -- unlike every
// other test in this package, which only ever needs to drain a single
// sessionCmd-shaped (tea.Cmd -> one tea.Msg -> done) round trip -- also
// finds and applies whatever list.FilterMatchesMsg comes back (see
// filterMatchesWithin). This is the harness
// TestFilteringByKeystrokeActuallyNarrowsTheDocumentList needs precisely
// because applyFilterMatches (model.go) is itself the fix for a message
// that previously had nowhere to go; a test driving list.Model.
// SetFilterText directly (as the earlier, weaker
// TestDocumentFilterMatchesOnValueNotJustLabel does) bypasses that message
// entirely and would have stayed green even with the bug this proves fixed.
func applyKeyAndDrainFilter(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	if fm, ok := filterMatchesWithin(cmd, 20*time.Millisecond); ok {
		next, _ := m.Update(fm)
		m = next.(Model)
	}
	return m
}

func typeIntoFilter(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = applyKeyAndDrainFilter(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range s {
		m = applyKeyAndDrainFilter(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// TestFilteringByKeystrokeActuallyNarrowsTheDocumentList is the reported
// bug, reproduced exactly: a "node" property and a "version" property (the
// real f3sctl root document has both), filtered by typing "/node" one
// keystroke at a time through Model.Update the way a real terminal
// delivers it -- not list.Model.SetFilterText, which sidesteps the
// FilterMatchesMsg round trip applyFilterMatches (model.go) exists to
// close. Before that fix, VisibleItems() still returned every row: the
// message list.Model's own filterItems cmd produced had nowhere to land.
func TestFilteringByKeystrokeActuallyNarrowsTheDocumentList(t *testing.T) {
	m := newTestModel2(t, &render.RenderedDocument{
		Title: "f3s homelab control",
		Rows: []render.Row{
			{Label: "apiVersion", Sublabel: "1", Kind: render.RowKindProperty},
			{Label: "node", Sublabel: "pi0.lan.buetow.org", Kind: render.RowKindProperty},
			{Label: "version", Sublabel: "v0.6.1", Kind: render.RowKindProperty},
		},
	})

	m = typeIntoFilter(t, m, "node")

	visible := m.document.rows.VisibleItems()
	if len(visible) != 1 {
		labels := make([]string, len(visible))
		for i, it := range visible {
			labels[i] = it.(documentRowItem).row.Label
		}
		t.Fatalf("VisibleItems() after typing \"node\" = %v, want exactly the node row", labels)
	}
	if row, ok := visible[0].(documentRowItem); !ok || row.row.Label != "node" {
		t.Errorf("VisibleItems()[0] = %#v, want the node row", visible[0])
	}
}

// newTestModel2 builds a Model already on the Document screen with doc as
// its current, synced document -- everything
// TestFilteringByKeystrokeActuallyNarrowsTheDocumentList needs and nothing
// newTestModel/newLinkedTestModel (model_test.go/document_test.go) already
// provide, since both are built around a fixed fixture document rather than
// an arbitrary one a filtering test needs to control precisely.
func newTestModel2(t *testing.T, doc *render.RenderedDocument) Model {
	t.Helper()
	m, _ := newTestModel()
	m.base = screenDocument
	m = m.applyWindowSize(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.document = m.document.syncRows(doc)
	return m
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
