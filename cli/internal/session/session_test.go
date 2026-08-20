// Ported from flutter/test/services/session_test.dart -- the cases that
// exercise behaviour internal/session itself is responsible for: the
// nav-vs-action dispatch in Activate, and the invoke -> live-watch-or-refetch
// glue internal/action's package comment defers to this package. Every other
// behaviour session_test.dart re-checks (fetching, the four document states,
// the safe/unsafe split, field filling, the bounded 409 retry, the live poll
// filtering) is already pinned in nav_test.go/action's tests/live_test.go
// against the very packages this one composes, so it is not duplicated here --
// only reached through Activate/Answer far enough to prove the composition
// itself is wired correctly.
//
// Matched from test-session.js (via session_test.dart):
// testOpenAndRender/testFollowLinkAndBack/testEmbeddedEntity/
// testPropertyOpensOverlay (Activate's dispatch), testUnsafeActionAsksFirst/
// testSafeActionGoesStraightThrough/testDeclining (Activate reaching action
// and back), testConfirmingInvokesAndRefetches/testConflictRefetchesAndDoesNotRetry/
// testAuthFailureDoesNotRefetch/testWithdrawnAction/testRequiredCheckbox/
// testRequiredFieldIsAskedFor/testNothingHeardSendsNothing (the
// invoke-outcome -> notice/refetch glue), testLiveModeStartsOn202/
// testPollTargetIsRelClassMatch/testProgressIsReportedInTheServersWords/
// testWatchingEndsAndRefetches/testDeadlineComesFromTheServer/
// testNavigatingAwayStopsWatching (the invoke-outcome -> live-watch glue, and
// stopping it on Back), and testSaveAndRunAQuickAction/testQuickDocument/
// testUnsaveableRow/testShortcutToDeletedBackend (saveQuick/runQuick are this
// package's own composition of internal/quick with nav/action).
//
// Deliberately NOT ported from session_test.dart (out of scope in this Go
// port -- see internal/session's package comment):
//
//   - The "setActionPendingCheck wiring" group. The Dart SessionService owns an
//     IdleRefreshClock and wires ActionService.hasPending into it at
//     construction; here the idle-refresh clock is a caller's job, not this
//     package's, so there is no clock to wire and no test to pin it.
//   - The "dispose during an in-flight action outcome (821)" test. With no
//     ChangeNotifier there is nothing to dispose, and with synchronous methods
//     there is no async tail landing after a caller has backed away -- the same
//     reason internal/nav's own tests drop their dispose cases.
//
// Unlike the Dart file's MockClient at the http boundary, the fake here fakes
// at the Get/Request boundary internal/nav, internal/action and internal/live
// each inject (the same shape internal/nav's and internal/live's own tests
// use), so the request keys are the resolved URLs the Dart file's
// '${method} ${url}' produces verbatim -- base is the literal
// "http://pantry.example/" so the keys match exactly. internal/quick has no
// instance to inject (its storage lives in internal/config), so its tests
// point config at a t.TempDir() file exactly the way quick_test.go does.
package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/quick"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
	"github.com/snonux/restforge/cli/internal/urlresolve"
)

const base = "http://pantry.example/"

func testBackend() backend.Backend {
	return backend.Backend{Name: "pantry", BaseURL: base, Secret: "open-sesame"}
}

// k builds the request key the fake routes and records by, the same shape
// the Dart file's '${method} ${url}' produces: method, a space, and the
// resolved URL (base + path).
func k(method, path string) string { return method + " " + base + path }

// --- fixtures (the same documents test-session.js's ROOT/shelves use) ---

func rootDoc() map[string]any {
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
			map[string]any{"rel": []any{"kettle-job"}, "href": "/job"},
		},
		"actions": []any{
			map[string]any{"name": "brew", "title": "Brew a pot of tea", "method": "POST", "href": "/brew", "fields": []any{}},
			map[string]any{
				"name": "cool", "title": "Let the kettle cool", "method": "POST", "href": "/cool",
				"fields": []any{map[string]any{"name": "confirm", "type": "checkbox", "required": true, "title": "The kettle is still hot. Cool it anyway?"}},
			},
			map[string]any{"name": "peek", "title": "Look inside", "method": "GET", "href": "/peek"},
			map[string]any{
				"name": "label", "title": "Label a jar", "method": "POST", "href": "/label",
				"fields": []any{map[string]any{"name": "text", "type": "text", "required": true, "title": "What should the jar say?"}},
			},
		},
	}
}

func shelvesDoc() map[string]any {
	return map[string]any{
		"class":      []any{"shelf-list"},
		"title":      "Shelves",
		"properties": map[string]any{"count": float64(1)},
		"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/shelves"}},
	}
}

// rootNoActions is the root with every action withdrawn, for the tests that
// simulate the server pulling an action between one request and the next.
func rootNoActions() map[string]any {
	return map[string]any{
		"class": []any{"pantry"},
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}
}

func jobBody(overrides map[string]any) map[string]any {
	props := map[string]any{"state": "running", "id": float64(7), "step": "heating the water", "staleAfterSeconds": float64(120)}
	for k, v := range overrides {
		props[k] = v
	}
	return map[string]any{
		"class":      []any{"kettle-job"},
		"properties": props,
		"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/job"}},
	}
}

// route is one canned reply: a status code and a decoded body. status 0
// means 200 (the zero value a test gets when it only sets Body).
type route struct {
	status int
	body   map[string]any
}

// --- a wall-clock-free clock and timer, driven by tick -------------------

type fakeTimer struct {
	clock    *fakeClock
	fireAt   time.Time
	callback func()
	active   bool
}

func (t *fakeTimer) Stop() bool {
	if !t.active {
		return false
	}
	t.active = false
	t.clock.remove(t)
	return true
}

type fakeClock struct {
	now     time.Time
	pending []*fakeTimer
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.UnixMilli(500000)} }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) CreateTimer(d time.Duration, callback func()) live.Timer {
	t := &fakeTimer{clock: c, fireAt: c.now.Add(d), callback: callback, active: true}
	c.pending = append(c.pending, t)
	return t
}

func (c *fakeClock) remove(t *fakeTimer) {
	for i, p := range c.pending {
		if p == t {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return
		}
	}
}

// tick advances the clock by d, firing every timer due at or before the new
// time, earliest first, re-scanning after each firing -- mirrors
// live_service_test.dart's FakeClock.tick (and live_test.go's fakeClock.tick),
// so a fired callback that reschedules a timer still inside this same advance
// is fired too.
func (c *fakeClock) tick(d time.Duration) {
	until := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.pending {
			if !t.fireAt.After(until) && (next == nil || t.fireAt.Before(next.fireAt)) {
				next = t
			}
		}
		if next == nil {
			break
		}
		c.now = next.fireAt
		c.remove(next)
		next.callback()
	}
	c.now = until
}

// --- a fake backend, standing in for internal/httpclient.Client ----------

// fakeClient is nav's httpGetter, action's requester and live's httpGetter
// in one: a route table keyed by "METHOD URL" (the resolved URL), a log of
// what was requested, and the form fields POST bodies carried. Mirrors
// session_test.dart's Env/MockClient, faking at the Get/Request boundary
// rather than the transport the way action's own httptest-based tests do --
// this package is testing the composition, not the transport.
type fakeClient struct {
	routes    map[string]route
	requested []string
	fields    []map[string]string
}

func newFakeClient() *fakeClient {
	return &fakeClient{routes: map[string]route{
		k("GET", ""):        {body: rootDoc()},
		k("GET", "shelves"): {body: shelvesDoc()},
		k("POST", "brew"):   {body: map[string]any{"properties": map[string]any{"state": "done", "id": float64(7)}}},
		k("POST", "cool"):   {body: map[string]any{"properties": map[string]any{"state": "done"}}},
		k("GET", "peek"):    {body: map[string]any{"properties": map[string]any{"seen": true}}},
		k("POST", "label"):  {body: map[string]any{"properties": map[string]any{"state": "done"}}},
	}}
}

func (f *fakeClient) Get(be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	return f.Request(be, href, "GET", nil)
}

// GetContext is nav's httpGetter seam (see n31). ctx is ignored: none of
// this package's tests supersede a fetch mid-flight (that is
// internal/nav's own test suite's job -- see its cancellation tests), so
// there is nothing here to observe cancelling it.
func (f *fakeClient) GetContext(_ context.Context, be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	return f.Get(be, href)
}

func (f *fakeClient) Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
	target := urlresolve.Resolve(href, be.BaseURL)
	key := method + " " + target
	f.requested = append(f.requested, key)
	if fields != nil {
		f.fields = append(f.fields, fields)
	}
	rt, ok := f.routes[key]
	if !ok {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Client, Status: 404, Message: "no such thing here"}
	}
	status := rt.status
	if status == 0 {
		status = 200
	}
	if kind, isFailure := classifyStatus(status); isFailure {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: kind, Status: status, Message: failureMessage(kind, status, rt.body)}
	}
	body := rt.body
	if body == nil {
		body = map[string]any{}
	}
	return httpclient.HTTPResponse{Status: status, URL: target, Entity: body}, nil
}

func (f *fakeClient) reset() {
	f.requested = nil
	f.fields = nil
}

// classifyStatus mirrors httpclient's status-to-Kind map (see
// httpclient.go): 2xx succeeds, 401/403 is Auth, 409 is Conflict, 5xx is
// Server, anything else is Client. Duplicated here only because those are
// unexported in httpclient and this fake fakes above them rather than
// running a real Client that would classify itself.
func classifyStatus(status int) (failure.Kind, bool) {
	switch {
	case status >= 200 && status < 300:
		return 0, false
	case status == 401 || status == 403:
		return failure.Auth, true
	case status == 409:
		return failure.Conflict, true
	case status >= 500:
		return failure.Server, true
	default:
		return failure.Client, true
	}
}

// failureMessage mirrors httpclient's describeFailure: the server's own
// wording (its properties.message) beats anything this fake could invent,
// the same precedence the real client applies.
func failureMessage(kind failure.Kind, status int, body map[string]any) string {
	if props, ok := body["properties"].(map[string]any); ok {
		if msg, ok := props["message"].(string); ok && msg != "" {
			return msg
		}
	}
	switch kind {
	case failure.Auth:
		return "auth rejected"
	case failure.Conflict:
		return "state changed"
	default:
		return fmt.Sprintf("HTTP %d", status)
	}
}

// --- one test's fixture ---------------------------------------------------

type env struct {
	clock   *fakeClock
	fake    *fakeClient
	nav     *nav.Nav
	actions *action.Action
	live    *live.Live
	session *session.Session
}

func newEnv(t *testing.T) *env {
	t.Helper()
	clock := newFakeClock()
	fake := newFakeClient()
	n := nav.New(fake)
	a := action.New(fake, action.WithLog(func(string) {}), action.WithClock(clock.Now))
	l := live.New(fake,
		live.WithNow(clock.Now),
		live.WithCreateTimer(clock.CreateTimer),
		live.WithLog(func(string) {}),
	)
	return &env{clock: clock, fake: fake, nav: n, actions: a, live: l, session: session.New(n, a, l)}
}

func (e *env) reset() { e.fake.reset() }

// openRoot opens the backend and returns the root's row targets keyed by
// label, for tests that press one by label -- mirrors session_test.dart's
// openRoot().
func (e *env) openRoot(t *testing.T) map[string]render.RowTarget {
	t.Helper()
	e.session.OpenBackend(testBackend())
	e.reset()
	out := map[string]render.RowTarget{}
	if doc := e.session.Document(); doc != nil {
		for _, row := range doc.Rows {
			out[row.Label] = row.Target
		}
	}
	return out
}

// openRootRows returns the whole rows keyed by label, for tests that call
// SaveQuick -- which needs the whole row (label and target), not just the
// target Activate needs.
func (e *env) openRootRows(t *testing.T) map[string]render.Row {
	t.Helper()
	e.session.OpenBackend(testBackend())
	e.reset()
	out := map[string]render.Row{}
	if doc := e.session.Document(); doc != nil {
		for _, row := range doc.Rows {
			out[row.Label] = row
		}
	}
	return out
}

// withConfig points config at a fresh temp file and seeds it with backends,
// the same isolation quick_test.go uses -- mirrors session_test.dart's
// settings.saveBackends, adapted to this port's single config file.
func withConfig(t *testing.T, backends []backend.Backend) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "config.toml"))
	if err := config.SaveBackends(backends); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}
}

// withEmptyConfig points config at a fresh, empty temp file, for the
// shortcut tests that touch quick storage (Count/Add) but do not need a
// backend configured -- every test that touches quick must isolate config
// this way, or it would read or write the user's real ~/.config/restforge/
// config.toml the way a prior run of TestSavingPastMaxQuickIsRefused once
// did before this helper existed.
func withEmptyConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "config.toml"))
}

// noticeOfType is a typed getter that fails the test if Notice is not the
// expected concrete notice kind -- the Go equivalent of session_test.dart's
// `notice as ActionOutcomeReported`. The notice kinds are value types, not
// pointers, matching the convention every other closed interface in this
// codebase follows (render.RowTarget, action.AskOutcome).
func noticeOfType[T session.SessionNotice](t *testing.T, s *session.Session) T {
	t.Helper()
	n, ok := s.Notice().(T)
	if !ok {
		t.Fatalf("Notice = %T, want %T", s.Notice(), *new(T))
	}
	return n
}

func questionIsConfirm(t *testing.T, s *session.Session) session.ConfirmQuestion {
	t.Helper()
	q, ok := s.Question().(session.ConfirmQuestion)
	if !ok {
		t.Fatalf("Question = %T, want ConfirmQuestion", s.Question())
	}
	return q
}

func questionIsValue(t *testing.T, s *session.Session) session.ValueQuestion {
	t.Helper()
	q, ok := s.Question().(session.ValueQuestion)
	if !ok {
		t.Fatalf("Question = %T, want ValueQuestion", s.Question())
	}
	return q
}

func docTitle(s *session.Session) string {
	if doc := s.Document(); doc != nil {
		return doc.Title
	}
	return ""
}

// --- Activate dispatches by target type -----------------------------------

func TestFetchTargetFollowsHrefAndShowsDocument(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["shelves"])

	if got, want := e.fake.requested, []string{k("GET", "shelves")}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if docTitle(e.session) != "Shelves" {
		t.Errorf("title = %q, want Shelves", docTitle(e.session))
	}
}

func TestEmbeddedTargetOpensWithoutRequest(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["Top shelf"])

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	if docTitle(e.session) != "Top shelf" {
		t.Errorf("title = %q, want Top shelf", docTitle(e.session))
	}
	if !e.session.CanGoBack() {
		t.Error("CanGoBack = false, want true")
	}
}

func TestDetailTargetOpensForReadingAskingNothing(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["kettle"])

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	d := e.session.Detail()
	if d == nil {
		t.Fatal("Detail = nil, want a value")
	}
	if d.Heading != "kettle" || d.Body != "cold" {
		t.Errorf("Detail = {%q, %q}, want {kettle, cold}", d.Heading, d.Body)
	}
	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry underneath unchanged", docTitle(e.session))
	}
}

func TestDismissDetailClosesItWithoutTouchingNavigation(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["kettle"])

	e.session.DismissDetail()

	if e.session.Detail() != nil {
		t.Error("Detail = non-nil, want nil after DismissDetail")
	}
	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry", docTitle(e.session))
	}
}

func TestBackPopsStackAndStopsAnyLiveWatch(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["shelves"])
	e.reset()

	e.session.Back()

	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry", docTitle(e.session))
	}
	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none: back asks the server nothing", e.fake.requested)
	}
}

// --- Activate on an action target ----------------------------------------

func TestUnsafeActionAsksBeforeActing(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["Brew a pot of tea"])

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	q := questionIsConfirm(t, e.session)
	if q.Heading != "Brew a pot of tea" {
		t.Errorf("ConfirmQuestion.Heading = %q, want the server's wording", q.Heading)
	}
	if e.session.Document() == nil {
		t.Error("Document = nil, want the document still underneath")
	}
}

func TestSafeActionGoesStraightThrough(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["Look inside"])

	// Not gated by a confirmation -- but still followed by the same
	// unconditional re-fetch every invoked action gets: "never carry a
	// document across an action" makes no exception for a safe one.
	if got := e.fake.requested; len(got) == 0 || got[0] != k("GET", "peek") {
		t.Errorf("requested = %v, want first %q", got, k("GET", "peek"))
	}
	foundRoot := false
	for _, r := range e.fake.requested {
		if r == k("GET", "") {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Errorf("requested = %v, want it to contain the re-fetch %q", e.fake.requested, k("GET", ""))
	}
	if e.session.Question() != nil {
		t.Error("Question = non-nil, want nil: a safe action does not ask")
	}
}

func TestDecliningSendsNothingAndClearsQuestion(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()

	e.session.Answer(false)

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	if e.session.Question() != nil {
		t.Error("Question = non-nil, want nil")
	}
	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry", docTitle(e.session))
	}
}

func TestConfirmingInvokesAndRefetches(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()

	e.session.Answer(true)

	if got, want := e.fake.requested, []string{k("POST", "brew"), k("GET", "")}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	notice := noticeOfType[session.ActionOutcomeReported](t, e.session)
	if notice.Heading != "Brew a pot of tea" {
		t.Errorf("Heading = %q, want the action's label", notice.Heading)
	}
	if notice.Message != "done" {
		t.Errorf("Message = %q, want done (the server's own state)", notice.Message)
	}
	if !contains(notice.Body, "id: 7") {
		t.Errorf("Body = %q, want it to contain the rendered id", notice.Body)
	}
	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry", docTitle(e.session))
	}
}

func TestRequiredCheckboxIsTheConfirmationAndFillsTheField(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)

	e.session.Activate(rows["Let the kettle cool"])
	q := questionIsConfirm(t, e.session)
	if q.Body != "The kettle is still hot. Cool it anyway?" {
		t.Errorf("ConfirmQuestion.Body = %q, want the checkbox's title", q.Body)
	}
	e.reset()
	e.session.Answer(true)

	if len(e.fake.fields) == 0 || e.fake.fields[0]["confirm"] != "true" {
		t.Errorf("fields = %v, want the checkbox filled from the confirmation", e.fake.fields)
	}
}

func TestConflictRefetchesInsteadOfRetrying(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.fake.routes[k("POST", "brew")] = route{status: 409, body: map[string]any{"properties": map[string]any{"message": "a brew is already running"}}}
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()

	e.session.Answer(true)

	if got, want := e.fake.requested, []string{k("POST", "brew"), k("GET", "")}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requested = %v, want %v (a conflict re-fetches, never retries)", got, want)
	}
	notice := noticeOfType[session.ActionFailed](t, e.session)
	if notice.Failure.Kind != failure.Conflict {
		t.Errorf("Failure.Kind = %v, want conflict", notice.Failure.Kind)
	}
	if notice.Failure.Message != "a brew is already running" {
		t.Errorf("Failure.Message = %q, want the server's message", notice.Failure.Message)
	}
}

func TestAuthFailureDoesNotRefetch(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.fake.routes[k("POST", "brew")] = route{status: 401, body: map[string]any{"properties": map[string]any{"message": "API key rejected"}}}
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()

	e.session.Answer(true)

	if got := e.fake.requested; len(got) != 1 || got[0] != k("POST", "brew") {
		t.Errorf("requested = %v, want only the POST: auth tells us nothing new about the document", got)
	}
	notice := noticeOfType[session.ActionFailed](t, e.session)
	if notice.Failure.Kind != failure.Auth {
		t.Errorf("Failure.Kind = %v, want auth", notice.Failure.Kind)
	}
	if docTitle(e.session) != "The pantry" {
		t.Errorf("title = %q, want The pantry unchanged", docTitle(e.session))
	}
}

func TestAnsweringAQuestionTheServerWithdrewSendsNothing(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["Brew a pot of tea"])
	// The confirmation is pending; now the document changes underneath it,
	// exactly as if the server withdrew the action.
	e.fake.routes[k("GET", "")] = route{body: rootNoActions()}
	e.session.Refresh()
	e.reset()

	e.session.Answer(true)

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none: the action is no longer offered", e.fake.requested)
	}
}

func TestRequiredFieldWithNoDefaultIsAskedForOutLoud(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	// "label" is a POST, so it asks to confirm before fillFields ever gets
	// a chance to notice the missing field -- mirrors test-session.js
	// calling session.answer(true) before the value prompt appears.
	e.session.Activate(rows["Label a jar"])
	questionIsConfirm(t, e.session)

	e.session.Answer(true)
	q := questionIsValue(t, e.session)
	if q.Label != "What should the jar say?" {
		t.Errorf("ValueQuestion.Label = %q, want the field's title", q.Label)
	}

	e.reset()
	e.session.AnswerValue("plum jam")
	if len(e.fake.fields) == 0 || e.fake.fields[0]["text"] != "plum jam" {
		t.Errorf("fields = %v, want the spoken value filled in", e.fake.fields)
	}
}

func TestEmptyAnswerToValueQuestionSendsNothing(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.session.Activate(rows["Label a jar"])
	e.session.Answer(true)
	e.reset()

	e.session.AnswerValue("")

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	notice := noticeOfType[session.ActionRefused](t, e.session)
	if !contains(notice.Reason, "Nothing was heard") {
		t.Errorf("Reason = %q, want it to mention nothing was heard", notice.Reason)
	}
}

func TestActionServerNoLongerOffersIsReportedNotGuessed(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.fake.routes[k("GET", "")] = route{body: rootNoActions()}
	e.session.Refresh()
	e.reset()

	e.session.Activate(rows["Brew a pot of tea"])

	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none", e.fake.requested)
	}
	if _, ok := e.session.Notice().(session.ActionWithdrawn); !ok {
		t.Fatalf("Notice = %T, want ActionWithdrawn", e.session.Notice())
	}
}

// --- live mode: the invoke-outcome-to-watch glue -------------------------

// startBrew: a 202 with a running job, watched from there on -- mirrors
// test-session.js's startBrew().
func startBrew(t *testing.T, e *env) map[string]render.RowTarget {
	t.Helper()
	rows := e.openRoot(t)
	e.fake.routes[k("POST", "brew")] = route{status: 202, body: jobBody(nil)}
	e.fake.routes[k("GET", "job")] = route{body: jobBody(nil)}
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()
	e.session.Answer(true)
	return rows
}

func Test202StartsAWatchInsteadOfCallingItDone(t *testing.T) {
	e := newEnv(t)
	startBrew(t, e)

	if got, want := e.fake.requested, []string{k("POST", "brew")}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want only the POST: a still-running job is not re-fetched as if done", got)
	}
	if !e.session.IsLive() {
		t.Error("IsLive = false, want true")
	}
	notice := noticeOfType[session.ActionOutcomeReported](t, e.session)
	// The fixture's job body carries a "state", which live.ResultText prefers
	// over the 202-derived "Accepted" fallback -- the server's own word beats
	// a generic one whenever it gave one.
	if notice.Message != "running" {
		t.Errorf("Message = %q, want running", notice.Message)
	}
}

func TestJobPolledByMatchingClassAgainstOriginLinks(t *testing.T) {
	e := newEnv(t)
	startBrew(t, e)
	e.reset()

	e.clock.tick(live.PollInterval)

	if got, want := e.fake.requested, []string{k("GET", "job")}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
}

func TestProgressReportedInServersOwnWords(t *testing.T) {
	e := newEnv(t)
	startBrew(t, e)
	e.fake.routes[k("GET", "job")] = route{body: jobBody(map[string]any{"step": "steeping"})}
	e.reset()

	e.clock.tick(live.PollInterval)

	notice := noticeOfType[session.ActionProgress](t, e.session)
	if notice.Step != "steeping" {
		t.Errorf("Step = %q, want steeping", notice.Step)
	}
	if !e.session.IsLive() {
		t.Error("IsLive = false, want true: still watching")
	}
}

func TestWatchingEndsAndOriginDocumentIsRefetched(t *testing.T) {
	e := newEnv(t)
	startBrew(t, e)
	e.fake.routes[k("GET", "job")] = route{body: jobBody(map[string]any{"state": "done", "step": ""})}
	e.reset()

	e.clock.tick(live.PollInterval)

	if got, want := e.fake.requested, []string{k("GET", "job"), k("GET", "")}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	notice := noticeOfType[session.ActionOutcomeReported](t, e.session)
	if notice.Message != "done" {
		t.Errorf("Message = %q, want done", notice.Message)
	}
	if e.session.IsLive() {
		t.Error("IsLive = true, want false: the watch is over")
	}
}

func TestGivingUpIsReportedAsGivingUpAndStillRefetches(t *testing.T) {
	e := newEnv(t)
	rows := e.openRoot(t)
	e.fake.routes[k("POST", "brew")] = route{status: 202, body: jobBody(map[string]any{"staleAfterSeconds": float64(1)})}
	e.fake.routes[k("GET", "job")] = route{body: jobBody(map[string]any{"staleAfterSeconds": float64(1)})}
	e.session.Activate(rows["Brew a pot of tea"])
	e.reset()
	e.session.Answer(true)

	// 1s of budget plus the 60s buffer: a poll at 70s is outside it.
	e.clock.tick(70 * time.Second)

	if _, ok := e.session.Notice().(session.ActionGaveUp); !ok {
		t.Fatalf("Notice = %T, want ActionGaveUp", e.session.Notice())
	}
	if e.session.IsLive() {
		t.Error("IsLive = true, want false")
	}
	if last := e.fake.requested[len(e.fake.requested)-1]; last != k("GET", "") {
		t.Errorf("last request = %q, want the re-fetch %q: giving up still re-reads the document", last, k("GET", ""))
	}
}

func TestLeavingWatchedDocumentStopsTheWatch(t *testing.T) {
	e := newEnv(t)
	startBrew(t, e)
	e.reset()

	e.session.Back()
	e.clock.tick(30 * time.Second)

	for _, r := range e.fake.requested {
		if contains(r, "job") {
			t.Errorf("requested %q: nothing polls a job for a screen nobody is looking at", r)
		}
	}
}

// --- saved shortcuts (SaveQuick/RunQuick) --------------------------------

func TestSavingActionAsksNothingRunningRereadsHolderAndStillAsks(t *testing.T) {
	e := newEnv(t)
	rows := e.openRootRows(t)
	withConfig(t, []backend.Backend{testBackend()})

	outcome, err := e.session.SaveQuick(rows["Brew a pot of tea"])
	if err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	if outcome != session.QuickSaveSaved {
		t.Errorf("SaveQuick = %v, want saved", outcome)
	}
	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none: saving asks the server nothing", e.fake.requested)
	}

	saved, err := quick.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("loaded %d shortcuts, want 1", len(saved))
	}
	e.reset()

	runOutcome, err := e.session.RunQuick(saved[0])
	if err != nil {
		t.Fatalf("RunQuick error = %v", err)
	}
	if runOutcome != session.QuickRunOpened {
		t.Errorf("RunQuick = %v, want opened", runOutcome)
	}
	if got, want := e.fake.requested, []string{k("GET", "")}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v: the holder is re-read so the action is looked up by name in a current document", got, want)
	}
	if _, ok := e.session.Question().(session.ConfirmQuestion); !ok {
		t.Errorf("Question = %T, want ConfirmQuestion: it still asks before acting, exactly as a hand-pressed action would", e.session.Question())
	}
	for _, r := range e.fake.requested {
		if startsWith(r, "POST") {
			t.Errorf("requested %q: the press alone sends nothing", r)
		}
	}

	e.reset()
	e.session.Answer(true)
	if len(e.fake.requested) == 0 || e.fake.requested[0] != k("POST", "brew") {
		t.Errorf("requested = %v, want first %q: confirming is what invokes it", e.fake.requested, k("POST", "brew"))
	}
}

func TestSavingLinkRunningFetchesDirectlyWithNothingToGoBackTo(t *testing.T) {
	e := newEnv(t)
	rows := e.openRootRows(t)
	withConfig(t, []backend.Backend{testBackend()})

	outcome, err := e.session.SaveQuick(rows["shelves"])
	if err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	if outcome != session.QuickSaveSaved {
		t.Errorf("SaveQuick = %v, want saved", outcome)
	}

	saved, err := quick.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("loaded %d shortcuts, want 1", len(saved))
	}
	e.reset()

	runOutcome, err := e.session.RunQuick(saved[0])
	if err != nil {
		t.Fatalf("RunQuick error = %v", err)
	}
	if runOutcome != session.QuickRunOpened {
		t.Errorf("RunQuick = %v, want opened", runOutcome)
	}
	if got, want := e.fake.requested, []string{k("GET", "shelves")}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if docTitle(e.session) != "Shelves" {
		t.Errorf("title = %q, want Shelves", docTitle(e.session))
	}
	if e.session.CanGoBack() {
		t.Error("CanGoBack = true, want false: straight to the destination, nothing was walked")
	}
}

func TestPropertyRowCannotBeAShortcut(t *testing.T) {
	withEmptyConfig(t)
	e := newEnv(t)
	rows := e.openRootRows(t)

	outcome, err := e.session.SaveQuick(rows["kettle"])
	if err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	if outcome != session.QuickSaveNotSaveable {
		t.Errorf("SaveQuick = %v, want notSaveable", outcome)
	}
	if count, _ := quick.Count(); count != 0 {
		t.Errorf("Count = %d, want 0: nothing is stored", count)
	}
}

func TestEmbeddedSubEntityRowCannotBeAShortcut(t *testing.T) {
	withEmptyConfig(t)
	e := newEnv(t)
	rows := e.openRootRows(t)

	outcome, err := e.session.SaveQuick(rows["Top shelf"])
	if err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	if outcome != session.QuickSaveNotSaveable {
		t.Errorf("SaveQuick = %v, want notSaveable", outcome)
	}
	if count, _ := quick.Count(); count != 0 {
		t.Errorf("Count = %d, want 0: nothing is stored", count)
	}
}

func TestShortcutToDeletedBackendExplainsItself(t *testing.T) {
	e := newEnv(t)
	rows := e.openRootRows(t)
	withConfig(t, []backend.Backend{testBackend()})
	if _, err := e.session.SaveQuick(rows["shelves"]); err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	saved, err := quick.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}

	other := backend.Backend{Name: "other", BaseURL: "http://elsewhere.example/", Secret: "x"}
	withConfig(t, []backend.Backend{other})
	e.reset()

	outcome, err := e.session.RunQuick(saved[0])
	if err != nil {
		t.Fatalf("RunQuick error = %v", err)
	}
	if outcome != session.QuickRunBackendMissing {
		t.Errorf("RunQuick = %v, want backendMissing", outcome)
	}
	if len(e.fake.requested) != 0 {
		t.Errorf("requested = %v, want none: it does not reach for the old server", e.fake.requested)
	}
}

func TestSavingPastMaxQuickIsRefused(t *testing.T) {
	withEmptyConfig(t)
	e := newEnv(t)
	rows := e.openRootRows(t)
	for i := 0; i < quick.MaxQuick; i++ {
		if _, err := quick.Add(quick.QuickItem{
			Label:   fmt.Sprintf("n%d", i),
			BaseURL: base,
			Kind:    quick.KindDocument,
			Href:    fmt.Sprintf("/n%d", i),
		}); err != nil {
			t.Fatalf("Add error = %v", err)
		}
	}

	outcome, err := e.session.SaveQuick(rows["shelves"])
	if err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	if outcome != session.QuickSaveFull {
		t.Errorf("SaveQuick = %v, want full", outcome)
	}
	if count, _ := quick.Count(); count != quick.MaxQuick {
		t.Errorf("Count = %d, want %d", count, quick.MaxQuick)
	}
}

func TestActionShortcutWhoseActionWithdrewReportsThat(t *testing.T) {
	e := newEnv(t)
	rows := e.openRootRows(t)
	withConfig(t, []backend.Backend{testBackend()})
	if _, err := e.session.SaveQuick(rows["Brew a pot of tea"]); err != nil {
		t.Fatalf("SaveQuick error = %v", err)
	}
	saved, err := quick.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	// The next fetch of the holder no longer offers "brew" at all.
	e.fake.routes[k("GET", "")] = route{body: rootWithActions([]any{})}
	e.reset()

	outcome, err := e.session.RunQuick(saved[0])
	if err != nil {
		t.Fatalf("RunQuick error = %v", err)
	}
	if outcome != session.QuickRunOpened {
		t.Errorf("RunQuick = %v, want opened", outcome)
	}
	if _, ok := e.session.Notice().(session.ActionWithdrawn); !ok {
		t.Fatalf("Notice = %T, want ActionWithdrawn", e.session.Notice())
	}
	if e.session.Question() != nil {
		t.Error("Question = non-nil, want nil: a withdrawn action is a real answer, not a question")
	}
}

// rootWithActions returns the root with its actions replaced by actions (nil
// for the default set, an empty slice for "all withdrawn").
func rootWithActions(actions []any) map[string]any {
	doc := rootDoc()
	if actions == nil {
		actions = doc["actions"].([]any)
	}
	doc["actions"] = actions
	return doc
}

// --- small string helpers (avoid pulling in strings just for two calls) --

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
