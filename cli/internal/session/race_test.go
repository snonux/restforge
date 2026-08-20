// race_test.go exists to prove the fix for the data race g31 found: a
// Bubble Tea caller's render goroutine reading Session's exported accessors
// (Document, State, Failure, Detail, Question, Notice, IsLive, Backend,
// Href, CanGoBack) while a cmd goroutine is still inside a Session method
// that mutates the same fields -- see internal/tui/cmd.go's package comment
// for why that overlap is real, not theoretical.
//
// The rest of this file's tests build a Session synchronously and call its
// methods one at a time, which never exercises this: go test -race only
// flags an actual concurrent unsynchronized access, and a test that never
// creates one proves nothing about it. This file does, deliberately: it
// runs a goroutine calling mutating Session methods back to back against a
// goroutine hammering every read accessor, with `go test -race` as the
// judge. Before Session and nav.Nav gained their own sync.RWMutex, this
// test reliably reported a race (confirmed by hand while writing it,
// reverting the mutex, and rerunning it with -race); after, it passes
// clean.
package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// slowClient wraps a *fakeClient with a small, fixed delay before every
// response, so the writer goroutine below actually spends measurable wall
// time between taking the lock around its first field write and its last --
// widening the window a concurrent reader has to race against it. Without
// this, a fake in-memory response can return before the reader goroutine
// even gets scheduled, and the race, while still theoretically present,
// might not reliably reproduce under Go's race detector's own scheduling.
type slowClient struct {
	*fakeClient
	delay time.Duration
}

func (c *slowClient) Get(be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	time.Sleep(c.delay)
	return c.fakeClient.Get(be, href)
}

// GetContext is nav's httpGetter seam (see n31). ctx is ignored here, same
// as tui's fakeClient.GetContext: this test's race is about concurrent
// reads during Session's own field mutations, not about cancellation --
// see internal/nav's own tests for cancellation proof.
func (c *slowClient) GetContext(_ context.Context, be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	time.Sleep(c.delay)
	return c.fakeClient.Get(be, href)
}

func (c *slowClient) Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
	time.Sleep(c.delay)
	return c.fakeClient.Request(be, href, method, fields)
}

// newRaceEnv builds a Session on a slowClient -- otherwise the same wiring
// newEnv uses (a real clock, no fake timer substitution needed since this
// test never advances a watch).
func newRaceEnv() *session.Session {
	client := &slowClient{fakeClient: newFakeClient(), delay: 2 * time.Millisecond}
	n := nav.New(client)
	a := action.New(client, action.WithLog(func(string) {}))
	l := live.New(client, live.WithLog(func(string) {}))
	return session.New(n, a, l)
}

// TestConcurrentReadsDuringMutation drives Session the way a running
// Bubble Tea program does: one goroutine standing in for the eventLoop's
// repeated View calls, reading every exported accessor in a tight loop,
// concurrently with another goroutine standing in for a stream of
// sessionCmd goroutines, calling OpenBackend/Activate/Back back to back.
// `go test -race ./internal/session/...` is the actual assertion here --
// a clean run is what "no data race" means; there is nothing further to
// assert about the values read, since a reader can legitimately observe
// either the state before or after any given write.
func TestConcurrentReadsDuringMutation(t *testing.T) {
	s := newRaceEnv()
	be := testBackend()

	const rounds = 40
	var wg sync.WaitGroup

	stop := make(chan struct{})

	// Reader: every accessor View touches, hammered continuously.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.Backend()
			_ = s.State()
			_ = s.Failure()
			_ = s.Document()
			_ = s.Href()
			_ = s.CanGoBack()
			_ = s.Detail()
			_ = s.Question()
			_ = s.Notice()
			_ = s.IsLive()
		}
	}()

	// Writer: the sequence a user browsing around actually produces --
	// open a backend, follow a link, invoke a safe action, go back -- each
	// one a real HTTP round trip through slowClient, so this goroutine is
	// genuinely still inside a Session method (and so still writing) while
	// the reader above keeps calling accessors.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			s.OpenBackend(be)
			doc := s.Document()
			if doc != nil {
				for _, row := range doc.Rows {
					if _, ok := row.Target.(render.FetchTarget); ok {
						s.Activate(row.Target)
						break
					}
				}
			}
			s.Back()
		}
		close(stop)
	}()

	wg.Wait()
}
