package live

// White-box test for failureKind's fallback Kind. Living in package live,
// not live_test, is what gives this file direct access to the unexported
// failureKind function -- poll_test.go's fakeGetter only ever returns
// *failure.Failure (see its fail branch), so the fallback branch is
// otherwise unreachable through that harness.

import (
	"errors"
	"testing"

	"github.com/snonux/restforge/cli/internal/failure"
)

// TestFailureKindFallsBackToTheSharedKind checks that a plain,
// non-*failure.Failure error comes out named after Kind: Config -- the
// fallback failure.From's doc comment documents as the one shared,
// conscious choice for action, nav and live's identical defensive case
// (a hand-rolled test double behind httpGetter returning some other error
// type), rather than each package silently picking its own.
func TestFailureKindFallsBackToTheSharedKind(t *testing.T) {
	if got, want := failureKind(errors.New("boom")), failure.Config.String(); got != want {
		t.Errorf("failureKind(plain error) = %q, want %q", got, want)
	}
}

// TestFailureKindReadsAnActualFailuresKind is the non-fallback case: an
// actual *failure.Failure's own Kind passes through unchanged.
func TestFailureKindReadsAnActualFailuresKind(t *testing.T) {
	err := &failure.Failure{Kind: failure.Unreachable, Message: "no answer"}
	if got, want := failureKind(err), "unreachable"; got != want {
		t.Errorf("failureKind(*Failure) = %q, want %q", got, want)
	}
}
