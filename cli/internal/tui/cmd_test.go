package tui

import "testing"

// TestSessionCmd pins the shape sessionCmd's own doc comment promises: fn
// runs (to completion) when the returned tea.Cmd is invoked, and a
// sessionUpdatedMsg comes back once it has. The Bubble Tea runtime is what
// actually calls a tea.Cmd on its own goroutine in production; calling it
// directly here is enough to check the contract without needing a running
// Program.
func TestSessionCmd(t *testing.T) {
	called := false
	cmd := sessionCmd(func() { called = true })

	msg := cmd()

	if !called {
		t.Error("sessionCmd's tea.Cmd did not run fn")
	}
	if _, ok := msg.(sessionUpdatedMsg); !ok {
		t.Errorf("sessionCmd's tea.Cmd returned %T, want sessionUpdatedMsg", msg)
	}
}
