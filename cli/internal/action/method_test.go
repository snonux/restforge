package action_test

// Ported from action_service_test.dart's "the safe/unsafe split" group.
// Added beyond test-actions.js (per that file's own header comment): direct
// coverage of IsSafeMethod/SafeMethods, since pebble/tools/test-actions.js
// only exercises the split indirectly, through brew/peek.

import (
	"maps"
	"testing"

	"github.com/snonux/restforge/cli/internal/action"
)

func TestSafeMethodsAreSafe(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "OPTIONS", "TRACE"} {
		if !action.IsSafeMethod(m) {
			t.Errorf("IsSafeMethod(%q) = false, want true", m)
		}
	}
}

func TestUnsafeMethodsAreNotSafe(t *testing.T) {
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if action.IsSafeMethod(m) {
			t.Errorf("IsSafeMethod(%q) = true, want false", m)
		}
	}
}

func TestIsSafeMethodIsCaseInsensitive(t *testing.T) {
	if !action.IsSafeMethod("get") {
		t.Error(`IsSafeMethod("get") = false, want true`)
	}
	if action.IsSafeMethod("post") {
		t.Error(`IsSafeMethod("post") = true, want false`)
	}
}

func TestSafeMethodsIsExactlyRFC9110sDivision(t *testing.T) {
	want := map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true, "TRACE": true}
	if !maps.Equal(action.SafeMethods, want) {
		t.Errorf("SafeMethods = %v, want %v", action.SafeMethods, want)
	}
}
