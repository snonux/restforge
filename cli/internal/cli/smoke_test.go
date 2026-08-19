// Smoke tests for the restforge binary's mode-by-invocation dispatch. They
// build the real binary into a temp dir once and run it as a subprocess, so
// they exercise the Cobra wiring, the global flags and the exit-code
// convention end to end -- the dispatch skeleton this task ships, before the
// real subcommands and TUI arrive in later tasks.
//
// The no-subcommand (TUI) path is deliberately NOT covered here: it needs a
// controlling TTY, which a `go test` subprocess does not have, and asserting
// on the "could not open a new TTY" error would only test the environment,
// not the dispatch. The TUI path gets a real pty harness when the real TUI
// lands; for now the version/--help/usage paths below are the dispatch logic
// that matters.
package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/version"
)

// moduleRoot is the cli/ directory (the module root), resolved from this
// test file's location so `go build ./cmd/restforge` runs where the module
// lives regardless of where `go test` was invoked from.
var moduleRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}()

// binDir is the single temp dir the built binary lives in for the whole
// suite, created in TestMain and removed when the suite ends -- one build,
// shared across every subtest, rather than a build per case.
var binDir string

// restforgeBin builds the restforge binary into binDir the first time a test
// needs it, and returns its path. Building once and sharing across subtests
// keeps the suite fast -- a `go build` per case would dominate the runtime
// for no extra coverage.
func restforgeBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(binDir, "restforge")
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/restforge")
	cmd.Dir = moduleRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// TestMain creates and removes the shared build dir so the binary is built
// once and cleaned up with the suite, not leaked or rebuilt per test.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "restforge-smoke-")
	if err != nil {
		panic(err)
	}
	binDir = dir
	code := m.Run()
	_ = os.RemoveAll(binDir) // best-effort cleanup; a failure leaves a temp dir behind
	os.Exit(code)
}

// run executes the built binary with args and reports its exit code and
// combined output. It fails the test if the build or run itself broke in a
// way no exit code could express (a signal, a missing binary).
func run(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return code, string(out)
}

func TestHelpExitsZeroAndListsVersion(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "--help")
	if code != 0 {
		t.Fatalf("--help exit = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "version") {
		t.Fatalf("--help output did not list the version subcommand:\n%s", out)
	}
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("--help output did not list available commands:\n%s", out)
	}
}

func TestVersionSubcommandPrintsPlaceholder(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "version")
	if code != 0 {
		t.Fatalf("version exit = %d, want 0; output:\n%s", code, out)
	}
	want := version.Version
	if got := strings.TrimSpace(out); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionFlagMatchesSubcommand(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "--version")
	if code != 0 {
		t.Fatalf("--version exit = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, version.Version) {
		t.Fatalf("--version output did not contain %q:\n%s", version.Version, out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "no-such-command")
	if code != 2 {
		t.Fatalf("unknown command exit = %d, want 2 (usage); output:\n%s", code, out)
	}
	if !strings.Contains(out, `unknown command "no-such-command"`) {
		t.Fatalf("unknown command output missing the message:\n%s", out)
	}
}

func TestBadOutputFlagIsUsageError(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "--output", "xml", "version")
	if code != 2 {
		t.Fatalf("bad --output exit = %d, want 2 (usage); output:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid --output") {
		t.Fatalf("bad --output output missing the message:\n%s", out)
	}
}

func TestBadFlagIsUsageError(t *testing.T) {
	bin := restforgeBin(t)
	code, out := run(t, bin, "--no-such-flag")
	if code != 2 {
		t.Fatalf("bad flag exit = %d, want 2 (usage); output:\n%s", code, out)
	}
}
