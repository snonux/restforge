// Integration test: drives the cli client's SERVICE layer -- internal/session,
// composing internal/nav/action/live/httpclient -- against the real fixture
// Siren API (pebble/tools/fake-siren-server.py) over real HTTP on localhost,
// plus at least one restforge get and one restforge act invocation run as a
// subprocess so the binary's argument parsing and output formatting are
// exercised end to end too. No mocks at the HTTP boundary, no terminal.
//
// Ported from flutter/test/integration/browse_confirm_act_refetch_test.dart,
// whose header comment carries the reasoning for everything that carries over
// unchanged: the fixture server is one Python process holding mutable state
// (a kettle, a brew count, a running job) with no reset endpoint, so the
// awkward cases below are woven into one ordered scenario rather than
// independent cases that would race each other; a 401 (wrong key), a 409
// (two confirmed brews racing), a non-JSON response (/notjson), a request
// that outruns the read timeout (/slow, against a shortened budget so the
// test does not wait out the 20s production default), a job that runs for a
// genuine 24 seconds server-side (watched through internal/live, polled
// faster than the 10s production default so the wait is bounded), and a
// required field only a human can fill (label-jar's "text").
//
// The one departure from the Dart port is the timer: the Dart test injects a
// real 2s Timer because Dart's event loop is single-threaded, so the
// LiveService callback runs on the same thread that reads session state --
// no race. Go's live.Start fires its poll on the timer's goroutine, so a real
// timer here would race the test's reads (internal/session's package comment
// spells this out, and its own tests inject a hand-driven fake for the same
// reason). This test injects a manual scheduler -- createTimer records the
// callback, tick() fires it synchronously on the test goroutine -- and drives
// ticks while sleeping real time, so every poll and callback runs on the test
// goroutine and there is no data race, while the server's own 24s clock (which
// the test cannot and should not fake) still gates the job's progress.
package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

const fixtureSecret = "open-sesame"

// moduleRoot is the cli/ directory (the module root), resolved from this
// test file's location so `go build ./cmd/restforge` and locating the fixture
// script run from the right place regardless of where `go test` was invoked.
var moduleRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}()

// fixtureScript locates pebble/tools/fake-siren-server.py by walking up from
// moduleRoot (cli/), one level to the repo root that also holds pebble/.
func fixtureScript() (string, error) {
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(moduleRoot, strings.Repeat("..", i), "pebble", "tools", "fake-siren-server.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find pebble/tools/fake-siren-server.py above %s", moduleRoot)
}

// fixtureProcess, baseURL and binPath are set up once by TestMain and shared
// by every test (the fixture is one process with shared mutable state).
var (
	fixtureProcess *exec.Cmd
	baseURL        string
	binPath        string
	configPath     string
)

// manualScheduler is a hand-driven stand-in for time.AfterFunc: createTimer
// records the callback instead of scheduling it, and tick() fires the most
// recently created timer synchronously on the caller's goroutine. Driving
// ticks from the test keeps every live poll and its session callbacks on the
// test goroutine, so the 24s real-time watch below has no data race -- see the
// package comment.
type manualScheduler struct {
	mu      sync.Mutex
	current *manualTimer
}

type manualTimer struct {
	scheduler *manualScheduler
	callback  func()
	active    bool
}

func (s *manualScheduler) create(_ time.Duration, callback func()) live.Timer {
	t := &manualTimer{scheduler: s, callback: callback, active: true}
	s.mu.Lock()
	s.current = t
	s.mu.Unlock()
	return t
}

// tick fires the current timer, if any, on this goroutine.
func (s *manualScheduler) tick() {
	s.mu.Lock()
	t := s.current
	s.current = nil
	s.mu.Unlock()
	if t != nil && t.active {
		t.active = false
		t.callback()
	}
}

func (t *manualTimer) Stop() bool {
	was := t.active
	t.active = false
	return was
}

// TestMain starts the fixture server, writes a one-backend config the
// subprocess tests read, builds the restforge binary, runs the suite, then
// tears it all down -- so `go test ./...` is one command with no manual
// setup, matching flutter/AGENTS.md's "neither needs an emulator" bar.
func TestMain(m *testing.M) {
	script, err := fixtureScript()
	if err != nil {
		panic(err)
	}
	port, err := freePort()
	if err != nil {
		panic(err)
	}
	baseURL = fmt.Sprintf("http://127.0.0.1:%d/", port)

	cmd := exec.Command("python3", script, fmt.Sprintf("%d", port))
	cmd.Stderr = newLineDrainer()
	cmd.Stdout = io.Discard // the fixture logs to stderr; stdout is the HTTP socket
	if err := cmd.Start(); err != nil {
		panic(fmt.Errorf("could not start fixture: %w", err))
	}
	fixtureProcess = cmd
	if err := waitForReady(cmd.Stderr.(*lineDrainer), 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		panic(err)
	}

	// A config the subprocess get/act tests read through RESTFORGE_CONFIG.
	configPath = filepath.Join(tempDir, "config.toml")
	_ = os.Setenv(config.ConfigEnvVar, configPath)
	if err := config.SaveBackends([]backend.Backend{
		{Name: "fixture", BaseURL: baseURL, Secret: fixtureSecret},
	}); err != nil {
		killFixture()
		panic(err)
	}

	// Build the binary once, shared by the subprocess tests.
	binPath = filepath.Join(tempDir, "restforge")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/restforge")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		killFixture()
		panic(fmt.Errorf("go build: %v\n%s", err, out))
	}

	code := m.Run()
	killFixture()
	_ = os.Unsetenv(config.ConfigEnvVar)
	os.Exit(code)
}

// tempDir returns a process-wide temp dir for the config and binary.
var tempDir = func() string {
	dir, err := os.MkdirTemp("", "restforge-integration-")
	if err != nil {
		panic(err)
	}
	return dir
}()

func killFixture() {
	if fixtureProcess != nil && fixtureProcess.Process != nil {
		_ = fixtureProcess.Process.Kill()
		_, _ = fixtureProcess.Process.Wait()
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// lineDrainer collects stderr lines so waitForReady can poll for the fixture's
// startup banner, and keeps the pipe drained for the whole run so the child
// never blocks on a full pipe.
type lineDrainer struct {
	mu    sync.Mutex
	lines []string
	rest  bytes.Buffer
}

func newLineDrainer() *lineDrainer { return &lineDrainer{} }

func (d *lineDrainer) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rest.Write(p)
	for {
		line, err := d.rest.ReadBytes('\n')
		if err != nil {
			d.rest.Write(line) // put back the incomplete line
			break
		}
		d.lines = append(d.lines, strings.TrimRight(string(line), "\n"))
	}
	return len(p), nil
}

func (d *lineDrainer) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.lines...)
}

func waitForReady(d *lineDrainer, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		for _, line := range d.snapshot() {
			if strings.Contains(line, "fixture Siren server on") {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("fixture did not report ready in %s; stderr so far:\n%s", budget, strings.Join(d.snapshot(), "\n"))
}

// newSession builds the production stack with shortened timeouts and a
// hand-driven timer, returning the session and its scheduler so the test can
// drive the watch ticks itself.
func newSession(t *testing.T) (*session.Session, *manualScheduler) {
	t.Helper()
	http := httpclient.New(
		httpclient.WithGetTimeout(4*time.Second),
		httpclient.WithActionTimeout(8*time.Second),
	)
	sched := &manualScheduler{}
	n := nav.New(http)
	a := action.New(http, action.WithLog(func(string) {}))
	l := live.New(http,
		live.WithCreateTimer(sched.create),
		live.WithLog(func(string) {}),
	)
	return session.New(n, a, l), sched
}

// findRow returns the first row of doc with the given kind and label, or
// fails the test with a dump of what was actually there -- a bare "not found"
// gives no clue which fetch produced the document being searched.
func findRow(t *testing.T, doc *render.RenderedDocument, kind render.RowKind, label, what string) render.Row {
	t.Helper()
	for _, row := range doc.Rows {
		if row.Kind == kind && row.Label == label {
			return row
		}
	}
	t.Fatalf("no row for %s among: %s", what, rowsSummary(doc))
	return render.Row{}
}

func rowsSummary(doc *render.RenderedDocument) string {
	parts := make([]string, 0, len(doc.Rows))
	for _, r := range doc.Rows {
		parts = append(parts, fmt.Sprintf("%d:%s", r.Kind, r.Label))
	}
	return strings.Join(parts, ", ")
}

// propertyValue finds the property row named label and returns its sublabel
// (the rendered value), failing the test if it is absent.
func propertyValue(t *testing.T, doc *render.RenderedDocument, label string) string {
	t.Helper()
	row := findRow(t, doc, render.RowKindProperty, label, label+" property")
	return row.Sublabel
}

// actionLabels is the set of action-row labels on doc, for asserting which
// actions the server currently offers.
func actionLabels(doc *render.RenderedDocument) map[string]bool {
	out := make(map[string]bool)
	for _, row := range doc.Rows {
		if row.Kind == render.RowKindAction {
			out[row.Label] = true
		}
	}
	return out
}

func TestBrowseConfirmActRefetch(t *testing.T) {
	// === Awkward case: a 401 for a wrong key ==============================
	// A throwaway session with the wrong secret, run before the main session
	// opens so nothing about it depends on state the happy path creates.
	wrong, _ := newSession(t)
	wrong.OpenBackend(backend.Normalise(backend.Backend{Name: "wrong-key", BaseURL: baseURL, Secret: "not-the-right-key"}))
	if wrong.State() != nav.StateError {
		t.Fatalf("wrong-secret state = %v, want StateError", wrong.State())
	}
	f := wrong.Failure()
	if f == nil || f.Kind != failure.Auth || f.Status != 401 || f.Message != "API key rejected" {
		t.Fatalf("wrong-secret failure = %+v, want Auth/401/\"API key rejected\"", f)
	}
	if wrong.Document() != nil {
		t.Errorf("wrong-secret document should be nil, got %v", wrong.Document().Title)
	}

	s, sched := newSession(t)
	s.OpenBackend(backend.Normalise(backend.Backend{Name: "fixture", BaseURL: baseURL, Secret: fixtureSecret}))
	if s.State() != nav.StateOK {
		t.Fatalf("open state = %v, want StateOK", s.State())
	}
	doc := s.Document()
	if doc.Title != "The pantry" {
		t.Fatalf("root title = %q, want %q", doc.Title, "The pantry")
	}
	if got := propertyValue(t, doc, "kettle"); got != "cold" {
		t.Fatalf("kettle = %q, want %q", got, "cold")
	}
	labels := actionLabels(doc)
	if !labels["Brew a pot of tea"] || !labels["Write a label for a jar"] {
		t.Errorf("root actions = %v, want brew and label-jar", labels)
	}
	if labels["Let the kettle cool"] {
		t.Errorf("cool-down offered before the kettle is hot")
	}

	// === Open a property =================================================
	kettleRow := findRow(t, doc, render.RowKindProperty, "kettle", "kettle property")
	s.Activate(kettleRow.Target)
	if s.Detail() == nil || s.Detail().Heading != "kettle" || s.Detail().Body != "cold" {
		t.Fatalf("detail = %+v, want heading=kettle body=cold", s.Detail())
	}
	s.DismissDetail()
	if s.Detail() != nil {
		t.Errorf("detail not cleared")
	}

	// === Follow a link, then back ========================================
	shelvesRow := findRow(t, doc, render.RowKindLink, "shelves", "shelves link")
	s.Activate(shelvesRow.Target)
	if s.State() != nav.StateOK || s.Document().Title != "Shelves" {
		t.Fatalf("after shelves: state=%v title=%q", s.State(), s.Document().Title)
	}
	if !s.CanGoBack() {
		t.Errorf("canGoBack false after following a link")
	}
	s.Back()
	if s.CanGoBack() {
		t.Errorf("canGoBack true at the root")
	}
	if s.Document().Title != "The pantry" {
		t.Fatalf("after back: title=%q, want The pantry", s.Document().Title)
	}

	// === Awkward case: a required field only a human can fill ============
	doc = s.Document()
	labelRow := findRow(t, doc, render.RowKindAction, "Write a label for a jar", "label-jar action")
	s.Activate(labelRow.Target)
	if _, ok := s.Question().(session.ConfirmQuestion); !ok {
		t.Fatalf("question = %T, want ConfirmQuestion", s.Question())
	}
	s.Answer(true)
	vq, ok := s.Question().(session.ValueQuestion)
	if !ok || vq.Label != "What should the label say?" {
		t.Fatalf("question = %+v, want ValueQuestion label %q", s.Question(), "What should the label say?")
	}
	s.AnswerValue("Earl Grey")
	if _, ok := s.Notice().(session.ActionOutcomeReported); !ok {
		t.Fatalf("notice = %T, want ActionOutcomeReported", s.Notice())
	}
	if s.IsLive() {
		t.Errorf("a 200 with no state should not be watched")
	}
	if s.State() != nav.StateOK {
		t.Errorf("state = %v, want StateOK (re-fetched, not the POST reply)", s.State())
	}

	// === Awkward case: a response that is not JSON at all =================
	s.Activate(render.FetchTarget{Href: "/notjson"})
	if s.State() != nav.StateError {
		t.Fatalf("notjson state = %v, want StateError", s.State())
	}
	f = s.Failure()
	if f == nil || f.Kind != failure.Parse || f.Status != 200 || f.Message != "response was not JSON" {
		t.Fatalf("notjson failure = %+v, want Parse/200/\"response was not JSON\"", f)
	}
	if s.Document() == nil || s.Document().Title != "The pantry" {
		t.Errorf("a failed request is not an answer; pantry should stay, got %v", s.Document())
	}

	// === Invoke an action, confirm it =====================================
	doc = s.Document()
	brewRow := findRow(t, doc, render.RowKindAction, "Brew a pot of tea", "brew action")
	s.Activate(brewRow.Target)
	confirm, ok := s.Question().(session.ConfirmQuestion)
	if !ok {
		t.Fatalf("question = %T, want ConfirmQuestion", s.Question())
	}
	if confirm.Heading != "Brew a pot of tea" || confirm.Body != "Brew a pot of tea?  POST to this server." {
		t.Fatalf("confirm = %+v", confirm)
	}
	s.Answer(true)
	reported, ok := s.Notice().(session.ActionOutcomeReported)
	if !ok || reported.Message != "running" {
		t.Fatalf("notice = %+v, want ActionOutcomeReported message %q", s.Notice(), "running")
	}
	if !s.IsLive() {
		t.Errorf("a 202 with a running job should be watched")
	}
	if got := propertyValue(t, s.Document(), "kettle"); got != "cold" {
		t.Errorf("kettle = %q before re-fetch, want cold (stale doc still shown)", got)
	}

	// === Awkward case: a 409 -- two confirmed brews racing ===============
	s.Activate(brewRow.Target)
	s.Answer(true)
	failed, ok := s.Notice().(session.ActionFailed)
	if !ok {
		t.Fatalf("notice = %T, want ActionFailed", s.Notice())
	}
	if failed.Failure == nil || failed.Failure.Kind != failure.Conflict || failed.Failure.Status != 409 || failed.Failure.Message != "a brew is already running" {
		t.Fatalf("conflict failure = %+v, want Conflict/409", failed.Failure)
	}
	if s.State() != nav.StateOK {
		t.Errorf("state = %v, want StateOK (conflict re-fetched)", s.State())
	}
	if got := propertyValue(t, s.Document(), "kettle"); got != "hot" {
		t.Errorf("kettle = %q, want hot (re-fetched after conflict)", got)
	}
	if got := propertyValue(t, s.Document(), "brews"); got != "1" {
		t.Errorf("brews = %q, want 1", got)
	}
	if actionLabels(s.Document())["Let the kettle cool"] {
		t.Errorf("cool-down offered while still brewing")
	}
	if !s.IsLive() {
		t.Errorf("the first job's watch should survive the second, failed action")
	}

	// === Awkward case: a job that takes a genuine 24 seconds, and the =====
	// === unconditional re-fetch once it ends ==============================
	sawProgress := false
	deadline := time.Now().Add(40 * time.Second)
	for s.IsLive() && time.Now().Before(deadline) {
		sched.tick()
		if _, ok := s.Notice().(session.ActionProgress); ok {
			sawProgress = true
		}
		if !s.IsLive() {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if s.IsLive() {
		t.Fatalf("the 24s job did not finish within the wait budget")
	}
	if !sawProgress {
		t.Errorf("expected at least one progress step while watching")
	}
	done, ok := s.Notice().(session.ActionOutcomeReported)
	if !ok || done.Message != "done" {
		t.Fatalf("notice = %+v, want ActionOutcomeReported message %q", s.Notice(), "done")
	}
	if s.Document().Title != "The pantry" {
		t.Errorf("after watch: title=%q, want The pantry (re-fetched, not the job)", s.Document().Title)
	}
	if got := propertyValue(t, s.Document(), "brews"); got != "1" {
		t.Errorf("brews = %q, want 1", got)
	}
	if got := propertyValue(t, s.Document(), "kettle"); got != "hot" {
		t.Errorf("kettle = %q, want hot", got)
	}
	if !actionLabels(s.Document())["Let the kettle cool"] {
		t.Errorf("cool-down not offered after the kettle is hot and the job done")
	}

	// === Subprocess: restforge get and act, run before /slow =============
	// /slow blocks the single-threaded fixture server for 30s, so the
	// subprocess invocations run here -- before /slow -- or they would time
	// out waiting for a server that is still asleep.
	runSubprocess(t, []string{"get"}, "The pantry")
	runSubprocess(t, []string{"act", "label-jar", "--yes", "--value", "subprocess label"}, "The pantry")

	s.Activate(render.FetchTarget{Href: "/slow"})
	if s.State() != nav.StateUnreachable {
		t.Fatalf("/slow state = %v, want StateUnreachable (timeout)", s.State())
	}
	f = s.Failure()
	if f == nil || f.Kind != failure.Timeout || f.Message != "timed out" {
		t.Fatalf("/slow failure = %+v, want Timeout/\"timed out\"", f)
	}
	if s.Document() == nil || s.Document().Title != "The pantry" {
		t.Errorf("a failed request is not an answer; pantry should stay, got %v", s.Document())
	}

}

// runSubprocess runs the built binary with the given args against the
// fixture (config via RESTFORGE_CONFIG, set in TestMain and inherited) and
// asserts exit 0 and that stdout contains want.
func runSubprocess(t *testing.T, args []string, want string) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restforge %v: %v\n%s", args, err, out)
	}
	if !strings.Contains(string(out), want) {
		t.Errorf("restforge %v output missing %q; got:\n%s", args, want, out)
	}
}
