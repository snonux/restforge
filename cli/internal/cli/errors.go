package cli

import "fmt"

// usageError wraps err as an [*ExitError] with code [ExitUsage]: the command
// was invoked wrong. See the package comment.
func usageError(err error) error {
	return &ExitError{Code: ExitUsage, Err: err}
}

// usageErrorf is usageError with Sprintf formatting convenience.
func usageErrorf(format string, args ...any) error {
	return usageError(fmt.Errorf(format, args...))
}
