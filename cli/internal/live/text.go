package live

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/siren"
)

// stateText is the state property as a string, and whether it was present
// as a string at all -- the one place that names the state property, so a
// rename on the server changes one call site, not a grep across callers.
// This package owns the job-state vocabulary (StateRunning, StateNone); the
// message text helpers below (ProgressText, DoneText, ResultText) build on
// it, so a property-name change does not silently drift between the watch
// logic and what a notice says. Mirrors stateText.
func stateText(entity siren.Entity) (string, bool) {
	state, ok := entity.Properties["state"].(string)
	return state, ok
}

// stateValue stringifies whatever the state property holds, for
// ProgressText's fallback -- "null" when it is absent, matching Dart's
// string interpolation of a missing dynamic value.
func stateValue(entity siren.Entity) string {
	state, ok := entity.Properties["state"]
	if !ok {
		return "null"
	}
	return fmt.Sprintf("%v", state)
}

// ProgressText is the text for a "still running" notice: the server's
// step, or its state when it did not send one. Mirrors the progress-text in
// actions.js's liveHandlers.onProgress.
func ProgressText(entity siren.Entity) string {
	if step, ok := entity.Properties["step"].(string); ok && step != "" {
		return step
	}
	return stateValue(entity)
}

// DoneText is the text for a "done" notice: the server's state, or "Done"
// when it did not send one. Mirrors actions.js's onDone.
func DoneText(entity siren.Entity) string {
	if state, ok := stateText(entity); ok && state != "" {
		return state
	}
	return "Done"
}

// ResultText is the text for an action's own response banner: the server's
// state, or "Accepted" (a 202 -- the request was accepted, a job started) /
// "Done" when it did not send one. Mirrors resultBanner in actions.js.
func ResultText(entity siren.Entity, status int) string {
	if state, ok := stateText(entity); ok && state != "" {
		return state
	}
	if status == 202 {
		return "Accepted"
	}
	return "Done"
}
