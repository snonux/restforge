package cli

import (
	"errors"
	"fmt"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
)

// usageError wraps err as an [*ExitError] with code [ExitUsage]: the command
// was invoked wrong. See the package comment.
func usageError(err error) error {
	return &ExitError{Code: ExitUsage, Err: err}
}

// usageErrorf is usageError with Sprintf formatting convenience.
func usageErrorf(format string, args ...any) error {
	return usageError(fmt.Errorf(format, args...))
}

// refusedError wraps err as an [*ExitError] with code [ExitRefused]: an
// action was refused by the server (409 Conflict) or withdrawn by the user
// (a declined confirmation). See the package comment.
func refusedError(err error) error {
	return &ExitError{Code: ExitRefused, Err: err}
}

// exitForFailure maps a *failure.Failure onto the exit-code convention: a
// Conflict (the server refused because state was stale) becomes exit 3
// (refused), and every other kind stays a general failure (exit 1) -- see
// the package comment. Non-failure errors pass through unchanged.
func exitForFailure(err error) error {
	var f *failure.Failure
	if errors.As(err, &f) && f.Kind == failure.Conflict {
		return refusedError(err)
	}
	return err
}

// selectBackend picks the backend a one-shot subcommand targets from the
// configured list: --backend NAME selects by name; with no name, the sole
// configured backend is used; with none, that is a config-state failure
// (exit 1); with several and no name, the invocation is incomplete (exit 2).
func selectBackend(backends []backend.Backend, name string) (backend.Backend, error) {
	if name != "" {
		for i := range backends {
			if backends[i].Name == name {
				return backends[i], nil
			}
		}
		return backend.Backend{}, usageErrorf("no configured backend named %q", name)
	}
	switch len(backends) {
	case 0:
		return backend.Backend{}, fmt.Errorf("no backends configured; add one with `restforge backends add`")
	case 1:
		return backends[0], nil
	default:
		return backend.Backend{}, usageErrorf("%d backends configured; specify --backend NAME", len(backends))
	}
}
