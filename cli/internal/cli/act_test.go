// Tests for the restforge act subcommand. They run the command in process
// against an httptest Siren server that mimics the fake-siren fixture's
// action shapes (a safe GET action, an unsafe POST that starts a watchable
// job, a required-field action, a 409, and an action needing two fields), so
// they exercise the confirmation gate, the field/value filling, the bounded
// exit codes and the blocking live watch end to end -- deterministically and
// fast, the same shape internal/action's and internal/live's own tests use,
// rather than the ~24-second wall-clock the real fixture's brew job takes.
package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
)

// actServer is a configurable Siren fixture for act tests. jobReply is what
// GET /job returns (the watch's poll target): "done" for a completed job, a
// property-less body for a give-up. posts records every POST path received,
// so a test can assert an unsafe action was refused and sent nothing.
type actServer struct {
	mu        sync.Mutex
	postPaths []string
	jobReply  map[string]any
}

func (s *actServer) handle(w http.ResponseWriter, r *http.Request) {
	writeJSON := func(status int, doc map[string]any) {
		w.Header().Set("Content-Type", "application/siren+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(doc)
	}
	switch r.Method {
	case http.MethodGet:
		switch r.URL.Path {
		case "/":
			writeJSON(200, rootActDoc())
		case "/job":
			writeJSON(200, s.jobReply)
		case "/peek":
			writeJSON(200, map[string]any{"class": []any{"peek-result"}, "title": "Inside",
				"properties": map[string]any{"items": float64(3)}})
		default:
			writeJSON(404, errDoc("nothing there"))
		}
	case http.MethodPost:
		s.mu.Lock()
		s.postPaths = append(s.postPaths, r.URL.Path)
		s.mu.Unlock()
		switch r.URL.Path {
		case "/brew":
			writeJSON(202, map[string]any{"class": []any{"kettle-job"},
				"properties": map[string]any{"state": "running", "id": float64(1), "staleAfterSeconds": float64(120)},
				"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/job"}}})
		case "/label":
			writeJSON(200, map[string]any{"class": []any{"label"}, "title": "Labelled",
				"properties": map[string]any{"label": "set"}})
		case "/broken":
			writeJSON(409, errDoc("the server refused"))
		case "/two":
			writeJSON(200, map[string]any{"class": []any{"two"}})
		case "/cool":
			writeJSON(200, map[string]any{"class": []any{"pantry"}, "title": "The pantry"})
		default:
			writeJSON(404, errDoc("nothing to do there"))
		}
	}
}

// rootActDoc is the document the action lives on: a self link, a kettle-job
// link (the watch's poll target), and one action per behaviour the tests
// exercise. The rel/class vocabulary is the fixture's, confined to this
// _test.go file (excluded from the genericity check).
func rootActDoc() map[string]any {
	return map[string]any{
		"class":      []any{"pantry"},
		"title":      "The pantry",
		"properties": map[string]any{"kettle": "cold"},
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": "/"},
			map[string]any{"rel": []any{"kettle-job"}, "href": "/job"},
		},
		"actions": []any{
			map[string]any{"name": "peek", "title": "Look inside", "method": "GET", "href": "/peek", "fields": []any{}},
			map[string]any{"name": "brew", "title": "Brew a pot of tea", "method": "POST", "href": "/brew",
				"fields": []any{map[string]any{"name": "strength", "type": "range", "value": "3"}}},
			map[string]any{"name": "label-jar", "title": "Write a label for a jar", "method": "POST", "href": "/label",
				"fields": []any{map[string]any{"name": "text", "type": "text", "required": true, "title": "What should the label say?"}}},
			map[string]any{"name": "cool-down", "title": "Let the kettle cool", "method": "POST", "href": "/cool",
				"fields": []any{map[string]any{"name": "confirm", "type": "checkbox", "required": true, "title": "Cool it anyway?"}}},
			map[string]any{"name": "broken", "title": "Always refused", "method": "POST", "href": "/broken", "fields": []any{}},
			map[string]any{"name": "two-required", "title": "Needs two", "method": "POST", "href": "/two",
				"fields": []any{
					map[string]any{"name": "a", "type": "text", "required": true},
					map[string]any{"name": "b", "type": "text", "required": true},
				}},
		},
	}
}

func errDoc(msg string) map[string]any {
	return map[string]any{"class": []any{"error"}, "properties": map[string]any{"message": msg}}
}

// actBackend writes one backend pointing at srv and returns its name.
func actBackend(t *testing.T, srv *httptest.Server) {
	t.Helper()
	writeConfig(t, backend.Backend{Name: "pantry", BaseURL: srv.URL, Secret: "open-sesame"})
}

// posts returns the POST paths the server recorded, for "sent nothing" asserts.
func (s *actServer) posts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.postPaths...)
}

func TestActSafeGetRunsYesNotNeeded(t *testing.T) {
	srv, _ := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, out, errs := execute(t, "act", "peek")
	if code != 0 {
		t.Fatalf("act peek exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "Inside") {
		t.Errorf("act peek output missing the result; got:\n%s", out)
	}
}

// newActServerWithRecorder builds the fixture server and returns it with
// its recorder, so a test can assert which POSTs it received.
func newActServerWithRecorder(t *testing.T, jobReply map[string]any) (*httptest.Server, *actServer) {
	t.Helper()
	s := &actServer{jobReply: jobReply}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func TestActUnsafeWithoutYesRefusesAndSendsNothing(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "brew")
	if code != 3 {
		t.Fatalf("act brew (no --yes) exit = %d, want 3 (refused); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "Brew a pot of tea") || !strings.Contains(errs, "unsafe") {
		t.Errorf("act brew stderr missing the refusal message; got:\n%s", errs)
	}
	if got := rec.posts(); len(got) != 0 {
		t.Errorf("act brew (no --yes) sent POSTs %v; an unsafe action without --yes must send nothing", got)
	}
}

func TestActUnsafeWithYesInvokesAndWatchesToDone(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, map[string]any{
		"class":      []any{"kettle-job"},
		"properties": map[string]any{"state": "done", "id": float64(1), "step": ""},
	})
	actBackend(t, srv)

	code, out, errs := execute(t, "act", "brew", "--yes")
	if code != 0 {
		t.Fatalf("act brew --yes exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !contains(rec.posts(), "/brew") {
		t.Errorf("act brew --yes did not POST /brew; got %v", rec.posts())
	}
	// The watch polls /job once and the server replies done, so the result
	// is the completed job.
	if !strings.Contains(out, "done") {
		t.Errorf("act brew --yes output missing the completed job; got:\n%s\nstderr:\n%s", out, errs)
	}
}

func TestActWatchGivesUpWhenNothingReportsProgress(t *testing.T) {
	srv, _ := newActServerWithRecorder(t, map[string]any{
		"class":      []any{"kettle-job"},
		"properties": map[string]any{"id": float64(1)}, // no "state": not judgeable
	})
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "brew", "--yes")
	if code != 0 {
		t.Fatalf("act brew --yes (give-up) exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "gave up watching") {
		t.Errorf("act brew --yes (give-up) stderr missing the note; got:\n%s", errs)
	}
}

func TestActRequiredValueWithoutValuePrintsAndExitsNonZero(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "label-jar", "--yes")
	if code == 0 {
		t.Fatalf("act label-jar --yes (no value) exit = 0, want non-zero; stderr:\n%s", errs)
	}
	if !strings.Contains(errs, "What should the label say?") {
		t.Errorf("act label-jar --yes stderr missing the field label; got:\n%s", errs)
	}
	if got := rec.posts(); len(got) != 0 {
		t.Errorf("act label-jar --yes (no value) sent POSTs %v; a missing value must not send", got)
	}
}

func TestActValueAnswersRequiredField(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, out, errs := execute(t, "act", "label-jar", "--yes", "--value", "hello")
	if code != 0 {
		t.Fatalf("act label-jar --yes --value exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !contains(rec.posts(), "/label") {
		t.Errorf("act label-jar --yes --value did not POST /label; got %v", rec.posts())
	}
	if !strings.Contains(out, "Labelled") {
		t.Errorf("act label-jar --yes --value output missing the result; got:\n%s", out)
	}
}

func TestActFieldSatisfiesRequiredField(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "label-jar", "--yes", "--field", "text=hi")
	if code != 0 {
		t.Fatalf("act label-jar --yes --field exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !contains(rec.posts(), "/label") {
		t.Errorf("act label-jar --yes --field did not POST /label; got %v", rec.posts())
	}
}

func TestActTwoRequiredFieldsRefused(t *testing.T) {
	srv, rec := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "two-required", "--yes")
	if code == 0 {
		t.Fatalf("act two-required --yes exit = 0, want non-zero; stderr:\n%s", errs)
	}
	if !strings.Contains(errs, "more than one field") {
		t.Errorf("act two-required --yes stderr missing the refusal reason; got:\n%s", errs)
	}
	if got := rec.posts(); len(got) != 0 {
		t.Errorf("act two-required --yes sent POSTs %v; a refused action must send nothing", got)
	}
}

func TestActConflictIsRefusedExitCode(t *testing.T) {
	srv, _ := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "broken", "--yes")
	if code != 3 {
		t.Fatalf("act broken --yes (409) exit = %d, want 3 (refused); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "conflict") {
		t.Errorf("act broken --yes stderr missing the conflict kind; got:\n%s", errs)
	}
}

func TestActNotOfferedIsGeneralFailure(t *testing.T) {
	srv, _ := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "no-such-action")
	if code != 1 {
		t.Fatalf("act no-such-action exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "not offered") {
		t.Errorf("act no-such-action stderr missing the message; got:\n%s", errs)
	}
}

func TestActBadFieldIsUsageError(t *testing.T) {
	srv, _ := newActServerWithRecorder(t, nil)
	actBackend(t, srv)

	code, _, errs := execute(t, "act", "peek", "--field", "no-equals-sign")
	if code != 2 {
		t.Fatalf("act --field (bad) exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "invalid --field") {
		t.Errorf("act --field (bad) stderr missing the message; got:\n%s", errs)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
