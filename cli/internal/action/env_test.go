package action_test

// Test harness shared by every scenario test in this package: a fake
// backend recording every request it received, mirroring
// action_service_test.dart's Env (route table plus a request/body log) and
// its testBackend/root/labelDoc fixtures. Kept in its own file so each
// _test.go carrying actual test functions stays focused on the group it
// tests, the same split action_service_test.dart's own file achieves with
// named groups instead of separate files.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// route is one canned reply, keyed by "METHOD /path" in env's route table.
//
// advanceClockBy, when set, moves env's clock forward by that much while
// this route is being served -- after the request is recorded but before
// the response is returned -- so a test can place the moment
// Action.retryable checks a confirmation's age on either side of the
// confirmation retry TTL, the same way a slow server's response arriving
// late would. Used only by the "bounded 409 retry" tests; every other test
// leaves it zero and the clock never moves. Mirrors action_service_test.dart's
// Route.
type route struct {
	status         int
	body           string
	advanceClockBy time.Duration
}

// env is a fake backend recording every request it received, mirroring
// action_service_test.dart's Env.
//
// sequences, when a key is present, serves successive routes to
// successive requests for that key (the last one repeating once
// exhausted) -- mirrors the Dart test's own use in the "bounded 409 retry"
// group, without this file needing to patch anything at the transport
// layer to make a conflict's *retry* succeed.
type env struct {
	mu        sync.Mutex
	routes    map[string]route
	sequences map[string][]route
	calls     map[string]int
	clock     time.Time
	requested []string
	bodies    []string

	srv     *httptest.Server
	backend backend.Backend
	action  *action.Action
}

// newEnv starts a loopback server and wires an *action.Action to it,
// seeded with defaultRoutes() overridden by routes, plus sequences for the
// bounded-retry tests. The clock starts at a fixed instant (mirroring the
// Dart fixture) and only ever moves via a route's advanceClockBy.
func newEnv(t *testing.T, routes map[string]route, sequences map[string][]route) *env {
	t.Helper()
	e := &env{
		routes:    mergeRoutes(defaultRoutes(), routes),
		sequences: sequences,
		calls:     map[string]int{},
		clock:     time.UnixMilli(1700000000000),
	}
	e.srv = httptest.NewServer(http.HandlerFunc(e.handle))
	t.Cleanup(e.srv.Close)
	e.backend = backend.Backend{
		Name:       "pantry",
		BaseURL:    e.srv.URL + "/",
		AuthHeader: "X-API-Key",
		Secret:     "open-sesame",
	}
	e.action = action.New(httpclient.New(), action.WithLog(func(string) {}), action.WithClock(e.now))
	return e
}

// defaultRoutes mirrors the base entries action_service_test.dart's Env
// constructor seeds before a test's overrides are applied.
func defaultRoutes() map[string]route {
	return map[string]route{
		"POST /brew":  {status: 200, body: `{"properties":{"state":"done","id":7}}`},
		"POST /cool":  {status: 200, body: `{"properties":{"state":"done"}}`},
		"GET /peek":   {status: 200, body: `{"properties":{"seen":true}}`},
		"POST /label": {status: 200, body: `{"properties":{"state":"done"}}`},
	}
}

func mergeRoutes(base, overrides map[string]route) map[string]route {
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

func (e *env) now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

func (e *env) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	body, _ := io.ReadAll(r.Body)

	e.mu.Lock()
	e.requested = append(e.requested, key)
	e.bodies = append(e.bodies, string(body))
	rt, ok := e.routeFor(key)
	if ok && rt.advanceClockBy != 0 {
		e.clock = e.clock.Add(rt.advanceClockBy)
	}
	e.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"properties":{"message":"no such thing here"}}`))
		return
	}
	w.WriteHeader(rt.status)
	_, _ = w.Write([]byte(rt.body))
}

// routeFor picks key's next route: from sequences if one was given for it
// (advancing that key's call count, clamped to the last entry once
// exhausted), otherwise the fixed entry in routes. Caller holds e.mu.
func (e *env) routeFor(key string) (route, bool) {
	if seq, ok := e.sequences[key]; ok && len(seq) > 0 {
		idx := e.calls[key]
		e.calls[key]++
		if idx >= len(seq) {
			idx = len(seq) - 1
		}
		return seq[idx], true
	}
	rt, ok := e.routes[key]
	return rt, ok
}

func (e *env) requestedKeys() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.requested))
	copy(out, e.requested)
	return out
}

func (e *env) sentBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *env) postCount() int {
	n := 0
	for _, r := range e.requestedKeys() {
		if strings.HasPrefix(r, "POST") {
			n++
		}
	}
	return n
}

// rootEntity mirrors action_service_test.dart's root() fixture: an action
// with no fields ("brew"), one with a checkbox that carries the
// confirmation ("cool"), and a safe one ("peek", defaulting to GET). Pass
// nil for the default actions, or an explicit (possibly empty) slice to
// simulate the server withdrawing an action.
func rootEntity(actions []siren.Action) siren.Entity {
	if actions == nil {
		actions = defaultActions()
	}
	return siren.Entity{
		Classes:    []string{"pantry"},
		Title:      "The pantry",
		Properties: map[string]any{"apiVersion": float64(1), "kettle": "cold"},
		Links:      []siren.Link{{Rel: []string{"self"}, Href: "/"}},
		Actions:    actions,
	}
}

func defaultActions() []siren.Action {
	return []siren.Action{
		{Name: "brew", Title: "Brew a pot of tea", Method: "POST", Href: "/brew"},
		{
			Name:   "cool",
			Title:  "Let the kettle cool",
			Method: "POST",
			Href:   "/cool",
			Fields: []siren.Field{
				{Name: "confirm", Type: "checkbox", Required: true, Title: "The kettle is still hot. Cool it anyway?"},
			},
		},
		{Name: "peek", Title: "Look inside", Href: "/peek", Method: "GET"},
	}
}

// labelDoc mirrors action_service_test.dart's labelDoc() fixture: a single
// action ("label") whose fields are supplied by each field-filling test.
func labelDoc(fields []siren.Field) siren.Entity {
	return siren.Entity{
		Classes: []string{"pantry"},
		Title:   "The pantry",
		Links:   []siren.Link{{Rel: []string{"self"}, Href: "/"}},
		Actions: []siren.Action{
			{Name: "label", Title: "Label a jar", Method: "POST", Href: "/label", Fields: fields},
		},
	}
}
