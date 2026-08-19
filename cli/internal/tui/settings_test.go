package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

func oneSettingsBackend() backend.Backend {
	return backend.Backend{Name: "alpha", BaseURL: testBaseURL, AuthHeader: "X-API-Key", Secret: "k"}
}

// withSettingsConfig points config.ConfigEnvVar at a fresh path inside
// t.TempDir() -- the same isolation withHomeConfig (home_test.go) gives
// homeInitCmd, needed here so settingsSaveCmd's real config.SaveBackends
// call never touches a real $HOME or $XDG_CONFIG_HOME.
func withSettingsConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "config.toml"))
}

// --- newSettingsModel / rebuildList --------------------------------------

func TestNewSettingsModelCopiesInitialBackends(t *testing.T) {
	initial := []backend.Backend{oneSettingsBackend()}
	s := newSettingsModel(initial)

	initial[0].Name = "mutated"
	if s.backends[0].Name != "alpha" {
		t.Errorf("newSettingsModel aliased the caller's slice: backends[0].Name = %q, want %q", s.backends[0].Name, "alpha")
	}
}

func TestRebuildListIncludesTrailingAddItemUnderCap(t *testing.T) {
	s := newSettingsModel([]backend.Backend{oneSettingsBackend()})

	items := s.list.Items()
	if len(items) != 2 {
		t.Fatalf("list.Items() len = %d, want 2 (one backend + add)", len(items))
	}
	if _, ok := items[1].(settingsAddItem); !ok {
		t.Errorf("items[1] = %T, want settingsAddItem", items[1])
	}
}

func TestRebuildListDropsAddItemAtCap(t *testing.T) {
	backends := make([]backend.Backend, backend.MaxBackends)
	for i := range backends {
		backends[i] = backend.Backend{Name: "b", BaseURL: testBaseURL, Secret: "k"}
	}
	s := newSettingsModel(backends)

	items := s.list.Items()
	if len(items) != backend.MaxBackends {
		t.Fatalf("list.Items() len = %d, want %d (no add item at cap)", len(items), backend.MaxBackends)
	}
	for _, it := range items {
		if _, ok := it.(settingsAddItem); ok {
			t.Error("list.Items() contains settingsAddItem at MaxBackends, want none")
		}
	}
}

// --- activateSelection: add vs edit ---------------------------------------

func TestActivateSelectionOnAddItemStartsBlankAdd(t *testing.T) {
	s := newSettingsModel(nil).resize(80, 24)

	s = s.activateSelection()

	if s.mode != settingsModeEdit {
		t.Fatalf("mode = %v, want settingsModeEdit", s.mode)
	}
	if s.editIndex != -1 {
		t.Errorf("editIndex = %d, want -1 for a new backend", s.editIndex)
	}
	if got := s.form.inputs[fieldAuthHeader].Value(); got != backend.DefaultAuthHeader {
		t.Errorf("auth header field = %q, want default %q", got, backend.DefaultAuthHeader)
	}
	if got := s.form.inputs[fieldName].Value(); got != "" {
		t.Errorf("name field = %q, want empty", got)
	}
}

func TestActivateSelectionOnBackendItemStartsEditWithBlankSecret(t *testing.T) {
	be := oneSettingsBackend()
	s := newSettingsModel([]backend.Backend{be}).resize(80, 24)
	s.list.Select(0)

	s = s.activateSelection()

	if s.mode != settingsModeEdit {
		t.Fatalf("mode = %v, want settingsModeEdit", s.mode)
	}
	if s.editIndex != 0 {
		t.Errorf("editIndex = %d, want 0", s.editIndex)
	}
	if got := s.form.inputs[fieldName].Value(); got != be.Name {
		t.Errorf("name field = %q, want %q", got, be.Name)
	}
	if got := s.form.inputs[fieldSecret].Value(); got != "" {
		t.Errorf("secret field = %q, want blank -- an existing secret must never be loaded into the field", got)
	}
	if s.form.secretOriginal != be.Secret {
		t.Errorf("form.secretOriginal = %q, want %q", s.form.secretOriginal, be.Secret)
	}
}

func TestStartAddNoopAtMaxBackends(t *testing.T) {
	backends := make([]backend.Backend, backend.MaxBackends)
	for i := range backends {
		backends[i] = backend.Backend{Name: "b", BaseURL: testBaseURL, Secret: "k"}
	}
	s := newSettingsModel(backends).resize(80, 24)

	s = s.startAdd()

	if s.mode != settingsModeList {
		t.Errorf("mode = %v, want settingsModeList unchanged at MaxBackends", s.mode)
	}
}

// --- confirmEdit -----------------------------------------------------------

func TestConfirmEditAppendsNewValidBackend(t *testing.T) {
	s := newSettingsModel(nil).resize(80, 24)
	s = s.startAdd()
	s.form.inputs[fieldName].SetValue("beta")
	s.form.inputs[fieldBaseURL].SetValue(testBaseURL)
	s.form.inputs[fieldSecret].SetValue("secret")

	s = s.confirmEdit()

	if s.mode != settingsModeList {
		t.Fatalf("mode = %v, want settingsModeList after a valid confirm", s.mode)
	}
	if len(s.backends) != 1 || s.backends[0].Name != "beta" {
		t.Fatalf("backends = %v, want one backend named beta", s.backends)
	}
	if s.backends[0].Secret != "secret" {
		t.Errorf("backends[0].Secret = %q, want %q", s.backends[0].Secret, "secret")
	}
}

func TestConfirmEditReportsValidateErrorAndStaysInEditMode(t *testing.T) {
	s := newSettingsModel(nil).resize(80, 24)
	s = s.startAdd() // name/baseURL/secret all blank -- backend.Validate must reject this

	s = s.confirmEdit()

	if s.mode != settingsModeEdit {
		t.Fatalf("mode = %v, want settingsModeEdit to stay for another attempt", s.mode)
	}
	if s.editError == "" {
		t.Error("editError is empty, want backend.Validate's own message")
	}
	if len(s.backends) != 0 {
		t.Errorf("backends = %v, want none -- an invalid row must never be added", s.backends)
	}
}

func TestConfirmEditOnExistingBackendKeepsSecretWhenFieldLeftBlank(t *testing.T) {
	be := oneSettingsBackend()
	s := newSettingsModel([]backend.Backend{be}).resize(80, 24)
	s.list.Select(0)
	s = s.activateSelection() // secret field is blank, secretOriginal = be.Secret
	s.form.inputs[fieldName].SetValue("renamed")

	s = s.confirmEdit()

	if s.mode != settingsModeList {
		t.Fatalf("mode = %v, want settingsModeList", s.mode)
	}
	if s.backends[0].Name != "renamed" {
		t.Errorf("backends[0].Name = %q, want renamed", s.backends[0].Name)
	}
	if s.backends[0].Secret != be.Secret {
		t.Errorf("backends[0].Secret = %q, want the original secret %q kept", s.backends[0].Secret, be.Secret)
	}
}

func TestConfirmEditOnExistingBackendReplacesSecretWhenTyped(t *testing.T) {
	be := oneSettingsBackend()
	s := newSettingsModel([]backend.Backend{be}).resize(80, 24)
	s.list.Select(0)
	s = s.activateSelection()
	s.form.inputs[fieldSecret].SetValue("newsecret")

	s = s.confirmEdit()

	if s.backends[0].Secret != "newsecret" {
		t.Errorf("backends[0].Secret = %q, want newsecret", s.backends[0].Secret)
	}
}

// --- cancelEdit --------------------------------------------------------

func TestCancelEditDiscardsAddWithoutTouchingBackends(t *testing.T) {
	s := newSettingsModel(nil).resize(80, 24)
	s = s.startAdd()
	s.form.inputs[fieldName].SetValue("never saved")

	s = s.cancelEdit()

	if s.mode != settingsModeList {
		t.Fatalf("mode = %v, want settingsModeList", s.mode)
	}
	if len(s.backends) != 0 {
		t.Errorf("backends = %v, want none -- cancelling an add must not add anything", s.backends)
	}
}

func TestCancelEditOnExistingBackendLeavesItUnchanged(t *testing.T) {
	be := oneSettingsBackend()
	s := newSettingsModel([]backend.Backend{be}).resize(80, 24)
	s.list.Select(0)
	s = s.activateSelection()
	s.form.inputs[fieldName].SetValue("would-be renamed")

	s = s.cancelEdit()

	if s.backends[0].Name != be.Name {
		t.Errorf("backends[0].Name = %q, want the original %q -- cancel must not write the form", s.backends[0].Name, be.Name)
	}
}

// --- deleteSelected ------------------------------------------------------

func TestDeleteSelectedRemovesTheBackendUnderTheCursor(t *testing.T) {
	a := backend.Backend{Name: "a", BaseURL: testBaseURL, Secret: "k"}
	b := backend.Backend{Name: "b", BaseURL: testBaseURL, Secret: "k"}
	s := newSettingsModel([]backend.Backend{a, b}).resize(80, 24)
	s.list.Select(0)

	s = s.deleteSelected()

	if len(s.backends) != 1 || s.backends[0].Name != "b" {
		t.Fatalf("backends = %v, want only b left", s.backends)
	}
}

func TestDeleteSelectedNoopOnAddItem(t *testing.T) {
	s := newSettingsModel(nil).resize(80, 24)
	s.list.Select(0) // the only item is the trailing Add row

	s = s.deleteSelected()

	if len(s.backends) != 0 {
		t.Errorf("backends = %v, want none removed by deleting the Add row", s.backends)
	}
}

// --- updateSettings: list mode key routing --------------------------------

func TestUpdateSettingsListEnterOnAddSwitchesToEditMode(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if nm.settings.mode != settingsModeEdit {
		t.Errorf("mode = %v, want settingsModeEdit", nm.settings.mode)
	}
}

func TestUpdateSettingsListDeleteKeyRemovesSelected(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel([]backend.Backend{oneSettingsBackend()}).resize(80, 24)
	m.settings.list.Select(0)

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	nm := next.(Model)

	if len(nm.settings.backends) != 0 {
		t.Errorf("backends = %v, want none after 'd' on the only row", nm.settings.backends)
	}
}

func TestUpdateSettingsListSaveKeyReturnsSaveCmd(t *testing.T) {
	withSettingsConfig(t)
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel([]backend.Backend{oneSettingsBackend()}).resize(80, 24)

	next, cmd := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	nm := next.(Model)

	if !nm.settings.saving {
		t.Error("saving = false, want true once 's' is pressed")
	}
	if cmd == nil {
		t.Fatal("updateSettings('s') returned a nil cmd, want settingsSaveCmd")
	}
	msg := cmd()
	saved, ok := msg.(settingsSavedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want settingsSavedMsg", msg)
	}
	if saved.err != nil {
		t.Errorf("settingsSavedMsg.err = %v, want nil", saved.err)
	}
}

// --- updateSettings: edit mode key routing --------------------------------

func TestUpdateSettingsEditTabMovesFocusForward(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)
	m.settings = m.settings.startAdd()

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyTab})
	nm := next.(Model)

	if nm.settings.form.focus != fieldBaseURL {
		t.Errorf("form.focus = %d, want fieldBaseURL after one Tab from fieldName", nm.settings.form.focus)
	}
}

func TestUpdateSettingsEditUpWrapsToLastField(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)
	m.settings = m.settings.startAdd() // starts focused on fieldName

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyUp})
	nm := next.(Model)

	if nm.settings.form.focus != fieldStartRel {
		t.Errorf("form.focus = %d, want fieldStartRel (wrap from fieldName going up)", nm.settings.form.focus)
	}
}

func TestUpdateSettingsEditTypingGoesToFocusedField(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)
	m.settings = m.settings.startAdd()

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	nm := next.(Model)

	if got := nm.settings.form.inputs[fieldName].Value(); got != "x" {
		t.Errorf("name field = %q, want %q typed into the focused field", got, "x")
	}
}

func TestUpdateSettingsEditEnterConfirmsAndReturnsToListMode(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)
	m.settings = m.settings.startAdd()
	m.settings.form.inputs[fieldName].SetValue("gamma")
	m.settings.form.inputs[fieldBaseURL].SetValue(testBaseURL)
	m.settings.form.inputs[fieldSecret].SetValue("k")

	next, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if nm.settings.mode != settingsModeList {
		t.Fatalf("mode = %v, want settingsModeList", nm.settings.mode)
	}
	if len(nm.settings.backends) != 1 || nm.settings.backends[0].Name != "gamma" {
		t.Errorf("backends = %v, want one backend named gamma", nm.settings.backends)
	}
}

// --- Model wiring: openSettings, handleBack, currentScreenView -----------

func TestOpenSettingsSeedsFromHomeBackends(t *testing.T) {
	m, _ := newTestModel()
	m.home = m.home.applyLoaded(homeLoadedMsg{backends: []backend.Backend{oneSettingsBackend()}})

	m = m.openSettings()

	if m.base != screenSettings {
		t.Fatalf("base = %v, want screenSettings", m.base)
	}
	if len(m.settings.backends) != 1 || m.settings.backends[0].Name != "alpha" {
		t.Errorf("settings.backends = %v, want one backend named alpha, seeded from Home", m.settings.backends)
	}
}

func TestHandleBackInSettingsEditModeReturnsToListModeOnly(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)
	m.settings = m.settings.startAdd()

	m = m.handleBack()

	if m.base != screenSettings {
		t.Errorf("base = %v, want screenSettings unchanged (Back should step down, not exit)", m.base)
	}
	if m.settings.mode != settingsModeList {
		t.Errorf("settings.mode = %v, want settingsModeList after one Back from edit mode", m.settings.mode)
	}
}

func TestHandleBackInSettingsListModeReturnsToHome(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings
	m.settings = newSettingsModel(nil).resize(80, 24)

	m = m.handleBack()

	if m.base != screenHome {
		t.Errorf("base = %v, want screenHome", m.base)
	}
}

func TestCurrentScreenViewRendersSettings(t *testing.T) {
	m, _ := newTestModel()
	m = m.openSettings()

	view := m.currentScreenView()
	if !strings.Contains(view, "Settings") {
		t.Errorf("currentScreenView() = %q, want it to contain the Settings heading", view)
	}
}

// --- applySettingsSaved ----------------------------------------------------

func TestApplySettingsSavedOnSuccessReturnsHomeAndReloads(t *testing.T) {
	withSettingsConfig(t)
	m, _ := newTestModel()
	m.base = screenSettings

	next, cmd := m.applySettingsSaved(settingsSavedMsg{})
	nm := next.(Model)

	if nm.base != screenHome {
		t.Errorf("base = %v, want screenHome", nm.base)
	}
	if cmd == nil {
		t.Fatal("applySettingsSaved on success returned a nil cmd, want homeInitCmd")
	}
	if _, ok := cmd().(homeLoadedMsg); !ok {
		t.Error("applySettingsSaved's cmd did not produce a homeLoadedMsg")
	}
}

func TestApplySettingsSavedOnErrorStaysOnSettingsWithNotice(t *testing.T) {
	m, _ := newTestModel()
	m.base = screenSettings

	next, cmd := m.applySettingsSaved(settingsSavedMsg{err: errLoad})
	nm := next.(Model)

	if nm.base != screenSettings {
		t.Errorf("base = %v, want screenSettings unchanged on a save error", nm.base)
	}
	if nm.settings.notice == "" {
		t.Error("settings.notice is empty, want a report of the save error")
	}
	if cmd != nil {
		t.Error("applySettingsSaved on error returned a non-nil cmd, want nil (stay put, no reload)")
	}
}

// --- settingsSaveCmd -------------------------------------------------------

func TestSettingsSaveCmdWritesThroughConfig(t *testing.T) {
	withSettingsConfig(t)

	msg := settingsSaveCmd([]backend.Backend{oneSettingsBackend()})()
	saved, ok := msg.(settingsSavedMsg)
	if !ok {
		t.Fatalf("settingsSaveCmd()() = %T, want settingsSavedMsg", msg)
	}
	if saved.err != nil {
		t.Fatalf("settingsSavedMsg.err = %v, want nil", saved.err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Errorf("LoadBackends() = %v, want one backend named alpha", got)
	}
}

// --- filtering: matches a backend's value, not just its name -------------

// TestSettingsFilterMatchesOnBaseURLNotJustName mirrors
// TestDocumentFilterMatchesOnValueNotJustLabel (document_test.go): a filter
// query that only appears in a backend's base URL (its value) must still
// find it, proving settingsBackendItem.FilterValue() covers more than the
// name alone.
func TestSettingsFilterMatchesOnBaseURLNotJustName(t *testing.T) {
	m := newSettingsModel([]backend.Backend{
		{Name: "alpha", BaseURL: "https://alpha.example/", Secret: "k"},
		{Name: "beta", BaseURL: "https://distinctivehost.example/", Secret: "k"},
	}).resize(80, 24)

	m.list.SetFilterText("distinctivehost")

	visible := m.list.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("VisibleItems() len = %d after filtering on a base-URL-only term, want 1", len(visible))
	}
	item, ok := visible[0].(settingsBackendItem)
	if !ok || item.backend.Name != "beta" {
		t.Errorf("VisibleItems()[0] = %#v, want the backend whose BaseURL matched", visible[0])
	}
}

func TestSettingsFilteringIsEnabled(t *testing.T) {
	m := newSettingsModel(nil)
	if !m.list.FilteringEnabled() {
		t.Error("FilteringEnabled() = false, want true")
	}
}
