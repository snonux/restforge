// Ported from flutter/test/services/http_service_test.dart (itself ported
// from pebble/tools/test-http.js). The Dart file drives HttpService against
// a MockClient stand-in; here an httptest.Server plays the same role, so
// every case runs over a real (loopback) socket rather than a fake
// transport -- which is also what lets the unreachable/timeout cases below
// exercise real connection-failure and context-deadline behaviour instead
// of simulating it.
//
// The cases worth keeping are the same ones test-http.js keeps: not the
// happy path so much as what a caller can rely on when the network is not
// cooperating -- an unreachable host must resolve as Unreachable, not
// silently or as some other kind; a timeout is its own kind, distinct from
// Unreachable, because "no answer within budget" and "no answer ever" are
// different facts about the request; and nothing sent to the injected log
// function ever contains the secret, no matter which of those happens.
package httpclient_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
)

const testSecret = "this-is-the-secret-value"

// testBackend returns a Backend pointed at srv -- the Go tests' stand-in
// for the Dart tests' fixed https://host.example.org backend, substituting
// the httptest server's OS-assigned address for the fixed hostname.
func testBackend(srv *httptest.Server) backend.Backend {
	return backend.Backend{
		Name:       "test",
		BaseURL:    srv.URL + "/",
		AuthHeader: "X-API-Key",
		Secret:     testSecret,
	}
}

// newServer starts an httptest.Server running handler and registers it to
// close when t completes.
func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// jsonHandler replies with status and body under Content-Type: application/json.
func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// closedPortBaseURL returns an "http://host:port/" that nothing is
// listening on: a listener is opened and immediately closed, so the port
// is known free of anything but guaranteed to refuse the next connection
// -- a reliable way to trigger a real connection-refused error without
// depending on a specific unused port staying unused.
func closedPortBaseURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("closing listener: %v", err)
	}
	return "http://" + addr + "/"
}

func asFailure(t *testing.T, err error) *failure.Failure {
	t.Helper()
	f, ok := err.(*failure.Failure)
	if !ok {
		t.Fatalf("error is not *failure.Failure: %v (%T)", err, err)
	}
	return f
}

func TestGetSuccessYieldsParsedDocument(t *testing.T) {
	var seenHeader http.Header
	var seenPath string
	var seenBody []byte
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Clone()
		seenPath = r.URL.Path
		seenBody, _ = io.ReadAll(r.Body)
		jsonHandler(http.StatusOK, `{"class":["status"],"properties":{"apiVersion":1}}`)(w, r)
	})
	c := httpclient.New()
	be := testBackend(srv)

	resp, err := c.Get(be, "/status")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200", resp.Status)
	}

	entity, ok := resp.Entity.(map[string]any)
	if !ok {
		t.Fatalf("Entity is not a map: %#v", resp.Entity)
	}
	props, ok := entity["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not a map: %#v", entity["properties"])
	}
	if props["apiVersion"] != float64(1) {
		t.Errorf("apiVersion = %v, want 1", props["apiVersion"])
	}

	if got := seenHeader.Get("X-API-Key"); got != testSecret {
		t.Errorf("X-API-Key header = %q, want %q", got, testSecret)
	}
	if seenPath != "/status" {
		t.Errorf("path = %q, want /status", seenPath)
	}
	if len(seenBody) != 0 {
		t.Errorf("GET body = %q, want empty (a GET sends no body)", seenBody)
	}
	if strings.Contains(resp.URL, testSecret) {
		t.Errorf("URL contains the secret: %q", resp.URL)
	}
}

func TestActionPOST202IsSuccessNotFailure(t *testing.T) {
	var seenContentType string
	var seenBody []byte
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		jsonHandler(http.StatusAccepted, `{"properties":{"state":"running"}}`)(w, r)
	})
	c := httpclient.New()
	be := testBackend(srv)

	resp, err := c.Request(be, "/act", "POST", map[string]string{"force": "true"})
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if resp.Status != http.StatusAccepted {
		t.Errorf("Status = %d, want 202", resp.Status)
	}
	if seenContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", seenContentType)
	}
	if string(seenBody) != "force=true" {
		t.Errorf("body = %q, want %q (the fields are form-encoded)", seenBody, "force=true")
	}
}

func TestActionPOSTFieldLessSendsEmptyBodyNotNothing(t *testing.T) {
	// The shape carries over from http.js's send('') vs send(): an action
	// with no fields still declares Content-Type and sends an empty body,
	// because some servers reject a POST with neither.
	var seenContentType string
	var seenBody []byte
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		jsonHandler(http.StatusOK, "{}")(w, r)
	})
	c := httpclient.New()
	be := testBackend(srv)

	if _, err := c.Request(be, "/act", "POST", nil); err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if len(seenBody) != 0 {
		t.Errorf("body = %q, want empty", seenBody)
	}
	if seenContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", seenContentType)
	}
}

func TestUnreachableConnectionFailureResolvesAsUnreachable(t *testing.T) {
	be := backend.Backend{
		Name:       "test",
		BaseURL:    closedPortBaseURL(t),
		AuthHeader: "X-API-Key",
		Secret:     testSecret,
	}
	c := httpclient.New()

	_, err := c.Get(be, "/status")
	if err == nil {
		t.Fatal("expected an error")
	}
	if f := asFailure(t, err); f.Kind != failure.Unreachable {
		t.Errorf("Kind = %v, want Unreachable", f.Kind)
	}
}

func TestUnreachableNamesTheHost(t *testing.T) {
	base := closedPortBaseURL(t)
	be := backend.Backend{Name: "test", BaseURL: base, AuthHeader: "X-API-Key", Secret: testSecret}
	c := httpclient.New()

	_, err := c.Get(be, "/status")
	f := asFailure(t, err)
	wantHost := strings.TrimSuffix(base, "/")
	if !strings.Contains(f.Message, wantHost) {
		t.Errorf("message = %q, want to contain %q", f.Message, wantHost)
	}
}

func TestTimeoutOutrunsBudgetIsItsOwnKind(t *testing.T) {
	release := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	// Registered after newServer, so cleanup order (LIFO) unblocks the
	// hung handler before srv.Close() waits for it -- otherwise Close
	// blocks forever on this handler's still-outstanding request.
	t.Cleanup(func() { close(release) })

	c := httpclient.New(httpclient.WithGetTimeout(5 * time.Millisecond))
	be := testBackend(srv)

	_, err := c.Get(be, "/status")
	if f := asFailure(t, err); f.Kind != failure.Timeout {
		t.Errorf("Kind = %v, want Timeout", f.Kind)
	}
}

func TestGetGetsShortBudgetActionGetsLongBudget(t *testing.T) {
	// The two budgets are injected here so the test does not wait 20 real
	// seconds to prove they differ -- production always uses the
	// top-level GetTimeout/ActionTimeout constants (asserted below).
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		jsonHandler(http.StatusOK, "{}")(w, r)
	})
	c := httpclient.New(
		httpclient.WithGetTimeout(5*time.Millisecond),
		httpclient.WithActionTimeout(200*time.Millisecond),
	)
	be := testBackend(srv)

	_, getErr := c.Get(be, "/status")
	if f := asFailure(t, getErr); f.Kind != failure.Timeout {
		t.Errorf("GET Kind = %v, want Timeout: a GET must not get the long, action-sized budget", f.Kind)
	}

	if _, err := c.Request(be, "/act", "POST", nil); err != nil {
		t.Errorf("POST error = %v, want nil: an action must not get the short, GET-sized budget", err)
	}
}

func TestProductionBudgetsGet20sAction60sAndDiffer(t *testing.T) {
	if httpclient.GetTimeout != 20*time.Second {
		t.Errorf("GetTimeout = %v, want 20s", httpclient.GetTimeout)
	}
	if httpclient.ActionTimeout != 60*time.Second {
		t.Errorf("ActionTimeout = %v, want 60s", httpclient.ActionTimeout)
	}
	if httpclient.GetTimeout == httpclient.ActionTimeout {
		t.Error("GetTimeout and ActionTimeout must differ")
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		kind   failure.Kind
	}{
		{401, failure.Auth},
		{403, failure.Auth},
		{409, failure.Conflict},
		{404, failure.Client},
		{400, failure.Client},
		{500, failure.Server},
		{503, failure.Server},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_maps_to_%s", tc.status, tc.kind), func(t *testing.T) {
			srv := newServer(t, jsonHandler(tc.status, "{}"))
			c := httpclient.New()
			be := testBackend(srv)

			_, err := c.Get(be, "/x")
			f := asFailure(t, err)
			if f.Kind != tc.kind {
				t.Errorf("Kind = %v, want %v", f.Kind, tc.kind)
			}
			if f.Status != tc.status {
				t.Errorf("Status = %d, want %d", f.Status, tc.status)
			}
		})
	}
}

func TestServerOwnWordingUsedWhenErrorBodyCarriesOne(t *testing.T) {
	srv := newServer(t, jsonHandler(http.StatusConflict, `{"properties":{"message":"a job is already running"}}`))
	c := httpclient.New()
	be := testBackend(srv)

	_, err := c.Get(be, "/x")
	f := asFailure(t, err)
	if f.Message != "a job is already running" {
		t.Errorf("Message = %q, want %q", f.Message, "a job is already running")
	}
}

func TestNonJSON200IsParseError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>not siren at all</html>"))
	})
	c := httpclient.New()
	be := testBackend(srv)

	_, err := c.Get(be, "/x")
	if f := asFailure(t, err); f.Kind != failure.Parse {
		t.Errorf("Kind = %v, want Parse", f.Kind)
	}
}

func TestEmptyBody200IsAlsoParseError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c := httpclient.New()
	be := testBackend(srv)

	_, err := c.Get(be, "/x")
	if f := asFailure(t, err); f.Kind != failure.Parse {
		t.Errorf("Kind = %v, want Parse", f.Kind)
	}
}

func TestBadAuthHeaderNameIsConfigAndNoRequestIsEverSent(t *testing.T) {
	var calls int
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		jsonHandler(http.StatusOK, "{}")(w, r)
	})
	be := backend.Backend{
		Name:       "broken",
		BaseURL:    srv.URL + "/",
		AuthHeader: "X API Key",
		Secret:     testSecret,
	}
	c := httpclient.New()

	_, err := c.Get(be, "/x")
	if f := asFailure(t, err); f.Kind != failure.Config {
		t.Errorf("Kind = %v, want Config", f.Kind)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: a config problem must not reach the network", calls)
	}
}

func TestLogSecretNeverReachesIt(t *testing.T) {
	srv := newServer(t, jsonHandler(http.StatusOK, "{}"))
	var logs []string
	c := httpclient.New(httpclient.WithLog(func(m string) { logs = append(logs, m) }))
	be := testBackend(srv)

	if _, err := c.Get(be, "/status"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if strings.Contains(strings.Join(logs, "\n"), testSecret) {
		t.Error("log output contains the secret")
	}
}

func TestLogResponseHeadersAreLoggedInFull(t *testing.T) {
	srv := newServer(t, jsonHandler(http.StatusOK, "{}"))
	var logs []string
	c := httpclient.New(httpclient.WithLog(func(m string) { logs = append(logs, m) }))
	be := testBackend(srv)

	if _, err := c.Get(be, "/status"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "Content-Type: application/json") {
		t.Errorf("logs do not contain the response header, got: %v", logs)
	}
}

func TestLogStatusAndElapsedTimeAreLogged(t *testing.T) {
	srv := newServer(t, jsonHandler(http.StatusOK, "{}"))
	var logs []string
	c := httpclient.New(httpclient.WithLog(func(m string) { logs = append(logs, m) }))
	be := testBackend(srv)

	if _, err := c.Get(be, "/status"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	re := regexp.MustCompile(`-> 200 \(\d+ms\)`)
	for _, line := range logs {
		if re.MatchString(line) {
			return
		}
	}
	t.Errorf("no log line matched the status+elapsed pattern, got: %v", logs)
}

// --- n31: GetContext/RequestContext actually cancel, not just discard -----
//
// Get/Request keep working exactly as before (every test above uses them,
// unchanged, under context.Background()). These tests are for the new
// ctx-aware entry points nav.Nav uses: proving that cancelling the caller's
// context aborts the round trip in flight instead of leaving it running to
// its own httpclient.GetTimeout/ActionTimeout budget for nothing.

// TestGetContextCancelledMidFlightAbortsWithoutWaitingForServer starts a
// request against a handler that never replies, waits until the server has
// actually received it, then cancels the caller's context and asserts
// GetContext returns promptly with an error -- not after the production
// GetTimeout (20s) or ActionTimeout (60s) budget New() defaults to, which
// this test would time out long before if cancellation merely discarded the
// result instead of aborting the request.
func TestGetContextCancelledMidFlightAbortsWithoutWaitingForServer(t *testing.T) {
	reached := make(chan struct{})
	release := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(reached)
		<-release
	})
	t.Cleanup(func() { close(release) })

	c := httpclient.New()
	be := testBackend(srv)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := c.GetContext(ctx, be, "/status")
		errCh <- err
	}()

	<-reached // the request is in flight on the server, blocked
	cancel()

	select {
	case err := <-errCh:
		// Kind is asserted, not just "an error": classifyTransportError's
		// own doc comment explains why a cancellation deliberately lands on
		// Unreachable rather than a dedicated Kind (nav's generation guard
		// already discards a cancelled call's result before ever looking at
		// Kind) -- pinning it here catches a future regression that
		// misclassifies cancellation as, say, Timeout instead.
		if f := asFailure(t, err); f.Kind != failure.Unreachable {
			t.Errorf("Kind = %v, want Unreachable", f.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetContext() did not return within 5s of ctx being cancelled -- the round trip was left running instead of being aborted")
	}
}

// TestGetContextAlreadyCancelledNeverReachesTheServer is the negative
// case: a context cancelled before the call is even made must fail fast
// and never dial the server at all, the same guarantee
// TestBadAuthHeaderNameIsConfigAndNoRequestIsEverSent proves for a config
// problem.
func TestGetContextAlreadyCancelledNeverReachesTheServer(t *testing.T) {
	var calls int
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		jsonHandler(http.StatusOK, "{}")(w, r)
	})
	c := httpclient.New()
	be := testBackend(srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before GetContext is ever called

	_, err := c.GetContext(ctx, be, "/status")
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0: an already-cancelled context must not reach the network", calls)
	}
}

// TestRequestContextCancelledMidFlightAbortsAnAction is
// TestGetContextCancelledMidFlightAbortsWithoutWaitingForServer's sibling
// for the POST/action path, proving RequestContext (not just its GetContext
// wrapper) honours cancellation too.
func TestRequestContextCancelledMidFlightAbortsAnAction(t *testing.T) {
	reached := make(chan struct{})
	release := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(reached)
		<-release
	})
	t.Cleanup(func() { close(release) })

	c := httpclient.New()
	be := testBackend(srv)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := c.RequestContext(ctx, be, "/act", "POST", nil)
		errCh <- err
	}()

	<-reached
	cancel()

	select {
	case err := <-errCh:
		if f := asFailure(t, err); f.Kind != failure.Unreachable {
			t.Errorf("Kind = %v, want Unreachable", f.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RequestContext() did not return within 5s of ctx being cancelled -- the round trip was left running instead of being aborted")
	}
}

func TestLogSecretNeverReachesItEvenWhenTheRequestFails(t *testing.T) {
	be := backend.Backend{
		Name:       "test",
		BaseURL:    closedPortBaseURL(t),
		AuthHeader: "X-API-Key",
		Secret:     testSecret,
	}
	var logs []string
	c := httpclient.New(httpclient.WithLog(func(m string) { logs = append(logs, m) }))

	_, _ = c.Get(be, "/status")
	if strings.Contains(strings.Join(logs, "\n"), testSecret) {
		t.Error("log output contains the secret")
	}
}
