// Ported from flutter/test/services/quick_service_test.dart, itself ported
// from pebble/tools/test-quick.js -- the cases that matter are the ways a
// shortcut can go wrong, and they are all versions of pointing somewhere
// other than where the user meant: at a reordered backend, at an action
// the server has since withdrawn, or at an address that was never the
// server's to give.
//
// Unlike the Dart file, there is no injected SettingsService/SecretStore
// fake: this package and internal/config both talk to one TOML file, so
// each test isolates itself with t.TempDir() and t.Setenv(ConfigEnvVar,
// ...) exactly like internal/config/store_test.go does, rather than a
// storage double.
package quick_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/quick"
)

const (
	a = "https://a.example/api/"
	b = "https://b.example/api/"
)

// withConfig points config.ConfigEnvVar at a fresh path inside t.TempDir()
// and seeds it with backends -- mirrors quick_service_test.dart's setUp():
// two backends configured by default so BackendFor has something to
// resolve against.
func withConfig(t *testing.T, backends []backend.Backend) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(config.ConfigEnvVar, path)
	if err := config.SaveBackends(backends); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}
	return path
}

func defaultBackends() []backend.Backend {
	return []backend.Backend{
		{Name: "alpha", BaseURL: a, Secret: "k"},
		{Name: "beta", BaseURL: b, Secret: "k"},
	}
}

func setUp(t *testing.T) {
	t.Helper()
	withConfig(t, defaultBackends())
}

func action(label, base, holder, name string) quick.QuickItem {
	return quick.QuickItem{
		Label:       label,
		BackendName: "x",
		BaseURL:     base,
		Kind:        quick.KindAction,
		Holder:      holder,
		Name:        name,
	}
}

func document(label, base, href string) quick.QuickItem {
	return quick.QuickItem{
		Label:       label,
		BackendName: "x",
		BaseURL:     base,
		Kind:        quick.KindDocument,
		Href:        href,
	}
}

func mustAdd(t *testing.T, item quick.QuickItem) *quick.QuickItem {
	t.Helper()
	got, err := quick.Add(item)
	if err != nil {
		t.Fatalf("Add(%+v) error = %v", item, err)
	}
	return got
}

// --- add and list ---------------------------------------------------------

func TestShortcutsAreKeptInOrder(t *testing.T) {
	setUp(t)

	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	mustAdd(t, document("Status", a, "/api/status"))

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].Label != "Power off" || loaded[1].Label != "Status" {
		t.Fatalf("Load() labels = %v, want [Power off Status]", loaded)
	}
}

func TestBackendForNamesTheBackend(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", a, "/api/", "power-off"))

	item, err := quick.Get(0)
	if err != nil || item == nil {
		t.Fatalf("Get(0) = %v, %v", item, err)
	}
	got, err := quick.BackendFor(*item)
	if err != nil {
		t.Fatalf("BackendFor() error = %v", err)
	}
	if got == nil || got.Name != "alpha" {
		t.Fatalf("BackendFor() = %v, want backend named alpha", got)
	}
}

// --- add is idempotent -----------------------------------------------------

func TestAddDoesNotDuplicateTheSameTarget(t *testing.T) {
	setUp(t)

	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	mustAdd(t, action("Power off (renamed)", a, "/api/", "power-off"))

	count, err := quick.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Count() = %d, want 1", count)
	}
}

func TestAddIdempotentTheNewerLabelWins(t *testing.T) {
	setUp(t)

	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	mustAdd(t, action("Power off (renamed)", a, "/api/", "power-off"))

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Label != "Power off (renamed)" {
		t.Fatalf("Load() = %v, want single renamed entry", loaded)
	}
}

// --- a backend is resolved by URL, not position -----------------------------
// The failure this guards: reordering backends must not re-point a
// shortcut at a different server.

func TestResolvesToTheBackendItWasSavedFrom(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", b, "/api/", "power-off"))

	item, err := quick.Get(0)
	if err != nil || item == nil {
		t.Fatalf("Get(0) = %v, %v", item, err)
	}
	got, err := quick.BackendFor(*item)
	if err != nil {
		t.Fatalf("BackendFor() error = %v", err)
	}
	if got == nil || got.Name != "beta" {
		t.Fatalf("BackendFor() = %v, want backend named beta", got)
	}
}

func TestStillResolvesAfterTheListIsReordered(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", b, "/api/", "power-off"))

	if err := config.SaveBackends([]backend.Backend{
		{Name: "beta", BaseURL: b, Secret: "k"},
		{Name: "alpha", BaseURL: a, Secret: "k"},
	}); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	item, err := quick.Get(0)
	if err != nil || item == nil {
		t.Fatalf("Get(0) = %v, %v", item, err)
	}
	got, err := quick.BackendFor(*item)
	if err != nil {
		t.Fatalf("BackendFor() error = %v", err)
	}
	if got == nil || got.BaseURL != b {
		t.Fatalf("BackendFor() = %v, want BaseURL %q", got, b)
	}
}

func TestStillResolvesAfterTheBackendIsRenamed(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", b, "/api/", "power-off"))

	if err := config.SaveBackends([]backend.Backend{
		{Name: "beta renamed", BaseURL: b, Secret: "k"},
	}); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	item, err := quick.Get(0)
	if err != nil || item == nil {
		t.Fatalf("Get(0) = %v, %v", item, err)
	}
	got, err := quick.BackendFor(*item)
	if err != nil {
		t.Fatalf("BackendFor() error = %v", err)
	}
	if got == nil || got.BaseURL != b {
		t.Fatalf("BackendFor() = %v, want BaseURL %q", got, b)
	}
}

// --- a missing backend is shown, not dropped --------------------------------
// Kept and resolvable-to-nil, not silently dropped from the list: something
// the user saved going missing without explanation is worse than a
// shortcut a caller can report as pointing nowhere -- see the package
// comment on why rows()-style rendering is not this package's job; this
// only checks BackendFor's half of the contract.

func TestAShortcutToADeletedBackendSurvives(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", a, "/api/", "power-off"))

	if err := config.SaveBackends([]backend.Backend{
		{Name: "beta", BaseURL: b, Secret: "k"},
	}); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	count, err := quick.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Count() = %d, want 1", count)
	}
}

func TestItResolvesToNothing(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", a, "/api/", "power-off"))

	if err := config.SaveBackends([]backend.Backend{
		{Name: "beta", BaseURL: b, Secret: "k"},
	}); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	item, err := quick.Get(0)
	if err != nil || item == nil {
		t.Fatalf("Get(0) = %v, %v", item, err)
	}
	got, err := quick.BackendFor(*item)
	if err != nil {
		t.Fatalf("BackendFor() error = %v", err)
	}
	if got != nil {
		t.Fatalf("BackendFor() = %v, want nil", got)
	}
}

// --- BackendsFor resolves a list against a supplied backend list -----------
// The batch, in-memory resolver a caller uses so N shortcuts cost one
// backend load, not N+1 config reads. These pin the matching (by base URL,
// first match wins, nil for gone) and -- critically -- that it is pure: it
// resolves against the *passed* list and never reads storage.

func TestBackendsForResolvesEachShortcutByBaseURL(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	mustAdd(t, document("Status", b, "/api/status"))
	items, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	resolved := quick.BackendsFor(items, []backend.Backend{
		{Name: "alpha", BaseURL: a, Secret: "k"},
		{Name: "beta", BaseURL: b, Secret: "k"},
	})
	if len(resolved) != 2 || resolved[0] == nil || resolved[0].Name != "alpha" || resolved[1] == nil || resolved[1].Name != "beta" {
		t.Fatalf("BackendsFor() = %v, want [alpha beta]", resolved)
	}
}

func TestBackendsForReturnsNilPerItemNotInTheSuppliedList(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a1"))
	mustAdd(t, document("two", b, "/api/status"))
	const c = "https://c.example/api/"
	mustAdd(t, document("three", c, "/api/other"))
	items, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	resolved := quick.BackendsFor(items, []backend.Backend{
		{Name: "alpha", BaseURL: a, Secret: "k"},
		{Name: "beta", BaseURL: b, Secret: "k"},
	})
	if len(resolved) != 3 {
		t.Fatalf("BackendsFor() = %v, want 3 entries", resolved)
	}
	if resolved[0] == nil || resolved[0].Name != "alpha" {
		t.Errorf("resolved[0] = %v, want alpha", resolved[0])
	}
	if resolved[1] == nil || resolved[1].Name != "beta" {
		t.Errorf("resolved[1] = %v, want beta", resolved[1])
	}
	if resolved[2] != nil {
		t.Errorf("resolved[2] = %v, want nil", resolved[2])
	}
}

func TestBackendsForIsPureAgainstThePassedList(t *testing.T) {
	setUp(t)
	// Storage has alpha@baseUrl=a (from setUp); pass a list whose a-backend
	// is named differently, and an empty list, and confirm BackendsFor uses
	// only what it was handed -- never falling back to a storage read.
	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	loaded, err := quick.Load()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("Load() = %v, %v, want a single item", loaded, err)
	}
	item := loaded[0]

	fromPassed := quick.BackendsFor([]quick.QuickItem{item}, []backend.Backend{
		{Name: "not-from-storage", BaseURL: a, Secret: "x"},
	})
	if len(fromPassed) != 1 || fromPassed[0] == nil || fromPassed[0].Name != "not-from-storage" {
		t.Fatalf("BackendsFor() = %v, want not-from-storage", fromPassed)
	}

	fromEmpty := quick.BackendsFor([]quick.QuickItem{item}, nil)
	if len(fromEmpty) != 1 || fromEmpty[0] != nil {
		t.Fatalf("BackendsFor() with an empty list = %v, want [nil]", fromEmpty)
	}
}

func TestBackendsForFirstBackendWinsForADuplicateBaseURL(t *testing.T) {
	setUp(t)
	mustAdd(t, action("Power off", a, "/api/", "power-off"))
	loaded, err := quick.Load()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("Load() = %v, %v, want a single item", loaded, err)
	}
	item := loaded[0]

	resolved := quick.BackendsFor([]quick.QuickItem{item}, []backend.Backend{
		{Name: "first", BaseURL: a, Secret: "k"},
		{Name: "second", BaseURL: a, Secret: "k"},
	})
	if len(resolved) != 1 || resolved[0] == nil || resolved[0].Name != "first" {
		t.Fatalf("BackendsFor() = %v, want first", resolved)
	}

	// and BackendFor agrees, since they share the matching logic.
	got, err := quick.BackendFor(item)
	if err != nil || got == nil || got.BaseURL != a {
		t.Fatalf("BackendFor() = %v, %v, want BaseURL %q", got, err, a)
	}
}

// --- an incomplete shortcut is refused --------------------------------------

func TestAnActionWithoutAHolderIsRefused(t *testing.T) {
	setUp(t)
	got, err := quick.Add(quick.QuickItem{Label: "x", BaseURL: a, Kind: quick.KindAction, Name: "power-off"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Add() = %v, want nil", got)
	}
}

func TestAnActionWithoutANameIsRefused(t *testing.T) {
	setUp(t)
	got, err := quick.Add(quick.QuickItem{Label: "x", BaseURL: a, Kind: quick.KindAction, Holder: "/api/"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Add() = %v, want nil", got)
	}
}

func TestADocumentWithoutAnAddressIsRefused(t *testing.T) {
	setUp(t)
	got, err := quick.Add(document("x", a, ""))
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Add() = %v, want nil", got)
	}
}

func TestIncompleteShortcutsLeaveNothingStored(t *testing.T) {
	setUp(t)
	mustAdd(t, quick.QuickItem{Label: "x", BaseURL: a, Kind: quick.KindAction, Name: "power-off"})
	mustAdd(t, quick.QuickItem{Label: "x", BaseURL: a, Kind: quick.KindAction, Holder: "/api/"})
	mustAdd(t, document("x", a, ""))

	count, err := quick.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Count() = %d, want 0", count)
	}
}

// --- remove ------------------------------------------------------------------

func TestRemoveTakesTheNamedOne(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a"))
	mustAdd(t, action("two", a, "/api/", "b"))

	removed, err := quick.Remove(0)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !removed {
		t.Fatalf("Remove() = false, want true")
	}
}

func TestRemoveTheOtherSurvives(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a"))
	mustAdd(t, action("two", a, "/api/", "b"))
	if _, err := quick.Remove(0); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Label != "two" {
		t.Fatalf("Load() = %v, want single entry labelled two", loaded)
	}
}

func TestRemovingPastTheEndIsRefused(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a"))

	removed, err := quick.Remove(9)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if removed {
		t.Fatalf("Remove() = true, want false")
	}
}

// --- RemoveItem: identity-based removal, for a caller with an item in hand
// but no reliable position (a filtered bubbles/list.Model, for one) -------

func TestRemoveItemTakesTheMatchingOne(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a"))
	two := mustAdd(t, action("two", a, "/api/", "b"))

	removed, err := quick.RemoveItem(*two)
	if err != nil {
		t.Fatalf("RemoveItem() error = %v", err)
	}
	if !removed {
		t.Fatalf("RemoveItem() = false, want true")
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Label != "one" {
		t.Fatalf("Load() = %v, want single entry labelled one", loaded)
	}
}

// TestRemoveItemIgnoresPositionMatchesOnTargetOnly proves RemoveItem removes
// by target (BaseURL/Kind/Holder-or-Href/Name -- see same()), not by
// position: passing a QuickItem whose Label differs from what is stored but
// whose target is the same still removes the stored entry, and passing one
// whose Label happens to match but whose target does not is refused. This
// is the whole reason RemoveItem exists rather than every caller resolving
// its own index into Remove -- see RemoveItem's own doc comment.
func TestRemoveItemIgnoresPositionMatchesOnTargetOnly(t *testing.T) {
	setUp(t)
	mustAdd(t, action("original label", a, "/api/", "brew"))

	// Same target, different label: still matches and removes.
	removed, err := quick.RemoveItem(action("a different label entirely", a, "/api/", "brew"))
	if err != nil {
		t.Fatalf("RemoveItem() error = %v", err)
	}
	if !removed {
		t.Fatalf("RemoveItem() with a matching target but a different label = false, want true (target, not label, is identity)")
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Load() after RemoveItem = %v, want empty", loaded)
	}
}

func TestRemoveItemRefusesAnUnmatchedTarget(t *testing.T) {
	setUp(t)
	mustAdd(t, action("one", a, "/api/", "a"))

	removed, err := quick.RemoveItem(action("one", a, "/api/", "does-not-exist"))
	if err != nil {
		t.Fatalf("RemoveItem() error = %v", err)
	}
	if removed {
		t.Fatalf("RemoveItem() for an unstored target = true, want false")
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("Load() after a refused RemoveItem = %v, want the original entry untouched", loaded)
	}
}

// --- the shortcut cap ----------------------------------------------------

func TestTheListIsCappedAtMaxQuick(t *testing.T) {
	setUp(t)
	for i := 0; i < quick.MaxQuick+5; i++ {
		mustAdd(t, action(fmt.Sprintf("n%d", i), a, "/api/", fmt.Sprintf("a%d", i)))
	}

	count, err := quick.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != quick.MaxQuick {
		t.Fatalf("Count() = %d, want %d", count, quick.MaxQuick)
	}
}

// --- corrupt storage degrades to no shortcuts -------------------------------

func TestUnparseableStorageReadsAsEmpty(t *testing.T) {
	path := withConfig(t, defaultBackends())
	if err := os.WriteFile(path, []byte("not toml at all {{{"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Load() = %v, want empty", loaded)
	}
}

func TestQuickNotAnArrayOfTablesReadsAsEmpty(t *testing.T) {
	path := withConfig(t, defaultBackends())
	if err := os.WriteFile(path, []byte(`quick = "nope"`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Load() = %v, want empty", loaded)
	}
}

func TestUnusableQuickMembersAreDropped(t *testing.T) {
	path := withConfig(t, defaultBackends())
	// A quick table with only a label (no base_url, no holder/name/href) is
	// the TOML analogue of the Dart test's junk JSON array members
	// ([null,3,{"label":"x"}]): it decodes fine but usable() rejects it.
	content := "[[quick]]\nlabel = \"x\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Load() = %v, want empty", loaded)
	}
}

// --- an action is stored by name, never by its own href ---------------------

func TestTheActionNameIsWhatIsKept(t *testing.T) {
	setUp(t)
	mustAdd(t, quick.QuickItem{
		Label:   "Power off",
		BaseURL: a,
		Kind:    quick.KindAction,
		Holder:  "/api/",
		Name:    "power-off",
	})

	stored, err := quick.Get(0)
	if err != nil || stored == nil {
		t.Fatalf("Get(0) = %v, %v", stored, err)
	}
	if stored.Name != "power-off" {
		t.Errorf("Name = %q, want %q", stored.Name, "power-off")
	}
	if stored.Holder != "/api/" {
		t.Errorf("Holder = %q, want %q", stored.Holder, "/api/")
	}
}

// --- save ----------------------------------------------------------------

func TestSaveReturnsThePersistedListOnSuccess(t *testing.T) {
	setUp(t)

	got, err := quick.Save([]quick.QuickItem{action("Power off", a, "/api/", "power-off")})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Save() = %v, want 1 item", got)
	}
}

// --- a platform write failure ----------------------------------------------
// config.SaveQuick returns an error on a write failure (a directory that
// cannot be written to, standing in for a full disk here) -- that is
// config.SaveQuick's own documented contract. Add and Remove must surface
// it rather than silently reporting success while nothing was persisted.
// Simulated by making the config directory read-only after seeding it, so
// os.CreateTemp (writeAtomic's first step) fails.

// readOnlyDir seeds a config file with backends and (optionally) one
// existing quick item, then chmods the directory read-only so any further
// write through it fails. Returns the directory so the test can restore
// write permission in cleanup (t.TempDir()'s own removal needs it back).
func readOnlyDir(t *testing.T, seedQuick []quick.QuickItem) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	t.Setenv(config.ConfigEnvVar, path)

	if err := config.SaveBackends(defaultBackends()); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}
	if len(seedQuick) > 0 {
		if _, err := quick.Save(seedQuick); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700) // let t.TempDir()'s own cleanup remove it
	})
	return dir
}

func TestAddReturnsAnErrorWhenTheWriteFails(t *testing.T) {
	readOnlyDir(t, nil)

	got, err := quick.Add(action("Power off", a, "/api/", "power-off"))

	if err == nil {
		t.Fatal("Add() error = nil, want a write failure surfaced")
	}
	if got != nil {
		t.Fatalf("Add() = %v, want nil on a failed write", got)
	}
}

func TestRemoveReturnsAnErrorWhenTheWriteFailsAndTheEntrySurvives(t *testing.T) {
	seed := []quick.QuickItem{action("one", a, "/api/", "a")}
	dir := readOnlyDir(t, seed)

	removed, err := quick.Remove(0)

	if err == nil {
		t.Fatal("Remove() error = nil, want a write failure surfaced")
	}
	if removed {
		t.Fatal("Remove() = true, want false on a failed write")
	}

	// The entry is still on disk -- writeAtomic failed before renaming
	// anything over the config file. Restore write access to read it back
	// (t.Cleanup already restores it at the end, but this test needs it
	// sooner).
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	loaded, err := quick.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Label != "one" {
		t.Fatalf("Load() = %v, want the entry to have survived the failed write", loaded)
	}
}
