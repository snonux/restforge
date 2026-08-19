package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/tui"
	"github.com/snonux/restforge/cli/internal/version"
)

// newRoot builds the root command and its persistent flags. Split out from
// [Run] so a test can build the command tree and inspect it (asserting the
// version subcommand exists, for example) without executing it.
func newRoot() *cobra.Command {
	g := &globalFlags{output: "text"}

	root := &cobra.Command{
		Use: "restforge",
		// Short/Long: one-line and full description shown by --help.
		Short: "a generic Siren hypermedia browser for the terminal",
		Long: "restforge is a generic Siren hypermedia browser for the terminal,\n" +
			"the Go/Charm sibling of the Pebble and Flutter clients. With no\n" +
			"subcommand it opens the interactive TUI; with a subcommand it runs\n" +
			"one-shot and exits with a code reflecting the outcome (see --help).",

		// Version wires Cobra's built-in --version flag to the same constant
		// the `version` subcommand prints, so the two never drift.
		Version: version.Version,

		// Args replaces Cobra's default legacyArgs so an unknown subcommand
		// surfaces as a usage error (exit 2) rather than a plain error that
		// would fall through to general failure (exit 1). With no args it
		// allows the TUI launch below.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageErrorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			return nil
		},

		// A subcommand dispatches itself; root.RunE only runs when no
		// subcommand matched. With no args that launches the TUI; the Args
		// validator above has already rejected any stray argument.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},

		SilenceErrors: true,
		SilenceUsage:  true,
	}

	wireRootFlags(root, g)
	root.AddCommand(newVersionCmd(g))
	root.AddCommand(newGetCmd(g))
	return root
}

// wireRootFlags registers the root command's persistent flags, the flag-error
// wrapper that maps flag-parse failures to a usage exit code, and the
// PersistentPreRunE that applies --config and validates --output before any
// subcommand runs. Split out of [newRoot] so the command's dispatch stays
// readable and the flag wiring reads as one unit.
func wireRootFlags(root *cobra.Command, g *globalFlags) {
	root.PersistentFlags().StringVar(&g.configPath, "config", "",
		"path to the config file (overrides $"+config.ConfigEnvVar+" and the default location)")
	root.PersistentFlags().StringVar(&g.backendName, "backend", "",
		"name of the configured backend a subcommand targets")
	root.PersistentFlags().StringVar(&g.output, "output", "text",
		"rendering format for one-shot commands (text|json)")

	// Flag parse errors (an unknown flag, a missing value) are usage errors
	// (exit 2), not general failures: the user invoked the command wrong and
	// nothing reached the server. FlagErrorFunc wraps them as such before
	// they reach the dispatch layer.
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError(err)
	})

	// PersistentPreRunE applies the --config override through config's own
	// env-var seam, and validates --output, before any subcommand runs.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if g.configPath != "" {
			// Match the existing seam: ConfigEnvVar already overrides
			// config.Path(), so --config just sets that env var for the
			// process, rather than threading a parallel path everywhere.
			// Setenv can only fail on a malformed name; ConfigEnvVar is a
			// fixed valid name, so the error is not actionable here.
			_ = os.Setenv(config.ConfigEnvVar, g.configPath)
		}
		if !validOutputs[g.output] {
			return usageErrorf("invalid --output %q (want text or json)", g.output)
		}
		return nil
	}
}

// runTUI launches the interactive TUI. A thin wrapper so the root's RunE
// reads as one line and the TUI entry point has an obvious name for a later
// task to replace with the real internal/tui.
func runTUI() error {
	if err := tui.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// Run executes the command tree against args (normally os.Args[1:]) and
// returns the process exit code the binary should exit with. It is the only
// entry point main.go calls, keeping main.go to argument plumbing.
func Run(args []string) int {
	return run(newRoot(), args, os.Stdout, os.Stderr)
}

// run executes cmd against args, routing command output to out and any error
// to errs (since the root silences Cobra's own error/usage printing), and
// returns the exit code. Split out from [Run] so a test can wire its own
// out/err sinks and assert on captured output.
func run(cmd *cobra.Command, args []string, out, errs io.Writer) int {
	cmd.SetArgs(args)
	cmd.SetOut(out)
	if err := cmd.Execute(); err != nil {
		// If reporting the error itself fails (a broken stderr), there is
		// nothing left to do but return the exit code the error maps to.
		_, _ = fmt.Fprintln(errs, err)
		return ExitCode(err)
	}
	return ExitOK
}
