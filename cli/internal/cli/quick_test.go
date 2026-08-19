// Tests for the `restforge quick` one-shot subcommands (list, remove, run).
// They run the command tree in process against a config file pointed at a
// temp file through config.ConfigEnvVar (the established isolation pattern
// from internal/quick/quick_test.go's withConfig and internal/session's
// withEmptyConfig), so no test touches a real $HOME or $XDG_CONFIG_HOME or
// the real user config file. The fixture Siren server and root/shelves
// documents are reused from get_test.go (same package), so quick run's
// document rendering is exercised against the same shape get is.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/quick"
)

// withQuickConfig points config at a fresh, empty temp file for one test,
// the same shape internal/session/session_test.go's withEmptyConfig uses.
func withQuickConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, t.TempDir()+"/config.toml")
}

// seedBackends saves backends through config (so they are normalised and
// stored), the same path `backends add` would. The fixture server's URL gets
// a trailing slash from backend.Normalise on save; seedShortcuts must use
// that same normalised base URL so quick.BackendFor's by-base-URL match
// resolves.
func seedBackends(t *testing.T, backends ...backend.Backend) {
	t.Helper()
	if err := config.SaveBackends(backends); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}
}

// seedShortcuts saves shortcuts through quick.Save, which writes the "quick"
// array while preserving the "backends" array seedBackends just wrote.
func seedShortcuts(t *testing.T, items ...quick.QuickItem) {
	t.Helper()
	if _, err := quick.Save(items); err != nil {
		t.Fatalf("quick.Save: %v", err)
	}
}

// execQuick runs the command tree against args, capturing stdout and stderr.
func execQuick(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(newRoot(), args, &out, &errs)
	return code, out.String(), errs.String()
}

// runQuickDirect calls the run command's body with buffer sinks and a
// text/json globalFlags, so a test can assert on the rendered result without
// rounding through Cobra's dispatch. It writes the returned error to errs,
// mirroring the production run() dispatch so a test's stderr assertions see
// the same message a caller would.
func runQuickDirect(t *testing.T, output, arg string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	err := runQuickRun(&out, &errs, &globalFlags{output: output}, arg)
	if err != nil {
		_, _ = fmt.Fprintln(&errs, err)
	}
	return ExitCode(err), out.String(), errs.String()
}

// docShortcut builds a document shortcut pointing at href on the fixture
// backend's (normalised) base URL, so BackendFor resolves it.
func docShortcut(label, baseURL, href string) quick.QuickItem {
	return quick.QuickItem{
		Label:       label,
		BackendName: "pantry",
		BaseURL:     baseURL,
		Kind:        quick.KindDocument,
		Href:        href,
	}
}

func TestQuickListEmptyPrintsHeader(t *testing.T) {
	withQuickConfig(t)
	code, out, errs := execQuick(t, "quick", "list")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "LABEL\tKIND\tBACKEND\tTARGET") {
		t.Fatalf("list output missing header; got:\n%s", out)
	}
}

func TestQuickListEmptyJSON(t *testing.T) {
	withQuickConfig(t)
	code, out, errs := execQuick(t, "quick", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var arr []quickItemJSON
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("list --output json did not print a JSON array: %v\n%s", err, out)
	}
	if len(arr) != 0 {
		t.Fatalf("list --output json = %v, want an empty array", arr)
	}
}

func TestQuickListRoundTrips(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/" // the normalised base URL BackendFor matches on
	seedShortcuts(t,
		docShortcut("shelves", base, "/shelves"),
		quick.QuickItem{Label: "brew", BackendName: "pantry", BaseURL: base,
			Kind: quick.KindAction, Holder: "/", Name: "brew"},
	)

	code, out, errs := execQuick(t, "quick", "list")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "shelves\tdocument\tpantry\t/shelves") {
		t.Fatalf("list missing the document row; got:\n%s", out)
	}
	if !strings.Contains(out, "brew\taction\tpantry\tbrew on /") {
		t.Fatalf("list missing the action row; got:\n%s", out)
	}
}

func TestQuickListJSON(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t,
		docShortcut("shelves", base, "/shelves"),
		quick.QuickItem{Label: "brew", BackendName: "pantry", BaseURL: base,
			Kind: quick.KindAction, Holder: "/", Name: "brew"},
	)
	code, out, errs := execQuick(t, "quick", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var arr []quickItemJSON
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("list --output json did not print a JSON array: %v\n%s", err, out)
	}
	if len(arr) != 2 {
		t.Fatalf("list --output json len = %d, want 2", len(arr))
	}
	// The document shortcut carries href; the action one carries holder/name.
	var doc, act quickItemJSON
	for _, r := range arr {
		if r.Label == "shelves" {
			doc = r
		}
		if r.Label == "brew" {
			act = r
		}
	}
	if doc.Href != "/shelves" || doc.Holder != "" || doc.Name != "" {
		t.Errorf("document shortcut JSON = %+v, want href only", doc)
	}
	if act.Href != "" || act.Holder != "/" || act.Name != "brew" {
		t.Errorf("action shortcut JSON = %+v, want holder/name only", act)
	}
}

// TestQuickRemoveByIndexDropsTheRightEntry seeds two, removes index 0, and
// asserts the second remains -- the task's "remove drops the right entry"
// check applied to quick.
func TestQuickRemoveByIndexDropsTheRightEntry(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t,
		docShortcut("shelves", base, "/shelves"),
		docShortcut("kettle", base, "/"),
	)
	code, out, errs := execQuick(t, "quick", "remove", "0")
	if code != 0 {
		t.Fatalf("remove 0 exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, `removed shortcut "shelves"`) {
		t.Fatalf("remove output missing the confirmation; got:\n%s", out)
	}
	_, listOut, _ := execQuick(t, "quick", "list")
	if strings.Contains(listOut, "shelves\t") {
		t.Fatalf("remove left shelves in the list:\n%s", listOut)
	}
	if !strings.Contains(listOut, "kettle\t") {
		t.Fatalf("remove dropped the wrong entry; list:\n%s", listOut)
	}
}

func TestQuickRemoveByLabel(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t, docShortcut("shelves", base, "/shelves"))
	code, _, errs := execQuick(t, "quick", "remove", "shelves")
	if code != 0 {
		t.Fatalf("remove by label exit = %d, want 0; stderr:\n%s", code, errs)
	}
	items, err := quick.Load()
	if err != nil {
		t.Fatalf("quick.Load: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("remove by label left %d shortcuts", len(items))
	}
}

func TestQuickRemoveAmbiguousLabelIsGeneralFailure(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t,
		docShortcut("shelves", base, "/shelves"),
		docShortcut("shelves", base, "/"),
	)
	code, _, errs := execQuick(t, "quick", "remove", "shelves")
	if code != 1 {
		t.Fatalf("remove ambiguous exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "disambiguate by index") {
		t.Errorf("remove ambiguous stderr missing the message; got:\n%s", errs)
	}
}

func TestQuickRemoveNotFoundIsGeneralFailure(t *testing.T) {
	withQuickConfig(t)
	code, _, errs := execQuick(t, "quick", "remove", "nope")
	if code != 1 {
		t.Fatalf("remove unknown exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, `no shortcut labeled "nope"`) {
		t.Errorf("remove unknown stderr missing the message; got:\n%s", errs)
	}
}

func TestQuickRemoveIndexOutOfRangeIsGeneralFailure(t *testing.T) {
	withQuickConfig(t)
	code, _, errs := execQuick(t, "quick", "remove", "5")
	if code != 1 {
		t.Fatalf("remove out-of-range exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "no shortcut at index 5") {
		t.Errorf("remove out-of-range stderr missing the message; got:\n%s", errs)
	}
}

func TestQuickRemoveNeedsArg(t *testing.T) {
	withQuickConfig(t)
	code, _, errs := execQuick(t, "quick", "remove")
	if code != 2 {
		t.Fatalf("remove with no arg exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "exactly one LABEL-OR-INDEX") {
		t.Errorf("remove with no arg stderr missing the message; got:\n%s", errs)
	}
}

// TestQuickRunDocument renders a document shortcut end to end through
// session.RunQuick, asserting the fetched document's title and a row appear
// -- the task's "renders whatever document ... results" case.
func TestQuickRunDocument(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t, docShortcut("shelves", base, "/shelves"))

	code, out, errs := runQuickDirect(t, "text", "shelves")
	if code != 0 {
		t.Fatalf("run document exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "Shelves") {
		t.Fatalf("run document output missing the title; got:\n%s", out)
	}
	if !strings.Contains(out, "property\tcount\t1") {
		t.Fatalf("run document output missing a row; got:\n%s", out)
	}
}

func TestQuickRunDocumentJSON(t *testing.T) {
	withQuickConfig(t)
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	base := srv.URL + "/"
	seedShortcuts(t, docShortcut("shelves", base, "/shelves"))

	code, out, errs := runQuickDirect(t, "json", "shelves")
	if code != 0 {
		t.Fatalf("run document --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("run document --output json did not print valid JSON: %v\n%s", err, out)
	}
	if doc["title"] != "Shelves" {
		t.Errorf("run document --output json title = %v, want %q", doc["title"], "Shelves")
	}
}

// TestQuickRunBackendMissingIsNonZeroExit is the task's explicit invariant:
// a shortcut whose backend has been removed must produce a clear, non-zero
// exit, never a silent no-op.
func TestQuickRunBackendMissingIsNonZeroExit(t *testing.T) {
	withQuickConfig(t)
	// A backend is configured, but the shortcut points at a different base
	// URL, so BackendFor resolves to nil -> QuickRunBackendMissing.
	srv := newFixtureServer(t)
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
	seedShortcuts(t, docShortcut("gone", "https://nowhere.example/api/", "/x"))

	code, _, errs := runQuickDirect(t, "text", "gone")
	if code != 1 {
		t.Fatalf("run backend-missing exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "no longer configured") {
		t.Errorf("run backend-missing stderr missing the message; got:\n%s", errs)
	}
}

func TestQuickRunUnreachableIsGeneralFailure(t *testing.T) {
	withQuickConfig(t)
	// A backend pointing at a closed server: RunQuick adopts it and fetches,
	// nav fails with Unreachable, which renderQuickRun maps to exit 1.
	srv := newFixtureServer(t)
	base := srv.URL + "/"
	srv.Close()
	seedBackends(t, backend.Backend{Name: "pantry", BaseURL: base, Secret: "open-sesame"})
	seedShortcuts(t, docShortcut("shelves", base, "/shelves"))

	code, _, errs := runQuickDirect(t, "text", "shelves")
	if code != 1 {
		t.Fatalf("run unreachable exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "unreachable") {
		t.Errorf("run unreachable stderr missing the kind; got:\n%s", errs)
	}
}

func TestQuickRunNotFoundIsGeneralFailure(t *testing.T) {
	withQuickConfig(t)
	code, _, errs := runQuickDirect(t, "text", "nope")
	if code != 1 {
		t.Fatalf("run unknown exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, `no shortcut labeled "nope"`) {
		t.Errorf("run unknown stderr missing the message; got:\n%s", errs)
	}
}

func TestQuickRunNeedsArg(t *testing.T) {
	withQuickConfig(t)
	code, _, errs := execQuick(t, "quick", "run")
	if code != 2 {
		t.Fatalf("run with no arg exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "exactly one LABEL-OR-INDEX") {
		t.Errorf("run with no arg stderr missing the message; got:\n%s", errs)
	}
}
