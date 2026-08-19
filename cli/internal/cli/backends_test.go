// Tests for the `restforge backends` one-shot subcommands (list, add, remove,
// edit). They run the command tree in process (via run/newRoot, capturing
// stdout and stderr) against a config file pointed at a temp file through
// config.ConfigEnvVar -- the established isolation pattern
// internal/quick/quick_test.go's withConfig and internal/session's
// withEmptyConfig use -- so no test touches a real $HOME or $XDG_CONFIG_HOME
// or the real user config file. They assert the round-trip the task asks
// for: add then list round-trips, remove drops the right entry, edit
// partially updates, and the secret never appears in list's plain-text or
// JSON output.
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// withEmptyConfig points config at a fresh, empty temp file for one test,
// the same shape internal/session/session_test.go's withEmptyConfig uses.
// Each test gets its own file so add/remove/edit state never leaks between
// cases.
func withEmptyConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.ConfigEnvVar, t.TempDir()+"/config.toml")
}

// execBackends runs the command tree against args, capturing stdout and
// stderr. A thin wrapper around the package's existing run/newRoot pair so
// these tests read like get_test.go's execute.
func execBackends(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(newRoot(), args, &out, &errs)
	return code, out.String(), errs.String()
}

const testSecret = "open-sesame"

// addOne runs `backends add` with a secret piped into --secret-stdin, the
// source the --help text steers callers to. Returns the exit code so a test
// can assert on a failure before asserting on list's output.
func addOne(t *testing.T, name, baseURL string) (int, string, string) {
	t.Helper()
	return execBackendsStdin(t, strings.NewReader(testSecret),
		"backends", "add", "--name", name, "--base-url", baseURL, "--secret-stdin")
}

// execBackendsStdin is execBackends with a wired stdin, for --secret-stdin.
// The root's run() helper does not expose stdin, so this builds the tree and
// calls the backends add RunE path through Cobra's own Execute against a
// buffer-backed root -- the simplest way to feed stdin without reshaping run.
func execBackendsStdin(t *testing.T, stdin *strings.Reader, args ...string) (int, string, string) {
	t.Helper()
	root := newRoot()
	var out, errs bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errs)
	// Cobra reads stdin through the command's InOrStdin; SetIn wires it.
	root.SetIn(stdin)
	if err := root.Execute(); err != nil {
		_, _ = (&errs).WriteString(err.Error() + "\n")
		return ExitCode(err), out.String(), errs.String()
	}
	return ExitOK, out.String(), errs.String()
}

func TestBackendsListEmptyPrintsHeader(t *testing.T) {
	withEmptyConfig(t)
	code, out, errs := execBackends(t, "backends", "list")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, "NAME\tBASE-URL\tAUTH-HEADER\tSTART-REL\tSECRET") {
		t.Fatalf("list output missing header; got:\n%s", out)
	}
	if strings.Contains(out, testSecret) {
		t.Fatalf("list output leaked the secret:\n%s", out)
	}
}

func TestBackendsListEmptyJSON(t *testing.T) {
	withEmptyConfig(t)
	code, out, errs := execBackends(t, "backends", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var arr []backendJSON
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("list --output json did not print a JSON array: %v\n%s", err, out)
	}
	if len(arr) != 0 {
		t.Fatalf("list --output json = %v, want an empty array", arr)
	}
}

// TestBackendsAddThenListRoundTrips is the task's headline assertion: add a
// backend, then list shows its name, base URL, auth header and start rel,
// never the secret, in both text and JSON.
func TestBackendsAddThenListRoundTrips(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add exit = %d, want 0; stderr:\n%s", code, errs)
	}

	// Text output: header + one row, every field but the secret.
	code, out, errs := execBackends(t, "backends", "list")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0; stderr:\n%s", code, errs)
	}
	wantRow := "pantry\thttps://host/api/\t" + backend.DefaultAuthHeader + "\t\tset"
	if !strings.Contains(out, wantRow) {
		t.Fatalf("list text output missing %q; got:\n%s", wantRow, out)
	}
	if strings.Contains(out, testSecret) {
		t.Fatalf("list text output leaked the secret:\n%s", out)
	}

	// JSON output: the same fields, the secret redacted to "set".
	code, out, errs = execBackends(t, "backends", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list --output json exit = %d, want 0; stderr:\n%s", code, errs)
	}
	var arr []backendJSON
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("list --output json did not print a JSON array: %v\n%s", err, out)
	}
	if len(arr) != 1 || arr[0].Name != "pantry" {
		t.Fatalf("list --output json = %+v, want one pantry entry", arr)
	}
	if arr[0].Secret != "set" {
		t.Errorf("list --output json secret = %q, want %q", arr[0].Secret, "set")
	}
	if arr[0].AuthHeader != backend.DefaultAuthHeader {
		t.Errorf("list --output json auth_header = %q, want %q", arr[0].AuthHeader, backend.DefaultAuthHeader)
	}
	if strings.Contains(out, testSecret) {
		t.Fatalf("list --output json leaked the secret:\n%s", out)
	}
}

func TestBackendsAddWithStartRelAndAuthHeader(t *testing.T) {
	withEmptyConfig(t)
	code, _, errs := execBackendsStdin(t, strings.NewReader(testSecret),
		"backends", "add", "--name", "pantry", "--base-url", "https://host/api/",
		"--auth-header", "X-Token", "--start-rel", "start", "--secret-stdin")
	if code != 0 {
		t.Fatalf("add exit = %d, want 0; stderr:\n%s", code, errs)
	}
	_, out, _ := execBackends(t, "backends", "list")
	wantRow := "pantry\thttps://host/api/\tX-Token\tstart\tset"
	if !strings.Contains(out, wantRow) {
		t.Fatalf("list output missing %q; got:\n%s", wantRow, out)
	}
}

func TestBackendsAddSecretFromEnv(t *testing.T) {
	withEmptyConfig(t)
	t.Setenv(secretEnvVar, testSecret)
	code, _, errs := execBackends(t, "backends", "add", "--name", "pantry", "--base-url", "https://host/api/")
	if code != 0 {
		t.Fatalf("add exit = %d, want 0; stderr:\n%s", code, errs)
	}
	// The stored secret must be the env var's value: load through config and
	// check directly, since list never prints it.
	backends, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if len(backends) != 1 || backends[0].Secret != testSecret {
		t.Fatalf("stored secret = %q, want %q", backends[0].Secret, testSecret)
	}
}

func TestBackendsAddMissingSecretIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	// No --secret, no --secret-stdin, no env: Validate rejects "secret is
	// required", a config-state failure (exit 1), not a usage error.
	code, _, errs := execBackends(t, "backends", "add", "--name", "pantry", "--base-url", "https://host/api/")
	if code != 1 {
		t.Fatalf("add without secret exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "secret is required") {
		t.Errorf("add without secret stderr missing the message; got:\n%s", errs)
	}
}

func TestBackendsAddInvalidBaseURLIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	t.Setenv(secretEnvVar, testSecret)
	code, _, errs := execBackends(t, "backends", "add", "--name", "pantry", "--base-url", "not-a-url")
	if code != 1 {
		t.Fatalf("add with bad base URL exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "base URL must be absolute") {
		t.Errorf("add with bad base URL stderr missing the message; got:\n%s", errs)
	}
}

func TestBackendsAddDuplicateNameIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("first add exit = %d, want 0; stderr:\n%s", code, errs)
	}
	code, _, errs := addOne(t, "pantry", "https://other/api/")
	if code != 1 {
		t.Fatalf("duplicate add exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "already exists") {
		t.Errorf("duplicate add stderr missing the message; got:\n%s", errs)
	}
}

// TestBackendsRemoveDropsTheRightEntry adds two, removes the first, and
// asserts the second is the one that remains -- the task's "remove drops the
// right entry" check, not just "remove drops an entry".
func TestBackendsRemoveDropsTheRightEntry(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add pantry exit = %d; stderr:\n%s", code, errs)
	}
	if code, _, errs := addOne(t, "cellar", "https://other/api/"); code != 0 {
		t.Fatalf("add cellar exit = %d; stderr:\n%s", code, errs)
	}
	code, out, errs := execBackends(t, "backends", "remove", "pantry")
	if code != 0 {
		t.Fatalf("remove exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, `removed backend "pantry"`) {
		t.Fatalf("remove output missing the confirmation; got:\n%s", out)
	}
	_, listOut, _ := execBackends(t, "backends", "list")
	if strings.Contains(listOut, "pantry\t") {
		t.Fatalf("remove left pantry in the list:\n%s", listOut)
	}
	if !strings.Contains(listOut, "cellar\thttps://other/api/") {
		t.Fatalf("remove dropped the wrong entry; list:\n%s", listOut)
	}
}

func TestBackendsRemoveNotFoundIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	code, _, errs := execBackends(t, "backends", "remove", "nope")
	if code != 1 {
		t.Fatalf("remove unknown exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, `no configured backend named "nope"`) {
		t.Errorf("remove unknown stderr missing the message; got:\n%s", errs)
	}
}

func TestBackendsRemoveNeedsNameArg(t *testing.T) {
	withEmptyConfig(t)
	code, _, errs := execBackends(t, "backends", "remove")
	if code != 2 {
		t.Fatalf("remove with no arg exit = %d, want 2 (usage); stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "exactly one NAME") {
		t.Errorf("remove with no arg stderr missing the message; got:\n%s", errs)
	}
}

// TestBackendsEditPartialUpdate checks that --base-url changes only the base
// URL and leaves the secret, auth header and start rel untouched -- the
// partial-update contract. The secret is verified through config, since list
// never prints it.
func TestBackendsEditPartialUpdate(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add exit = %d; stderr:\n%s", code, errs)
	}
	code, out, errs := execBackends(t, "backends", "edit", "pantry", "--base-url", "https://moved/api/")
	if code != 0 {
		t.Fatalf("edit exit = %d, want 0; stderr:\n%s", code, errs)
	}
	if !strings.Contains(out, `updated backend "pantry"`) {
		t.Fatalf("edit output missing the confirmation; got:\n%s", out)
	}
	backends, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("edit changed the backend count to %d", len(backends))
	}
	be := backends[0]
	if be.BaseURL != "https://moved/api/" {
		t.Errorf("edit base URL = %q, want %q", be.BaseURL, "https://moved/api/")
	}
	if be.Secret != testSecret {
		t.Errorf("edit dropped the secret (got %q, want %q)", be.Secret, testSecret)
	}
	if be.AuthHeader != backend.DefaultAuthHeader {
		t.Errorf("edit changed auth header to %q, want %q", be.AuthHeader, backend.DefaultAuthHeader)
	}
}

func TestBackendsEditStartRelCanBeCleared(t *testing.T) {
	withEmptyConfig(t)
	code, _, errs := execBackendsStdin(t, strings.NewReader(testSecret),
		"backends", "add", "--name", "pantry", "--base-url", "https://host/api/",
		"--start-rel", "start", "--secret-stdin")
	if code != 0 {
		t.Fatalf("add exit = %d; stderr:\n%s", code, errs)
	}
	// --start-rel "" is "passed as empty", not "not passed": cleared.
	code, _, errs = execBackends(t, "backends", "edit", "pantry", "--start-rel", "")
	if code != 0 {
		t.Fatalf("edit --start-rel '' exit = %d, want 0; stderr:\n%s", code, errs)
	}
	backends, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if backends[0].StartRel != "" {
		t.Errorf("edit --start-rel '' left start rel = %q, want empty", backends[0].StartRel)
	}
}

func TestBackendsEditSecretFromStdin(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add exit = %d; stderr:\n%s", code, errs)
	}
	const newSecret = "rotated-key"
	code, _, errs := execBackendsStdin(t, strings.NewReader(newSecret),
		"backends", "edit", "pantry", "--secret-stdin")
	if code != 0 {
		t.Fatalf("edit --secret-stdin exit = %d, want 0; stderr:\n%s", code, errs)
	}
	backends, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if backends[0].Secret != newSecret {
		t.Errorf("edit --secret-stdin secret = %q, want %q", backends[0].Secret, newSecret)
	}
}

func TestBackendsEditRenameCollidesWithExisting(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add pantry exit = %d; stderr:\n%s", code, errs)
	}
	if code, _, errs := addOne(t, "cellar", "https://other/api/"); code != 0 {
		t.Fatalf("add cellar exit = %d; stderr:\n%s", code, errs)
	}
	code, _, errs := execBackends(t, "backends", "edit", "pantry", "--name", "cellar")
	if code != 1 {
		t.Fatalf("rename to existing exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "already exists") {
		t.Errorf("rename stderr missing the message; got:\n%s", errs)
	}
}

// TestBackendsEditRenameNormalisedCollides guards the bug where a --name
// that trims to an existing name (surrounding whitespace) must still be
// rejected after Normalise, not sneak past the collision check on the raw
// value and let SaveBackends store a duplicate.
func TestBackendsEditRenameNormalisedCollides(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add pantry exit = %d; stderr:\n%s", code, errs)
	}
	if code, _, errs := addOne(t, "cellar", "https://other/api/"); code != 0 {
		t.Fatalf("add cellar exit = %d; stderr:\n%s", code, errs)
	}
	code, _, errs := execBackends(t, "backends", "edit", "cellar", "--name", " pantry ")
	if code != 1 {
		t.Fatalf("rename to normalised-existing exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "already exists") {
		t.Errorf("rename stderr missing the message; got:\n%s", errs)
	}
	backends, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if len(backends) != 2 {
		t.Fatalf("edit stored a duplicate; got %d backends", len(backends))
	}
}

func TestBackendsEditNotFoundIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	code, _, errs := execBackends(t, "backends", "edit", "nope", "--base-url", "https://x/api/")
	if code != 1 {
		t.Fatalf("edit unknown exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, `no configured backend named "nope"`) {
		t.Errorf("edit unknown stderr missing the message; got:\n%s", errs)
	}
}

func TestBackendsEditInvalidBaseURLIsGeneralFailure(t *testing.T) {
	withEmptyConfig(t)
	if code, _, errs := addOne(t, "pantry", "https://host/api/"); code != 0 {
		t.Fatalf("add exit = %d; stderr:\n%s", code, errs)
	}
	code, _, errs := execBackends(t, "backends", "edit", "pantry", "--base-url", "not-a-url")
	if code != 1 {
		t.Fatalf("edit bad base URL exit = %d, want 1; stderr:\n%s", code, errs)
	}
	if !strings.Contains(errs, "base URL must be absolute") {
		t.Errorf("edit bad base URL stderr missing the message; got:\n%s", errs)
	}
}

// TestBackendsAddPreservedByLoadBackends sanity-checks that SaveBackends'
// normalisation (trailing-slash fix-up, default auth header) is visible to
// list, so the round-trip is through the real storage path, not an
// in-memory shortcut.
func TestBackendsAddPreservedByLoadBackends(t *testing.T) {
	withEmptyConfig(t)
	t.Setenv(secretEnvVar, testSecret)
	// No trailing slash: Normalise adds it; list must show it.
	code, _, errs := execBackends(t, "backends", "add", "--name", "pantry", "--base-url", "https://host/api")
	if code != 0 {
		t.Fatalf("add exit = %d; stderr:\n%s", code, errs)
	}
	_, out, _ := execBackends(t, "backends", "list")
	if !strings.Contains(out, "pantry\thttps://host/api/\t") {
		t.Fatalf("list did not show the normalised base URL; got:\n%s", out)
	}
}
