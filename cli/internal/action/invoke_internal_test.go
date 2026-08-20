package action

// White-box test for toFailure's fallback Kind. Living in package action,
// not action_test, is what gives this file the direct access the fake
// below needs to satisfy the unexported requester interface -- the same
// reason fields_internal_test.go lives here rather than in action_test.
//
// toFailure's non-*failure.Failure branch is otherwise unreachable through
// invoke_test.go's env: that harness drives requests through a real
// httpclient.Client pointed at an httptest.Server, which always returns
// *failure.Failure on error (see httpclient's package comment). Reaching
// the fallback branch needs a requester that returns some other error
// type, which only a hand-rolled fake -- built here, in this package --
// can do.

import (
	"errors"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/siren"
)

// plainErrRequester always fails with a plain error, never a
// *failure.Failure -- the non-conforming test double toFailure's fallback
// exists to defend against.
type plainErrRequester struct{}

func (plainErrRequester) Request(be backend.Backend, href, method string, fields map[string]string) (httpclient.HTTPResponse, error) {
	return httpclient.HTTPResponse{}, errors.New("boom")
}

// TestToFailureFallsBackToTheSharedKind checks that a plain, non-
// *failure.Failure error from the injected requester comes out as
// Kind: Config -- the fallback failure.From's doc comment documents as the
// one shared, conscious choice for action, nav and live's identical
// defensive case, rather than each package silently picking its own.
func TestToFailureFallsBackToTheSharedKind(t *testing.T) {
	a := New(plainErrRequester{})
	entity := siren.Entity{
		Actions: []siren.Action{{Name: "peek", Method: "GET", Href: "/peek"}},
	}

	outcome := a.Ask(backend.Backend{}, entity, "peek")

	invoked, ok := outcome.(ActionInvoked)
	if !ok {
		t.Fatalf("outcome = %#v, want ActionInvoked", outcome)
	}
	failed, ok := invoked.Outcome.(InvokeFailed)
	if !ok {
		t.Fatalf("Outcome = %#v, want InvokeFailed", invoked.Outcome)
	}
	if failed.Failure.Kind != failure.Config {
		t.Errorf("Kind = %v, want %v (the shared fallback -- see failure.From)", failed.Failure.Kind, failure.Config)
	}
}
