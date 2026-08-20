// Internal (package nav) tests that need direct access to unexported
// fields -- specifically Nav.cancel, which nav_test.go's external-package
// tests cannot reach. Kept in its own file, separate from nav_test.go's
// black-box suite, the same split invoke_internal_test.go and
// poll_internal_test.go use in internal/action and internal/live.
package nav

import (
	"context"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
)

// stubGetter is a minimal httpGetter that either always succeeds with
// entity, or -- when fail is set -- always fails. Enough for this file's
// one concern (what happens to n.cancel once a fetch lands), which needs
// no routing or mid-flight blocking the way nav_test.go's fuller fakeClient
// does for its own, black-box tests.
type stubGetter struct {
	entity map[string]any
	fail   bool
}

func (s stubGetter) GetContext(_ context.Context, _ backend.Backend, _ string) (httpclient.HTTPResponse, error) {
	if s.fail {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Unreachable, Message: "stubGetter: fail is set"}
	}
	return httpclient.HTTPResponse{Status: 200, Entity: s.entity}, nil
}

func testBackend() backend.Backend {
	return backend.Backend{Name: "x", BaseURL: "https://example.test/"}
}

// TestCancelIsReleasedOnceAFetchLandsOnItsOwn is the regression test for a
// gap a code review of n31 found: every site that applies a fetch's own
// outcome (still current, neither superseded nor invalidated) must call
// releaseCancelLocked, or n.cancel goes on pointing at a finished fetch's
// now-pointless cancel func until some unrelated later call happens to
// supersede it -- contradicting Nav.cancel's own doc comment ("a fetch is
// genuinely in flight right now") the moment this fetch lands, and leaving
// a context.CancelFunc uncalled indefinitely rather than released once its
// context's last use is done.
func TestCancelIsReleasedOnceAFetchLandsOnItsOwn(t *testing.T) {
	n := New(stubGetter{entity: map[string]any{
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}})

	n.OpenRoot(testBackend())

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		t.Error("n.cancel is non-nil after OpenRoot's own fetch landed successfully, want nil: a finished fetch's cancel func must be released, not left for some unrelated later call to clean up")
	}
}

// TestCancelIsReleasedOnFailureToo is
// TestCancelIsReleasedOnceAFetchLandsOnItsOwn's failure-path sibling:
// applyFailureIfCurrent, not pushIfCurrent, is what must release n.cancel
// when the fetch that owns it lands with an error instead of a document.
func TestCancelIsReleasedOnFailureToo(t *testing.T) {
	n := New(stubGetter{fail: true})

	n.OpenRoot(testBackend())

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		t.Error("n.cancel is non-nil after OpenRoot's own fetch landed with an error, want nil: a finished (failed) fetch's cancel func must be released too")
	}
}
