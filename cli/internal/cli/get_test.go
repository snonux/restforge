// Tests for the restforge get subcommand. They run the command in process
// (via run/newRoot, capturing stdout and stderr) against an httptest Siren
// server and a real config file written through internal/config, so they
// exercise the full dispatch -- flag parsing, backend selection, fetch,
// rendering and exit codes -- without building a binary or needing a
// terminal, the same shape internal/httpclient's tests use.
package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// rootSiren is a small Siren root: two properties, one embedded sub-entity,
// a self link and a shelves link, and one (empty-fields) action. Enough to
// exercise every row kind get renders.
func rootSiren() map[string]any {
	return map[string]any{
		"class":      []any{"pantry"},
		"title":      "The pantry",
		"properties": map[string]any{"apiVersion": float64(1), "kettle": "cold"},
		"entities": []any{
			map[string]any{
				"class":      []any{"shelf"},
				"title":      "Top shelf",
				"properties": map[string]any{"name": "top", "jars": float64(4)},
			},
		},
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": "/"},
			map[string]any{"rel": []any{"shelves"}, "href": "/shelves"},
		},
		"actions": []any{
			map[string]any{"name": "brew", "title": "Brew tea", "method": "POST", "href": "/brew", "fields": []any{}},
		},
	}
}

// shelvesSiren is the document at the root's "shelves" rel.
func shelvesSiren() map[string]any {
	return map[string]any{
		"class":      []any{"shelf-list"},
		"title":      "Shelves",
		"properties": map[string]any{"count": float64(1)},
		"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/shelves"}},
	}
}

// newFixtureServer serves rootSiren at "/" and shelvesSiren at "/shelves".
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	writeJSON := func(w http.ResponseWriter, doc map[string]any) {
		w.Header().Set("Content-Type", "application/siren+json")
		_ = json.NewEncoder(w).Encode(doc)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/shelves", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, shelvesSiren()) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, rootSiren()) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// writeConfig stores backends through the real config path (ConfigEnvVar
// pointed at a temp file) so LoadBackends in runGet reads exactly what a
// user's config would hold.
func writeConfig(t *testing.T, backends ...backend.Backend) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, t.TempDir()+"/config.toml")
	if err := config.SaveBackends(backends); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}
}

// execute runs the command tree against args, capturing stdout and stderr.
func execute(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(newRoot(), args, &out, &errs)
	return code, out.String(), errs.String()
}

func fixtureBackend(t *testing.T, srv *httptest.Server) backend.Backend {
	return backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"}
}

func TestGetRootAsText(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, out, errs := execute(t, "get")
	if code != 0 {
		t.Fatalf("get exit = %d, want 0; stderr:\n%s", code, errs)
	}
	for _, want := range []string{
		"The pantry",              // title line first
		"property\tapiVersion\t1", // a property row
		"property\tkettle\tcold",  // the other property
		"entity\tTop shelf\t",     // the sub-entity row
		"link\tshelves\t/shelves", // the shelves link (no title: hint is the href)
		"action\tBrew tea\tPOST",  // the action row
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get output missing %q; got:\n%s", want, out)
		}
	}
}

func TestGetHref(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, out, errs := execute(t, "get", "/shelves")
	if code != 0 {
		t.Fatalf("get /shelves exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "Shelves") {
		t.Fatalf("get /shelves output missing title; got:\n%s", out)
	}
	if !strings.Contains(out, "property\tcount\t1") {
		t.Fatalf("get /shelves output missing the count row; got:\n%s", out)
	}
}

func TestGetRel(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, out, errs := execute(t, "get", "--rel", "shelves")
	if code != 0 {
		t.Fatalf("get --rel shelves exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "Shelves") {
		t.Fatalf("get --rel shelves output missing title; got:\n%s", out)
	}
}

func TestGetJSON(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, out, errs := execute(t, "get", "--output", "json")
	if code != 0 {
		t.Fatalf("get --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("get --output json did not print valid JSON: %v\n%s", err, out)
	}
	if got := doc["title"]; got != "The pantry" {
		t.Errorf("get --output json title = %v, want %q", got, "The pantry")
	}
}

func TestGetNoBackendsConfigured(t *testing.T) {
	// No config file at all: LoadBackends returns ([], nil), and selecting
	// a backend with none configured is a config-state failure (exit 1).
	t.Setenv(config.ConfigEnvVar, t.TempDir()+"/none.toml")
	code, _, errs := execute(t, "get")
	if code != 1 {
		t.Fatalf("get with no backends exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "no backends configured") {
		t.Errorf("get with no backends stderr missing the message; got:\n%s", errs)
	}
}

func TestGetBackendNotFound(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, _, errs := execute(t, "get", "--backend", "nope")
	if code != 2 {
		t.Fatalf("get --backend nope exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, `no configured backend named "nope"`) {
		t.Errorf("get --backend nope stderr missing the message; got:\n%s", errs)
	}
}

func TestGetMultipleBackendsNeedsName(t *testing.T) {
	srv := newFixtureServer(t)
	other := fixtureBackend(t, srv)
	other.Name = "other"
	writeConfig(t, fixtureBackend(t, srv), other)

	code, _, errs := execute(t, "get")
	if code != 2 {
		t.Fatalf("get with two backends exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "specify --backend") {
		t.Errorf("get with two backends stderr missing the message; got:\n%s", errs)
	}
}

func TestGetTooManyArgsIsUsageError(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, _, errs := execute(t, "get", "/shelves", "/jars")
	if code != 2 {
		t.Fatalf("get with two HREFs exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "at most one HREF") {
		t.Errorf("get with two HREFs stderr missing the message; got:\n%s", errs)
	}
}

func TestGetRelAndHrefAreMutuallyExclusive(t *testing.T) {
	srv := newFixtureServer(t)
	writeConfig(t, fixtureBackend(t, srv))

	code, _, errs := execute(t, "get", "--rel", "shelves", "/shelves")
	if code != 2 {
		t.Fatalf("get --rel + HREF exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "mutually exclusive") {
		t.Errorf("get --rel + HREF stderr missing the message; got:\n%s", errs)
	}
}

func TestGetUnreachableIsGeneralFailure(t *testing.T) {
	// A server that has been closed refuses connections: httpclient
	// classifies that as failure.Unreachable, which maps to exit 1.
	srv := httptest.NewServer(http.NewServeMux())
	base := srv.URL
	srv.Close()
	writeConfig(t, backend.Backend{Name: "pantry", BaseURL: base, Secret: "open-sesame"})

	code, _, errs := execute(t, "get")
	if code != 1 {
		t.Fatalf("get unreachable exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "unreachable") {
		t.Errorf("get unreachable stderr missing the kind; got:\n%s", errs)
	}
}
