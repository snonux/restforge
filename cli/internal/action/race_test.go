package action_test

// race_test.go proves the concurrency bug r31 found and fixed: before r31,
// Action.pending and Action.confirmedRetry were read and written with no
// synchronization at all -- yet more than one goroutine can genuinely call
// into the same *Action concurrently. See internal/tui/cmd.go's package
// comment for the concrete mechanism: every screen wraps a Session method
// that does I/O (Activate, Answer, AnswerValue) in sessionCmd, which
// bubbletea runs on its own goroutine without waiting for it, and nothing
// gates a second keypress from dispatching another one before the first
// has returned -- Confirm's own updateConfirm doc comment notes 'y' goes
// through sessionCmd while 'n' and the Back key's Answer(false)/
// CancelPending (model.go's handleBack) are called directly, synchronously,
// on Update's own goroutine, so a sessionCmd goroutine and Update's own can
// be inside this same *Action at once, not just two sessionCmd goroutines
// racing each other.
//
// Two things follow from that, both exercised below:
//
//   - two overlapping Answer(true) calls (a double press of Confirm's 'y')
//     could both see the same pendingAction and both send it -- an actual
//     duplicate request to the server, not just a benign race.
//   - CancelPending (the Back key, called directly) racing a still-running
//     Ask/invoke (a safe action's own request, or an unsafe one's pending
//     awaiting a spoken value) touched the same pendingAction from two
//     goroutines with no synchronization at all.
//
// `go test -race` is the actual judge here, the same way session/
// race_test.go, nav_test.go and live/poll_test.go prove their own fixes:
// reverting this task's mutex/casPending/takePendingIf and rerunning these
// two tests with -race reliably reports a race (confirmed by hand while
// writing this file); with the fix in place, both pass clean.

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// blockingRequester wraps a real *httpclient.Client and pauses every
// Request call on release until it is closed, recording how many calls
// actually reached it -- mirrors live_test.go's fakeGetter.block and
// nav_test.go's fakeClient.block: forcing a request to sit in flight is
// what lets a test put a second, concurrent call in the exact window this
// task's fix (or its absence) matters.
type blockingRequester struct {
	*httpclient.Client

	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingRequester(base *httpclient.Client) *blockingRequester {
	return &blockingRequester{
		Client:  base,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingRequester) Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.Client.Request(be, href, method, fields)
}

func (r *blockingRequester) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// raceServer starts a loopback server answering every request with a fixed
// 200, plus the backend.Backend to reach it -- everything these two tests
// need from a server, without newEnv's route table (neither test cares
// what is returned, only how many requests actually arrived).
func raceServer(t *testing.T) backend.Backend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"properties":{"state":"done"}}`))
	}))
	t.Cleanup(srv.Close)
	return backend.Backend{Name: "pantry", BaseURL: srv.URL + "/", AuthHeader: "X-API-Key", Secret: "s"}
}

// TestConcurrentConfirmedAnswersOnlySendOnce proves the double-send bug: two
// goroutines both calling Answer(true) against the same pending
// confirmation -- the double-'y'-press scenario this file's own header
// comment describes -- must not both reach the server. Before r31, both
// would see a.pending non-nil and both call send/doSend; after, only the
// first to take Action's lock in takePendingIf claims it, and the second
// gets the same nil ("nothing to do") a lone, un-raced double-press would
// already have produced if the first had returned first.
func TestConcurrentConfirmedAnswersOnlySendOnce(t *testing.T) {
	be := raceServer(t)
	requester := newBlockingRequester(httpclient.New())
	a := action.New(requester, action.WithLog(func(string) {}))

	entity := siren.Entity{
		Links:   []siren.Link{{Rel: []string{"self"}, Href: "/"}},
		Actions: []siren.Action{{Name: "brew", Title: "Brew", Method: "POST", Href: "/brew"}},
	}
	if _, ok := a.Ask(be, entity, "brew").(action.ConfirmationRequired); !ok {
		t.Fatal("Ask did not raise a confirmation -- test fixture is wrong")
	}

	outcomes := make([]action.InvokeOutcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcomes[i] = a.Answer(true, be, entity)
		}(i)
	}
	close(start) // both goroutines race takePendingIf at once

	select {
	case <-requester.started:
	case <-time.After(5 * time.Second):
		t.Fatal("neither Answer call ever reached the requester")
	}
	close(requester.release)
	wg.Wait()

	if got := requester.count(); got != 1 {
		t.Errorf("requester was called %d times, want exactly 1: the confirmed action must not be sent twice", got)
	}

	nils, succeeded := 0, 0
	for _, o := range outcomes {
		switch {
		case o == nil:
			nils++
		default:
			if _, ok := o.(action.InvokeSucceeded); ok {
				succeeded++
			}
		}
	}
	if nils != 1 || succeeded != 1 {
		t.Errorf("outcomes = %#v, want exactly one nil (lost the race, nothing to do) and one InvokeSucceeded", outcomes)
	}
	if a.HasPending() {
		t.Error("HasPending = true after both calls settled, want false")
	}
}

// TestCancelPendingDuringInFlightAskDoesNotRace proves the second half of
// r31's finding: the Back key's CancelPending (model.go's handleBack calls
// it directly and synchronously, never through sessionCmd -- see this
// file's header comment) can run concurrently with a still-running Ask/
// invoke for a safe action, whose own request is genuinely in flight for
// as long as the server takes to answer. Before r31, Ask set a.pending and
// invoke's send() read and nilled it with no lock at all, so a concurrent
// CancelPending's own unsynchronized nil write raced both -- and, timed
// unluckily, could have handed invoke a nil a.pending to dereference.
// After r31, invoke never reads a.pending at all (it works from the
// pendingAction it was already handed -- see invoke's own doc comment),
// and CancelPending only ever touches a.pending itself under a.mu, so this
// is race-free by construction; -race is what actually proves it.
func TestCancelPendingDuringInFlightAskDoesNotRace(t *testing.T) {
	be := raceServer(t)
	requester := newBlockingRequester(httpclient.New())
	a := action.New(requester, action.WithLog(func(string) {}))

	entity := siren.Entity{
		Links:   []siren.Link{{Rel: []string{"self"}, Href: "/"}},
		Actions: []siren.Action{{Name: "peek", Title: "Peek", Method: "GET", Href: "/peek"}},
	}

	// Both goroutines start together, before Ask has done anything at all:
	// the vulnerable window this test targets is Ask/invoke's own brief,
	// pre-network handling of the pendingAction (entity lookup, FillFields,
	// the pending = nil send() used to do inline) -- all of it local
	// computation that finishes before the request ever reaches
	// requester.started, so hammering CancelPending only after that signal
	// (as an earlier version of this test did) arrives too late to
	// overlap it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	start := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for {
			select {
			case <-stop:
				return
			default:
			}
			a.CancelPending()
			_ = a.HasPending()
		}
	}()

	askDone := make(chan action.AskOutcome, 1)
	go func() {
		<-start
		askDone <- a.Ask(be, entity, "peek") // safe method: sends immediately
	}()
	close(start)

	select {
	case <-requester.started:
	case <-time.After(5 * time.Second):
		t.Fatal("Ask's own request never reached the requester")
	}

	close(requester.release)
	outcome := <-askDone
	close(stop)
	wg.Wait()

	invoked, ok := outcome.(action.ActionInvoked)
	if !ok {
		t.Fatalf("Ask outcome = %#v, want ActionInvoked", outcome)
	}
	if _, ok := invoked.Outcome.(action.InvokeSucceeded); !ok {
		t.Errorf("inner outcome = %#v, want InvokeSucceeded: a concurrent CancelPending must not corrupt an in-flight safe action", invoked.Outcome)
	}
	if got := requester.count(); got != 1 {
		t.Errorf("requester was called %d times, want exactly 1", got)
	}
}
