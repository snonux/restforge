package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// secretEnvVar is the environment variable a caller can export to supply a
// backend's secret without putting it on a command line (where `ps` and
// shell history can read it) -- the same rule ../../AGENTS.md states: "A
// secret goes in a request header, never a query string, and never into a
// commit, a command line (where ps can read it) or a log." The --secret
// flag exists for convenience, but --secret-stdin and this env var are the
// preferred sources, and the add subcommand's --help text says so.
const secretEnvVar = "RESTFORGE_SECRET"

// newBackendsCmd builds the `restforge backends` parent and its four
// one-shot subcommands for scripting backend configuration without opening
// the TUI's settings screen: list, add, remove and edit. Each loads and
// saves through internal/config and validates through internal/backend,
// surfacing Validate's human-readable messages, so a script gets the same
// wording the TUI's settings screen would show. The parent itself has no
// RunE: with no subcommand Cobra prints its help, the same shape `restforge`
// with no subcommand would have if it did not launch the TUI.
func newBackendsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backends",
		Short: "manage configured backends without opening the TUI",
		Long: "Manage the backends stored in the config file -- list them,\n" +
			"add one, remove one, or partially edit one -- for scripting, without\n" +
			"opening the TUI's settings screen. A backend's secret is never\n" +
			"printed (see list --help); when adding or editing, prefer --secret-stdin\n" +
			"or the " + secretEnvVar + " environment variable over --secret, since a\n" +
			"real key on a literal command line ends up in shell history and in\n" +
			"`ps` output.",
	}
	cmd.AddCommand(newBackendsListCmd(g))
	cmd.AddCommand(newBackendsAddCmd(g))
	cmd.AddCommand(newBackendsRemoveCmd(g))
	cmd.AddCommand(newBackendsEditCmd(g))
	return cmd
}

// --- list ----------------------------------------------------------------

// newBackendsListCmd builds `restforge backends list`: print the configured
// backends as a table (text, the default) or a JSON array (--output json).
// The secret itself is never printed: a redacted marker ("set" or "empty")
// stands in for it, in both the text and JSON forms, so list output is safe
// to paste or log.
func newBackendsListCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "print configured backends as a table or JSON array",
		Long: "Print the configured backends. The default text output is a\n" +
			"tab-separated table with one row per backend and a header line:\n" +
			"NAME, BASE-URL, AUTH-HEADER, START-REL and a redacted SECRET marker\n" +
			"(\"set\" or \"empty\") -- the secret itself is never printed. --output\n" +
			"json prints the same information as a JSON array, with the secret\n" +
			"likewise redacted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendsList(cmd.OutOrStdout(), g)
		},
	}
	return cmd
}

// runBackendsList is the list command's body, split out of the RunE closure
// so it reads as straight-line logic and a test can call it with a buffer.
func runBackendsList(out io.Writer, g *globalFlags) error {
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("backends list: could not load backends: %w", err)
	}
	if g.output == "json" {
		return printBackendsJSON(out, backends)
	}
	return printBackendsText(out, backends)
}

// printBackendsText writes a header line followed by one tab-separated row
// per backend. The shape mirrors the rest of the CLI's text output (one
// row per line, tab-separated) so a caller can pipe it through cut/awk; the
// header lets a person orient and a script skip the first line.
func printBackendsText(out io.Writer, backends []backend.Backend) error {
	if _, err := fmt.Fprintln(out, "NAME\tBASE-URL\tAUTH-HEADER\tSTART-REL\tSECRET"); err != nil {
		return err
	}
	for _, b := range backends {
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
			b.Name, b.BaseURL, b.AuthHeader, b.StartRel, secretMarker(b.Secret))
		if _, err := fmt.Fprintln(out, row); err != nil {
			return err
		}
	}
	return nil
}

// printBackendsJSON writes backends as a pretty-printed JSON array, with the
// secret redacted to the same "set"/"empty" marker the text form uses. The
// field names are snake_case to match the config file's on-disk shape, so a
// caller piping both through jq sees the same keys.
func printBackendsJSON(out io.Writer, backends []backend.Backend) error {
	rows := make([]backendJSON, len(backends))
	for i, b := range backends {
		rows[i] = backendJSON{
			Name:       b.Name,
			BaseURL:    b.BaseURL,
			AuthHeader: b.AuthHeader,
			StartRel:   b.StartRel,
			Secret:     secretMarker(b.Secret),
		}
	}
	return PrintJSON(out, rows)
}

// backendJSON is the JSON shape of one row of `backends list --output json`,
// with the secret replaced by a redacted marker (see secretMarker).
type backendJSON struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	AuthHeader string `json:"auth_header"`
	StartRel   string `json:"start_rel"`
	Secret     string `json:"secret"` // redacted: "set" or "empty", never the value
}

// secretMarker reports the redacted stand-in for a backend's secret: "set"
// when one is configured, "empty" when not. It never returns the value
// itself, so list output is safe to paste or log (../../AGENTS.md).
func secretMarker(secret string) string {
	if secret != "" {
		return "set"
	}
	return "empty"
}

// --- add -----------------------------------------------------------------

// addOpts carries the parsed `backends add` flags into runBackendsAdd, so
// the body stays free of pflag lookups and a test can call it with plain
// values. secretChanged is whether --secret was passed explicitly (distinct
// from "passed as the empty string"), so the secret resolution order can
// tell an explicit empty from "not passed".
type addOpts struct {
	name          string
	baseURL       string
	authHeader    string
	startRel      string
	secret        string
	secretChanged bool
	secretStdin   bool
}

// newBackendsAddCmd builds `restforge backends add`: append a new backend
// to the config. The secret is accepted three ways, in order of preference
// most-explicit first: the --secret flag, --secret-stdin (read one line from
// stdin), or the RESTFORGE_SECRET environment variable. --help and the
// examples steer callers to the latter two, since a real key on a literal
// command line ends up in shell history and in `ps` output.
func newBackendsAddCmd(g *globalFlags) *cobra.Command {
	var o addOpts
	cmd := &cobra.Command{
		Use:   "add --name NAME --base-url URL [--auth-header H] [--secret S | --secret-stdin | " + secretEnvVar + "=S] [--start-rel R]",
		Short: "add a configured backend",
		Long: "Add a backend to the config file. --name and --base-url are\n" +
			"required; --auth-header defaults to " + backend.DefaultAuthHeader + " when omitted;\n" +
			"--start-rel is optional.\n\n" +
			"The secret is accepted three ways, most-explicit first:\n" +
			"  --secret S            put S on a literal command line (avoid; see below)\n" +
			"  --secret-stdin        read one line from standard input\n" +
			"  $" + secretEnvVar + "           read the value from the environment\n\n" +
			"Prefer --secret-stdin or $" + secretEnvVar + ": a real key passed to --secret\n" +
			"is visible in shell history and in `ps` output, exactly what a secret\n" +
			"must never be. The secret is never printed back by `backends list`.\n\n" +
			"Examples:\n" +
			"  printf '%%s' \"$KEY\" | restforge backends add --name prod --base-url https://host/api/ --secret-stdin\n" +
			"  RESTFORGE_SECRET=\"$KEY\" restforge backends add --name prod --base-url https://host/api/",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.secretChanged = cmd.Flags().Changed("secret")
			return runBackendsAdd(cmd.OutOrStdout(), cmd.InOrStdin(), o)
		},
	}
	cmd.Flags().StringVar(&o.name, "name", "", "backend name (required)")
	cmd.Flags().StringVar(&o.baseURL, "base-url", "", "base URL, absolute, ending in '/' (required)")
	cmd.Flags().StringVar(&o.authHeader, "auth-header", "",
		"header the secret is sent in (default: "+backend.DefaultAuthHeader+")")
	cmd.Flags().StringVar(&o.secret, "secret", "",
		"the API key (avoid; prefer --secret-stdin or $"+secretEnvVar+")")
	cmd.Flags().BoolVar(&o.secretStdin, "secret-stdin", false,
		"read the secret as one line from standard input")
	cmd.Flags().StringVar(&o.startRel, "start-rel", "",
		"optional link rel to follow from the root after connecting")
	return cmd
}

// runBackendsAdd is the add command's body, split out of the RunE closure so
// it reads as straight-line logic and a test can call it with buffers.
func runBackendsAdd(out io.Writer, stdin io.Reader, o addOpts) error {
	secret, err := resolveAddSecret(o, stdin)
	if err != nil {
		return err
	}
	be := backend.Backend{
		Name:       o.name,
		BaseURL:    o.baseURL,
		AuthHeader: o.authHeader,
		Secret:     secret,
		StartRel:   o.startRel,
	}
	saved, err := validateOne(be)
	if err != nil {
		return err
	}
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("backends add: could not load backends: %w", err)
	}
	if err := checkAddConstraints(backends, saved.Name); err != nil {
		return err
	}
	if err := config.SaveBackends(append(backends, saved)); err != nil {
		return fmt.Errorf("backends add: %w", err)
	}
	_, _ = fmt.Fprintf(out, "added backend %q\n", saved.Name)
	return nil
}

// resolveAddSecret applies the add command's secret resolution order: an
// explicit --secret wins, then --secret-stdin, then the RESTFORGE_SECRET
// environment variable, then empty (which backend.Validate rejects with
// "secret is required"). Reading stdin is only done when --secret-stdin was
// passed, so an interactive caller is never blocked waiting for input.
func resolveAddSecret(o addOpts, stdin io.Reader) (string, error) {
	if o.secretChanged {
		return o.secret, nil
	}
	if o.secretStdin {
		return readStdinLine(stdin)
	}
	return os.Getenv(secretEnvVar), nil
}

// checkAddConstraints guards the two cases backend.Validate and
// config.SaveBackends cannot report cleanly themselves: a duplicate name
// (SaveBackends would silently keep both, since names are not unique-keyed
// on disk) and the MaxBackends cap (SaveBackends caps silently, which would
// drop the backend the caller just asked to add). Both are config-state
// failures, exit 1.
func checkAddConstraints(backends []backend.Backend, name string) error {
	for _, b := range backends {
		if b.Name == name {
			return fmt.Errorf("backends add: a backend named %q already exists (use `backends edit` to change it)", name)
		}
	}
	if len(backends) >= backend.MaxBackends {
		return fmt.Errorf("backends add: at most %d backends may be configured", backend.MaxBackends)
	}
	return nil
}

// --- remove --------------------------------------------------------------

// newBackendsRemoveCmd builds `restforge backends remove NAME`: drop the
// named backend from the config. NAME is a positional argument; a name that
// is not configured is a config-state failure (exit 1), not a usage error,
// because the invocation itself was well-formed.
func newBackendsRemoveCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "remove a configured backend by name",
		Long:  "Remove the backend named NAME from the config file.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErrorf("remove takes exactly one NAME argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackendsRemove(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

// runBackendsRemove is the remove command's body, split out of the RunE
// closure so it reads as straight-line logic and a test can call it.
func runBackendsRemove(out io.Writer, name string) error {
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("backends remove: could not load backends: %w", err)
	}
	idx := findBackend(backends, name)
	if idx < 0 {
		return fmt.Errorf("backends remove: no configured backend named %q", name)
	}
	removed := backends[idx]
	backends = append(backends[:idx], backends[idx+1:]...)
	if err := config.SaveBackends(backends); err != nil {
		return fmt.Errorf("backends remove: %w", err)
	}
	_, _ = fmt.Fprintf(out, "removed backend %q\n", removed.Name)
	return nil
}

// --- edit ----------------------------------------------------------------

// editOpts carries the parsed `backends edit` flags plus which were changed,
// so the body can apply a partial update: only the flags passed change, and
// unspecified fields keep their existing values. The *Changed booleans let
// "passed as the empty string" (e.g. `--start-rel ""` to clear it) be told
// apart from "not passed", which a plain empty default cannot.
type editOpts struct {
	name              string
	baseURL           string
	authHeader        string
	startRel          string
	secret            string
	nameChanged       bool
	baseURLChanged    bool
	authHeaderChanged bool
	secretChanged     bool
	secretStdin       bool
	startRelChanged   bool
}

// newBackendsEditCmd builds `restforge backends edit NAME [--base-url ...]`:
// a partial update of an existing backend. Only the flags passed change;
// unspecified fields keep their existing values. The secret, when changing,
// follows the same sources as add (--secret or --secret-stdin); the env var
// is not consulted for edit, so an ambient RESTFORGE_SECRET never silently
// overwrites a stored secret on an unrelated edit.
func newBackendsEditCmd(g *globalFlags) *cobra.Command {
	var o editOpts
	cmd := &cobra.Command{
		Use:   "edit NAME [--name NAME] [--base-url URL] [--auth-header H] [--secret S | --secret-stdin] [--start-rel R]",
		Short: "partially update a configured backend",
		Long: "Update the backend named NAME in the config file. Only the\n" +
			"flags passed change; unspecified fields keep their existing values.\n" +
			"To clear an optional field, pass it as the empty string\n" +
			"(e.g. --start-rel \"\"). The secret, when changing, is read from\n" +
			"--secret or --secret-stdin (prefer the latter; see `backends add --help`).",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErrorf("edit takes exactly one NAME argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			flagChanged(cmd, &o)
			return runBackendsEdit(cmd.OutOrStdout(), cmd.InOrStdin(), args[0], o)
		},
	}
	cmd.Flags().StringVar(&o.name, "name", "", "new backend name")
	cmd.Flags().StringVar(&o.baseURL, "base-url", "", "new base URL")
	cmd.Flags().StringVar(&o.authHeader, "auth-header", "", "new auth header")
	cmd.Flags().StringVar(&o.secret, "secret", "",
		"new secret (avoid; prefer --secret-stdin)")
	cmd.Flags().BoolVar(&o.secretStdin, "secret-stdin", false,
		"read the new secret as one line from standard input")
	cmd.Flags().StringVar(&o.startRel, "start-rel", "", "new start rel")
	return cmd
}

// flagChanged records which edit flags were passed, so runBackendsEdit can
// apply a partial update. Kept here rather than inline in the RunE closure
// so the closure reads as one line and the changed-detection reads as one
// unit, the same split wireRootFlags keeps for the root's flag wiring.
func flagChanged(cmd *cobra.Command, o *editOpts) {
	flags := cmd.Flags()
	o.nameChanged = flags.Changed("name")
	o.baseURLChanged = flags.Changed("base-url")
	o.authHeaderChanged = flags.Changed("auth-header")
	o.secretChanged = flags.Changed("secret")
	o.startRelChanged = flags.Changed("start-rel")
}

// runBackendsEdit is the edit command's body, split out of the RunE closure
// so it reads as straight-line logic and a test can call it with buffers.
func runBackendsEdit(out io.Writer, stdin io.Reader, name string, o editOpts) error {
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("backends edit: could not load backends: %w", err)
	}
	idx := findBackend(backends, name)
	if idx < 0 {
		return fmt.Errorf("backends edit: no configured backend named %q", name)
	}
	updated := backends[idx]
	if err := applyEdit(&updated, o, stdin); err != nil {
		return err
	}
	saved, err := validateOne(updated)
	if err != nil {
		return err
	}
	// Check the rename for a collision only after normalising, so a --name
	// like " pantry " (which trims to "pantry") cannot sneak past an existing
	// "pantry" and let SaveBackends store a duplicate.
	if saved.Name != backends[idx].Name && hasOtherBackend(backends, backends[idx].Name, saved.Name) {
		return fmt.Errorf("backends edit: a backend named %q already exists", saved.Name)
	}
	backends[idx] = saved
	if err := config.SaveBackends(backends); err != nil {
		return fmt.Errorf("backends edit: %w", err)
	}
	_, _ = fmt.Fprintf(out, "updated backend %q\n", saved.Name)
	return nil
}

// applyEdit mutates be with only the fields editOpts marked changed,
// resolving the new secret from --secret or --secret-stdin when either was
// passed and leaving the existing secret untouched otherwise. The rename
// collision check is deliberately not done here: it must run after
// normalisation (see runBackendsEdit), so applyEdit only assigns.
func applyEdit(be *backend.Backend, o editOpts, stdin io.Reader) error {
	if o.nameChanged {
		be.Name = o.name
	}
	if o.baseURLChanged {
		be.BaseURL = o.baseURL
	}
	if o.authHeaderChanged {
		be.AuthHeader = o.authHeader
	}
	if o.startRelChanged {
		be.StartRel = o.startRel
	}
	return applyEditSecret(be, o, stdin)
}

// applyEditSecret updates be.Secret only when the caller asked to change it
// (--secret or --secret-stdin); otherwise the existing secret is kept, the
// core of the partial-update contract. Split out of applyEdit so the
// non-secret field assignments read as one block.
func applyEditSecret(be *backend.Backend, o editOpts, stdin io.Reader) error {
	if o.secretChanged {
		be.Secret = o.secret
		return nil
	}
	if o.secretStdin {
		s, err := readStdinLine(stdin)
		if err != nil {
			return fmt.Errorf("backends edit: %w", err)
		}
		be.Secret = s
	}
	return nil
}

// --- shared helpers ------------------------------------------------------

// validateOne normalises be and runs backend.Validate on the result,
// returning the normalised form or the validation error wrapped so a script
// sees the same wording the TUI's settings screen would show. The wrapping
// is a plain error (not an *ExitError), so it maps to the general-failure
// exit (1) through [ExitCode] -- Validate's errors are config-state
// failures, not usage errors.
func validateOne(be backend.Backend) (backend.Backend, error) {
	n := backend.Normalise(be)
	if err := backend.Validate(n); err != nil {
		return backend.Backend{}, fmt.Errorf("backends: %w", err)
	}
	return n, nil
}

// findBackend returns the index of the backend named name in backends, or
// -1 when none matches. A linear scan is fine at backend.MaxBackends scale.
func findBackend(backends []backend.Backend, name string) int {
	for i, b := range backends {
		if b.Name == name {
			return i
		}
	}
	return -1
}

// hasOtherBackend reports whether a backend named newName exists in all,
// other than the one currently named oldName (the one being renamed). Used
// by edit to keep a --name change from creating a duplicate.
func hasOtherBackend(all []backend.Backend, oldName, newName string) bool {
	for _, b := range all {
		if b.Name == oldName {
			continue
		}
		if b.Name == newName {
			return true
		}
	}
	return false
}

// readStdinLine reads one line from stdin (the secret for --secret-stdin),
// trimming the trailing newline and any carriage return. A final line with
// no newline is still returned (io.EOF after a partial read), so a caller
// piping `printf '%s' "$KEY"` without a trailing newline works as well as
// `echo "$KEY"`. Surrounding whitespace is left for backend.Normalise to
// trim, the same place every other field is trimmed.
func readStdinLine(stdin io.Reader) (string, error) {
	r := bufio.NewReader(stdin)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("could not read secret from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
