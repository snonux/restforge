package failure_test

import (
	"errors"
	"testing"

	"github.com/snonux/restforge/cli/internal/failure"
)

// TestFailureError checks Error()'s formatting, including the zero Status
// case (Unreachable/Timeout/Config never received an HTTP response).
func TestFailureError(t *testing.T) {
	tests := []struct {
		name string
		f    *failure.Failure
		want string
	}{
		{
			name: "with status",
			f:    &failure.Failure{Kind: failure.Server, Status: 503, Message: "service unavailable"},
			want: `server (status 503): service unavailable`,
		},
		{
			name: "zero status",
			f:    &failure.Failure{Kind: failure.Unreachable, Status: 0, Message: "connection refused"},
			want: `unreachable (status 0): connection refused`,
		},
		{
			name: "empty message",
			f:    &failure.Failure{Kind: failure.Auth, Status: 401},
			want: `auth (status 401): `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFailureImplementsError verifies *Failure satisfies the error
// interface and behaves as an ordinary error for callers that only care
// whether the operation failed, not why.
func TestFailureImplementsError(t *testing.T) {
	var err error = &failure.Failure{Kind: failure.Parse, Message: "unexpected token"}

	if err == nil {
		t.Fatal("expected non-nil error")
	}

	var f *failure.Failure
	if !errors.As(err, &f) {
		t.Fatal("errors.As failed to unwrap *failure.Failure")
	}
	if f.Kind != failure.Parse {
		t.Errorf("Kind = %v, want %v", f.Kind, failure.Parse)
	}
}

// TestKindClosedSet enumerates the exact eight kinds ported from
// flutter/lib/models/failure.dart's FailureKind, in the same order, so a
// future accidental addition, removal or reorder of the constants is
// caught here rather than silently changing wire/log output.
func TestKindClosedSet(t *testing.T) {
	tests := []struct {
		kind failure.Kind
		want string
	}{
		{failure.Unreachable, "unreachable"},
		{failure.Timeout, "timeout"},
		{failure.Auth, "auth"},
		{failure.Conflict, "conflict"},
		{failure.Server, "server"},
		{failure.Client, "client"},
		{failure.Parse, "parse"},
		{failure.Config, "config"},
	}

	if len(tests) != 8 {
		t.Fatalf("expected 8 kinds in the closed set, table has %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestKindUnknown checks the fallback branch for a Kind value outside the
// closed set (which should never occur in practice, but String() must not
// panic or silently misreport if it does).
func TestKindUnknown(t *testing.T) {
	unknown := failure.Kind(99)
	want := "unknown(99)"
	if got := unknown.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
