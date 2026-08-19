package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/version"
)

// newVersionCmd builds the `restforge version` subcommand, which prints
// the single version constant internal/version holds. It is a trivial
// placeholder now: a later versioning task replaces that constant with the
// real semver (see internal/version's package comment), at which point
// this command, the root's built-in --version flag, and the User-Agent
// header all read the one value.
func newVersionCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the restforge version and exit",
		Long: "Print the restforge version and exit. The value is a " +
			"placeholder until the versioning task wires the real semver.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", version.Version)
			return nil
		},
	}
}
